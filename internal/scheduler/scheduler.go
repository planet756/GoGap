package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"gogap/internal/domain"
	"gogap/internal/source"
	"gogap/internal/store"
)

const (
	DefaultQuoteInterval = 120 * time.Second
	DefaultQuoteMaxAge   = 60 * time.Second
	DefaultNAVBasis      = "官方已披露单位净值"
	EstimateNAVBasis     = "盘中估值"
)

type Clock interface {
	Now() time.Time
}

type Ticker interface {
	C() <-chan Tick
	Stop()
}

type Tick struct {
	At time.Time
}

type TickerFactory func(time.Duration) Ticker

type SnapshotStore interface {
	SaveFundSnapshot(context.Context, string, []byte, time.Time) error
	SaveFundSnapshots(context.Context, []store.FundBatchItem, time.Time) error
	ListFundSnapshots(context.Context) ([]store.FundCacheItem, error)
}

type Options struct {
	PoolProvider  source.FundPoolProvider
	QuoteAdapter  source.QuoteAdapter
	NAVAdapter    source.NAVAdapter
	Metadata      source.MetadataAdapter
	Store         SnapshotStore
	Clock         Clock
	NewTicker     TickerFactory
	QuoteInterval time.Duration
	QuoteMaxAge   time.Duration
	NAVBasis      string
	Disclaimer    string
	OnSnapshot    func(domain.SnapshotResponse)
}

type Service struct {
	poolProvider    source.FundPoolProvider
	quoteAdapter    source.QuoteAdapter
	navAdapter      source.NAVAdapter
	metadataAdapter source.MetadataAdapter
	store           SnapshotStore
	clock           Clock
	newTicker       TickerFactory

	quoteInterval time.Duration
	quoteMaxAge   time.Duration
	navBasis      string
	disclaimer    string
	onSnapshot    func(domain.SnapshotResponse)
	refreshMu     sync.Mutex

	mu             sync.RWMutex
	seeds          []domain.FundSeed
	navs           map[string]source.NAVResult
	metadataItems  map[string]source.MetadataResult
	quotes         map[string]source.QuoteResult
	navFetchedAt   time.Time
	lastQuoteFetch time.Time
	items          []domain.FundSnapshot
	lastErrors     []string
	lastErrorLog   string
	quoteFailures  int
	progress       *domain.ProgressState
}

type navChangePercentFiller interface {
	FillMissingChangePercent(context.Context, map[string]source.NAVResult, []string) (map[string]source.NAVResult, error)
}

func NewService(options Options) (*Service, error) {
	if options.PoolProvider == nil {
		return nil, errors.New("scheduler: pool provider is required")
	}
	if options.QuoteAdapter == nil {
		return nil, errors.New("scheduler: quote adapter is required")
	}
	if options.NAVAdapter == nil {
		return nil, errors.New("scheduler: nav adapter is required")
	}
	if options.Store == nil {
		return nil, errors.New("scheduler: store is required")
	}

	clock := options.Clock
	if clock == nil {
		clock = realClock{}
	}
	newTicker := options.NewTicker
	if newTicker == nil {
		newTicker = func(interval time.Duration) Ticker { return newRealTicker(interval) }
	}
	quoteInterval := options.QuoteInterval
	if quoteInterval <= 0 {
		quoteInterval = DefaultQuoteInterval
	}
	quoteMaxAge := options.QuoteMaxAge
	if quoteMaxAge <= 0 {
		quoteMaxAge = DefaultQuoteMaxAge
	}
	navBasis := options.NAVBasis
	if navBasis == "" {
		navBasis = DefaultNAVBasis
	}
	disclaimer := options.Disclaimer
	if disclaimer == "" {
		disclaimer = source.ComplianceDisclaimer
	}

	return &Service{
		poolProvider:    options.PoolProvider,
		quoteAdapter:    options.QuoteAdapter,
		navAdapter:      options.NAVAdapter,
		metadataAdapter: options.Metadata,
		store:           options.Store,
		clock:           clock,
		newTicker:       newTicker,
		quoteInterval:   quoteInterval,
		quoteMaxAge:     quoteMaxAge,
		navBasis:        navBasis,
		disclaimer:      disclaimer,
		onSnapshot:      options.OnSnapshot,
		navs:            map[string]source.NAVResult{},
		metadataItems:   map[string]source.MetadataResult{},
		quotes:          map[string]source.QuoteResult{},
	}, nil
}

func (s *Service) Start(ctx context.Context) error {
	s.refreshMu.Lock()
	loadedCache, err := s.loadStartupSnapshot(ctx)
	if err != nil {
		s.refreshMu.Unlock()
		return err
	}
	if !loadedCache {
		if err := s.refreshAfterStartupLoad(ctx); err != nil {
			if errors.Is(err, context.Canceled) || ctx.Err() != nil {
				s.refreshMu.Unlock()
				return err
			}
		}
	}
	s.refreshMu.Unlock()

	ticker := s.newTicker(s.quoteInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case tick := <-ticker.C():
			if !tick.At.IsZero() {
				s.setTickTime(tick.At)
			}
			if err := s.RefreshTick(ctx); err != nil {
				s.applyCachedError(ctx, err)
			}
		}
	}
}

func (s *Service) refreshAfterStartupLoad(ctx context.Context) error {
	s.publishProgress("拉取场内行情", 20)
	if err := s.RefreshQuotes(ctx); err != nil {
		s.logRefreshFailure("exchange quotes", err, true)
		if s.deferTransientQuoteError() {
			s.publishStalePartialSnapshot()
			return nil
		}
		s.applyCachedError(ctx, err)
		if len(s.CurrentSnapshot().Items) > 0 {
			return nil
		}
		return err
	}
	if s.shouldRefreshNAVLocked(s.clock.Now()) {
		s.publishProgress("拉取官方净值", 55)
		if err := s.RefreshNAV(ctx); err != nil {
			s.logRefreshFailure("official NAV", err, true)
			s.applyCachedError(ctx, err)
			if len(s.CurrentSnapshot().Items) > 0 {
				return nil
			}
			return err
		}
	}
	s.publishProgress("拉取基金资料", 75)
	if err := s.RefreshMetadata(ctx); err != nil {
		s.logRefreshFailure("fund metadata", err, true)
	}
	s.publishProgress("拉取完成", 100)
	log.Printf("scheduler startup: ready (%d rows)", len(s.CurrentSnapshot().Items))
	return nil
}

func (s *Service) loadStartupSnapshot(ctx context.Context) (bool, error) {
	log.Printf("scheduler startup: start")
	if err := s.loadSeeds(ctx); err != nil {
		return false, err
	}
	log.Printf("scheduler startup: fund list ready (%d funds)", len(s.enabledSeeds()))
	if err := s.loadLatestCache(ctx); err != nil {
		return false, err
	}
	if len(s.CurrentSnapshot().Items) == 0 {
		return false, nil
	}
	s.publishSnapshot()
	log.Printf("scheduler startup: cache ready (%d rows)", len(s.CurrentSnapshot().Items))
	return true, nil
}

func (s *Service) Refresh(ctx context.Context) error {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	log.Printf("scheduler refresh: start")
	s.publishProgress("拉取基金列表", 0)
	if err := s.loadSeeds(ctx); err != nil {
		return err
	}
	log.Printf("scheduler refresh: fund list ready (%d funds)", len(s.enabledSeeds()))
	s.publishProgress("读取本地缓存", 10)
	if err := s.loadLatestCache(ctx); err != nil {
		return err
	}
	s.publishProgress("拉取场内行情", 20)
	if err := s.RefreshQuotes(ctx); err != nil {
		s.logRefreshFailure("exchange quotes", err, false)
		if s.deferTransientQuoteError() {
			s.publishStalePartialSnapshot()
			return nil
		}
		s.applyCachedError(ctx, err)
		if len(s.CurrentSnapshot().Items) > 0 {
			return nil
		}
		return err
	}
	if s.shouldRefreshNAVLocked(s.clock.Now()) {
		s.publishProgress("拉取官方净值", 55)
		if err := s.RefreshNAV(ctx); err != nil {
			s.logRefreshFailure("official NAV", err, false)
			s.applyCachedError(ctx, err)
			if len(s.CurrentSnapshot().Items) > 0 {
				return nil
			}
			return err
		}
	}
	s.publishProgress("拉取基金资料", 75)
	if err := s.RefreshMetadata(ctx); err != nil {
		s.logRefreshFailure("fund metadata", err, false)
	}
	s.publishProgress("拉取完成", 100)
	log.Printf("scheduler refresh: ready (%d rows)", len(s.CurrentSnapshot().Items))
	return nil
}

func (s *Service) setTickTime(at time.Time) {
	if setter, ok := s.clock.(interface{ Set(time.Time) }); ok {
		setter.Set(at)
	}
}

func (s *Service) RefreshTick(ctx context.Context) error {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	log.Printf("scheduler refresh: start")
	if s.shouldRefreshNAVLocked(s.clock.Now()) {
		if err := s.RefreshNAV(ctx); err != nil {
			s.logRefreshFailure("official NAV", err, true)
			return err
		}
	}
	if err := s.RefreshMetadata(ctx); err != nil {
		s.logRefreshFailure("fund metadata", err, true)
	}
	if err := s.RefreshQuotes(ctx); err != nil {
		s.logRefreshFailure("exchange quotes", err, true)
		if s.deferTransientQuoteError() {
			s.publishStalePartialSnapshot()
			return nil
		}
		return err
	}
	log.Printf("scheduler refresh: ready (%d rows)", len(s.CurrentSnapshot().Items))
	return nil
}

func (s *Service) RefreshNAV(ctx context.Context) error {
	seeds := s.enabledSeeds()
	codes := make([]string, 0, len(seeds))
	for _, seed := range seeds {
		codes = append(codes, seed.NAVCode)
	}

	results, err := s.navAdapter.FetchNAVs(ctx, codes)
	if err != nil {
		return fmt.Errorf("refresh official NAV: %w", err)
	}
	if filler, ok := s.navAdapter.(navChangePercentFiller); ok {
		results, err = s.fillEligibleNAVChangePercents(ctx, filler, seeds, results)
		if err != nil {
			return fmt.Errorf("refresh official NAV change percent: %w", err)
		}
	}

	navs := make(map[string]source.NAVResult, len(results))
	for code, result := range results {
		navs[code] = result
	}

	s.mu.Lock()
	for code, nav := range navs {
		s.navs[code] = nav
	}
	s.navFetchedAt = s.clock.Now()
	s.mu.Unlock()
	return s.rebuildCurrentSnapshots(ctx, s.clock.Now(), nil)
}

func (s *Service) fillEligibleNAVChangePercents(ctx context.Context, filler navChangePercentFiller, seeds []domain.FundSeed, navs map[string]source.NAVResult) (map[string]source.NAVResult, error) {
	s.mu.RLock()
	metadata := make(map[string]source.MetadataResult, len(s.metadataItems))
	for code, item := range s.metadataItems {
		metadata[code] = item
	}
	quotes := cloneQuotes(s.quotes)
	s.mu.RUnlock()

	if len(metadata) == 0 || len(quotes) == 0 {
		return navs, nil
	}
	codes := make([]string, 0, len(seeds))
	for _, seed := range seeds {
		if navs[seed.NAVCode].ChangePercent != nil || !isSeedSnapshotEligible(seed, navs[seed.NAVCode], metadata[seed.Code], quotes[seed.QuoteSecID], s.clock.Now()) {
			continue
		}
		codes = append(codes, seed.NAVCode)
	}
	if len(codes) == 0 {
		return navs, nil
	}
	return filler.FillMissingChangePercent(ctx, navs, codes)
}

func (s *Service) RefreshMetadata(ctx context.Context) error {
	if s.metadataAdapter == nil {
		return nil
	}
	seeds := s.enabledSeeds()
	codes := make([]string, 0, len(seeds))
	for _, seed := range seeds {
		codes = append(codes, seed.Code)
	}
	progressAdapter, supportsProgress := s.metadataAdapter.(interface {
		FetchMetadataWithProgress(context.Context, []string, func(int, int)) (map[string]source.MetadataResult, error)
	})
	var results map[string]source.MetadataResult
	var err error
	if supportsProgress {
		results, err = progressAdapter.FetchMetadataWithProgress(ctx, codes, func(done int, total int) {
			if total <= 0 {
				return
			}
			s.publishProgress("拉取基金资料", 75+done*24/total)
		})
	} else {
		results, err = s.metadataAdapter.FetchMetadata(ctx, codes)
	}
	if err != nil {
		return fmt.Errorf("refresh fund metadata: %w", err)
	}
	s.mu.Lock()
	for code, result := range results {
		s.metadataItems[code] = result
	}
	s.mu.Unlock()
	if err := s.rebuildCurrentSnapshots(ctx, s.clock.Now(), nil); err != nil {
		return err
	}
	s.startAsyncNAVChangePercentFill(ctx)
	return nil
}

func (s *Service) startAsyncNAVChangePercentFill(ctx context.Context) {
	if _, ok := s.navAdapter.(navChangePercentFiller); !ok {
		return
	}
	go func() {
		defer recoverPanic("async NAV change fill")
		if err := s.fillStoredEligibleNAVChangePercents(ctx); err != nil {
			return
		}
		_ = s.rebuildCurrentSnapshots(ctx, s.clock.Now(), nil)
	}()
}

func recoverPanic(label string) {
	if r := recover(); r != nil {
		log.Printf("scheduler recovered panic in %s: %v", label, r)
	}
}

func (s *Service) fillStoredEligibleNAVChangePercents(ctx context.Context) error {
	filler, ok := s.navAdapter.(navChangePercentFiller)
	if !ok {
		return nil
	}
	seeds := s.enabledSeeds()
	s.mu.RLock()
	navs := make(map[string]source.NAVResult, len(s.navs))
	for code, nav := range s.navs {
		navs[code] = nav
	}
	s.mu.RUnlock()
	filled, err := s.fillEligibleNAVChangePercents(ctx, filler, seeds, navs)
	if err != nil {
		return err
	}
	s.mu.Lock()
	for code, nav := range filled {
		s.navs[code] = nav
	}
	s.mu.Unlock()
	return nil
}

func (s *Service) RefreshQuotes(ctx context.Context) error {
	seeds := s.enabledSeeds()
	secIDs := make([]string, 0, len(seeds))
	for _, seed := range seeds {
		secIDs = append(secIDs, seed.QuoteSecID)
	}

	quotes, err := s.quoteAdapter.FetchQuotes(ctx, secIDs)
	if err != nil {
		return fmt.Errorf("refresh exchange quotes: %w", err)
	}

	now := s.clock.Now()
	items := s.buildSnapshots(seeds, quotes, now, nil)
	s.mu.Lock()
	s.quotes = cloneQuotes(quotes)
	s.mu.Unlock()
	s.resetQuoteFailures()
	return s.replaceSnapshots(ctx, items, now, nil)
}

func (s *Service) rebuildCurrentSnapshots(ctx context.Context, at time.Time, extraErrors []string) error {
	s.mu.RLock()
	if len(s.quotes) == 0 || len(s.seeds) == 0 {
		s.mu.RUnlock()
		return nil
	}
	seeds := append([]domain.FundSeed(nil), s.seeds...)
	quotes := cloneQuotes(s.quotes)
	s.mu.RUnlock()
	items := s.buildSnapshots(seeds, quotes, at, extraErrors)
	return s.replaceSnapshots(ctx, items, at, extraErrors)
}

func (s *Service) deferTransientQuoteError() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.quoteFailures++
	return s.quoteFailures <= 2 && len(s.items) > 0
}

func (s *Service) resetQuoteFailures() {
	s.mu.Lock()
	s.quoteFailures = 0
	s.lastErrorLog = ""
	s.mu.Unlock()
}

func (s *Service) publishStalePartialSnapshot() {
	s.mu.Lock()
	items := cloneSnapshots(s.items)
	for i := range items {
		items[i].Source = domain.SourceStatePartial
		items[i].Stale = domain.StaleStateStale
	}
	s.items = items
	s.lastErrors = nil
	s.mu.Unlock()
	s.publishSnapshot()
}

func (s *Service) CurrentSnapshot() domain.SnapshotResponse {
	s.mu.RLock()
	defer s.mu.RUnlock()

	items := cloneSnapshots(s.items)
	errorsCopy := cloneErrors(s.lastErrors)
	return domain.SnapshotResponse{
		Disclaimer: s.disclaimer,
		Items:      items,
		Source:     aggregateSource(items, errorsCopy),
		Stale:      aggregateStale(items),
		Errors:     errorsCopy,
		Progress:   cloneProgress(s.progress),
	}
}

// IsValidFundCode returns true if the code is in the enabled fund pool.
func (s *Service) IsValidFundCode(code string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, seed := range s.seeds {
		if seed.Code == code {
			return true
		}
	}
	return false
}

func (s *Service) loadSeeds(ctx context.Context) error {
	seeds, err := s.poolProvider.FundPool(ctx)
	if err != nil {
		return fmt.Errorf("load fund pool: %w", err)
	}
	filtered := make([]domain.FundSeed, 0, len(seeds))
	for _, seed := range seeds {
		if seed.Enabled {
			filtered = append(filtered, seed)
		}
	}
	sort.Slice(filtered, func(i, j int) bool { return filtered[i].Code < filtered[j].Code })

	s.mu.Lock()
	s.seeds = filtered
	s.mu.Unlock()
	return nil
}

func (s *Service) loadLatestCache(ctx context.Context) error {
	cached, err := s.store.ListFundSnapshots(ctx)
	if err != nil {
		return fmt.Errorf("load fund snapshot cache: %w", err)
	}
	items := make([]domain.FundSnapshot, 0, len(cached))
	for _, item := range cached {
		var snapshot domain.FundSnapshot
		if err := json.Unmarshal(item.SnapshotJSON, &snapshot); err != nil {
			return fmt.Errorf("decode cached snapshot %s: %w", item.Code, err)
		}
		snapshot.Errors = cloneErrors(snapshot.Errors)
		items = append(items, snapshot)
	}
	sortSnapshots(items)

	s.mu.Lock()
	if len(items) > 0 {
		s.items = items
	}
	s.mu.Unlock()
	return nil
}

func (s *Service) replaceSnapshots(ctx context.Context, items []domain.FundSnapshot, at time.Time, extraErrors []string) error {
	sortSnapshots(items)
	if s.snapshotUnchanged(items, extraErrors) {
		s.mu.Lock()
		s.lastQuoteFetch = at
		s.mu.Unlock()
		return nil
	}
	batchItems := make([]store.FundBatchItem, 0, len(items))
	for _, item := range items {
		payload, err := json.Marshal(item)
		if err != nil {
			return fmt.Errorf("marshal snapshot %s: %w", item.Code, err)
		}
		batchItems = append(batchItems, store.FundBatchItem{Code: item.Code, SnapshotJSON: payload})
	}
	if err := s.store.SaveFundSnapshots(ctx, batchItems, at); err != nil {
		return err
	}

	s.mu.Lock()
	s.items = cloneSnapshots(items)
	s.lastQuoteFetch = at
	s.lastErrors = cloneErrors(extraErrors)
	s.mu.Unlock()
	s.publishSnapshot()
	return nil
}

func (s *Service) snapshotUnchanged(items []domain.FundSnapshot, extraErrors []string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return snapshotsEqual(s.items, items) && errorsEqual(s.lastErrors, cloneErrors(extraErrors))
}

func (s *Service) applyCachedError(ctx context.Context, cause error) {
	message := cause.Error()
	cached, err := s.store.ListFundSnapshots(ctx)
	if err == nil && len(cached) > 0 {
		items := make([]domain.FundSnapshot, 0, len(cached))
		for _, item := range cached {
			var snapshot domain.FundSnapshot
			if decodeErr := json.Unmarshal(item.SnapshotJSON, &snapshot); decodeErr != nil {
				continue
			}
			snapshot.Source = domain.SourceStateError
			snapshot.Stale = domain.StaleStateStale
			snapshot.Errors = appendError(snapshot.Errors, message)
			items = append(items, snapshot)
		}
		if len(items) > 0 {
			sortSnapshots(items)
			s.mu.Lock()
			s.items = items
			s.lastErrors = []string{message}
			s.mu.Unlock()
			s.publishSnapshot()
			return
		}
	}

	s.mu.Lock()
	items := cloneSnapshots(s.items)
	for i := range items {
		items[i].Source = domain.SourceStateError
		items[i].Stale = domain.StaleStateStale
		items[i].Errors = appendError(items[i].Errors, message)
	}
	s.items = items
	s.lastErrors = []string{message}
	s.lastErrorLog = message
	s.mu.Unlock()
	s.publishSnapshot()
}

func (s *Service) logRefreshFailure(source string, err error, startup bool) {
	message := err.Error()
	s.mu.Lock()
	if s.lastErrorLog == message {
		s.mu.Unlock()
		return
	}
	s.lastErrorLog = message
	s.mu.Unlock()
	if startup {
		log.Printf("scheduler startup refresh failed (%s): %v", source, err)
		return
	}
	log.Printf("scheduler refresh failed (%s): %v", source, err)
}

func (s *Service) publishSnapshot() {
	if s.onSnapshot != nil {
		s.onSnapshot(s.CurrentSnapshot())
	}
}

func (s *Service) publishProgress(label string, percent int) {
	s.mu.Lock()
	s.progress = &domain.ProgressState{Label: label, Percent: percent}
	s.mu.Unlock()
	s.publishSnapshot()
}

func (s *Service) shouldRefreshNAVLocked(now time.Time) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.navs) == 0 || s.navFetchedAt.IsZero() {
		return true
	}
	if !sameLocalDate(s.navFetchedAt, now) {
		return true
	}
	for _, nav := range s.navs {
		if s.navResultStale(nav, now) {
			return true
		}
		if isTradingHours(now) && !hasSameDayOfficialNAV(nav, now) && !hasUsableEstimate(nav, now) {
			return true
		}
	}
	return false
}

func (s *Service) RefreshWatchlist(ctx context.Context) error {
	return nil
}

func (s *Service) enabledSeeds() []domain.FundSeed {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]domain.FundSeed(nil), s.seeds...)
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

type realTicker struct {
	ticker *time.Ticker
	ch     chan Tick
	done   chan struct{}
	once   sync.Once
}

func newRealTicker(interval time.Duration) *realTicker {
	ticker := &realTicker{ticker: time.NewTicker(interval), ch: make(chan Tick), done: make(chan struct{})}
	go ticker.forward()
	return ticker
}

func (t *realTicker) C() <-chan Tick { return t.ch }

func (t *realTicker) Stop() {
	t.once.Do(func() {
		close(t.done)
		t.ticker.Stop()
	})
}

func (t *realTicker) forward() {
	defer close(t.ch)
	for {
		select {
		case <-t.done:
			return
		case at := <-t.ticker.C:
			select {
			case <-t.done:
				return
			case t.ch <- Tick{At: at}:
			}
		}
	}
}
