package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"gogap/internal/api"
	apiTemplates "gogap/internal/api/templates"
	"gogap/internal/config"
	"gogap/internal/domain"
	"gogap/internal/scheduler"
	"gogap/internal/source"
	"gogap/internal/source/eastmoney"
	"gogap/internal/sse"
	"gogap/internal/store"
	"gogap/web"
)

const appVersion = "1.0.0"

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func run(args []string) error {
	if isVersionRequest(args) {
		fmt.Printf("GoGap %s\n", appVersion)
		return nil
	}
	cfg, err := config.Parse(args)
	if err != nil {
		return err
	}
	logFile, err := configureLogging(cfg.LogPath)
	if err != nil {
		return err
	}
	if logFile != nil {
		defer logFile.Close()
	}
	log.Printf("GoGap config: addr=%s db=%s poll=%s sources=%s", cfg.Addr, cfg.DBPath, cfg.PollInterval, strings.Join(cfg.Sources.Live.Names(), ","))
	if err := ensureDBDir(cfg.DBPath); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(ctx, cfg.DBPath)
	if err != nil {
		return err
	}
	defer st.Close()

	pool, quoteAdapter, navAdapter, metadataAdapter, err := sourceAdapters(cfg.Sources, st)
	if err != nil {
		return err
	}
	hub := sse.NewHub(sse.DefaultBufferSize)
	readyLog := newStartupReadyLogger(cfg.Addr, cfg.PollInterval)
	svc, err := scheduler.NewService(scheduler.Options{
		PoolProvider:  pool,
		QuoteAdapter:  quoteAdapter,
		NAVAdapter:    navAdapter,
		Metadata:      metadataAdapter,
		Store:         st,
		QuoteInterval: cfg.PollInterval,
		OnSnapshot: func(snapshot domain.SnapshotResponse) {
			readyLog(snapshot)
			_ = hub.BroadcastSnapshot(snapshot)
		},
	})
	if err != nil {
		return err
	}

	server := api.NewServerWithSSE(svc, st, pool, hub)
	handler, err := appHandler(server.Handler())
	if err != nil {
		return err
	}

	serverErr := make(chan error, 1)
	go func() {
		if err := svc.Start(ctx); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("scheduler stopped: %v", err)
		}
	}()

	httpServer := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		log.Printf("GoGap listening on http://%s", cfg.Addr)
		serverErr <- httpServer.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		log.Printf("GoGap shutting down")
		hub.Close()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			return err
		}
		return nil
	case err := <-serverErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func newStartupReadyLogger(addr string, pollInterval time.Duration) func(domain.SnapshotResponse) {
	logged := false
	return func(snapshot domain.SnapshotResponse) {
		if logged || snapshot.Progress == nil || snapshot.Progress.Percent < 100 {
			return
		}
		logged = true
		log.Printf("GoGap ready: url=http://%s rows=%d poll=%s", addr, len(snapshot.Items), pollInterval)
	}
}

func isVersionRequest(args []string) bool {
	return len(args) == 1 && (args[0] == "--version" || args[0] == "-version")
}

func configureLogging(path string) (*os.File, error) {
	if path == "" {
		return nil, nil
	}
	if err := ensureDBDir(path); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open log file %s: %w", path, err)
	}
	log.SetOutput(file)
	return file, nil
}

func sourceAdapters(cfg config.SourceConfig, sourceCache sourceCacheStore) (source.FundPoolProvider, source.QuoteAdapter, source.NAVAdapter, source.MetadataAdapter, error) {
	var sharedClient *eastmoney.Client
	for _, name := range cfg.Live.Quote {
		if name == config.SourceEastMoney {
			sharedClient = eastmoney.NewClient(liveEastMoneyOptions(sourceCache))
			break
		}
	}
	if sharedClient == nil {
		return nil, nil, nil, nil, fmt.Errorf("fund discovery source unavailable")
	}
	pool := eastmoney.NewDiscoveryPoolProvider(eastmoney.NewDiscoveryAdapter(sharedClient))
	quoteAdapter, err := source.NewMultiQuoteAdapter(liveQuoteSources(cfg.Live, sharedClient))
	if err != nil {
		return nil, nil, nil, nil, err
	}
	navAdapter, err := source.NewMultiNAVAdapter(liveNAVSources(cfg.Live, sharedClient))
	if err != nil {
		return nil, nil, nil, nil, err
	}
	return pool, quoteAdapter, navAdapter, liveMetadataSource(cfg.Live, sharedClient), nil
}

func liveMetadataSource(cfg config.LiveSourceConfig, client *eastmoney.Client) source.MetadataAdapter {
	for _, name := range cfg.NAV {
		if name == config.SourceEastMoney {
			return eastmoney.NewMetadataAdapter(client)
		}
	}
	return nil
}

func liveQuoteSources(cfg config.LiveSourceConfig, client *eastmoney.Client) []source.NamedQuoteAdapter {
	sources := make([]source.NamedQuoteAdapter, 0, len(cfg.Quote))
	for _, name := range cfg.Quote {
		if name == config.SourceEastMoney {
			sources = append(sources, source.NamedQuoteAdapter{Name: name, Adapter: eastmoney.NewQuoteAdapter(client)})
		}
	}
	return sources
}

func liveNAVSources(cfg config.LiveSourceConfig, client *eastmoney.Client) []source.NamedNAVAdapter {
	sources := make([]source.NamedNAVAdapter, 0, len(cfg.NAV))
	for _, name := range cfg.NAV {
		if name == config.SourceEastMoney {
			sources = append(sources, source.NamedNAVAdapter{Name: name, Adapter: eastmoney.NewNAVAdapter(client)})
		}
	}
	return sources
}

func liveEastMoneyOptions(sourceCache sourceCacheStore) eastmoney.Options {
	return eastmoney.Options{CacheFallback: sourceCacheFallback{store: sourceCache}}
}

type sourceCacheStore interface {
	LoadSourceCache(context.Context, string) (store.SourceCacheItem, bool, error)
}

type sourceCacheFallback struct {
	store sourceCacheStore
}

func (f sourceCacheFallback) Quote(ctx context.Context, secID string) (eastmoney.CachedQuote, bool, error) {
	var result source.QuoteResult
	stale, ok, err := f.load(ctx, "quote:"+secID, &result)
	if err != nil || !ok {
		return eastmoney.CachedQuote{}, ok, err
	}
	return eastmoney.CachedQuote{Result: result, Stale: stale}, true, nil
}

func (f sourceCacheFallback) NAV(ctx context.Context, code string) (eastmoney.CachedNAV, bool, error) {
	var result source.NAVResult
	stale, ok, err := f.load(ctx, "nav:"+code, &result)
	if err != nil || !ok {
		return eastmoney.CachedNAV{}, ok, err
	}
	return eastmoney.CachedNAV{Result: result, Stale: stale}, true, nil
}

func (f sourceCacheFallback) load(ctx context.Context, key string, target any) (bool, bool, error) {
	if f.store == nil {
		return false, false, nil
	}
	item, ok, err := f.store.LoadSourceCache(ctx, key)
	if err != nil || !ok {
		return false, ok, err
	}
	if len(item.Payload) == 0 {
		return false, false, nil
	}
	if err := json.Unmarshal(item.Payload, target); err != nil {
		return false, false, fmt.Errorf("decode source cache %s: %w", key, err)
	}
	return time.Now().UTC().After(item.ExpiresAt), true, nil
}

func ensureDBDir(dbPath string) error {
	if dbPath == "" || strings.HasPrefix(dbPath, "file:") {
		return nil
	}
	dir := filepath.Dir(dbPath)
	if dir == "." || dir == "" {
		return nil
	}
	return os.MkdirAll(dir, 0o755)
}

func appHandler(apiHandler http.Handler) (http.Handler, error) {
	assets, err := staticAssets()
	if err != nil {
		return nil, err
	}
	assetPaths, err := dashboardAssets()
	if err != nil {
		return nil, err
	}
	dashboard, err := template.ParseFS(apiTemplates.Files, "*.html")
	if err != nil {
		return nil, fmt.Errorf("parse dashboard templates: %w", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/api/", apiHandler)
	mux.Handle("/events", apiHandler)
	mux.Handle("/assets/", http.FileServer(http.FS(assets)))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			mux.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := dashboard.ExecuteTemplate(w, "dashboard", assetPaths); err != nil {
			http.Error(w, "render dashboard", http.StatusInternalServerError)
		}
	}), nil
}

type dashboardAssetPaths struct {
	CSSPath string
	JSPath  string
}

func staticAssets() (fs.FS, error) {
	assets, err := fs.Sub(web.Dist, "dist")
	if err != nil {
		return nil, fmt.Errorf("embedded frontend assets unavailable; run pnpm --dir web build before embedded Go commands: %w", err)
	}
	if _, err := fs.Stat(assets, "assets"); err != nil {
		return nil, fmt.Errorf("embedded frontend assets unavailable; run pnpm --dir web build before embedded Go commands: %w", err)
	}
	return assets, nil
}

func dashboardAssets() (dashboardAssetPaths, error) {
	indexHTML, err := web.Dist.ReadFile("dist/index.html")
	if err != nil {
		return dashboardAssetPaths{}, fmt.Errorf("embedded frontend index unavailable; run pnpm --dir web build before embedded Go commands: %w", err)
	}
	paths := dashboardAssetPaths{
		CSSPath: firstAsset(indexHTML, `href="(/assets/[^"]+\.css)"`),
		JSPath:  firstAsset(indexHTML, `src="(/assets/[^"]+\.js)"`),
	}
	if paths.CSSPath == "" || paths.JSPath == "" {
		return dashboardAssetPaths{}, errors.New("embedded frontend assets missing CSS or JS path; run pnpm --dir web build before embedded Go commands")
	}
	return paths, nil
}

func firstAsset(content []byte, pattern string) string {
	matches := regexp.MustCompile(pattern).FindSubmatch(content)
	if len(matches) < 2 {
		return ""
	}
	return string(matches[1])
}
