package imageassets

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
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

func TestNewStoreMigratesAssetSchemaWithoutDroppingMetadata(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, filepath.FromSlash(defaultAssetSQLitePath))
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		t.Fatalf("mkdir db dir: %v", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if _, err := db.Exec(`
		CREATE TABLE image_assets (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL,
			prompt TEXT NOT NULL,
			revised_prompt TEXT NOT NULL DEFAULT '',
			mode TEXT NOT NULL,
			model TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			conversation_id TEXT NOT NULL DEFAULT '',
			turn_id TEXT NOT NULL DEFAULT '',
			image_id TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT '',
			image_url TEXT NOT NULL DEFAULT '',
			image_b64_json TEXT NOT NULL DEFAULT '',
			file_id TEXT NOT NULL DEFAULT '',
			gen_id TEXT NOT NULL DEFAULT '',
			source_account_id TEXT NOT NULL DEFAULT '',
			category TEXT NOT NULL DEFAULT '',
			note TEXT NOT NULL DEFAULT '',
			favorite INTEGER NOT NULL DEFAULT 0
		);
		CREATE TABLE image_asset_tags (
			asset_id TEXT NOT NULL,
			tag TEXT NOT NULL,
			PRIMARY KEY(asset_id, tag)
		);
		CREATE VIRTUAL TABLE image_assets_fts USING fts5(
			asset_id UNINDEXED,
			title,
			prompt,
			revised_prompt,
			note,
			tokenize = 'unicode61'
		);
		INSERT INTO image_assets (
			id, title, prompt, revised_prompt, mode, model, created_at, updated_at,
			conversation_id, turn_id, image_id, status, image_url, image_b64_json,
			file_id, gen_id, source_account_id, category, note, favorite
		) VALUES (
			'asset-1', 'Title', 'Prompt', '', 'generate', 'gpt-image-2',
			'2026-05-18T00:00:00Z', '2026-05-18T00:00:00Z',
			'conv', 'turn', 'img', 'success', '/v1/files/image/result-a.png', '',
			'file', 'gen', 'acct', '海报', 'keep note', 1
		);
		INSERT INTO image_asset_tags(asset_id, tag) VALUES('asset-1', '精选');
	`); err != nil {
		_ = db.Close()
		t.Fatalf("seed legacy schema: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()

	item, err := store.Get(context.Background(), "asset-1")
	if err != nil {
		t.Fatalf("get asset: %v", err)
	}
	if item == nil {
		t.Fatal("asset = nil, want migrated asset")
	}
	if item.Category != "海报" || item.Note != "keep note" || !item.Favorite {
		t.Fatalf("metadata was not preserved: %#v", item)
	}
	if item.Filename != "result-a.png" || item.StorageKind != "local" {
		t.Fatalf("file metadata = filename %q storage %q, want inferred local file", item.Filename, item.StorageKind)
	}
	if len(item.Tags) != 1 || item.Tags[0] != "精选" {
		t.Fatalf("tags = %#v, want preserved tag", item.Tags)
	}
}

func TestSaveImageBytesUsesStableContentAddressedFiles(t *testing.T) {
	root := t.TempDir()
	first, err := SaveImageBytes(filepath.Join(root, "data", "images"), []byte("image-payload"), FileSaveOptions{
		SourceKind:  "result",
		OriginalURL: "https://example.test/image.png",
	})
	if err != nil {
		t.Fatalf("SaveImageBytes first: %v", err)
	}
	second, err := SaveImageBytes(filepath.Join(root, "data", "images"), []byte("image-payload"), FileSaveOptions{
		SourceKind: "result",
	})
	if err != nil {
		t.Fatalf("SaveImageBytes second: %v", err)
	}
	if first.Filename != second.Filename {
		t.Fatalf("filenames = %q and %q, want same content-addressed name", first.Filename, second.Filename)
	}
	if !strings.HasPrefix(first.URL, ImageFileURLPrefix+"result-") {
		t.Fatalf("url = %q, want result asset URL", first.URL)
	}
	if first.SHA256 == "" || first.SizeBytes != int64(len("image-payload")) {
		t.Fatalf("file info = %#v, want hash and size", first)
	}
	if _, err := os.Stat(first.Path); err != nil {
		t.Fatalf("saved file missing: %v", err)
	}
}

func TestMoveImageFilesKeepsConflictingTargetFile(t *testing.T) {
	root := t.TempDir()
	sourceDir := filepath.Join(root, "data", "tmp", "image")
	targetDir := filepath.Join(root, "data", "images")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("mkdir source dir: %v", err)
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("mkdir target dir: %v", err)
	}
	sourcePath := filepath.Join(sourceDir, "same.png")
	targetPath := filepath.Join(targetDir, "same.png")
	if err := os.WriteFile(sourcePath, []byte("source"), 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}
	if err := os.WriteFile(targetPath, []byte("target"), 0o644); err != nil {
		t.Fatalf("write target file: %v", err)
	}

	if err := MoveImageFiles(sourceDir, targetDir); err != nil {
		t.Fatalf("MoveImageFiles returned error: %v", err)
	}

	content, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("read target file: %v", err)
	}
	if string(content) != "target" {
		t.Fatalf("target content = %q, want existing target preserved", string(content))
	}
	if _, err := os.Stat(sourcePath); !os.IsNotExist(err) {
		t.Fatalf("source file still exists after conflict move, err=%v", err)
	}

	matches, err := filepath.Glob(filepath.Join(targetDir, "same-*.png"))
	if err != nil {
		t.Fatalf("glob moved conflict: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("moved conflicts = %#v, want exactly one hashed file", matches)
	}
	movedContent, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatalf("read moved conflict: %v", err)
	}
	if string(movedContent) != "source" {
		t.Fatalf("moved conflict content = %q, want source content", string(movedContent))
	}
}

func TestMoveImageFilesRemovesIdenticalDuplicate(t *testing.T) {
	root := t.TempDir()
	sourceDir := filepath.Join(root, "data", "tmp", "image")
	targetDir := filepath.Join(root, "data", "images")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("mkdir source dir: %v", err)
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("mkdir target dir: %v", err)
	}
	sourcePath := filepath.Join(sourceDir, "same.png")
	targetPath := filepath.Join(targetDir, "same.png")
	if err := os.WriteFile(sourcePath, []byte("same"), 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}
	if err := os.WriteFile(targetPath, []byte("same"), 0o644); err != nil {
		t.Fatalf("write target file: %v", err)
	}

	if err := MoveImageFiles(sourceDir, targetDir); err != nil {
		t.Fatalf("MoveImageFiles returned error: %v", err)
	}

	if _, err := os.Stat(targetPath); err != nil {
		t.Fatalf("target duplicate missing: %v", err)
	}
	if _, err := os.Stat(sourcePath); !os.IsNotExist(err) {
		t.Fatalf("source duplicate still exists, err=%v", err)
	}
}
