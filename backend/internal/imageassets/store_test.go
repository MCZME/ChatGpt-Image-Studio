package imageassets

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestNewStoreResetsLegacySchema(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, filepath.FromSlash(defaultAssetSQLitePath))
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		t.Fatalf("mkdir db dir: %v", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}
	if _, err := db.Exec(`
		CREATE TABLE image_assets (
			id TEXT PRIMARY KEY,
			raw_json BLOB NOT NULL
		);
	`); err != nil {
		_ = db.Close()
		t.Fatalf("create legacy schema: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}

	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()

	if _, err := store.Save(context.Background(), Asset{
		ID:        "asset-1",
		Title:     "legacy upgraded",
		Prompt:    "prompt",
		CreatedAt: "2026-05-18T00:00:00Z",
	}); err != nil {
		t.Fatalf("save after upgrade: %v", err)
	}
}

func TestListFilteredAllowsQuotedQuery(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()

	if _, err := store.Save(context.Background(), Asset{
		ID:        "asset-quoted",
		Title:     "y'zhou portrait",
		Prompt:    "portrait of y'zhou at sunset",
		CreatedAt: "2026-05-18T00:00:00Z",
	}); err != nil {
		t.Fatalf("save asset: %v", err)
	}

	items, err := store.ListFiltered(context.Background(), FilterOptions{
		Query: "y'zhou",
	})
	if err != nil {
		t.Fatalf("list filtered: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items len = %d, want 1", len(items))
	}
	if items[0].ID != "asset-quoted" {
		t.Fatalf("item id = %q, want %q", items[0].ID, "asset-quoted")
	}
}
