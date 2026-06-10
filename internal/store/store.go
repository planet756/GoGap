package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"gogap/internal/domain"

	_ "modernc.org/sqlite"
)

const currentSchemaVersion = 1

type Store struct {
	db *sql.DB
}

type FundCacheItem struct {
	Code         string
	SnapshotJSON []byte
	UpdatedAt    time.Time
}

type SourceCacheItem struct {
	Key       string
	Payload   []byte
	FetchedAt time.Time
	ExpiresAt time.Time
	Error     string
}

func Open(ctx context.Context, path string) (*Store, error) {
	db, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	store := &Store{db: db}
	if err := store.Migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}

	return store, nil
}

func dsn(path string) string {
	if strings.HasPrefix(path, "file:") {
		separator := "?"
		if strings.Contains(path, "?") {
			separator = "&"
		}
		return path + separator + sqliteOptions()
	}
	return "file:" + url.PathEscape(path) + "?" + sqliteOptions()
}

func sqliteOptions() string {
	return "_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(ON)&_txlock=immediate"
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) Migrate(ctx context.Context) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS schema_migrations (version integer primary key, applied_at text not null)`,
		`CREATE TABLE IF NOT EXISTS fund_cache (code text primary key, snapshot_json blob not null, updated_at text not null)`,
		`CREATE TABLE IF NOT EXISTS source_cache (key text primary key, payload blob, fetched_at text not null, expires_at text not null, error text not null)`,
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration: %w", err)
	}
	defer tx.Rollback()

	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("run migration: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations (version, applied_at) VALUES (?, ?)`, currentSchemaVersion, formatTime(time.Now().UTC())); err != nil {
		return fmt.Errorf("record migration: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration: %w", err)
	}
	if err := s.migrateWatchlist(ctx); err != nil {
		return err
	}
	return nil
}

func (s *Store) migrateWatchlist(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS watchlist (visitor_id text not null, code text not null, created_at text not null, primary key (visitor_id, code))`)
	if err != nil {
		return fmt.Errorf("create watchlist: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(watchlist)`)
	if err != nil {
		return fmt.Errorf("inspect watchlist: %w", err)
	}
	defer rows.Close()
	hasVisitorID := false
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return fmt.Errorf("scan watchlist schema: %w", err)
		}
		if name == "visitor_id" {
			hasVisitorID = true
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate watchlist schema: %w", err)
	}
	if hasVisitorID {
		return nil
	}
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE watchlist RENAME TO watchlist_legacy`); err != nil {
		return fmt.Errorf("rename legacy watchlist: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE watchlist (visitor_id text not null, code text not null, created_at text not null, primary key (visitor_id, code))`); err != nil {
		return fmt.Errorf("create scoped watchlist: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO watchlist (visitor_id, code, created_at) SELECT 'local', code, created_at FROM watchlist_legacy`); err != nil {
		return fmt.Errorf("copy legacy watchlist: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `DROP TABLE watchlist_legacy`); err != nil {
		return fmt.Errorf("drop legacy watchlist: %w", err)
	}
	return nil
}

func (s *Store) AddWatchlist(ctx context.Context, visitorID string, code string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO watchlist (visitor_id, code, created_at) VALUES (?, ?, ?) ON CONFLICT(visitor_id, code) DO NOTHING`, visitorID, code, formatTime(time.Now().UTC()))
	if err != nil {
		return fmt.Errorf("add watchlist %s: %w", code, err)
	}
	return nil
}

func (s *Store) RemoveWatchlist(ctx context.Context, visitorID string, code string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM watchlist WHERE visitor_id = ? AND code = ?`, visitorID, code)
	if err != nil {
		return fmt.Errorf("remove watchlist %s: %w", code, err)
	}
	return nil
}

func (s *Store) WatchlistContains(ctx context.Context, visitorID string, code string) (bool, error) {
	var exists int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM watchlist WHERE visitor_id = ? AND code = ?`, visitorID, code).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check watchlist %s: %w", code, err)
	}
	return true, nil
}

func (s *Store) ListWatchlistCodes(ctx context.Context, visitorID string) ([]string, error) {
	items, err := s.ListWatchlist(ctx, visitorID)
	if err != nil {
		return nil, err
	}
	codes := make([]string, 0, len(items))
	for _, item := range items {
		codes = append(codes, item.Code)
	}
	return codes, nil
}

func (s *Store) ListWatchlist(ctx context.Context, visitorID string) ([]domain.WatchlistItem, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT code, created_at FROM watchlist WHERE visitor_id = ? ORDER BY created_at ASC, code ASC`, visitorID)
	if err != nil {
		return nil, fmt.Errorf("list watchlist: %w", err)
	}
	defer rows.Close()

	items := []domain.WatchlistItem{}
	for rows.Next() {
		var item domain.WatchlistItem
		if err := rows.Scan(&item.Code, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan watchlist: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate watchlist: %w", err)
	}
	return items, nil
}

func (s *Store) SaveFundSnapshot(ctx context.Context, code string, snapshotJSON []byte, updatedAt time.Time) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO fund_cache (code, snapshot_json, updated_at) VALUES (?, ?, ?) ON CONFLICT(code) DO UPDATE SET snapshot_json = excluded.snapshot_json, updated_at = excluded.updated_at`, code, snapshotJSON, formatTime(updatedAt))
	if err != nil {
		return fmt.Errorf("save fund snapshot %s: %w", code, err)
	}
	return nil
}

// FundBatchItem holds a single fund snapshot for batch saving.
type FundBatchItem struct {
	Code         string
	SnapshotJSON []byte
}

// SaveFundSnapshots writes multiple fund snapshots in a single transaction.
func (s *Store) SaveFundSnapshots(ctx context.Context, items []FundBatchItem, updatedAt time.Time) error {
	if len(items) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin save fund snapshots: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `INSERT INTO fund_cache (code, snapshot_json, updated_at) VALUES (?, ?, ?) ON CONFLICT(code) DO UPDATE SET snapshot_json = excluded.snapshot_json, updated_at = excluded.updated_at`)
	if err != nil {
		return fmt.Errorf("prepare save fund snapshots: %w", err)
	}
	defer stmt.Close()

	ts := formatTime(updatedAt)
	for _, item := range items {
		if _, err := stmt.ExecContext(ctx, item.Code, item.SnapshotJSON, ts); err != nil {
			return fmt.Errorf("save fund snapshot %s: %w", item.Code, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit save fund snapshots: %w", err)
	}
	return nil
}

func (s *Store) LoadFundSnapshot(ctx context.Context, code string) (FundCacheItem, bool, error) {
	var item FundCacheItem
	var updatedAt string
	err := s.db.QueryRowContext(ctx, `SELECT code, snapshot_json, updated_at FROM fund_cache WHERE code = ?`, code).Scan(&item.Code, &item.SnapshotJSON, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return FundCacheItem{}, false, nil
	}
	if err != nil {
		return FundCacheItem{}, false, fmt.Errorf("load fund snapshot %s: %w", code, err)
	}
	parsed, err := parseTime(updatedAt)
	if err != nil {
		return FundCacheItem{}, false, err
	}
	item.UpdatedAt = parsed
	return item, true, nil
}

func (s *Store) ListFundSnapshots(ctx context.Context) ([]FundCacheItem, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT code, snapshot_json, updated_at FROM fund_cache ORDER BY code ASC`)
	if err != nil {
		return nil, fmt.Errorf("list fund snapshots: %w", err)
	}
	defer rows.Close()

	items := []FundCacheItem{}
	for rows.Next() {
		var item FundCacheItem
		var updatedAt string
		if err := rows.Scan(&item.Code, &item.SnapshotJSON, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan fund snapshot: %w", err)
		}
		parsed, err := parseTime(updatedAt)
		if err != nil {
			return nil, err
		}
		item.UpdatedAt = parsed
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate fund snapshots: %w", err)
	}
	return items, nil
}

func (s *Store) SaveSourceCache(ctx context.Context, item SourceCacheItem) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO source_cache (key, payload, fetched_at, expires_at, error) VALUES (?, ?, ?, ?, ?) ON CONFLICT(key) DO UPDATE SET payload = excluded.payload, fetched_at = excluded.fetched_at, expires_at = excluded.expires_at, error = excluded.error`, item.Key, item.Payload, formatTime(item.FetchedAt), formatTime(item.ExpiresAt), item.Error)
	if err != nil {
		return fmt.Errorf("save source cache %s: %w", item.Key, err)
	}
	return nil
}

func (s *Store) LoadSourceCache(ctx context.Context, key string) (SourceCacheItem, bool, error) {
	var item SourceCacheItem
	var fetchedAt, expiresAt string
	err := s.db.QueryRowContext(ctx, `SELECT key, payload, fetched_at, expires_at, error FROM source_cache WHERE key = ?`, key).Scan(&item.Key, &item.Payload, &fetchedAt, &expiresAt, &item.Error)
	if errors.Is(err, sql.ErrNoRows) {
		return SourceCacheItem{}, false, nil
	}
	if err != nil {
		return SourceCacheItem{}, false, fmt.Errorf("load source cache %s: %w", key, err)
	}
	fetched, err := parseTime(fetchedAt)
	if err != nil {
		return SourceCacheItem{}, false, err
	}
	expires, err := parseTime(expiresAt)
	if err != nil {
		return SourceCacheItem{}, false, err
	}
	item.FetchedAt = fetched
	item.ExpiresAt = expires
	return item, true, nil
}



func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func parseTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse stored time %q: %w", value, err)
	}
	return parsed, nil
}
