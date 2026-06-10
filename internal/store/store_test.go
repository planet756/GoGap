package store

import (
	"context"
	"testing"
)

func TestWatchlistIsScopedByVisitor(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, "file:watchlist-scope?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := store.AddWatchlist(ctx, "visitor-a", "510300"); err != nil {
		t.Fatalf("AddWatchlist returned error: %v", err)
	}
	if err := store.AddWatchlist(ctx, "visitor-b", "159915"); err != nil {
		t.Fatalf("AddWatchlist returned error: %v", err)
	}

	aItems, err := store.ListWatchlist(ctx, "visitor-a")
	if err != nil {
		t.Fatalf("ListWatchlist visitor-a returned error: %v", err)
	}
	if len(aItems) != 1 || aItems[0].Code != "510300" {
		t.Fatalf("expected visitor-a to see only 510300, got %+v", aItems)
	}

	bContains, err := store.WatchlistContains(ctx, "visitor-b", "510300")
	if err != nil {
		t.Fatalf("WatchlistContains visitor-b returned error: %v", err)
	}
	if bContains {
		t.Fatal("expected visitor-b not to see visitor-a watchlist item")
	}

	if err := store.RemoveWatchlist(ctx, "visitor-a", "510300"); err != nil {
		t.Fatalf("RemoveWatchlist returned error: %v", err)
	}
	bItems, err := store.ListWatchlist(ctx, "visitor-b")
	if err != nil {
		t.Fatalf("ListWatchlist visitor-b returned error: %v", err)
	}
	if len(bItems) != 1 || bItems[0].Code != "159915" {
		t.Fatalf("expected removing visitor-a item not to affect visitor-b, got %+v", bItems)
	}
}
