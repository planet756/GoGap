package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gogap/internal/domain"
)

func TestWatchlistEndpointsAreVisitorScoped(t *testing.T) {
	store := newMemoryWatchlistStore()
	server := NewServer(snapshotStub{items: []domain.FundSnapshot{{Code: "510300", Name: "沪深300ETF"}}}, store, poolStub{seeds: []domain.FundSeed{{Code: "510300", Enabled: true}}}).Handler()

	add := httptest.NewRequest(http.MethodPost, "/api/watchlist", strings.NewReader(`{"code":"510300"}`))
	add.Header.Set("Content-Type", "application/json")
	add.AddCookie(&http.Cookie{Name: visitorCookieName, Value: "visitor-a"})
	addRecord := httptest.NewRecorder()
	server.ServeHTTP(addRecord, add)
	if addRecord.Code != http.StatusOK {
		t.Fatalf("expected visitor-a add to succeed, got %d %s", addRecord.Code, addRecord.Body.String())
	}

	a := httptest.NewRequest(http.MethodGet, "/api/watchlist", nil)
	a.AddCookie(&http.Cookie{Name: visitorCookieName, Value: "visitor-a"})
	aRecord := httptest.NewRecorder()
	server.ServeHTTP(aRecord, a)
	var aItems []domain.WatchlistItem
	if err := json.Unmarshal(aRecord.Body.Bytes(), &aItems); err != nil {
		t.Fatalf("decode visitor-a watchlist: %v", err)
	}
	if len(aItems) != 1 || aItems[0].Code != "510300" {
		t.Fatalf("expected visitor-a watchlist to persist, got %+v", aItems)
	}

	b := httptest.NewRequest(http.MethodGet, "/api/watchlist", nil)
	b.AddCookie(&http.Cookie{Name: visitorCookieName, Value: "visitor-b"})
	bRecord := httptest.NewRecorder()
	server.ServeHTTP(bRecord, b)
	var bItems []domain.WatchlistItem
	if err := json.Unmarshal(bRecord.Body.Bytes(), &bItems); err != nil {
		t.Fatalf("decode visitor-b watchlist: %v", err)
	}
	if len(bItems) != 0 {
		t.Fatalf("expected visitor-b watchlist to be isolated, got %+v", bItems)
	}
}

func TestRefreshSnapshotEndpointTriggersBackendRefresh(t *testing.T) {
	store := newMemoryWatchlistStore()
	snapshot := &refreshableSnapshotStub{items: []domain.FundSnapshot{{Code: "164824", Name: "印度基金LOF"}}}
	server := NewServer(snapshot, store, poolStub{seeds: []domain.FundSeed{{Code: "164824", Enabled: true}}}).Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/snapshot", nil)
	req.AddCookie(&http.Cookie{Name: visitorCookieName, Value: "visitor-a"})
	record := httptest.NewRecorder()
	server.ServeHTTP(record, req)

	if record.Code != http.StatusOK {
		t.Fatalf("expected refresh endpoint to succeed, got %d %s", record.Code, record.Body.String())
	}
	if snapshot.refreshes != 1 {
		t.Fatalf("expected backend refresh to be called once, got %d", snapshot.refreshes)
	}
	var response domain.SnapshotResponse
	if err := json.Unmarshal(record.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode refresh snapshot: %v", err)
	}
	if len(response.Items) != 1 || response.Items[0].Code != "164824" {
		t.Fatalf("expected refreshed snapshot response, got %+v", response.Items)
	}
}

func TestRefreshSnapshotEndpointHidesBackendErrorDetails(t *testing.T) {
	server := NewServer(&refreshableSnapshotStub{err: errors.New("upstream secret details")}, newMemoryWatchlistStore(), poolStub{}).Handler()
	record := httptest.NewRecorder()
	server.ServeHTTP(record, httptest.NewRequest(http.MethodPost, "/api/snapshot", nil))

	if record.Code != http.StatusInternalServerError {
		t.Fatalf("expected refresh failure status, got %d", record.Code)
	}
	if strings.Contains(record.Body.String(), "upstream secret details") {
		t.Fatalf("expected refresh endpoint to hide backend details, got %s", record.Body.String())
	}
}

func TestSnapshotMarksWatchlistForRequestVisitor(t *testing.T) {
	store := newMemoryWatchlistStore()
	if err := store.AddWatchlist(context.Background(), "visitor-a", "510300"); err != nil {
		t.Fatalf("AddWatchlist returned error: %v", err)
	}
	server := NewServer(snapshotStub{items: []domain.FundSnapshot{{Code: "510300", Name: "沪深300ETF"}}}, store, poolStub{seeds: []domain.FundSeed{{Code: "510300", Enabled: true}}}).Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/snapshot", nil)
	req.AddCookie(&http.Cookie{Name: visitorCookieName, Value: "visitor-a"})
	record := httptest.NewRecorder()
	server.ServeHTTP(record, req)

	var snapshot domain.SnapshotResponse
	if err := json.Unmarshal(record.Body.Bytes(), &snapshot); err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}
	if len(snapshot.Items) != 1 || !snapshot.Items[0].InWatchlist {
		t.Fatalf("expected visitor-a snapshot to mark 510300 in watchlist, got %+v", snapshot.Items)
	}
}

type snapshotStub struct{ items []domain.FundSnapshot }

func (s snapshotStub) CurrentSnapshot() domain.SnapshotResponse {
	return domain.SnapshotResponse{Items: s.items, Source: domain.SourceStateReady, Stale: domain.StaleStateFresh}
}

type refreshableSnapshotStub struct {
	items     []domain.FundSnapshot
	err       error
	refreshes int
}

func (s *refreshableSnapshotStub) CurrentSnapshot() domain.SnapshotResponse {
	return domain.SnapshotResponse{Items: s.items, Source: domain.SourceStateReady, Stale: domain.StaleStateFresh}
}

func (s *refreshableSnapshotStub) Refresh(context.Context) error {
	s.refreshes++
	return s.err
}

type poolStub struct{ seeds []domain.FundSeed }

func (p poolStub) FundPool(context.Context) ([]domain.FundSeed, error) { return p.seeds, nil }

type memoryWatchlistStore struct {
	items map[string]map[string]domain.WatchlistItem
}

func newMemoryWatchlistStore() *memoryWatchlistStore {
	return &memoryWatchlistStore{items: map[string]map[string]domain.WatchlistItem{}}
}

func (s *memoryWatchlistStore) ListWatchlist(_ context.Context, visitorID string) ([]domain.WatchlistItem, error) {
	items := []domain.WatchlistItem{}
	for _, item := range s.items[visitorID] {
		items = append(items, item)
	}
	return items, nil
}

func (s *memoryWatchlistStore) AddWatchlist(_ context.Context, visitorID string, code string) error {
	if s.items[visitorID] == nil {
		s.items[visitorID] = map[string]domain.WatchlistItem{}
	}
	s.items[visitorID][code] = domain.WatchlistItem{Code: code}
	return nil
}

func (s *memoryWatchlistStore) RemoveWatchlist(_ context.Context, visitorID string, code string) error {
	delete(s.items[visitorID], code)
	return nil
}
