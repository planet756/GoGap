package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"gogap/internal/domain"
	"gogap/internal/sse"
)

const visitorCookieName = "gogap_visitor"

type SnapshotProvider interface {
	CurrentSnapshot() domain.SnapshotResponse
}

type WatchlistStore interface {
	ListWatchlist(ctx context.Context, visitorID string) ([]domain.WatchlistItem, error)
	AddWatchlist(ctx context.Context, visitorID string, code string) error
	RemoveWatchlist(ctx context.Context, visitorID string, code string) error
}

type FundPool interface {
	FundPool(ctx context.Context) ([]domain.FundSeed, error)
}

// FundCodeValidator validates whether a fund code is in the enabled pool.
type FundCodeValidator interface {
	IsValidFundCode(code string) bool
}

type WatchlistRefresher interface {
	RefreshWatchlist(ctx context.Context) error
}

type SnapshotRefresher interface {
	Refresh(ctx context.Context) error
}

type Server struct {
	snapshot          SnapshotProvider
	watchlist         WatchlistStore
	pool              FundPool
	codeValidator     FundCodeValidator
	refresh           WatchlistRefresher
	snapshotRefresh   SnapshotRefresher
	sseHub            *sse.Hub
	heartbeatInterval time.Duration

	refreshLimitMu   sync.Mutex
	lastRefreshTime  time.Time
	refreshCooldown  time.Duration
}

type SSEOptions struct {
	Hub               *sse.Hub
	SnapshotProvider  sse.SnapshotProvider
	HeartbeatInterval time.Duration
}

func NewServer(snapshot SnapshotProvider, watchlist WatchlistStore, pool FundPool) *Server {
	s := &Server{
		snapshot:  snapshot,
		watchlist: watchlist,
		pool:      pool,
		refreshCooldown: 10 * time.Second,
	}
	return finishServer(snapshot, s)
}

func NewServerWithSSE(snapshot SnapshotProvider, watchlist WatchlistStore, pool FundPool, hub *sse.Hub) *Server {
	s := &Server{
		snapshot:  snapshot,
		watchlist: watchlist,
		pool:      pool,
		sseHub:    hub,
		refreshCooldown: 10 * time.Second,
	}
	return finishServer(snapshot, s)
}

func finishServer(snapshot SnapshotProvider, s *Server) *Server {
	if r, ok := snapshot.(WatchlistRefresher); ok {
		s.refresh = r
	}
	if r, ok := snapshot.(SnapshotRefresher); ok {
		s.snapshotRefresh = r
	}
	if v, ok := snapshot.(FundCodeValidator); ok {
		s.codeValidator = v
	}
	return s
}

func NewEventsHandler(options SSEOptions) http.Handler {
	return sse.Handler{
		Hub:               options.Hub,
		SnapshotProvider:  options.SnapshotProvider,
		HeartbeatInterval: options.HeartbeatInterval,
	}
}

func RegisterEvents(mux *http.ServeMux, options SSEOptions) {
	mux.Handle("GET /events", NewEventsHandler(options))
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/funds", s.handleFunds)
	mux.HandleFunc("GET /api/snapshot", s.handleSnapshot)
	mux.HandleFunc("POST /api/snapshot", s.handleRefreshSnapshot)
	mux.HandleFunc("GET /api/watchlist", s.handleListWatchlist)
	mux.HandleFunc("POST /api/watchlist", s.handleAddWatchlist)
	mux.HandleFunc("DELETE /api/watchlist/{code}", s.handleRemoveWatchlist)
	if s.sseHub != nil {
		RegisterEvents(mux, SSEOptions{Hub: s.sseHub, SnapshotProvider: s.snapshot, HeartbeatInterval: s.heartbeatInterval})
	}
	return mux
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleFunds(w http.ResponseWriter, r *http.Request) {
	seeds, err := s.pool.FundPool(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "fund pool unavailable"})
		return
	}
	enabled := make([]domain.FundSeed, 0, len(seeds))
	for _, seed := range seeds {
		if seed.Enabled {
			enabled = append(enabled, seed)
		}
	}
	writeJSON(w, http.StatusOK, enabled)
}

func (s *Server) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	snap := domain.NormalizeSnapshotResponse(s.snapshot.CurrentSnapshot())
	if visitorID := visitorIDFromRequest(r); visitorID != "" {
		snap.Items = s.markWatchlist(r.Context(), visitorID, snap.Items)
	}
	writeJSON(w, http.StatusOK, snap)
}

func (s *Server) handleRefreshSnapshot(w http.ResponseWriter, r *http.Request) {
	if s.snapshotRefresh == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "snapshot refresh unavailable"})
		return
	}
	if !s.acquireRefreshSlot() {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "refresh rate limited, try again later"})
		return
	}
	if err := s.snapshotRefresh.Refresh(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "snapshot refresh failed"})
		return
	}
	s.handleSnapshot(w, r)
}

func (s *Server) acquireRefreshSlot() bool {
	s.refreshLimitMu.Lock()
	defer s.refreshLimitMu.Unlock()
	now := time.Now()
	if now.Sub(s.lastRefreshTime) < s.refreshCooldown {
		return false
	}
	s.lastRefreshTime = now
	return true
}

func (s *Server) handleListWatchlist(w http.ResponseWriter, r *http.Request) {
	visitorID, ok := requireVisitorID(w, r)
	if !ok {
		return
	}
	items, err := s.watchlist.ListWatchlist(r.Context(), visitorID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "watchlist unavailable"})
		return
	}
	if items == nil {
		items = []domain.WatchlistItem{}
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleAddWatchlist(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	code := strings.TrimSpace(req.Code)
	if code == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "code is required"})
		return
	}
	if !s.isValidCode(r.Context(), code) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid fund code: " + code})
		return
	}
	visitorID, ok := requireVisitorID(w, r)
	if !ok {
		return
	}
	if err := s.watchlist.AddWatchlist(r.Context(), visitorID, code); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to add watchlist"})
		return
	}
	s.refreshSnapshot(r.Context())
	items, _ := s.watchlist.ListWatchlist(r.Context(), visitorID)
	for _, item := range items {
		if item.Code == code {
			writeJSON(w, http.StatusOK, item)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"code": code})
}

func (s *Server) handleRemoveWatchlist(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	if code == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "code is required"})
		return
	}
	if !s.isValidCode(r.Context(), code) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid fund code: " + code})
		return
	}
	visitorID, ok := requireVisitorID(w, r)
	if !ok {
		return
	}
	if err := s.watchlist.RemoveWatchlist(r.Context(), visitorID, code); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to remove watchlist"})
		return
	}
	s.refreshSnapshot(r.Context())
	writeJSON(w, http.StatusOK, map[string]string{"removed": code})
}

func (s *Server) markWatchlist(ctx context.Context, visitorID string, items []domain.FundSnapshot) []domain.FundSnapshot {
	watchlist, err := s.watchlist.ListWatchlist(ctx, visitorID)
	if err != nil {
		return items
	}
	codes := make(map[string]bool, len(watchlist))
	for _, item := range watchlist {
		codes[item.Code] = true
	}
	for i := range items {
		items[i].InWatchlist = codes[items[i].Code]
	}
	return items
}

func requireVisitorID(w http.ResponseWriter, r *http.Request) (string, bool) {
	visitorID := visitorIDFromRequest(r)
	if visitorID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "visitor id is required"})
		return "", false
	}
	return visitorID, true
}

func visitorIDFromRequest(r *http.Request) string {
	if value := strings.TrimSpace(r.Header.Get("X-GoGap-Visitor-Id")); value != "" {
		return value
	}
	cookie, err := r.Cookie(visitorCookieName)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(cookie.Value)
}

func (s *Server) isValidCode(ctx context.Context, code string) bool {
	// Prefer cached validator from scheduler (avoids fetching full pool)
	if s.codeValidator != nil {
		return s.codeValidator.IsValidFundCode(code)
	}
	// Fallback to full pool lookup
	seeds, err := s.pool.FundPool(ctx)
	if err != nil {
		return false
	}
	for _, seed := range seeds {
		if seed.Enabled && seed.Code == code {
			return true
		}
	}
	return false
}

func (s *Server) refreshSnapshot(ctx context.Context) {
	if s.refresh != nil {
		_ = s.refresh.RefreshWatchlist(ctx)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
