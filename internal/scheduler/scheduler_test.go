package scheduler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gogap/internal/domain"
	"gogap/internal/source"
	"gogap/internal/store"
)

func TestDefaultQuoteInterval(t *testing.T) {
	if DefaultQuoteInterval != 120*time.Second {
		t.Fatalf("DefaultQuoteInterval = %v, want %v", DefaultQuoteInterval, 120*time.Second)
	}
}

func TestRefreshPublishesProgressiveHydrationStages(t *testing.T) {
	price := 4.2
	nav := 4.0
	snapshots := make(chan domain.SnapshotResponse, 16)

	svc, err := NewService(Options{
		PoolProvider: staticPool{seeds: []domain.FundSeed{{Code: "510300", Name: "沪深300ETF", Category: domain.CategoryETF, Exchange: "SH", QuoteSecID: "1.510300", NAVCode: "510300", Enabled: true}}},
		QuoteAdapter: quoteAdapterFunc(func(context.Context, []string) (map[string]source.QuoteResult, error) {
			return map[string]source.QuoteResult{"1.510300": {Code: "510300", Price: &price, Time: "2026-05-31T14:30:00+08:00", Tradable: true}}, nil
		}),
		NAVAdapter: navAdapterFunc(func(context.Context, []string) (map[string]source.NAVResult, error) {
			return map[string]source.NAVResult{"510300": {Code: "510300", NAV: &nav, NAVDate: "2026-05-29"}}, nil
		}),
		Metadata: metadataAdapterFunc(func(context.Context, []string) (map[string]source.MetadataResult, error) {
			return map[string]source.MetadataResult{"510300": {Code: "510300", PurchaseStatus: "开放申购", PurchaseLimit: "100万元", RedemptionStatus: "开放赎回", FundScale: "500亿元"}}, nil
		}),
		Store: noopStore{},
		Clock: fixedClock{at: time.Date(2026, 5, 31, 14, 30, 0, 0, time.Local)},
		OnSnapshot: func(snapshot domain.SnapshotResponse) {
			snapshots <- snapshot
		},
	})
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}

	if err := svc.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh returned error: %v", err)
	}

	first := nextSnapshotMatching(t, snapshots, func(snapshot domain.SnapshotResponse) bool {
		return len(snapshot.Items) == 1 && snapshot.Items[0].PurchaseLimit != ""
	})
	if len(first.Items) != 1 || first.Items[0].PurchaseLimit != "100万元" || first.Items[0].FundScale != "500亿元" {
		t.Fatalf("expected first visible snapshot to pass OTC metadata gate, got %+v", first.Items)
	}

	if final := svc.CurrentSnapshot().Progress; final == nil || final.Percent != 100 {
		t.Fatalf("expected final progress at 100, got %+v", final)
	}
}

func TestRefreshLogsConciseInitializationSummary(t *testing.T) {
	price := 4.2
	nav := 4.0
	var logs bytes.Buffer
	oldOutput := log.Writer()
	oldFlags := log.Flags()
	log.SetOutput(&logs)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(oldOutput)
		log.SetFlags(oldFlags)
	})

	svc, err := NewService(Options{
		PoolProvider: staticPool{seeds: []domain.FundSeed{{Code: "510300", Name: "沪深300ETF", Category: domain.CategoryETF, Exchange: "SH", QuoteSecID: "1.510300", NAVCode: "510300", Enabled: true}}},
		QuoteAdapter: quoteAdapterFunc(func(context.Context, []string) (map[string]source.QuoteResult, error) {
			return map[string]source.QuoteResult{"1.510300": {Code: "510300", Price: &price, Time: "2026-05-31T14:30:00+08:00", Tradable: true}}, nil
		}),
		NAVAdapter: navAdapterFunc(func(context.Context, []string) (map[string]source.NAVResult, error) {
			return map[string]source.NAVResult{"510300": {Code: "510300", NAV: &nav, NAVDate: "2026-05-29"}}, nil
		}),
		Metadata: metadataAdapterFunc(func(context.Context, []string) (map[string]source.MetadataResult, error) {
			return map[string]source.MetadataResult{"510300": {Code: "510300", PurchaseStatus: "开放申购", RedemptionStatus: "开放赎回"}}, nil
		}),
		Store: noopStore{},
		Clock: fixedClock{at: time.Date(2026, 5, 31, 14, 30, 0, 0, time.Local)},
	})
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}

	if err := svc.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh returned error: %v", err)
	}
	output := logs.String()
	for _, noisy := range []string{"pulling exchange quotes", "exchange quotes ready", "pulling official NAV", "official NAV ready", "pulling fund metadata", "fund metadata ready", "loading cached snapshot"} {
		if strings.Contains(output, noisy) {
			t.Fatalf("expected concise initialization logs without %q, got:\n%s", noisy, output)
		}
	}
	for _, want := range []string{"scheduler refresh: start", "scheduler refresh: fund list ready (1 funds)", "scheduler refresh: ready (1 rows)"} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected log %q, got:\n%s", want, output)
		}
	}
}

func TestRefreshTickLogsConciseRefreshSummary(t *testing.T) {
	price := 4.2
	nav := 4.0
	var logs bytes.Buffer
	oldOutput := log.Writer()
	oldFlags := log.Flags()
	log.SetOutput(&logs)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(oldOutput)
		log.SetFlags(oldFlags)
	})

	svc, err := NewService(Options{
		PoolProvider: staticPool{seeds: []domain.FundSeed{{Code: "510300", Name: "沪深300ETF", Category: domain.CategoryETF, Exchange: "SH", QuoteSecID: "1.510300", NAVCode: "510300", Enabled: true}}},
		QuoteAdapter: quoteAdapterFunc(func(context.Context, []string) (map[string]source.QuoteResult, error) {
			return map[string]source.QuoteResult{"1.510300": {Code: "510300", Price: &price, Time: "2026-06-02T13:28:00+08:00", Tradable: true}}, nil
		}),
		NAVAdapter: navAdapterFunc(func(context.Context, []string) (map[string]source.NAVResult, error) {
			return map[string]source.NAVResult{"510300": {Code: "510300", NAV: &nav, NAVDate: "2026-06-01"}}, nil
		}),
		Metadata: metadataAdapterFunc(func(context.Context, []string) (map[string]source.MetadataResult, error) {
			return map[string]source.MetadataResult{"510300": {Code: "510300", PurchaseStatus: "开放申购", RedemptionStatus: "开放赎回"}}, nil
		}),
		Store: noopStore{},
		Clock: fixedClock{at: time.Date(2026, 6, 2, 13, 28, 0, 0, time.Local)},
	})
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}
	if err := svc.loadSeeds(context.Background()); err != nil {
		t.Fatalf("loadSeeds returned error: %v", err)
	}

	if err := svc.RefreshTick(context.Background()); err != nil {
		t.Fatalf("RefreshTick returned error: %v", err)
	}
	output := logs.String()
	for _, want := range []string{"scheduler refresh: start", "scheduler refresh: ready (1 rows)"} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected log %q, got:\n%s", want, output)
		}
	}
}

func TestRefreshSerializesOverlappingRequests(t *testing.T) {
	price := 4.2
	nav := 4.0
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	var inFlight int32
	var maxInFlight int32
	svc, err := NewService(Options{
		PoolProvider: staticPool{seeds: []domain.FundSeed{{Code: "510300", Name: "沪深300ETF", Category: domain.CategoryETF, Exchange: "SH", QuoteSecID: "1.510300", NAVCode: "510300", Enabled: true}}},
		QuoteAdapter: quoteAdapterFunc(func(context.Context, []string) (map[string]source.QuoteResult, error) {
			current := atomic.AddInt32(&inFlight, 1)
			for {
				max := atomic.LoadInt32(&maxInFlight)
				if current <= max || atomic.CompareAndSwapInt32(&maxInFlight, max, current) {
					break
				}
			}
			started <- struct{}{}
			<-release
			atomic.AddInt32(&inFlight, -1)
			return map[string]source.QuoteResult{"1.510300": {Code: "510300", Price: &price, Time: "2026-06-02T13:28:00+08:00", Tradable: true}}, nil
		}),
		NAVAdapter: navAdapterFunc(func(context.Context, []string) (map[string]source.NAVResult, error) {
			return map[string]source.NAVResult{"510300": {Code: "510300", NAV: &nav, NAVDate: "2026-06-01"}}, nil
		}),
		Metadata: metadataAdapterFunc(func(context.Context, []string) (map[string]source.MetadataResult, error) {
			return map[string]source.MetadataResult{"510300": {Code: "510300", PurchaseStatus: "开放申购", RedemptionStatus: "开放赎回"}}, nil
		}),
		Store: noopStore{},
		Clock: fixedClock{at: time.Date(2026, 6, 2, 13, 28, 0, 0, time.Local)},
	})
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}
	if err := svc.loadSeeds(context.Background()); err != nil {
		t.Fatalf("loadSeeds returned error: %v", err)
	}

	firstDone := make(chan error, 1)
	secondDone := make(chan error, 1)
	go func() { firstDone <- svc.RefreshTick(context.Background()) }()
	<-started
	go func() { secondDone <- svc.RefreshTick(context.Background()) }()

	select {
	case <-started:
		t.Fatal("expected overlapping refresh to wait for the first refresh")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first RefreshTick returned error: %v", err)
	}
	if err := <-secondDone; err != nil {
		t.Fatalf("second RefreshTick returned error: %v", err)
	}
	if max := atomic.LoadInt32(&maxInFlight); max != 1 {
		t.Fatalf("expected refresh quote fetches not to overlap, max in-flight = %d", max)
	}
}

func TestRefreshTickSkipsPublishWhenSnapshotUnchanged(t *testing.T) {
	price := 4.2
	nav := 4.0
	published := make(chan domain.SnapshotResponse, 8)
	svc, err := NewService(Options{
		PoolProvider: staticPool{seeds: []domain.FundSeed{{Code: "510300", Name: "沪深300ETF", Category: domain.CategoryETF, Exchange: "SH", QuoteSecID: "1.510300", NAVCode: "510300", Enabled: true}}},
		QuoteAdapter: quoteAdapterFunc(func(context.Context, []string) (map[string]source.QuoteResult, error) {
			return map[string]source.QuoteResult{"1.510300": {Code: "510300", Price: &price, Time: "2026-06-02T13:28:00+08:00", Tradable: true}}, nil
		}),
		NAVAdapter: navAdapterFunc(func(context.Context, []string) (map[string]source.NAVResult, error) {
			return map[string]source.NAVResult{"510300": {Code: "510300", NAV: &nav, NAVDate: "2026-06-01"}}, nil
		}),
		Metadata: metadataAdapterFunc(func(context.Context, []string) (map[string]source.MetadataResult, error) {
			return map[string]source.MetadataResult{"510300": {Code: "510300", PurchaseStatus: "开放申购", RedemptionStatus: "开放赎回"}}, nil
		}),
		Store:      noopStore{},
		Clock:      fixedClock{at: time.Date(2026, 6, 2, 13, 28, 0, 0, time.Local)},
		OnSnapshot: func(snapshot domain.SnapshotResponse) { published <- snapshot },
	})
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}
	if err := svc.loadSeeds(context.Background()); err != nil {
		t.Fatalf("loadSeeds returned error: %v", err)
	}
	if err := svc.RefreshTick(context.Background()); err != nil {
		t.Fatalf("first RefreshTick returned error: %v", err)
	}
	if len(published) != 1 {
		t.Fatalf("expected first refresh to publish one snapshot, got %d", len(published))
	}
	<-published
	if err := svc.RefreshTick(context.Background()); err != nil {
		t.Fatalf("second RefreshTick returned error: %v", err)
	}
	if len(published) != 0 {
		t.Fatalf("expected unchanged refresh not to publish a duplicate snapshot, got %d publications", len(published))
	}
}

func TestRefreshPublishesStartupProgress(t *testing.T) {
	price := 4.2
	nav := 4.0
	snapshots := make(chan domain.SnapshotResponse, 16)
	svc, err := NewService(Options{
		PoolProvider: staticPool{seeds: []domain.FundSeed{{Code: "510300", Name: "沪深300ETF", Category: domain.CategoryETF, Exchange: "SH", QuoteSecID: "1.510300", NAVCode: "510300", Enabled: true}}},
		QuoteAdapter: quoteAdapterFunc(func(context.Context, []string) (map[string]source.QuoteResult, error) {
			return map[string]source.QuoteResult{"1.510300": {Code: "510300", Price: &price, Time: "2026-05-31T14:30:00+08:00", Tradable: true}}, nil
		}),
		NAVAdapter: navAdapterFunc(func(context.Context, []string) (map[string]source.NAVResult, error) {
			return map[string]source.NAVResult{"510300": {Code: "510300", NAV: &nav, NAVDate: "2026-05-29"}}, nil
		}),
		Metadata: metadataAdapterFunc(func(context.Context, []string) (map[string]source.MetadataResult, error) {
			return map[string]source.MetadataResult{"510300": {Code: "510300", PurchaseStatus: "开放申购", RedemptionStatus: "开放赎回", FundScale: "500亿元"}}, nil
		}),
		Store:      noopStore{},
		Clock:      fixedClock{at: time.Date(2026, 5, 31, 14, 30, 0, 0, time.Local)},
		OnSnapshot: func(snapshot domain.SnapshotResponse) { snapshots <- snapshot },
	})
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}

	if err := svc.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh returned error: %v", err)
	}

	first := nextSnapshot(t, snapshots)
	if first.Progress == nil || first.Progress.Percent != 0 || first.Progress.Label != "拉取基金列表" {
		t.Fatalf("expected initial fund-list progress, got %+v", first.Progress)
	}
	seenMetadata := false
	var finalProgress *domain.ProgressState
	for len(snapshots) > 0 {
		snapshot := <-snapshots
		if snapshot.Progress != nil && snapshot.Progress.Label == "拉取基金资料" {
			seenMetadata = true
		}
		if snapshot.Progress != nil {
			finalProgress = snapshot.Progress
		}
	}
	if !seenMetadata || finalProgress == nil || finalProgress.Percent != 100 || finalProgress.Label != "拉取完成" {
		t.Fatalf("expected metadata progress and final 100%% progress, seenMetadata=%v final=%+v", seenMetadata, finalProgress)
	}
}

func TestRefreshMetadataPublishesItemProgress(t *testing.T) {
	snapshots := make(chan domain.SnapshotResponse, 8)
	svc, err := NewService(Options{
		PoolProvider: staticPool{seeds: []domain.FundSeed{
			{Code: "510300", Name: "沪深300ETF", Category: domain.CategoryETF, Exchange: "SH", QuoteSecID: "1.510300", NAVCode: "510300", Enabled: true},
			{Code: "159915", Name: "创业板ETF", Category: domain.CategoryETF, Exchange: "SZ", QuoteSecID: "0.159915", NAVCode: "159915", Enabled: true},
		}},
		QuoteAdapter: noopQuoteAdapter{},
		NAVAdapter:   noopNAVAdapter{},
		Metadata: metadataProgressAdapterFunc(func(_ context.Context, codes []string, onProgress func(done int, total int)) (map[string]source.MetadataResult, error) {
			for index := range codes {
				onProgress(index+1, len(codes))
			}
			return map[string]source.MetadataResult{}, nil
		}),
		Store:      noopStore{},
		Clock:      fixedClock{at: time.Date(2026, 5, 31, 14, 30, 0, 0, time.Local)},
		OnSnapshot: func(snapshot domain.SnapshotResponse) { snapshots <- snapshot },
	})
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}

	if err := svc.loadSeeds(context.Background()); err != nil {
		t.Fatalf("loadSeeds returned error: %v", err)
	}
	if err := svc.RefreshMetadata(context.Background()); err != nil {
		t.Fatalf("RefreshMetadata returned error: %v", err)
	}

	first := nextSnapshot(t, snapshots)
	if first.Progress == nil || first.Progress.Label != "拉取基金资料" || first.Progress.Percent <= 75 {
		t.Fatalf("expected metadata item progress beyond 75%%, got %+v", first.Progress)
	}
}

func TestRefreshMetadataFillsMissingNAVChangePercentAsynchronously(t *testing.T) {
	price := 1.23
	nav := 1.2
	change := 1.29
	started := make(chan struct{})
	release := make(chan struct{})
	filler := &blockingNavFillerAdapter{results: map[string]source.NAVResult{"160106": {Code: "160106", NAV: &nav, NAVDate: "2026-06-01"}}, change: change, started: started, release: release}
	svc, err := NewService(Options{
		PoolProvider: staticPool{seeds: []domain.FundSeed{{Code: "160106", Name: "南方高增LOF", Category: domain.CategoryOtherLOF, Exchange: "SZ", QuoteSecID: "0.160106", NAVCode: "160106", Enabled: true}}},
		QuoteAdapter: quoteAdapterFunc(func(context.Context, []string) (map[string]source.QuoteResult, error) {
			return map[string]source.QuoteResult{"0.160106": {Code: "160106", Price: &price, Time: "2026-06-01T10:00:00+08:00", Tradable: true}}, nil
		}),
		NAVAdapter: filler,
		Metadata: metadataAdapterFunc(func(context.Context, []string) (map[string]source.MetadataResult, error) {
			return map[string]source.MetadataResult{"160106": {Code: "160106", PurchaseStatus: "开放申购", RedemptionStatus: "开放赎回"}}, nil
		}),
		Store: noopStore{},
		Clock: fixedClock{at: time.Date(2026, 6, 1, 10, 0, 0, 0, time.Local)},
	})
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}
	if err := svc.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh returned error: %v", err)
	}
	snapshot := svc.CurrentSnapshot()
	if len(snapshot.Items) != 1 || snapshot.Items[0].NAVChangePercent != nil {
		t.Fatalf("expected snapshot to stay ready without blocking for NAV percent fill, got %+v", snapshot.Items)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("expected async NAV percent fill to start")
	}
	close(release)
	for attempt := 0; attempt < 20; attempt++ {
		snapshot = svc.CurrentSnapshot()
		if len(snapshot.Items) == 1 && snapshot.Items[0].NAVChangePercent != nil && *snapshot.Items[0].NAVChangePercent == change {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("expected async NAV percent fill to update snapshot, got %+v", svc.CurrentSnapshot().Items)
}

func TestRefreshMetadataDoesNotBlockOnNAVChangePercentFallback(t *testing.T) {
	price := 1.23
	nav := 1.2
	filler := &navFillerAdapter{results: map[string]source.NAVResult{"160106": {Code: "160106", NAV: &nav, NAVDate: "2026-06-01"}}}
	svc, err := NewService(Options{
		PoolProvider: staticPool{seeds: []domain.FundSeed{{Code: "160106", Name: "南方高增LOF", Category: domain.CategoryOtherLOF, Exchange: "SZ", QuoteSecID: "0.160106", NAVCode: "160106", Enabled: true}}},
		QuoteAdapter: quoteAdapterFunc(func(context.Context, []string) (map[string]source.QuoteResult, error) {
			return map[string]source.QuoteResult{"0.160106": {Code: "160106", Price: &price, Time: "2026-06-01T10:00:00+08:00", Tradable: true}}, nil
		}),
		NAVAdapter: filler,
		Metadata: metadataAdapterFunc(func(context.Context, []string) (map[string]source.MetadataResult, error) {
			return map[string]source.MetadataResult{"160106": {Code: "160106", PurchaseStatus: "开放申购", RedemptionStatus: "开放赎回"}}, nil
		}),
		Store: noopStore{},
		Clock: fixedClock{at: time.Date(2026, 6, 1, 10, 0, 0, 0, time.Local)},
	})
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}
	if err := svc.loadSeeds(context.Background()); err != nil {
		t.Fatalf("loadSeeds returned error: %v", err)
	}
	svc.navs = filler.results
	svc.quotes = map[string]source.QuoteResult{"0.160106": {Code: "160106", Price: &price, Time: "2026-06-01T10:00:00+08:00", Tradable: true}}
	if err := svc.RefreshMetadata(context.Background()); err != nil {
		t.Fatalf("RefreshMetadata returned error: %v", err)
	}
	if calls := atomic.LoadInt32(&filler.fillCalls); calls > 1 {
		t.Fatalf("expected metadata refresh not to synchronously loop on NAV change percent fallback, got %d calls", calls)
	}
}

func TestStartupPublishesQuoteSnapshotBeforeNAVCompletes(t *testing.T) {
	quoteStarted := make(chan struct{})
	quoteMayReturn := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	svc, err := NewService(Options{
		PoolProvider: staticPool{seeds: []domain.FundSeed{{Code: "510300", Name: "沪深300ETF", Category: domain.CategoryETF, Exchange: "SH", QuoteSecID: "1.510300", NAVCode: "510300", Enabled: true}}},
		QuoteAdapter: quoteAdapterFunc(func(context.Context, []string) (map[string]source.QuoteResult, error) {
			close(quoteStarted)
			<-quoteMayReturn
			price := 4.2
			return map[string]source.QuoteResult{"1.510300": {Code: "510300", Price: &price, Time: "2026-05-31 14:30:00", Tradable: true}}, nil
		}),
		NAVAdapter: navAdapterFunc(func(ctx context.Context, _ []string) (map[string]source.NAVResult, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}),
		Store: noopStore{},
		Clock: fixedClock{at: time.Date(2026, 5, 31, 14, 30, 0, 0, time.Local)},
	})
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- svc.Refresh(ctx) }()

	select {
	case <-quoteStarted:
	case <-time.After(time.Second):
		t.Fatal("expected startup refresh to fetch quotes before waiting on NAV")
	}
	close(quoteMayReturn)

	var snapshot domain.SnapshotResponse
	deadline := time.After(time.Second)
	for {
		snapshot = svc.CurrentSnapshot()
		if snapshot.Progress != nil && snapshot.Progress.Label == "拉取场内行情" && snapshot.Progress.Percent >= 20 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("expected quote-first progress without item before OTC metadata, got %+v", snapshot.Progress)
		default:
			time.Sleep(time.Millisecond)
		}
	}

	if len(snapshot.Items) != 0 || snapshot.Source == domain.SourceStateError {
		t.Fatalf("expected non-error snapshot source, got %q", snapshot.Source)
	}

	cancel()
	<-done
}

func TestEmptySnapshotReportsLoading(t *testing.T) {
	if got := aggregateSource(nil, nil); got != domain.SourceStateLoading {
		t.Fatalf("aggregateSource(empty) = %q, want %q", got, domain.SourceStateLoading)
	}
	if got := aggregateStale(nil); got != domain.StaleStateUnknown {
		t.Fatalf("aggregateStale(empty) = %q, want %q", got, domain.StaleStateUnknown)
	}
}

func TestStartPublishesCacheWithoutImmediateRefresh(t *testing.T) {
	price := 1.23
	cached := domain.FundSnapshot{Code: "160106", Name: "南方高增LOF", Category: domain.CategoryOtherLOF, QuotePrice: &price, Source: domain.SourceStateReady, Stale: domain.StaleStateFresh}
	payload, err := json.Marshal(cached)
	if err != nil {
		t.Fatalf("marshal cached snapshot: %v", err)
	}
	snapshots := make(chan domain.SnapshotResponse, 1)
	ticks := make(chan Tick)
	store := memorySnapshotStore{cached: []store.FundCacheItem{{Code: cached.Code, SnapshotJSON: payload, UpdatedAt: time.Date(2026, 5, 31, 14, 30, 0, 0, time.Local)}}}
	svc, err := NewService(Options{
		PoolProvider: staticPool{seeds: []domain.FundSeed{{Code: "160106", Name: "南方高增LOF", Category: domain.CategoryOtherLOF, Exchange: "SZ", QuoteSecID: "0.160106", NAVCode: "160106", Enabled: true}}},
		QuoteAdapter: quoteAdapterFunc(func(context.Context, []string) (map[string]source.QuoteResult, error) {
			return nil, errors.New("quote fetch should wait for ticker")
		}),
		NAVAdapter: navAdapterFunc(func(context.Context, []string) (map[string]source.NAVResult, error) {
			return nil, errors.New("nav fetch should wait for ticker")
		}),
		Store:     store,
		Clock:     fixedClock{at: time.Date(2026, 6, 1, 10, 0, 0, 0, time.Local)},
		NewTicker: func(time.Duration) Ticker { return channelTicker{ticks: ticks} },
		OnSnapshot: func(snapshot domain.SnapshotResponse) {
			snapshots <- snapshot
		},
	})
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- svc.Start(ctx) }()
	t.Cleanup(func() {
		cancel()
		<-done
	})

	snapshot := nextSnapshot(t, snapshots)
	if len(snapshot.Items) != 1 || snapshot.Items[0].Code != "160106" || snapshot.Source != domain.SourceStateReady {
		t.Fatalf("expected cached startup snapshot without immediate refresh, got %+v", snapshot)
	}
}

func TestColdStartLoadsFundPoolOnce(t *testing.T) {
	price := 1.23
	nav := 1.2
	snapshots := make(chan domain.SnapshotResponse, 16)
	ticks := make(chan Tick)
	pool := &countingPool{seeds: []domain.FundSeed{{Code: "160106", Name: "南方高增LOF", Category: domain.CategoryOtherLOF, Exchange: "SZ", QuoteSecID: "0.160106", NAVCode: "160106", Enabled: true}}}
	svc, err := NewService(Options{
		PoolProvider: pool,
		QuoteAdapter: quoteAdapterFunc(func(context.Context, []string) (map[string]source.QuoteResult, error) {
			return map[string]source.QuoteResult{"0.160106": {Code: "160106", Price: &price, Time: "2026-06-01T10:00:00+08:00", Tradable: true}}, nil
		}),
		NAVAdapter: navAdapterFunc(func(context.Context, []string) (map[string]source.NAVResult, error) {
			return map[string]source.NAVResult{"160106": {Code: "160106", NAV: &nav, NAVDate: "2026-05-29"}}, nil
		}),
		Metadata: metadataAdapterFunc(func(context.Context, []string) (map[string]source.MetadataResult, error) {
			return map[string]source.MetadataResult{"160106": {Code: "160106", PurchaseStatus: "开放申购", RedemptionStatus: "开放赎回"}}, nil
		}),
		Store:     noopStore{},
		Clock:     fixedClock{at: time.Date(2026, 6, 1, 10, 0, 0, 0, time.Local)},
		NewTicker: func(time.Duration) Ticker { return channelTicker{ticks: ticks} },
		OnSnapshot: func(snapshot domain.SnapshotResponse) {
			snapshots <- snapshot
		},
	})
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- svc.Start(ctx) }()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	nextSnapshotMatching(t, snapshots, func(snapshot domain.SnapshotResponse) bool { return len(snapshot.Items) > 0 })
	if pool.calls != 1 {
		t.Fatalf("expected cold startup to load fund pool once, got %d", pool.calls)
	}
}

func TestRefreshKeepsSuccessfulQuotesFreshAfterSlowMetadata(t *testing.T) {
	clock := &manualClock{at: time.Date(2026, 6, 1, 10, 0, 0, 0, time.Local)}
	price := 1.23
	nav := 1.2
	quoteCalls := 0
	svc, err := NewService(Options{
		PoolProvider: staticPool{seeds: []domain.FundSeed{{Code: "160106", Name: "南方高增LOF", Category: domain.CategoryOtherLOF, Exchange: "SZ", QuoteSecID: "0.160106", NAVCode: "160106", Enabled: true}}},
		QuoteAdapter: quoteAdapterFunc(func(context.Context, []string) (map[string]source.QuoteResult, error) {
			quoteCalls++
			return map[string]source.QuoteResult{"0.160106": {Code: "160106", Price: &price, Time: clock.Now().Format(time.RFC3339), Tradable: true}}, nil
		}),
		NAVAdapter: navAdapterFunc(func(context.Context, []string) (map[string]source.NAVResult, error) {
			return map[string]source.NAVResult{"160106": {Code: "160106", NAV: &nav, NAVDate: "2026-05-29"}}, nil
		}),
		Metadata: metadataAdapterFunc(func(context.Context, []string) (map[string]source.MetadataResult, error) {
			clock.Set(clock.Now().Add(3 * time.Minute))
			return map[string]source.MetadataResult{"160106": {Code: "160106", PurchaseStatus: "开放申购", RedemptionStatus: "开放赎回", FundScale: "10亿元"}}, nil
		}),
		Store:       noopStore{},
		Clock:       clock,
		QuoteMaxAge: time.Minute,
	})
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}

	if err := svc.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh returned error: %v", err)
	}
	snapshot := svc.CurrentSnapshot()
	if quoteCalls != 1 || snapshot.Stale != domain.StaleStateFresh {
		t.Fatalf("expected slow metadata to preserve successful quote freshness, calls=%d snapshot=%+v", quoteCalls, snapshot)
	}
}

func TestAggregateSourceTreatsRowPartialAsReadyWhenFetchSucceeded(t *testing.T) {
	items := []domain.FundSnapshot{{Code: "160106", Source: domain.SourceStatePartial}}
	if got := aggregateSource(items, nil); got != domain.SourceStateReady {
		t.Fatalf("aggregateSource(row partial only) = %q, want %q", got, domain.SourceStateReady)
	}
}

func TestQuoteStaleUsesSuccessfulPresenceNotMarketTimestampAge(t *testing.T) {
	price := 1.23
	quote := source.QuoteResult{Code: "160106", Price: &price, Time: "2026-06-01T10:30:00+08:00", Tradable: true}
	if quoteStale(quote, true) {
		t.Fatal("expected successful quote result to remain fresh even when market timestamp is delayed")
	}
}

func TestAggregateStaleUsesFreshWhenAnyRowsAreFresh(t *testing.T) {
	items := []domain.FundSnapshot{
		{Code: "160106", Stale: domain.StaleStateFresh},
		{Code: "513100", Stale: domain.StaleStateStale},
	}
	if got := aggregateStale(items); got != domain.StaleStateFresh {
		t.Fatalf("aggregateStale(mixed fresh/stale) = %q, want %q", got, domain.StaleStateFresh)
	}
}

func TestBuildSnapshotsSkipsMissingAndAbsurdExchangeQuotes(t *testing.T) {
	validQuote := 4.0
	absurdQuote := 100.0
	nav := 0.27
	validNAV := 4.0
	svc, err := NewService(Options{
		PoolProvider: staticPool{seeds: []domain.FundSeed{
			{Code: "159001", Name: "保证金ETF", Category: domain.CategoryBondMoney, QuoteSecID: "0.159001", NAVCode: "159001", Enabled: true},
			{Code: "501092", Name: "互联互通LOF", Category: domain.CategoryOtherLOF, QuoteSecID: "1.501092", NAVCode: "501092", Enabled: true},
			{Code: "510300", Name: "沪深300ETF", Category: domain.CategoryETF, QuoteSecID: "1.510300", NAVCode: "510300", Enabled: true},
		}},
		QuoteAdapter: noopQuoteAdapter{},
		NAVAdapter:   noopNAVAdapter{},
		Store:        noopStore{},
	})
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}
	if err := svc.loadSeeds(context.Background()); err != nil {
		t.Fatalf("loadSeeds returned error: %v", err)
	}
	svc.navs = map[string]source.NAVResult{
		"159001": {Code: "159001", NAV: &nav, NAVDate: "2026-06-01"},
		"501092": {Code: "501092", NAV: &validNAV, NAVDate: "2026-06-01"},
		"510300": {Code: "510300", NAV: &validNAV, NAVDate: "2026-06-01"},
	}
	svc.metadataItems = map[string]source.MetadataResult{
		"159001": {Code: "159001", PurchaseStatus: "开放申购", RedemptionStatus: "开放赎回"},
		"501092": {Code: "501092", PurchaseStatus: "开放申购", RedemptionStatus: "开放赎回"},
		"510300": {Code: "510300", PurchaseStatus: "开放申购", RedemptionStatus: "开放赎回"},
	}
	items := svc.buildSnapshots(svc.enabledSeeds(), map[string]source.QuoteResult{
		"0.159001": {Code: "159001", Price: &absurdQuote, Tradable: true, Time: "2026-06-01T10:00:00+08:00"},
		"1.510300": {Code: "510300", Price: &validQuote, Tradable: true, Time: "2026-06-01T10:00:00+08:00"},
	}, time.Date(2026, 6, 1, 10, 0, 0, 0, time.Local), nil)
	if len(items) != 1 || items[0].Code != "510300" {
		t.Fatalf("expected only valid quote row to remain, got %+v", items)
	}
}

func TestBuildSnapshotsKeepsOnlyFundsTradableOnExchangeAndOTC(t *testing.T) {
	price := 4.0
	nav := 4.0
	activeTurnover := 10000000.0
	svc, err := NewService(Options{
		PoolProvider: staticPool{seeds: []domain.FundSeed{
			{Code: "501019", Name: "军工LOF", Category: domain.CategoryIndexLOF, QuoteSecID: "1.501019", NAVCode: "501019", Enabled: true},
			{Code: "510300", Name: "沪深300ETF", Category: domain.CategoryETF, QuoteSecID: "1.510300", NAVCode: "510300", Enabled: true},
			{Code: "501018", Name: "南方原油LOF", Category: domain.CategoryCommodity, QuoteSecID: "1.501018", NAVCode: "501018", Enabled: true},
			{Code: "501092", Name: "交银瑞思LOF", Category: domain.CategoryOtherLOF, QuoteSecID: "1.501092", NAVCode: "501092", Enabled: true},
			{Code: "180601", Name: "无状态基金", Category: domain.CategoryOtherLOF, QuoteSecID: "1.180601", NAVCode: "180601", Enabled: true},
		}},
		QuoteAdapter: noopQuoteAdapter{},
		NAVAdapter:   noopNAVAdapter{},
		Store:        noopStore{},
	})
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}
	if err := svc.loadSeeds(context.Background()); err != nil {
		t.Fatalf("loadSeeds returned error: %v", err)
	}
	svc.navs = map[string]source.NAVResult{
		"501019": {Code: "501019", NAV: &nav, NAVDate: "2026-06-01"},
		"510300": {Code: "510300", NAV: &nav, NAVDate: "2026-06-01"},
		"501018": {Code: "501018", NAV: &nav, NAVDate: "2026-06-01"},
		"501092": {Code: "501092", NAV: &nav, NAVDate: "2026-06-01"},
		"180601": {Code: "180601", NAV: &nav, NAVDate: "2026-06-01"},
	}
	svc.metadataItems = map[string]source.MetadataResult{
		"501019": {Code: "501019", PurchaseStatus: "开放申购", RedemptionStatus: "开放赎回"},
		"510300": {Code: "510300", PurchaseStatus: "场内交易", RedemptionStatus: "场内交易"},
		"501018": {Code: "501018", PurchaseStatus: "暂停申购", RedemptionStatus: "开放赎回"},
		"501092": {Code: "501092", PurchaseStatus: "开放申购", RedemptionStatus: "暂停赎回"},
	}
	items := svc.buildSnapshots(svc.enabledSeeds(), map[string]source.QuoteResult{
		"1.501019": {Code: "501019", Price: &price, Tradable: true, Time: "2026-06-01T10:00:00+08:00"},
		"1.510300": {Code: "510300", Price: &price, Tradable: true, TurnoverAmount: &activeTurnover, Time: "2026-06-01T10:00:00+08:00"},
		"1.501018": {Code: "501018", Price: &price, Tradable: true, Time: "2026-06-01T10:00:00+08:00"},
		"1.501092": {Code: "501092", Price: &price, Tradable: true, Time: "2026-06-01T10:00:00+08:00"},
		"1.180601": {Code: "180601", Price: &price, Tradable: true, Time: "2026-06-01T10:00:00+08:00"},
	}, time.Date(2026, 6, 1, 10, 0, 0, 0, time.Local), nil)
	if len(items) != 3 || items[0].Code != "501018" || items[1].Code != "501019" || items[2].Code != "501092" {
		t.Fatalf("expected OTC-channel funds plus allowed ETF exchange-only statuses to remain, got %+v", items)
	}
}

func TestBuildSnapshotsUsesExchangeQuoteNameForDisplay(t *testing.T) {
	price := 4.0
	nav := 4.0
	svc, err := NewService(Options{
		PoolProvider: staticPool{seeds: []domain.FundSeed{{Code: "501019", Name: "国泰国证航天军工指数(LOF)A", Category: domain.CategoryIndexLOF, QuoteSecID: "1.501019", NAVCode: "501019", Enabled: true}}},
		QuoteAdapter: noopQuoteAdapter{},
		NAVAdapter:   noopNAVAdapter{},
		Store:        noopStore{},
	})
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}
	if err := svc.loadSeeds(context.Background()); err != nil {
		t.Fatalf("loadSeeds returned error: %v", err)
	}
	svc.navs = map[string]source.NAVResult{"501019": {Code: "501019", NAV: &nav, NAVDate: "2026-06-01"}}
	svc.metadataItems = map[string]source.MetadataResult{"501019": {Code: "501019", PurchaseStatus: "开放申购", RedemptionStatus: "开放赎回"}}
	items := svc.buildSnapshots(svc.enabledSeeds(), map[string]source.QuoteResult{"1.501019": {Code: "501019", Name: "军工LOF", Price: &price, Tradable: true, Time: "2026-06-01T10:00:00+08:00"}}, time.Date(2026, 6, 1, 10, 0, 0, 0, time.Local), nil)
	if len(items) != 1 || items[0].Name != "军工LOF" {
		t.Fatalf("expected display name to come from exchange quote, got %+v", items)
	}
}

func TestBuildSnapshotsKeepsREITNamedLOFFunds(t *testing.T) {
	price := 1.0
	nav := 1.0
	svc, err := NewService(Options{
		PoolProvider: staticPool{seeds: []domain.FundSeed{{Code: "160140", Name: "美国REIT精选LOF", Category: domain.CategoryQDII, QuoteSecID: "0.160140", NAVCode: "160140", Enabled: true}}},
		QuoteAdapter: noopQuoteAdapter{},
		NAVAdapter:   noopNAVAdapter{},
		Store:        noopStore{},
	})
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}
	if err := svc.loadSeeds(context.Background()); err != nil {
		t.Fatalf("loadSeeds returned error: %v", err)
	}
	svc.navs = map[string]source.NAVResult{"160140": {Code: "160140", NAV: &nav, NAVDate: "2026-06-01", Category: domain.CategoryQDII}}
	svc.metadataItems = map[string]source.MetadataResult{"160140": {Code: "160140", PurchaseStatus: "开放申购", RedemptionStatus: "开放赎回"}}
	items := svc.buildSnapshots(svc.enabledSeeds(), map[string]source.QuoteResult{"0.160140": {Code: "160140", Name: "美国REIT精选LOF", Price: &price, Tradable: true, Time: "2026-06-01T10:00:00+08:00"}}, time.Date(2026, 6, 1, 10, 0, 0, 0, time.Local), nil)
	if len(items) != 1 || items[0].Name != "美国REIT精选LOF" || items[0].Category != domain.CategoryQDII {
		t.Fatalf("expected REIT-named LOF fund to be retained by type/category, got %+v", items)
	}
}

func TestBuildSnapshotsKeepsLimitOnlyListedQDIILOF(t *testing.T) {
	price := 1.264
	nav := 1.2957
	turnover := 19906251.205
	svc, err := NewService(Options{
		PoolProvider: staticPool{seeds: []domain.FundSeed{{Code: "164824", Name: "工银印度基金人民币", Category: domain.CategoryQDII, QuoteSecID: "0.164824", NAVCode: "164824", Enabled: true}}},
		QuoteAdapter: noopQuoteAdapter{},
		NAVAdapter:   noopNAVAdapter{},
		Store:        noopStore{},
	})
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}
	if err := svc.loadSeeds(context.Background()); err != nil {
		t.Fatalf("loadSeeds returned error: %v", err)
	}
	svc.navs = map[string]source.NAVResult{"164824": {Code: "164824", NAV: &nav, NAVDate: "2026-05-29", Category: domain.CategoryQDII}}
	svc.metadataItems = map[string]source.MetadataResult{"164824": {Code: "164824", PurchaseStatus: "限大额", RedemptionStatus: "开放赎回", PurchaseLimit: "50万"}}
	items := svc.buildSnapshots(svc.enabledSeeds(), map[string]source.QuoteResult{"0.164824": {Code: "164824", Name: "印度基金LOF", Price: &price, TurnoverAmount: &turnover, Tradable: true, Time: "2026-06-02T15:09:00+08:00"}}, time.Date(2026, 6, 2, 15, 10, 0, 0, time.Local), nil)
	if len(items) != 1 || items[0].Code != "164824" || items[0].PurchaseLimit != "50万" {
		t.Fatalf("expected limit-only listed QDII LOF to remain, got %+v", items)
	}
}

func TestBuildSnapshotsUsesTradingHoursEstimate(t *testing.T) {
	price := 5.0
	officialNAV := 4.0
	estimatedNAV := 4.5
	estimatedChange := 1.25
	svc, err := NewService(Options{
		PoolProvider: staticPool{seeds: []domain.FundSeed{{Code: "510300", Name: "沪深300ETF", Category: domain.CategoryETF, QuoteSecID: "1.510300", NAVCode: "510300", Enabled: true}}},
		QuoteAdapter: noopQuoteAdapter{},
		NAVAdapter:   noopNAVAdapter{},
		Store:        noopStore{},
	})
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}
	if err := svc.loadSeeds(context.Background()); err != nil {
		t.Fatalf("loadSeeds returned error: %v", err)
	}
	svc.navs = map[string]source.NAVResult{"510300": {Code: "510300", NAV: &officialNAV, NAVDate: "2026-06-01", EstimatedNAV: &estimatedNAV, EstimatedNAVTime: "2026-06-02 10:30", EstimatedChangePercent: &estimatedChange, Category: domain.CategoryIndexLOF}}
	svc.metadataItems = map[string]source.MetadataResult{"510300": {Code: "510300", PurchaseStatus: "开放申购", RedemptionStatus: "开放赎回"}}
	items := svc.buildSnapshots(svc.enabledSeeds(), map[string]source.QuoteResult{"1.510300": {Code: "510300", Price: &price, Tradable: true, Time: "2026-06-02T10:31:00+08:00"}}, time.Date(2026, 6, 2, 10, 31, 0, 0, time.FixedZone("CST", 8*60*60)), nil)
	if len(items) != 1 {
		t.Fatalf("expected one item, got %+v", items)
	}
	if items[0].NAV == nil || *items[0].NAV != estimatedNAV || items[0].NAVDate != "2026-06-02 10:30" || items[0].NAVBasis != EstimateNAVBasis {
		t.Fatalf("expected trading-hours estimate selected, got %+v", items[0])
	}
	if items[0].NAVChangePercent == nil || *items[0].NAVChangePercent != estimatedChange {
		t.Fatalf("expected estimated change percent selected, got %+v", items[0])
	}
	if items[0].PremiumRate == nil || *items[0].PremiumRate < 11.10 || *items[0].PremiumRate > 11.12 {
		t.Fatalf("expected premium calculated from estimated NAV, got %+v", items[0].PremiumRate)
	}
	if items[0].Stale != domain.StaleStateFresh {
		t.Fatalf("expected same-day estimate to be fresh during trading hours, got %q", items[0].Stale)
	}
}

func TestBuildSnapshotsPrefersSameDayOfficialNAVOverSameDayEstimate(t *testing.T) {
	price := 5.0
	officialNAV := 4.8
	officialChange := 2.0
	estimatedNAV := 4.5
	estimatedChange := 1.25
	svc, err := NewService(Options{
		PoolProvider: staticPool{seeds: []domain.FundSeed{{Code: "510300", Name: "沪深300ETF", Category: domain.CategoryETF, QuoteSecID: "1.510300", NAVCode: "510300", Enabled: true}}},
		QuoteAdapter: noopQuoteAdapter{}, NAVAdapter: noopNAVAdapter{}, Store: noopStore{},
	})
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}
	if err := svc.loadSeeds(context.Background()); err != nil {
		t.Fatalf("loadSeeds returned error: %v", err)
	}
	svc.navs = map[string]source.NAVResult{"510300": {Code: "510300", NAV: &officialNAV, NAVDate: "2026-06-02", ChangePercent: &officialChange, EstimatedNAV: &estimatedNAV, EstimatedNAVTime: "2026-06-02 10:30", EstimatedChangePercent: &estimatedChange, Category: domain.CategoryIndexLOF}}
	svc.metadataItems = map[string]source.MetadataResult{"510300": {Code: "510300", PurchaseStatus: "开放申购", RedemptionStatus: "开放赎回"}}
	items := svc.buildSnapshots(svc.enabledSeeds(), map[string]source.QuoteResult{"1.510300": {Code: "510300", Price: &price, Tradable: true, Time: "2026-06-02T10:31:00+08:00"}}, time.Date(2026, 6, 2, 10, 31, 0, 0, time.FixedZone("CST", 8*60*60)), nil)
	if len(items) != 1 {
		t.Fatalf("expected one item, got %+v", items)
	}
	if items[0].NAV == nil || *items[0].NAV != officialNAV || items[0].NAVDate != "2026-06-02" || items[0].NAVBasis != DefaultNAVBasis {
		t.Fatalf("expected same-day official NAV to outrank same-day estimate, got %+v", items[0])
	}
	if items[0].NAVChangePercent == nil || *items[0].NAVChangePercent != officialChange {
		t.Fatalf("expected official NAV change selected, got %+v", items[0])
	}
}

func TestBuildSnapshotsUsesSameDayEstimateOutsideTradingHours(t *testing.T) {
	price := 5.0
	officialNAV := 4.0
	estimatedNAV := 4.5
	svc, err := NewService(Options{
		PoolProvider: staticPool{seeds: []domain.FundSeed{{Code: "510300", Name: "沪深300ETF", Category: domain.CategoryETF, QuoteSecID: "1.510300", NAVCode: "510300", Enabled: true}}},
		QuoteAdapter: noopQuoteAdapter{}, NAVAdapter: noopNAVAdapter{}, Store: noopStore{},
	})
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}
	if err := svc.loadSeeds(context.Background()); err != nil {
		t.Fatalf("loadSeeds returned error: %v", err)
	}
	svc.navs = map[string]source.NAVResult{"510300": {Code: "510300", NAV: &officialNAV, NAVDate: "2026-06-01", EstimatedNAV: &estimatedNAV, EstimatedNAVTime: "2026-06-02 10:30"}}
	svc.metadataItems = map[string]source.MetadataResult{"510300": {Code: "510300", PurchaseStatus: "开放申购", RedemptionStatus: "开放赎回"}}
	items := svc.buildSnapshots(svc.enabledSeeds(), map[string]source.QuoteResult{"1.510300": {Code: "510300", Price: &price, Tradable: true, Time: "2026-06-02T16:00:00+08:00"}}, time.Date(2026, 6, 2, 16, 0, 0, 0, time.FixedZone("CST", 8*60*60)), nil)
	if len(items) != 1 || items[0].NAV == nil || *items[0].NAV != estimatedNAV || items[0].NAVDate != "2026-06-02 10:30" || items[0].NAVBasis != EstimateNAVBasis {
		t.Fatalf("expected same-day estimate to outrank previous NAV outside trading hours, got %+v", items)
	}
}

func TestBuildSnapshotsPreservesOfficialNAVWhenEstimateMissing(t *testing.T) {
	price := 5.0
	officialNAV := 4.0
	svc, err := NewService(Options{
		PoolProvider: staticPool{seeds: []domain.FundSeed{{Code: "510300", Name: "沪深300ETF", Category: domain.CategoryETF, QuoteSecID: "1.510300", NAVCode: "510300", Enabled: true}}},
		QuoteAdapter: noopQuoteAdapter{}, NAVAdapter: noopNAVAdapter{}, Store: noopStore{},
	})
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}
	if err := svc.loadSeeds(context.Background()); err != nil {
		t.Fatalf("loadSeeds returned error: %v", err)
	}
	svc.navs = map[string]source.NAVResult{"510300": {Code: "510300", NAV: &officialNAV, NAVDate: "2026-06-01"}}
	svc.metadataItems = map[string]source.MetadataResult{"510300": {Code: "510300", PurchaseStatus: "开放申购", RedemptionStatus: "开放赎回"}}
	items := svc.buildSnapshots(svc.enabledSeeds(), map[string]source.QuoteResult{"1.510300": {Code: "510300", Price: &price, Tradable: true, Time: "2026-06-02T10:31:00+08:00"}}, time.Date(2026, 6, 2, 10, 31, 0, 0, time.FixedZone("CST", 8*60*60)), nil)
	if len(items) != 1 || items[0].NAV == nil || *items[0].NAV != officialNAV || items[0].NAVBasis != DefaultNAVBasis {
		 t.Fatalf("expected official NAV when estimate is missing, got %+v", items)
	}
}

func TestBuildSnapshotsKeepsPreOpenNonTradableQuoteWithPreviousNAV(t *testing.T) {
	officialNAV := 4.0
	svc, err := NewService(Options{
		PoolProvider: staticPool{seeds: []domain.FundSeed{{Code: "510300", Name: "沪深300ETF", Category: domain.CategoryETF, QuoteSecID: "1.510300", NAVCode: "510300", Enabled: true}}},
		QuoteAdapter: noopQuoteAdapter{}, NAVAdapter: noopNAVAdapter{}, Store: noopStore{},
	})
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}
	if err := svc.loadSeeds(context.Background()); err != nil {
		t.Fatalf("loadSeeds returned error: %v", err)
	}
	svc.navs = map[string]source.NAVResult{"510300": {Code: "510300", NAV: &officialNAV, NAVDate: "2026-06-02"}}
	svc.metadataItems = map[string]source.MetadataResult{"510300": {Code: "510300", PurchaseStatus: "开放申购", RedemptionStatus: "开放赎回"}}
	items := svc.buildSnapshots(svc.enabledSeeds(), map[string]source.QuoteResult{"1.510300": {Code: "510300", Name: "沪深300ETF华泰柏瑞", Tradable: false, Time: "2026-06-03T09:01:00+08:00"}}, time.Date(2026, 6, 3, 9, 20, 0, 0, time.FixedZone("CST", 8*60*60)), nil)
	if len(items) != 1 {
		t.Fatalf("expected pre-open non-tradable quote to keep previous NAV snapshot, got %+v", items)
	}
	if items[0].NAV == nil || *items[0].NAV != officialNAV || items[0].NAVBasis != DefaultNAVBasis {
		t.Fatalf("expected previous official NAV fallback, got %+v", items[0])
	}
	if items[0].QuotePrice != nil || items[0].PremiumRate != nil || items[0].Source != domain.SourceStatePartial || items[0].Stale != domain.StaleStateClosed {
		t.Fatalf("expected partial closed snapshot without quote-derived values, got %+v", items[0])
	}
}

func TestTradingHoursRefreshesNAVWhenOnlyPreviousOfficialNAVWasFetchedToday(t *testing.T) {
	officialNAV := 4.0
	clock := &manualClock{at: time.Date(2026, 6, 3, 10, 0, 0, 0, time.FixedZone("CST", 8*60*60))}
	svc, err := NewService(Options{
		PoolProvider: staticPool{seeds: []domain.FundSeed{{Code: "510300", Name: "沪深300ETF", Category: domain.CategoryETF, QuoteSecID: "1.510300", NAVCode: "510300", Enabled: true}}},
		QuoteAdapter: noopQuoteAdapter{}, NAVAdapter: noopNAVAdapter{}, Store: noopStore{}, Clock: clock,
	})
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}
	svc.navFetchedAt = time.Date(2026, 6, 3, 8, 55, 0, 0, time.FixedZone("CST", 8*60*60))
	svc.navs = map[string]source.NAVResult{"510300": {Code: "510300", NAV: &officialNAV, NAVDate: "2026-06-02"}}
	if !svc.shouldRefreshNAVLocked(clock.Now()) {
		t.Fatalf("expected trading-hours NAV refresh when only previous official NAV was fetched today")
	}
}

func TestOTCTradabilityUsesExplicitStatusAllowlists(t *testing.T) {
	tests := []struct {
		name       string
		meta       source.MetadataResult
		tradable   bool
		purchaseOK bool
		redemptOK  bool
	}{
		{name: "open", meta: source.MetadataResult{PurchaseStatus: "开放申购", RedemptionStatus: "开放赎回"}, tradable: true, purchaseOK: true, redemptOK: true},
		{name: "paused", meta: source.MetadataResult{PurchaseStatus: "暂停申购", RedemptionStatus: "暂停赎回"}, tradable: true, purchaseOK: true, redemptOK: true},
		{name: "large limit", meta: source.MetadataResult{PurchaseStatus: "限大额", RedemptionStatus: "开放赎回"}, tradable: true, purchaseOK: true, redemptOK: true},
		{name: "closed period", meta: source.MetadataResult{PurchaseStatus: "封闭期", RedemptionStatus: "封闭期"}, tradable: false},
		{name: "new subscription period", meta: source.MetadataResult{PurchaseStatus: "认购期", RedemptionStatus: "认购期"}, tradable: false},
		{name: "exchange only", meta: source.MetadataResult{PurchaseStatus: "场内交易", RedemptionStatus: "场内交易"}, tradable: false},
		{name: "exchange history verbs", meta: source.MetadataResult{PurchaseStatus: "场内买入", RedemptionStatus: "场内卖出"}, tradable: false},
		{name: "stopped", meta: source.MetadataResult{PurchaseStatus: "停止申购", RedemptionStatus: "停止赎回"}, tradable: false},
		{name: "restricted glossary wording", meta: source.MetadataResult{PurchaseStatus: "限制申购", RedemptionStatus: "开放赎回"}, tradable: false, redemptOK: true},
		{name: "large stop glossary wording", meta: source.MetadataResult{PurchaseStatus: "停止大额申购", RedemptionStatus: "停止大额赎回"}, tradable: false},
		{name: "unknown purchase containing word", meta: source.MetadataResult{PurchaseStatus: "预约申购", RedemptionStatus: "开放赎回"}, tradable: false, redemptOK: true},
		{name: "unknown redemption containing word", meta: source.MetadataResult{PurchaseStatus: "开放申购", RedemptionStatus: "预约赎回"}, tradable: false, purchaseOK: true},
		{name: "empty", meta: source.MetadataResult{}, tradable: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isOTCTradable(tt.meta); got != tt.tradable {
				t.Fatalf("isOTCTradable(%+v) = %v, want %v", tt.meta, got, tt.tradable)
			}
			if got := isPurchaseTradable(tt.meta.PurchaseStatus); got != tt.purchaseOK {
				t.Fatalf("isPurchaseTradable(%q) = %v, want %v", tt.meta.PurchaseStatus, got, tt.purchaseOK)
			}
			if got := isRedemptionTradable(tt.meta.RedemptionStatus); got != tt.redemptOK {
				t.Fatalf("isRedemptionTradable(%q) = %v, want %v", tt.meta.RedemptionStatus, got, tt.redemptOK)
			}
		})
	}
}

func TestBuildSnapshotsPrefersNAVReturnedCategory(t *testing.T) {
	price := 4.0
	nav := 4.0
	svc, err := NewService(Options{
		PoolProvider: staticPool{seeds: []domain.FundSeed{
			{Code: "159792", Name: "无关键词一号", Category: domain.CategoryETF, QuoteSecID: "0.159792", NAVCode: "159792", Enabled: true},
			{Code: "510300", Name: "沪深300ETF", Category: domain.CategoryETF, QuoteSecID: "1.510300", NAVCode: "510300", Enabled: true},
		}},
		QuoteAdapter: noopQuoteAdapter{},
		NAVAdapter:   noopNAVAdapter{},
		Store:        noopStore{},
	})
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}
	if err := svc.loadSeeds(context.Background()); err != nil {
		t.Fatalf("loadSeeds returned error: %v", err)
	}
	svc.navs = map[string]source.NAVResult{
		"159792": {Code: "159792", NAV: &nav, NAVDate: "2026-06-01", Category: domain.CategoryQDII},
		"510300": {Code: "510300", NAV: &nav, NAVDate: "2026-06-01", Category: domain.CategoryIndexLOF},
	}
	svc.metadataItems = map[string]source.MetadataResult{
		"159792": {Code: "159792", PurchaseStatus: "开放申购", RedemptionStatus: "开放赎回"},
		"510300": {Code: "510300", PurchaseStatus: "开放申购", RedemptionStatus: "开放赎回"},
	}
	items := svc.buildSnapshots(svc.enabledSeeds(), map[string]source.QuoteResult{
		"0.159792": {Code: "159792", Price: &price, Tradable: true, Time: "2026-06-01T10:00:00+08:00"},
		"1.510300": {Code: "510300", Price: &price, Tradable: true, Time: "2026-06-01T10:00:00+08:00"},
	}, time.Date(2026, 6, 1, 10, 0, 0, 0, time.Local), nil)
	if len(items) != 2 || items[0].Category != domain.CategoryETF || items[1].Category != domain.CategoryETF {
		t.Fatalf("expected ETF seed category to have priority over NAV-returned categories, got %+v", items)
	}
}

func TestBuildSnapshotsRequiresValidExchangeQuoteAndOTCChannel(t *testing.T) {
	price := 4.0
	nav := 4.0
	svc, err := NewService(Options{
		PoolProvider: staticPool{seeds: []domain.FundSeed{
			{Code: "501019", Name: "有效LOF", Category: domain.CategoryIndexLOF, QuoteSecID: "1.501019", NAVCode: "501019", Enabled: true},
			{Code: "501020", Name: "无价格LOF", Category: domain.CategoryIndexLOF, QuoteSecID: "1.501020", NAVCode: "501020", Enabled: true},
			{Code: "501021", Name: "未交易LOF", Category: domain.CategoryIndexLOF, QuoteSecID: "1.501021", NAVCode: "501021", Enabled: true},
		}},
		QuoteAdapter: noopQuoteAdapter{},
		NAVAdapter:   noopNAVAdapter{},
		Store:        noopStore{},
	})
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}
	if err := svc.loadSeeds(context.Background()); err != nil {
		t.Fatalf("loadSeeds returned error: %v", err)
	}
	svc.navs = map[string]source.NAVResult{
		"501019": {Code: "501019", NAV: &nav, NAVDate: "2026-06-01"},
		"501020": {Code: "501020", NAV: &nav, NAVDate: "2026-06-01"},
		"501021": {Code: "501021", NAV: &nav, NAVDate: "2026-06-01"},
	}
	svc.metadataItems = map[string]source.MetadataResult{
		"501019": {Code: "501019", PurchaseStatus: "开放申购", RedemptionStatus: "开放赎回"},
		"501020": {Code: "501020", PurchaseStatus: "开放申购", RedemptionStatus: "开放赎回"},
		"501021": {Code: "501021", PurchaseStatus: "开放申购", RedemptionStatus: "开放赎回"},
	}
	items := svc.buildSnapshots(svc.enabledSeeds(), map[string]source.QuoteResult{
		"1.501019": {Code: "501019", Price: &price, Tradable: true, Time: "2026-06-01T10:00:00+08:00"},
		"1.501020": {Code: "501020", Price: nil, Tradable: true, Time: "2026-06-01T10:00:00+08:00"},
		"1.501021": {Code: "501021", Price: &price, Tradable: false, Time: "2026-06-01T10:00:00+08:00"},
	}, time.Date(2026, 6, 1, 10, 0, 0, 0, time.Local), nil)
	if len(items) != 1 || items[0].Code != "501019" {
		t.Fatalf("expected only valid exchange quote plus OTC-channel fund to remain, got %+v", items)
	}
}

func TestBuildSnapshotsAllowsETFExchangeOnlyStatus(t *testing.T) {
	price := 4.0
	nav := 4.0
	svc, err := NewService(Options{
		PoolProvider: staticPool{seeds: []domain.FundSeed{
			{Code: "510300", Name: "沪深300ETF", Category: domain.CategoryETF, QuoteSecID: "1.510300", NAVCode: "510300", Enabled: true},
			{Code: "159920", Name: "恒生ETF", Category: domain.CategoryETF, QuoteSecID: "0.159920", NAVCode: "159920", Enabled: true},
			{Code: "159934", Name: "黄金ETF", Category: domain.CategoryETF, QuoteSecID: "0.159934", NAVCode: "159934", Enabled: true},
			{Code: "501019", Name: "军工LOF", Category: domain.CategoryIndexLOF, QuoteSecID: "1.501019", NAVCode: "501019", Enabled: true},
		}},
		QuoteAdapter: noopQuoteAdapter{},
		NAVAdapter:   noopNAVAdapter{},
		Store:        noopStore{},
	})
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}
	if err := svc.loadSeeds(context.Background()); err != nil {
		t.Fatalf("loadSeeds returned error: %v", err)
	}
	svc.navs = map[string]source.NAVResult{
		"510300": {Code: "510300", NAV: &nav, NAVDate: "2026-06-01", Category: domain.CategoryIndexLOF},
		"159920": {Code: "159920", NAV: &nav, NAVDate: "2026-06-01", Category: domain.CategoryQDII},
		"159934": {Code: "159934", NAV: &nav, NAVDate: "2026-06-01", Category: domain.CategoryCommodity},
		"501019": {Code: "501019", NAV: &nav, NAVDate: "2026-06-01"},
	}
	svc.metadataItems = map[string]source.MetadataResult{
		"510300": {Code: "510300", PurchaseStatus: "场内交易", RedemptionStatus: "场内交易"},
		"159920": {Code: "159920", PurchaseStatus: "场内交易", RedemptionStatus: "场内交易"},
		"159934": {Code: "159934", PurchaseStatus: "场内交易", RedemptionStatus: "场内交易"},
		"501019": {Code: "501019", PurchaseStatus: "场内交易", RedemptionStatus: "场内交易"},
	}
	items := svc.buildSnapshots(svc.enabledSeeds(), map[string]source.QuoteResult{
		"1.510300": {Code: "510300", Price: &price, Tradable: true, Time: "2026-06-01T10:00:00+08:00"},
		"0.159920": {Code: "159920", Price: &price, Tradable: true, Time: "2026-06-01T10:00:00+08:00"},
		"0.159934": {Code: "159934", Price: &price, Tradable: true, Time: "2026-06-01T10:00:00+08:00"},
		"1.501019": {Code: "501019", Price: &price, Tradable: true, Time: "2026-06-01T10:00:00+08:00"},
	}, time.Date(2026, 6, 1, 10, 0, 0, 0, time.Local), nil)
	if len(items) != 2 || items[0].Code != "159920" || items[1].Code != "159934" || items[0].Category != domain.CategoryETF || items[1].Category != domain.CategoryETF {
		t.Fatalf("expected only QDII/HK/commodity ETF exchange-only status to be included while domestic ETF and non-ETF are excluded, got %+v", items)
	}
}

func TestBuildSnapshotsUsesQuoteMarketValueAsScaleFallback(t *testing.T) {
	price := 4.0
	nav := 4.0
	marketValue := 136091208738.0
	svc, err := NewService(Options{
		PoolProvider: staticPool{seeds: []domain.FundSeed{{Code: "510300", Name: "沪深300ETF", Category: domain.CategoryETF, QuoteSecID: "1.510300", NAVCode: "510300", Enabled: true}}},
		QuoteAdapter: noopQuoteAdapter{},
		NAVAdapter:   noopNAVAdapter{},
		Store:        noopStore{},
	})
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}
	if err := svc.loadSeeds(context.Background()); err != nil {
		t.Fatalf("loadSeeds returned error: %v", err)
	}
	svc.navs = map[string]source.NAVResult{"510300": {Code: "510300", NAV: &nav, NAVDate: "2026-06-01"}}
	svc.metadataItems = map[string]source.MetadataResult{"510300": {Code: "510300", PurchaseStatus: "开放申购", RedemptionStatus: "开放赎回"}}
	items := svc.buildSnapshots(svc.enabledSeeds(), map[string]source.QuoteResult{"1.510300": {Code: "510300", Price: &price, MarketValue: &marketValue, Tradable: true, Time: "2026-06-01T10:00:00+08:00"}}, time.Date(2026, 6, 1, 10, 0, 0, 0, time.Local), nil)
	if len(items) != 1 || items[0].FundScale != "1360.91亿元" {
		t.Fatalf("expected quote market value to populate scale, got %+v", items)
	}
}

func BenchmarkRefreshLargeUniverse(b *testing.B) {
	const totalFunds = 600
	price := 4.2
	nav := 4.0
	seeds := make([]domain.FundSeed, totalFunds)
	quotes := make(map[string]source.QuoteResult, totalFunds)
	navs := make(map[string]source.NAVResult, totalFunds)
	metadata := make(map[string]source.MetadataResult, totalFunds)
	for index := range seeds {
		code := fmt.Sprintf("%06d", 510000+index)
		secID := "1." + code
		seeds[index] = domain.FundSeed{Code: code, Name: "基金" + code, Category: domain.CategoryETF, Exchange: "SH", QuoteSecID: secID, NAVCode: code, Enabled: true}
		quotes[secID] = source.QuoteResult{Code: code, Price: &price, Time: "2026-06-02T13:28:00+08:00", Tradable: true}
		navs[code] = source.NAVResult{Code: code, NAV: &nav, NAVDate: "2026-06-01"}
		metadata[code] = source.MetadataResult{Code: code, PurchaseStatus: "开放申购", RedemptionStatus: "开放赎回", FundScale: "500亿元"}
	}
	svc, err := NewService(Options{
		PoolProvider: staticPool{seeds: seeds},
		QuoteAdapter: quoteAdapterFunc(func(context.Context, []string) (map[string]source.QuoteResult, error) {
			return quotes, nil
		}),
		NAVAdapter: navAdapterFunc(func(context.Context, []string) (map[string]source.NAVResult, error) {
			return navs, nil
		}),
		Metadata: metadataAdapterFunc(func(context.Context, []string) (map[string]source.MetadataResult, error) {
			return metadata, nil
		}),
		Store: noopStore{},
		Clock: fixedClock{at: time.Date(2026, 6, 2, 13, 28, 0, 0, time.Local)},
	})
	if err != nil {
		b.Fatalf("NewService returned error: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := svc.Refresh(context.Background()); err != nil {
			b.Fatalf("Refresh returned error: %v", err)
		}
		if rows := len(svc.CurrentSnapshot().Items); rows != totalFunds {
			b.Fatalf("Refresh produced %d rows, want %d", rows, totalFunds)
		}
	}
}

type staticPool struct{ seeds []domain.FundSeed }

func (p staticPool) FundPool(context.Context) ([]domain.FundSeed, error) { return p.seeds, nil }

type countingPool struct {
	seeds []domain.FundSeed
	calls int
}

func (p *countingPool) FundPool(context.Context) ([]domain.FundSeed, error) {
	p.calls++
	return p.seeds, nil
}

type quoteAdapterFunc func(context.Context, []string) (map[string]source.QuoteResult, error)

func (f quoteAdapterFunc) FetchQuotes(ctx context.Context, secIDs []string) (map[string]source.QuoteResult, error) {
	return f(ctx, secIDs)
}

type navAdapterFunc func(context.Context, []string) (map[string]source.NAVResult, error)

func (f navAdapterFunc) FetchNAVs(ctx context.Context, fundCodes []string) (map[string]source.NAVResult, error) {
	return f(ctx, fundCodes)
}

type metadataAdapterFunc func(context.Context, []string) (map[string]source.MetadataResult, error)

func (f metadataAdapterFunc) FetchMetadata(ctx context.Context, fundCodes []string) (map[string]source.MetadataResult, error) {
	return f(ctx, fundCodes)
}

type metadataProgressAdapterFunc func(context.Context, []string, func(int, int)) (map[string]source.MetadataResult, error)

func (f metadataProgressAdapterFunc) FetchMetadata(ctx context.Context, fundCodes []string) (map[string]source.MetadataResult, error) {
	return f(ctx, fundCodes, func(int, int) {})
}

func (f metadataProgressAdapterFunc) FetchMetadataWithProgress(ctx context.Context, fundCodes []string, onProgress func(int, int)) (map[string]source.MetadataResult, error) {
	return f(ctx, fundCodes, onProgress)
}

type noopQuoteAdapter struct{}

func (noopQuoteAdapter) FetchQuotes(context.Context, []string) (map[string]source.QuoteResult, error) {
	return nil, nil
}

type noopNAVAdapter struct{}

func (noopNAVAdapter) FetchNAVs(context.Context, []string) (map[string]source.NAVResult, error) {
	return nil, nil
}

type navFillerAdapter struct {
	results   map[string]source.NAVResult
	change    float64
	fillCalls int32
}

func (a *navFillerAdapter) FetchNAVs(context.Context, []string) (map[string]source.NAVResult, error) {
	return a.results, nil
}

func (a *navFillerAdapter) FillMissingChangePercent(_ context.Context, navs map[string]source.NAVResult, fundCodes []string) (map[string]source.NAVResult, error) {
	atomic.AddInt32(&a.fillCalls, 1)
	results := make(map[string]source.NAVResult, len(navs))
	for code, result := range navs {
		if code == "160106" {
			result.ChangePercent = &a.change
		}
		results[code] = result
	}
	return results, nil
}

type blockingNavFillerAdapter struct {
	results map[string]source.NAVResult
	change  float64
	started chan struct{}
	release chan struct{}
	called  bool
}

func (a *blockingNavFillerAdapter) FetchNAVs(context.Context, []string) (map[string]source.NAVResult, error) {
	return a.results, nil
}

func (a *blockingNavFillerAdapter) FillMissingChangePercent(ctx context.Context, navs map[string]source.NAVResult, fundCodes []string) (map[string]source.NAVResult, error) {
	if !a.called {
		a.called = true
		close(a.started)
	}
	select {
	case <-ctx.Done():
		return navs, ctx.Err()
	case <-a.release:
	}
	results := make(map[string]source.NAVResult, len(navs))
	for code, result := range navs {
		for _, target := range fundCodes {
			if code == target {
				result.ChangePercent = &a.change
			}
		}
		results[code] = result
	}
	return results, nil
}

func nextSnapshot(t *testing.T, snapshots <-chan domain.SnapshotResponse) domain.SnapshotResponse {
	t.Helper()
	select {
	case snapshot := <-snapshots:
		return snapshot
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for snapshot")
	}
	return domain.SnapshotResponse{}
}

func nextSnapshotWithItems(t *testing.T, snapshots <-chan domain.SnapshotResponse) domain.SnapshotResponse {
	t.Helper()
	return nextSnapshotMatching(t, snapshots, func(snapshot domain.SnapshotResponse) bool { return len(snapshot.Items) > 0 })
}

func nextSnapshotMatching(t *testing.T, snapshots <-chan domain.SnapshotResponse, match func(domain.SnapshotResponse) bool) domain.SnapshotResponse {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		select {
		case snapshot := <-snapshots:
			if match(snapshot) {
				return snapshot
			}
		case <-deadline:
			t.Fatal("timed out waiting for matching snapshot")
		}
	}
}

type fixedClock struct{ at time.Time }

func (c fixedClock) Now() time.Time { return c.at }

type manualClock struct{ at time.Time }

func (c *manualClock) Now() time.Time   { return c.at }
func (c *manualClock) Set(at time.Time) { c.at = at }

type noopStore struct{}

func (noopStore) SaveFundSnapshot(context.Context, string, []byte, time.Time) error             { return nil }
func (noopStore) SaveFundSnapshots(context.Context, []store.FundBatchItem, time.Time) error      { return nil }
func (noopStore) ListFundSnapshots(context.Context) ([]store.FundCacheItem, error)               { return nil, nil }

type memorySnapshotStore struct{ cached []store.FundCacheItem }

func (memorySnapshotStore) SaveFundSnapshot(context.Context, string, []byte, time.Time) error {
	return nil
}
func (memorySnapshotStore) SaveFundSnapshots(context.Context, []store.FundBatchItem, time.Time) error {
	return nil
}
func (s memorySnapshotStore) ListFundSnapshots(context.Context) ([]store.FundCacheItem, error) {
	return s.cached, nil
}

type channelTicker struct{ ticks <-chan Tick }

func (t channelTicker) C() <-chan Tick { return t.ticks }
func (channelTicker) Stop()            {}
