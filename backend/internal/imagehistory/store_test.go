package imagehistory

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"chatgpt2api/internal/config"
)

func newHistoryTestConfig(t *testing.T, backend string) *config.Config {
	t.Helper()
	root := t.TempDir()
	cfg := config.New(root)
	if err := cfg.Load(); err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.Storage.Backend = backend
	cfg.Storage.ImageDir = "data/images"
	cfg.Storage.SQLitePath = "data/history.sqlite"
	cfg.Storage.RedisAddr = "127.0.0.1:6379"
	cfg.Storage.RedisPassword = "123456"
	cfg.Storage.RedisDB = 0
	cfg.Storage.RedisPrefix = "chatgpt2api:history:test:" + strings.ReplaceAll(root, "\\", ":")
	return cfg
}

func testStorePersistenceAcrossReload(t *testing.T, backend string) {
	t.Helper()
	cfg := newHistoryTestConfig(t, backend)
	store, err := NewStore(cfg)
	if err != nil {
		if backend == "redis" {
			t.Skipf("redis backend is not reachable: %v", err)
		}
		t.Fatalf("NewStore(%s): %v", backend, err)
	}
	defer store.Close()

	payload := base64.StdEncoding.EncodeToString([]byte("persist-image-bytes"))
	if _, err := store.Save(context.Background(), Conversation{
		ID:        "persist-conv",
		Title:     "生成",
		Mode:      "generate",
		Prompt:    "persist",
		Model:     "gpt-image-2",
		Count:     1,
		CreatedAt: "2026-04-26T00:00:00Z",
		Status:    "success",
		Turns: []Turn{{
			ID:        "persist-turn",
			Title:     "生成",
			Mode:      "generate",
			Prompt:    "persist",
			Model:     "gpt-image-2",
			Count:     1,
			CreatedAt: "2026-04-26T00:00:00Z",
			Status:    "success",
			Images:    []Image{{ID: "persist-image", Status: "success", B64JSON: payload}},
		}},
	}); err != nil {
		t.Fatalf("Save(%s): %v", backend, err)
	}

	reloaded, err := NewStore(cfg)
	if err != nil {
		t.Fatalf("Reloaded NewStore(%s): %v", backend, err)
	}
	defer reloaded.Close()

	items, err := reloaded.List(context.Background())
	if err != nil {
		t.Fatalf("List(%s): %v", backend, err)
	}
	if len(items) != 1 || items[0].ID != "persist-conv" {
		t.Fatalf("reloaded items(%s) = %#v", backend, items)
	}
	if got := items[0].Turns[0].Images[0].URL; !strings.HasPrefix(got, "/v1/files/image/result-") {
		t.Fatalf("reloaded image url(%s) = %q", backend, got)
	}
}

func TestFileStoreExtractsImagesToServerDirectory(t *testing.T) {
	root := t.TempDir()
	cfg := config.New(root)
	if err := cfg.Load(); err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.Storage.Backend = "current"
	cfg.Storage.ImageDir = "data/images"

	store, err := NewStore(cfg)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	payload := base64.StdEncoding.EncodeToString([]byte("image-bytes"))
	created, err := store.Save(context.Background(), Conversation{
		ID:        "conv-1",
		Title:     "生成",
		Mode:      "generate",
		Prompt:    "test",
		Model:     "gpt-image-2",
		Count:     1,
		CreatedAt: "2026-04-26T00:00:00Z",
		Status:    "success",
		Turns: []Turn{
			{
				ID:        "turn-1",
				Title:     "生成",
				Mode:      "generate",
				Prompt:    "test",
				Model:     "gpt-image-2",
				Count:     1,
				CreatedAt: "2026-04-26T00:00:00Z",
				Status:    "success",
				SourceImages: []SourceImage{
					{ID: "source-1", Role: "image", Name: "source.png", DataURL: "data:image/png;base64," + payload},
				},
				Images: []Image{
					{ID: "image-1", Status: "success", B64JSON: payload},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if got := created.Turns[0].Images[0].B64JSON; got != "" {
		t.Fatalf("B64JSON should be stripped from stored history, got %q", got)
	}
	if got := created.Turns[0].Images[0].URL; !strings.HasPrefix(got, "/v1/files/image/result-") {
		t.Fatalf("stored result URL = %q", got)
	}
	if got := created.Turns[0].SourceImages[0].DataURL; got != "" {
		t.Fatalf("DataURL should be stripped from stored source, got %q", got)
	}
	if got := created.Turns[0].SourceImages[0].URL; !strings.HasPrefix(got, "/v1/files/image/source-") {
		t.Fatalf("stored source URL = %q", got)
	}

	matches, err := filepath.Glob(filepath.Join(root, "data", "images", "*.png"))
	if err != nil {
		t.Fatalf("glob image dir: %v", err)
	}
	if len(matches) != 2 {
		t.Fatalf("expected 2 server image files, got %d", len(matches))
	}
	for _, path := range matches {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected image file %s: %v", path, err)
		}
	}

	items, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 1 || items[0].ID != "conv-1" {
		t.Fatalf("List returned %#v", items)
	}
}

func TestDeleteOnlyRemovesUnreferencedImageFiles(t *testing.T) {
	root := t.TempDir()
	cfg := config.New(root)
	if err := cfg.Load(); err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.Storage.Backend = "current"
	cfg.Storage.ImageDir = "data/images"

	store, err := NewStore(cfg)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	payload := base64.StdEncoding.EncodeToString([]byte("shared-image-bytes"))
	first, err := store.Save(context.Background(), Conversation{
		ID:        "conv-1",
		Title:     "生成",
		Mode:      "generate",
		Prompt:    "one",
		Model:     "gpt-image-2",
		Count:     1,
		CreatedAt: "2026-04-26T00:00:00Z",
		Status:    "success",
		Turns: []Turn{{
			ID:        "turn-1",
			Title:     "生成",
			Mode:      "generate",
			Prompt:    "one",
			Model:     "gpt-image-2",
			Count:     1,
			CreatedAt: "2026-04-26T00:00:00Z",
			Status:    "success",
			Images:    []Image{{ID: "image-1", Status: "success", B64JSON: payload}},
		}},
	})
	if err != nil {
		t.Fatalf("Save first conversation: %v", err)
	}
	second, err := store.Save(context.Background(), Conversation{
		ID:        "conv-2",
		Title:     "生成",
		Mode:      "generate",
		Prompt:    "two",
		Model:     "gpt-image-2",
		Count:     1,
		CreatedAt: "2026-04-26T00:00:01Z",
		Status:    "success",
		Turns: []Turn{{
			ID:        "turn-2",
			Title:     "生成",
			Mode:      "generate",
			Prompt:    "two",
			Model:     "gpt-image-2",
			Count:     1,
			CreatedAt: "2026-04-26T00:00:01Z",
			Status:    "success",
			Images:    []Image{{ID: "image-2", Status: "success", B64JSON: payload}},
		}},
	})
	if err != nil {
		t.Fatalf("Save second conversation: %v", err)
	}

	sharedFilename := strings.TrimPrefix(first.Turns[0].Images[0].URL, "/v1/files/image/")
	if sharedFilename != strings.TrimPrefix(second.Turns[0].Images[0].URL, "/v1/files/image/") {
		t.Fatalf("expected deduplicated shared file, got %q and %q", first.Turns[0].Images[0].URL, second.Turns[0].Images[0].URL)
	}
	sharedPath := filepath.Join(root, "data", "images", sharedFilename)
	if _, err := os.Stat(sharedPath); err != nil {
		t.Fatalf("expected shared image file to exist: %v", err)
	}

	if err := store.Delete(context.Background(), "conv-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := os.Stat(sharedPath); err != nil {
		t.Fatalf("shared image should still exist after deleting one conversation: %v", err)
	}

	if err := store.Delete(context.Background(), "conv-2"); err != nil {
		t.Fatalf("Delete second: %v", err)
	}
	if _, err := os.Stat(sharedPath); !os.IsNotExist(err) {
		t.Fatalf("shared image should be removed after deleting last reference, err=%v", err)
	}
}

func TestSQLiteStorePersistsImageHistoryAcrossReload(t *testing.T) {
	testStorePersistenceAcrossReload(t, "sqlite")
}

func TestRedisStorePersistsImageHistoryAcrossReload(t *testing.T) {
	testStorePersistenceAcrossReload(t, "redis")
}

func TestStoreSavePersistsCategoryTagsAndSourceOrigins(t *testing.T) {
	root := t.TempDir()
	cfg := config.New(root)
	if err := cfg.Load(); err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.Storage.Backend = "current"
	cfg.Storage.ImageDir = "data/images"

	store, err := NewStore(cfg)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	galleryIndex := 7
	created, err := store.Save(context.Background(), Conversation{
		ID:        "conv-meta",
		Title:     "图库转创作",
		Mode:      "edit",
		Prompt:    "reuse gallery image",
		Model:     "gpt-image-2",
		Count:     1,
		Category:  "海报",
		Tags:      []string{"海报", " 横幅 ", "海报"},
		CreatedAt: "2026-05-26T00:00:00Z",
		Status:    "success",
		Turns: []Turn{{
			ID:        "turn-meta",
			Title:     "图库转创作",
			Mode:      "edit",
			Prompt:    "reuse gallery image",
			Model:     "gpt-image-2",
			Count:     1,
			Category:  "海报",
			Tags:      []string{"海报", "横幅", "海报"},
			CreatedAt: "2026-05-26T00:00:00Z",
			Status:    "success",
			SourceImages: []SourceImage{
				{
					ID:       "gallery-source",
					Role:     "image",
					Name:     "gallery.png",
					URL:      "/v1/files/image/gallery-source.png",
					Category: "参考图",
					Tags:     []string{"图库", "已选"},
					Source: &ImageSourceOrigin{
						Type:      "gallery",
						Confirmed: true,
						Gallery: &ImageSourceGalleryReference{
							AssetID:        "asset-7",
							Index:          &galleryIndex,
							ConversationID: "conv-gallery",
							TurnID:         "turn-gallery",
							ImageID:        "img-gallery",
						},
					},
				},
				{
					ID:   "pending-upload",
					Role: "image",
					Name: "local.png",
					Source: &ImageSourceOrigin{
						Type:      "file",
						Confirmed: false,
						FilePath:  `C:\temp\local.png`,
					},
				},
				{
					ID:   "pending-url",
					Role: "image",
					Name: "remote.png",
					Source: &ImageSourceOrigin{
						Type:      "url",
						Confirmed: false,
						URL:       "https://example.com/remote.png",
					},
				},
			},
			Images: []Image{{
				ID:     "result-meta",
				Status: "success",
				URL:    "/v1/files/image/result-meta.png",
			}},
		}},
	})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if created.Category != "海报" {
		t.Fatalf("created category = %q, want 海报", created.Category)
	}
	if len(created.Tags) != 2 || created.Tags[0] != "海报" || created.Tags[1] != "横幅" {
		t.Fatalf("created tags = %#v, want deduplicated tags", created.Tags)
	}
	if got := created.Turns[0].SourceImages[1].Source; got == nil || got.FilePath != `C:\temp\local.png` || got.Confirmed {
		t.Fatalf("pending file source = %#v, want unconfirmed file path", got)
	}
	if got := created.Turns[0].SourceImages[2].Source; got == nil || got.URL != "https://example.com/remote.png" || got.Confirmed {
		t.Fatalf("pending url source = %#v, want unconfirmed url", got)
	}

	loaded, err := store.Get(context.Background(), "conv-meta")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if loaded == nil {
		t.Fatal("Get returned nil conversation")
	}
	if loaded.Category != "海报" {
		t.Fatalf("loaded category = %q, want 海报", loaded.Category)
	}
	if len(loaded.Turns) != 1 {
		t.Fatalf("loaded turns = %d, want 1", len(loaded.Turns))
	}
	turn := loaded.Turns[0]
	if turn.Category != "海报" {
		t.Fatalf("turn category = %q, want 海报", turn.Category)
	}
	if len(turn.Tags) != 2 || turn.Tags[0] != "海报" || turn.Tags[1] != "横幅" {
		t.Fatalf("turn tags = %#v, want persisted tags", turn.Tags)
	}
	if got := turn.SourceImages[0].Source; got == nil || got.Gallery == nil || got.Gallery.AssetID != "asset-7" || got.Gallery.Index == nil || *got.Gallery.Index != 7 {
		t.Fatalf("gallery source = %#v, want persisted gallery reference", got)
	}

	asset, err := store.assetStore.Get(context.Background(), "conv-meta::turn-meta::result-meta")
	if err != nil {
		t.Fatalf("assetStore.Get: %v", err)
	}
	if asset == nil {
		t.Fatal("assetStore.Get returned nil asset")
	}
	if len(asset.SourceImages) != 3 {
		t.Fatalf("asset sourceImages len = %d, want 3", len(asset.SourceImages))
	}
	raw, err := os.ReadFile(filepath.Join(root, "data", "image-assets.db"))
	if err != nil {
		t.Fatalf("ReadFile(image-assets.db): %v", err)
	}
	if strings.Contains(string(raw), "data:image/") {
		t.Fatal("image asset db should not store source image dataUrl payloads")
	}
	if got := asset.SourceImages[0].Origin; got == nil || got.Gallery == nil || got.Gallery.AssetID != "asset-7" {
		t.Fatalf("asset gallery source = %#v, want gallery reference", got)
	}
	if got := asset.SourceImages[1].Origin; got == nil || got.FilePath != `C:\temp\local.png` || got.Confirmed {
		t.Fatalf("asset pending file source = %#v, want unconfirmed file path", got)
	}
	if got := asset.SourceImages[2].Origin; got == nil || got.URL != "https://example.com/remote.png" || got.Confirmed {
		t.Fatalf("asset pending url source = %#v, want unconfirmed remote url", got)
	}
}
