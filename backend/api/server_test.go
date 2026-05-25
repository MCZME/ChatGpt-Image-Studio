package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"chatgpt2api/internal/accounts"
	"chatgpt2api/internal/config"
	"chatgpt2api/internal/imageassets"
	"chatgpt2api/internal/imagehistory"
)

func ptrBool(value bool) *bool {
	return &value
}

func pngTestImageBytes() []byte {
	return []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53,
		0xde, 0x00, 0x00, 0x00, 0x0c, 0x49, 0x44, 0x41,
		0x54, 0x08, 0xd7, 0x63, 0xf8, 0xcf, 0xc0, 0x00,
		0x00, 0x03, 0x01, 0x01, 0x00, 0x18, 0xdd, 0x8d,
		0xb0, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e,
		0x44, 0xae, 0x42, 0x60, 0x82,
	}
}

func TestShouldUseOfficialResponses(t *testing.T) {
	tests := []struct {
		name              string
		preferredAccount  bool
		responsesEligible bool
		configuredRoute   string
		want              bool
	}{
		{
			name:              "paid account with eligible request uses responses",
			responsesEligible: true,
			configuredRoute:   "responses",
			want:              true,
		},
		{
			name:              "paid account with ineligible payload stays legacy",
			responsesEligible: false,
			configuredRoute:   "responses",
			want:              false,
		},
		{
			name:              "preferred source account stays legacy",
			preferredAccount:  true,
			responsesEligible: true,
			configuredRoute:   "responses",
			want:              false,
		},
		{
			name:              "legacy route stays legacy",
			responsesEligible: true,
			configuredRoute:   "legacy",
			want:              false,
		},
		{
			name:              "unknown route falls back to legacy",
			responsesEligible: true,
			configuredRoute:   "something-else",
			want:              false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldUseOfficialResponses(tt.preferredAccount, tt.responsesEligible, tt.configuredRoute); got != tt.want {
				t.Fatalf("shouldUseOfficialResponses() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConfiguredImageRoute(t *testing.T) {
	server := &Server{
		cfg: &config.Config{
			ChatGPT: config.ChatGPTConfig{
				FreeImageRoute: "responses",
				PaidImageRoute: "legacy",
			},
		},
	}

	if got := server.configuredImageRoute("Free"); got != "responses" {
		t.Fatalf("configuredImageRoute(Free) = %q, want %q", got, "responses")
	}
	if got := server.configuredImageRoute("Plus"); got != "legacy" {
		t.Fatalf("configuredImageRoute(Plus) = %q, want %q", got, "legacy")
	}
}

func TestMigrateImageFilesSkipsNestedTargetDirectory(t *testing.T) {
	rootDir := t.TempDir()
	cfg := config.New(rootDir)
	if err := cfg.Load(); err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	server := NewServer(cfg, nil, nil)

	oldDir := filepath.Join(rootDir, "data", "tmp", "image")
	if err := os.MkdirAll(oldDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(oldDir) returned error: %v", err)
	}
	sourcePath := filepath.Join(oldDir, "sample.png")
	if err := os.WriteFile(sourcePath, []byte("image"), 0o644); err != nil {
		t.Fatalf("WriteFile(sourcePath) returned error: %v", err)
	}

	previous := configPayload{}
	previous.Storage.ImageDir = "data/tmp/image"
	next := configPayload{}
	next.Storage.ImageDir = "data/tmp/image/nested"

	if err := server.migrateImageFilesIfNeeded(previous, next); err != nil {
		t.Fatalf("migrateImageFilesIfNeeded() returned error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(rootDir, "data", "tmp", "image", "nested", "sample.png")); err != nil {
		t.Fatalf("expected migrated file in nested dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(rootDir, "data", "tmp", "image", "nested", "nested", "sample.png")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected no recursive nested file, got err=%v", err)
	}
}

func TestNewServerMigratesLegacyDefaultImageFiles(t *testing.T) {
	rootDir := t.TempDir()
	cfg := config.New(rootDir)
	if err := cfg.Load(); err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	cfg.Storage.ImageDir = imageassets.DefaultImageDir

	legacyDir := filepath.Join(rootDir, "data", "tmp", "image")
	legacyPath := filepath.Join(legacyDir, "legacy.png")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(legacyDir) returned error: %v", err)
	}
	if err := os.WriteFile(legacyPath, []byte("legacy-image"), 0o644); err != nil {
		t.Fatalf("WriteFile(legacyPath) returned error: %v", err)
	}

	_ = NewServer(cfg, nil, nil)

	newPath := filepath.Join(rootDir, "data", "images", "legacy.png")
	if content, err := os.ReadFile(newPath); err != nil || string(content) != "legacy-image" {
		t.Fatalf("expected legacy image migrated to default dir, content=%q err=%v", string(content), err)
	}
	if _, err := os.Stat(legacyPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected legacy file removed after migration, got err=%v", err)
	}
}

func TestResolveImageFilePathFallsBackToOtherDataDirectories(t *testing.T) {
	rootDir := t.TempDir()
	cfg := config.New(rootDir)
	if err := cfg.Load(); err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	cfg.Storage.ImageDir = "data/new-images"
	server := NewServer(cfg, nil, nil)

	legacyDir := filepath.Join(rootDir, "data", "old-images")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(legacyDir) returned error: %v", err)
	}
	legacyPath := filepath.Join(legacyDir, "kept.png")
	if err := os.WriteFile(legacyPath, []byte("image"), 0o644); err != nil {
		t.Fatalf("WriteFile(legacyPath) returned error: %v", err)
	}

	got := server.resolveImageFilePath("kept.png")
	if !strings.EqualFold(filepath.Clean(got), filepath.Clean(legacyPath)) {
		t.Fatalf("resolveImageFilePath() = %q, want %q", got, legacyPath)
	}
}

func TestImportImageConversationsIntoSQLiteTarget(t *testing.T) {
	rootDir := t.TempDir()
	cfg := config.New(rootDir)
	if err := cfg.Load(); err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	cfg.App.AuthKey = "test-auth"
	server := NewServer(cfg, nil, nil)

	body := map[string]any{
		"items": []map[string]any{
			{
				"id":        "conv-1",
				"title":     "生成",
				"mode":      "generate",
				"prompt":    "test",
				"model":     "gpt-image-2",
				"count":     1,
				"createdAt": "2026-04-26T00:00:00Z",
				"status":    "success",
				"turns": []map[string]any{
					{
						"id":        "turn-1",
						"title":     "生成",
						"mode":      "generate",
						"prompt":    "test",
						"model":     "gpt-image-2",
						"count":     1,
						"createdAt": "2026-04-26T00:00:00Z",
						"status":    "success",
						"images": []map[string]any{
							{
								"id":       "img-1",
								"status":   "success",
								"b64_json": "aW1hZ2U=",
							},
						},
					},
				},
			},
		},
		"storage": map[string]any{
			"backend":                  "sqlite",
			"imageDir":                 "data/import-images",
			"sqlitePath":               "data/import-history.sqlite",
			"imageConversationStorage": "server",
			"imageDataStorage":         "server",
		},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("Marshal() returned error: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/image/conversations/import", bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer "+cfg.App.AuthKey)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	verifyCfg := config.New(rootDir)
	verifyCfg.Storage.Backend = "sqlite"
	verifyCfg.Storage.ImageDir = "data/import-images"
	verifyCfg.Storage.SQLitePath = "data/import-history.sqlite"
	store, err := imagehistory.NewStore(verifyCfg)
	if err != nil {
		t.Fatalf("NewStore(verify sqlite) returned error: %v", err)
	}
	defer store.Close()

	items, err := store.List(req.Context())
	if err != nil {
		t.Fatalf("List() returned error: %v", err)
	}
	if len(items) != 1 || items[0].ID != "conv-1" {
		t.Fatalf("imported items = %#v", items)
	}
}

func TestHandleListImageAssetsBuildsItemsFromServerHistory(t *testing.T) {
	rootDir := t.TempDir()
	cfg := config.New(rootDir)
	if err := cfg.Load(); err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	cfg.App.AuthKey = "test-auth"
	cfg.Storage.ImageConversationStorage = "server"
	cfg.Storage.ImageDataStorage = "server"
	cfg.Storage.ImageDir = "data/assets-images"

	historyStore, err := imagehistory.NewStore(cfg)
	if err != nil {
		t.Fatalf("NewStore(history) returned error: %v", err)
	}
	defer historyStore.Close()

	_, err = historyStore.Save(context.Background(), imagehistory.Conversation{
		ID:        "conv-assets",
		Title:     "测试图片",
		Mode:      "generate",
		Prompt:    "studio sunset poster",
		Model:     "gpt-image-2",
		Count:     1,
		CreatedAt: "2026-05-18T08:00:00Z",
		Status:    "success",
		Turns: []imagehistory.Turn{
			{
				ID:        "turn-assets",
				Title:     "测试图片",
				Mode:      "generate",
				Prompt:    "studio sunset poster",
				Model:     "gpt-image-2",
				Count:     1,
				CreatedAt: "2026-05-18T08:00:00Z",
				Status:    "success",
				Images: []imagehistory.Image{
					{
						ID:            "img-assets",
						Status:        "success",
						URL:           "/v1/files/image/result-a.png",
						RevisedPrompt: "sunset poster refined",
						FileID:        "file-assets",
						GenID:         "gen-assets",
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Save(history) returned error: %v", err)
	}

	server := NewServer(cfg, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/image/assets", nil)
	req.Header.Set("Authorization", "Bearer "+cfg.App.AuthKey)
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Items []imageAssetView `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal() returned error: %v", err)
	}
	if len(payload.Items) != 1 {
		t.Fatalf("items len = %d, want 1", len(payload.Items))
	}
	if payload.Items[0].Prompt != "studio sunset poster" {
		t.Fatalf("prompt = %q, want %q", payload.Items[0].Prompt, "studio sunset poster")
	}
	if payload.Items[0].ImageURL != "/v1/files/image/result-a.png" {
		t.Fatalf("imageUrl = %q, want %q", payload.Items[0].ImageURL, "/v1/files/image/result-a.png")
	}
}

func TestHandleUpdateImageAssetPersistsMetadata(t *testing.T) {
	rootDir := t.TempDir()
	cfg := config.New(rootDir)
	if err := cfg.Load(); err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	cfg.App.AuthKey = "test-auth"
	cfg.Storage.ImageConversationStorage = "server"
	cfg.Storage.ImageDataStorage = "server"
	cfg.Storage.ImageDir = "data/assets-images"

	historyStore, err := imagehistory.NewStore(cfg)
	if err != nil {
		t.Fatalf("NewStore(history) returned error: %v", err)
	}
	defer historyStore.Close()

	_, err = historyStore.Save(context.Background(), imagehistory.Conversation{
		ID:        "conv-assets",
		Title:     "测试图片",
		Mode:      "generate",
		Prompt:    "studio sunset poster",
		Model:     "gpt-image-2",
		Count:     1,
		CreatedAt: "2026-05-18T08:00:00Z",
		Status:    "success",
		Turns: []imagehistory.Turn{
			{
				ID:        "turn-assets",
				Title:     "测试图片",
				Mode:      "generate",
				Prompt:    "studio sunset poster",
				Model:     "gpt-image-2",
				Count:     1,
				CreatedAt: "2026-05-18T08:00:00Z",
				Status:    "success",
				Images: []imagehistory.Image{
					{
						ID:     "img-assets",
						Status: "success",
						URL:    "/v1/files/image/result-a.png",
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Save(history) returned error: %v", err)
	}

	server := NewServer(cfg, nil, nil)

	seedReq := httptest.NewRequest(http.MethodGet, "/api/image/assets", nil)
	seedReq.Header.Set("Authorization", "Bearer "+cfg.App.AuthKey)
	seedRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(seedRec, seedReq)
	if seedRec.Code != http.StatusOK {
		t.Fatalf("seed status = %d, body = %s", seedRec.Code, seedRec.Body.String())
	}

	updateBody := `{"category":"海报","tags":["落日","宣传"],"note":"首页横幅","favorite":true}`
	req := httptest.NewRequest(http.MethodPut, "/api/image/assets/conv-assets::turn-assets::img-assets", strings.NewReader(updateBody))
	req.Header.Set("Authorization", "Bearer "+cfg.App.AuthKey)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Item imageAssetView `json:"item"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal() returned error: %v", err)
	}
	if payload.Item.Category != "海报" {
		t.Fatalf("category = %q, want %q", payload.Item.Category, "海报")
	}
	if !payload.Item.Favorite {
		t.Fatal("favorite = false, want true")
	}
	if len(payload.Item.Tags) != 2 {
		t.Fatalf("tags = %#v, want 2 tags", payload.Item.Tags)
	}
}

func TestHandleBulkUpdateImageAssetsPersistsMetadata(t *testing.T) {
	rootDir := t.TempDir()
	cfg := config.New(rootDir)
	if err := cfg.Load(); err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	cfg.App.AuthKey = "test-auth"
	cfg.Storage.ImageConversationStorage = "server"
	cfg.Storage.ImageDataStorage = "server"
	cfg.Storage.ImageDir = "data/assets-images"

	historyStore, err := imagehistory.NewStore(cfg)
	if err != nil {
		t.Fatalf("NewStore(history) returned error: %v", err)
	}
	defer historyStore.Close()

	_, err = historyStore.Save(context.Background(), imagehistory.Conversation{
		ID:        "conv-assets",
		Title:     "测试图片",
		Mode:      "generate",
		Prompt:    "studio sunset poster",
		Model:     "gpt-image-2",
		Count:     2,
		CreatedAt: "2026-05-18T08:00:00Z",
		Status:    "success",
		Turns: []imagehistory.Turn{
			{
				ID:        "turn-assets",
				Title:     "测试图片",
				Mode:      "generate",
				Prompt:    "studio sunset poster",
				Model:     "gpt-image-2",
				Count:     2,
				CreatedAt: "2026-05-18T08:00:00Z",
				Status:    "success",
				Images: []imagehistory.Image{
					{ID: "img-1", Status: "success", URL: "/v1/files/image/result-a.png"},
					{ID: "img-2", Status: "success", URL: "/v1/files/image/result-b.png"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Save(history) returned error: %v", err)
	}

	server := NewServer(cfg, nil, nil)
	seedReq := httptest.NewRequest(http.MethodGet, "/api/image/assets", nil)
	seedReq.Header.Set("Authorization", "Bearer "+cfg.App.AuthKey)
	seedRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(seedRec, seedReq)
	if seedRec.Code != http.StatusOK {
		t.Fatalf("seed status = %d, body = %s", seedRec.Code, seedRec.Body.String())
	}

	updateBody := `{"ids":["conv-assets::turn-assets::img-1","conv-assets::turn-assets::img-2"],"category":"批量海报","tags":["批量","横幅"]}`
	req := httptest.NewRequest(http.MethodPut, "/api/image/assets", strings.NewReader(updateBody))
	req.Header.Set("Authorization", "Bearer "+cfg.App.AuthKey)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Items []imageAssetView `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal() returned error: %v", err)
	}
	if len(payload.Items) != 2 {
		t.Fatalf("items len = %d, want 2", len(payload.Items))
	}
	for _, item := range payload.Items {
		if item.Category != "批量海报" {
			t.Fatalf("category = %q, want %q", item.Category, "批量海报")
		}
		if len(item.Tags) != 2 {
			t.Fatalf("tags = %#v, want 2 tags", item.Tags)
		}
	}
}

func TestHandleListImageAssetsSupportsPagination(t *testing.T) {
	rootDir := t.TempDir()
	cfg := config.New(rootDir)
	if err := cfg.Load(); err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	cfg.App.AuthKey = "test-auth"
	cfg.Storage.ImageConversationStorage = "server"
	cfg.Storage.ImageDataStorage = "server"
	cfg.Storage.ImageDir = "data/assets-images"

	historyStore, err := imagehistory.NewStore(cfg)
	if err != nil {
		t.Fatalf("NewStore(history) returned error: %v", err)
	}
	defer historyStore.Close()

	for index := 0; index < 3; index++ {
		_, err = historyStore.Save(context.Background(), imagehistory.Conversation{
			ID:        fmt.Sprintf("conv-page-%d", index),
			Title:     "分页测试",
			Mode:      "generate",
			Prompt:    fmt.Sprintf("page asset %d", index),
			Model:     "gpt-image-2",
			Count:     1,
			CreatedAt: fmt.Sprintf("2026-05-18T08:00:0%dZ", index),
			Status:    "success",
			Turns: []imagehistory.Turn{
				{
					ID:        fmt.Sprintf("turn-page-%d", index),
					Title:     "分页测试",
					Mode:      "generate",
					Prompt:    fmt.Sprintf("page asset %d", index),
					Model:     "gpt-image-2",
					Count:     1,
					CreatedAt: fmt.Sprintf("2026-05-18T08:00:0%dZ", index),
					Status:    "success",
					Images: []imagehistory.Image{
						{
							ID:     fmt.Sprintf("img-page-%d", index),
							Status: "success",
							URL:    fmt.Sprintf("/v1/files/image/page-%d.png", index),
						},
					},
				},
			},
		})
		if err != nil {
			t.Fatalf("Save(history) returned error: %v", err)
		}
	}

	server := NewServer(cfg, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/image/assets?limit=2&offset=0", nil)
	req.Header.Set("Authorization", "Bearer "+cfg.App.AuthKey)
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Items      []imageAssetView `json:"items"`
		Total      int              `json:"total"`
		HasMore    bool             `json:"hasMore"`
		NextOffset int              `json:"nextOffset"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal() returned error: %v", err)
	}
	if len(payload.Items) != 2 {
		t.Fatalf("items len = %d, want 2", len(payload.Items))
	}
	if payload.Total != 3 {
		t.Fatalf("total = %d, want 3", payload.Total)
	}
	if !payload.HasMore {
		t.Fatal("hasMore = false, want true")
	}
	if payload.NextOffset != 2 {
		t.Fatalf("nextOffset = %d, want 2", payload.NextOffset)
	}
}

func TestHandleListImageAssetsSupportsSorting(t *testing.T) {
	rootDir := t.TempDir()
	cfg := config.New(rootDir)
	if err := cfg.Load(); err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	cfg.App.AuthKey = "test-auth"
	cfg.Storage.ImageConversationStorage = "server"
	cfg.Storage.ImageDataStorage = "server"
	cfg.Storage.ImageDir = "data/assets-images"

	historyStore, err := imagehistory.NewStore(cfg)
	if err != nil {
		t.Fatalf("NewStore(history) returned error: %v", err)
	}
	defer historyStore.Close()

	seed := []struct {
		id    string
		title string
	}{
		{id: "conv-b", title: "Bravo"},
		{id: "conv-a", title: "Alpha"},
	}

	for index, item := range seed {
		_, err = historyStore.Save(context.Background(), imagehistory.Conversation{
			ID:        item.id,
			Title:     item.title,
			Mode:      "generate",
			Prompt:    item.title,
			Model:     "gpt-image-2",
			Count:     1,
			CreatedAt: fmt.Sprintf("2026-05-18T08:00:0%dZ", index),
			Status:    "success",
			Turns: []imagehistory.Turn{
				{
					ID:        item.id + "-turn",
					Title:     item.title,
					Mode:      "generate",
					Prompt:    item.title,
					Model:     "gpt-image-2",
					Count:     1,
					CreatedAt: fmt.Sprintf("2026-05-18T08:00:0%dZ", index),
					Status:    "success",
					Images: []imagehistory.Image{
						{
							ID:     item.id + "-img",
							Status: "success",
							URL:    fmt.Sprintf("/v1/files/image/%s.png", item.id),
						},
					},
				},
			},
		})
		if err != nil {
			t.Fatalf("Save(history) returned error: %v", err)
		}
	}

	server := NewServer(cfg, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/image/assets?sort=title_asc", nil)
	req.Header.Set("Authorization", "Bearer "+cfg.App.AuthKey)
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Items []imageAssetView `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal() returned error: %v", err)
	}
	if len(payload.Items) < 2 {
		t.Fatalf("items len = %d, want at least 2", len(payload.Items))
	}
	if payload.Items[0].Title != "Alpha" || payload.Items[1].Title != "Bravo" {
		t.Fatalf("titles = [%s, %s], want [Alpha, Bravo]", payload.Items[0].Title, payload.Items[1].Title)
	}
}

func TestDeleteConversationKeepsAssetReferencedImageFile(t *testing.T) {
	rootDir := t.TempDir()
	cfg := config.New(rootDir)
	if err := cfg.Load(); err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	cfg.Storage.ImageConversationStorage = "server"
	cfg.Storage.ImageDataStorage = "server"
	cfg.Storage.ImageDir = "data/assets-images"

	historyStore, err := imagehistory.NewStore(cfg)
	if err != nil {
		t.Fatalf("NewStore(history) returned error: %v", err)
	}
	defer historyStore.Close()

	imagePath := filepath.Join(rootDir, "data", "assets-images", "result-keep.png")
	if err := os.MkdirAll(filepath.Dir(imagePath), 0o755); err != nil {
		t.Fatalf("MkdirAll(image dir) returned error: %v", err)
	}
	if err := os.WriteFile(imagePath, []byte("image"), 0o644); err != nil {
		t.Fatalf("WriteFile(imagePath) returned error: %v", err)
	}

	_, err = historyStore.Save(context.Background(), imagehistory.Conversation{
		ID:        "conv-keep",
		Title:     "测试图片",
		Mode:      "generate",
		Prompt:    "keep file",
		Model:     "gpt-image-2",
		Count:     1,
		CreatedAt: "2026-05-18T08:00:00Z",
		Status:    "success",
		Turns: []imagehistory.Turn{
			{
				ID:        "turn-keep",
				Title:     "测试图片",
				Mode:      "generate",
				Prompt:    "keep file",
				Model:     "gpt-image-2",
				Count:     1,
				CreatedAt: "2026-05-18T08:00:00Z",
				Status:    "success",
				Images: []imagehistory.Image{
					{ID: "img-keep", Status: "success", URL: "/v1/files/image/result-keep.png"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Save(history) returned error: %v", err)
	}
	assetStore, err := imageassets.NewStore(cfg.RootDir())
	if err != nil {
		t.Fatalf("NewStore(asset) returned error: %v", err)
	}
	defer assetStore.Close()
	if _, err := assetStore.UpdateMetadata(context.Background(), "conv-keep::turn-keep::img-keep", imageassets.MetadataPatch{
		Favorite: ptrBool(true),
	}); err != nil {
		t.Fatalf("UpdateMetadata(asset) returned error: %v", err)
	}

	if err := historyStore.Delete(context.Background(), "conv-keep"); err != nil {
		t.Fatalf("Delete(history) returned error: %v", err)
	}

	if _, err := os.Stat(imagePath); err != nil {
		t.Fatalf("expected image file to remain after conversation delete: %v", err)
	}
}

func TestHandleListImageAssetsReturnsStorageMetadata(t *testing.T) {
	rootDir := t.TempDir()
	cfg := config.New(rootDir)
	if err := cfg.Load(); err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	cfg.App.AuthKey = "test-auth"
	cfg.Storage.ImageConversationStorage = "server"
	cfg.Storage.ImageDataStorage = "server"
	cfg.Storage.ImageDir = "data/assets-images"

	imagePath := filepath.Join(rootDir, "data", "assets-images", "result-meta.png")
	if err := os.MkdirAll(filepath.Dir(imagePath), 0o755); err != nil {
		t.Fatalf("MkdirAll(image dir) returned error: %v", err)
	}
	if err := os.WriteFile(imagePath, []byte("metadata-image"), 0o644); err != nil {
		t.Fatalf("WriteFile(imagePath) returned error: %v", err)
	}

	historyStore, err := imagehistory.NewStore(cfg)
	if err != nil {
		t.Fatalf("NewStore(history) returned error: %v", err)
	}
	defer historyStore.Close()
	_, err = historyStore.Save(context.Background(), imagehistory.Conversation{
		ID:        "conv-meta",
		Title:     "Meta",
		Mode:      "generate",
		Prompt:    "metadata",
		Model:     "gpt-image-2",
		Count:     1,
		CreatedAt: "2026-05-18T08:00:00Z",
		Status:    "success",
		Turns: []imagehistory.Turn{{
			ID:        "turn-meta",
			Title:     "Meta",
			Mode:      "generate",
			Prompt:    "metadata",
			Model:     "gpt-image-2",
			Count:     1,
			CreatedAt: "2026-05-18T08:00:00Z",
			Status:    "success",
			Images: []imagehistory.Image{
				{ID: "img-meta", Status: "success", URL: "/v1/files/image/result-meta.png"},
			},
		}},
	})
	if err != nil {
		t.Fatalf("Save(history) returned error: %v", err)
	}

	server := NewServer(cfg, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/image/assets", nil)
	req.Header.Set("Authorization", "Bearer "+cfg.App.AuthKey)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Items []imageAssetView `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal() returned error: %v", err)
	}
	if len(payload.Items) != 1 {
		t.Fatalf("items len = %d, want 1", len(payload.Items))
	}
	item := payload.Items[0]
	if item.Filename != "result-meta.png" || item.SizeBytes != int64(len("metadata-image")) || item.SHA256 == "" {
		t.Fatalf("storage metadata = %#v, want filename, size, and sha256", item)
	}
}

func TestHandleImportImageAssetsSavesFilesAndMetadata(t *testing.T) {
	rootDir := t.TempDir()
	cfg := config.New(rootDir)
	if err := cfg.Load(); err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	cfg.App.AuthKey = "test-auth"
	cfg.Storage.ImageDir = "data/assets-images"

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("category", "参考图"); err != nil {
		t.Fatalf("WriteField(category) returned error: %v", err)
	}
	if err := writer.WriteField("tags", "外来, 素材"); err != nil {
		t.Fatalf("WriteField(tags) returned error: %v", err)
	}
	if err := writer.WriteField("note", "imported note"); err != nil {
		t.Fatalf("WriteField(note) returned error: %v", err)
	}
	part, err := writer.CreateFormFile("images", "sample.png")
	if err != nil {
		t.Fatalf("CreateFormFile() returned error: %v", err)
	}
	if _, err := part.Write(pngTestImageBytes()); err != nil {
		t.Fatalf("Write(image) returned error: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close(writer) returned error: %v", err)
	}

	server := NewServer(cfg, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/image/assets/import", &body)
	req.Header.Set("Authorization", "Bearer "+cfg.App.AuthKey)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Imported int              `json:"imported"`
		Items    []imageAssetView `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal() returned error: %v", err)
	}
	if payload.Imported != 1 || len(payload.Items) != 1 {
		t.Fatalf("import payload = %#v, want one imported item", payload)
	}
	item := payload.Items[0]
	if item.Title != "sample" || item.Category != "参考图" || item.Note != "imported note" {
		t.Fatalf("item metadata = %#v, want imported title/category/note", item)
	}
	if item.SourceKind != "import" || item.StorageKind != "local" {
		t.Fatalf("storage kind = source %q storage %q, want import/local", item.SourceKind, item.StorageKind)
	}
	if len(item.Tags) != 2 || item.Tags[0] != "外来" || item.Tags[1] != "素材" {
		t.Fatalf("tags = %#v, want imported tags", item.Tags)
	}
	if item.Filename == "" || !strings.HasPrefix(item.Filename, "import-") {
		t.Fatalf("filename = %q, want import-*", item.Filename)
	}
	if _, err := os.Stat(filepath.Join(rootDir, "data", "assets-images", item.Filename)); err != nil {
		t.Fatalf("expected imported image file: %v", err)
	}

	store, err := imageassets.NewStore(cfg.RootDir())
	if err != nil {
		t.Fatalf("NewStore(asset) returned error: %v", err)
	}
	defer store.Close()
	saved, err := store.Get(context.Background(), item.ID)
	if err != nil {
		t.Fatalf("Get(imported asset) returned error: %v", err)
	}
	if saved == nil || saved.SHA256 == "" || saved.ImageURL != item.ImageURL {
		t.Fatalf("saved asset = %#v, want persisted file metadata", saved)
	}
}

func TestHandleImportImageAssetsRejectsNonImage(t *testing.T) {
	rootDir := t.TempDir()
	cfg := config.New(rootDir)
	if err := cfg.Load(); err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	cfg.App.AuthKey = "test-auth"
	cfg.Storage.ImageDir = "data/assets-images"

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("images", "notes.txt")
	if err != nil {
		t.Fatalf("CreateFormFile() returned error: %v", err)
	}
	if _, err := part.Write([]byte("not an image")); err != nil {
		t.Fatalf("Write(text) returned error: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close(writer) returned error: %v", err)
	}

	server := NewServer(cfg, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/image/assets/import", &body)
	req.Header.Set("Authorization", "Bearer "+cfg.App.AuthKey)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Imported int                           `json:"imported"`
		Failed   []imageAssetImportFailureView `json:"failed"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal() returned error: %v", err)
	}
	if payload.Imported != 0 || len(payload.Failed) != 1 {
		t.Fatalf("payload = %#v, want one failed import", payload)
	}
}

func TestConfiguredImageModeTreatsLegacyMixAsStudio(t *testing.T) {
	server := &Server{
		cfg: &config.Config{
			ChatGPT: config.ChatGPTConfig{
				ImageMode: "mix",
			},
		},
	}

	if got := server.configuredImageMode(); got != "studio" {
		t.Fatalf("configuredImageMode() = %q, want %q", got, "studio")
	}
}

func TestHandleCreateAccountsRejectsOutsideStudioMode(t *testing.T) {
	rootDir := t.TempDir()
	cfg := config.New(rootDir)
	if err := cfg.Load(); err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	cfg.App.AuthKey = "test-auth"
	cfg.ChatGPT.ImageMode = "cpa"

	store, err := accounts.NewStore(cfg)
	if err != nil {
		t.Fatalf("NewStore() returned error: %v", err)
	}
	defer store.Close()

	server := NewServer(cfg, store, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/accounts", strings.NewReader(`{"tokens":["token-1"]}`))
	req.Header.Set("Authorization", "Bearer "+cfg.App.AuthKey)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Studio") {
		t.Fatalf("body = %s, want Studio mode error", rec.Body.String())
	}
}

func TestHandleRefreshAllAccountsWithEmptyStoreFinishesImmediately(t *testing.T) {
	rootDir := t.TempDir()
	cfg := config.New(rootDir)
	if err := cfg.Load(); err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	cfg.App.AuthKey = "test-auth"

	store, err := accounts.NewStore(cfg)
	if err != nil {
		t.Fatalf("NewStore() returned error: %v", err)
	}
	defer store.Close()

	server := NewServer(cfg, store, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/accounts/refresh-all", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+cfg.App.AuthKey)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Progress *accountRefreshRunResult `json:"progress"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal() returned error: %v", err)
	}
	if payload.Progress == nil {
		t.Fatal("progress should not be nil")
	}
	if payload.Progress.Running {
		t.Fatalf("progress.Running = true, want false")
	}
	if payload.Progress.Total != 0 || payload.Progress.Processed != 0 {
		t.Fatalf("progress = %#v, want empty finished run", payload.Progress)
	}
}

func TestResolveImageUpstreamModelFromConfig(t *testing.T) {
	server := &Server{
		cfg: &config.Config{
			ChatGPT: config.ChatGPTConfig{
				FreeImageModel: "auto",
				PaidImageModel: "gpt-5.4",
			},
		},
	}

	if got := server.resolveImageUpstreamModel("gpt-image-1", "Plus"); got != "gpt-5.4" {
		t.Fatalf("resolveImageUpstreamModel() = %q, want %q", got, "gpt-5.4")
	}
	if got := server.resolveImageUpstreamModel("gpt-image-2", "Free"); got != "auto" {
		t.Fatalf("resolveImageUpstreamModel() = %q, want %q", got, "auto")
	}
}

func TestResolveImageAcquireError(t *testing.T) {
	lastErr := errors.New("refresh failed")
	noAvailableErr := errors.New("read dir failed")

	tests := []struct {
		name             string
		mode             string
		err              error
		lastRetryableErr error
		wantMessage      string
		wantCode         string
	}{
		{
			name:        "cpa mode still maps empty pool when helper is used",
			mode:        "cpa",
			err:         accounts.ErrNoAvailableImageAuth,
			wantMessage: "当前没有可用的图片账号用于 CPA 模式",
			wantCode:    "no_cpa_image_accounts",
		},
		{
			name:             "retry exhaustion keeps last real error",
			mode:             "cpa",
			err:              accounts.ErrNoAvailableImageAuth,
			lastRetryableErr: lastErr,
			wantMessage:      lastErr.Error(),
		},
		{
			name:        "non sentinel error passes through",
			mode:        "cpa",
			err:         noAvailableErr,
			wantMessage: noAvailableErr.Error(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveImageAcquireError(tt.mode, tt.err, tt.lastRetryableErr)
			if got == nil {
				t.Fatal("resolveImageAcquireError() returned nil")
			}
			if got.Error() != tt.wantMessage {
				t.Fatalf("resolveImageAcquireError() error = %q, want %q", got.Error(), tt.wantMessage)
			}
			if tt.wantCode != "" && requestErrorCode(got) != tt.wantCode {
				t.Fatalf("resolveImageAcquireError() code = %q, want %q", requestErrorCode(got), tt.wantCode)
			}
		})
	}
}

func TestNormalizeGenerateImageSize(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "empty size uses default upstream behavior",
			input: "",
			want:  "",
		},
		{
			name:  "supported landscape size passes through",
			input: "1536x1024",
			want:  "1536x1024",
		},
		{
			name:  "uppercase separator is normalized",
			input: "1024X1536",
			want:  "1024x1536",
		},
		{
			name:  "unsupported large size now passes through normalized",
			input: "8192x8192",
			want:  "8192x8192",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeGenerateImageSize(tt.input)
			if got != tt.want {
				t.Fatalf("normalizeGenerateImageSize() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsImageRateLimitError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "http 429", err: errors.New("backend-api failed: HTTP 429"), want: true},
		{name: "too many requests", err: errors.New("Too Many Requests"), want: true},
		{name: "rate limit", err: errors.New("rate limit exceeded"), want: true},
		{name: "quota exceeded", err: errors.New("image generation quota exceeded"), want: true},
		{name: "non rate error", err: errors.New("internal server error"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isImageRateLimitError(tt.err); got != tt.want {
				t.Fatalf("isImageRateLimitError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsTransientImageStreamError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "responses sse internal error", err: errors.New("responses SSE read error: stream error: stream ID 1; INTERNAL_ERROR; received from peer"), want: true},
		{name: "unexpected eof", err: errors.New("SSE read error: unexpected EOF"), want: true},
		{name: "http2 connection lost", err: errors.New("http2: client connection lost"), want: true},
		{name: "non transient", err: errors.New("no images generated"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isTransientImageStreamError(tt.err); got != tt.want {
				t.Fatalf("isTransientImageStreamError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStudioPaidResolutionUsesPaidAccount(t *testing.T) {
	server, recorder := newImageModeCompatTestServerWithOptions(t, imageModeCompatScenario{
		imageMode:   "studio",
		accountType: "Plus",
		freeRoute:   "legacy",
		freeModel:   "auto",
		paidRoute:   "responses",
		paidModel:   "gpt-5.4-mini",
	}, compatTestServerOptions{
		accounts: []compatSeedAccount{
			{
				fileName:    "free.json",
				accessToken: "token-free-priority",
				accountType: "Free",
				priority:    100,
				quota:       5,
				status:      "正常",
			},
			{
				fileName:    "paid.json",
				accessToken: "token-paid",
				accountType: "Plus",
				priority:    1,
				quota:       5,
				status:      "正常",
			},
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(`{"prompt":"test prompt","size":"2560x1440","quality":"high","response_format":"b64_json"}`))
	req.Header.Set("Authorization", "Bearer "+server.cfg.App.APIKey)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	entries := server.reqLogs.list(1)
	if len(entries) != 1 {
		t.Fatalf("log entries = %d, want 1", len(entries))
	}
	entry := entries[0]
	if entry.AccountType != "Plus" {
		t.Fatalf("account type = %q, want %q", entry.AccountType, "Plus")
	}
	if entry.Size != "2560x1440" {
		t.Fatalf("log size = %q, want %q", entry.Size, "2560x1440")
	}
	if entry.Quality != "high" {
		t.Fatalf("log quality = %q, want %q", entry.Quality, "high")
	}
	if entry.ImageToolModel != "gpt-5.4-mini" {
		t.Fatalf("log image tool model = %q, want %q", entry.ImageToolModel, "gpt-5.4-mini")
	}
	if entry.PromptLength != 11 {
		t.Fatalf("log prompt length = %d, want 11", entry.PromptLength)
	}
	if recorder.lastFactory != "responses" {
		t.Fatalf("last factory = %q, want %q", recorder.lastFactory, "responses")
	}
	if got := recorder.callSequence[len(recorder.callSequence)-1]; !strings.Contains(got, "token-paid") {
		t.Fatalf("call sequence = %v, want paid token selected", recorder.callSequence)
	}
}

func TestStudioRateLimitedAccountRetriesWithNextAccount(t *testing.T) {
	server, recorder := newImageModeCompatTestServerWithOptions(t, imageModeCompatScenario{
		imageMode:   "studio",
		accountType: "Free",
		freeRoute:   "legacy",
		freeModel:   "auto",
		paidRoute:   "responses",
		paidModel:   "gpt-5.4-mini",
	}, compatTestServerOptions{
		accounts: []compatSeedAccount{
			{
				fileName:    "limited.json",
				accessToken: "token-limited",
				accountType: "Free",
				priority:    100,
				quota:       5,
				status:      "正常",
			},
			{
				fileName:    "fallback.json",
				accessToken: "token-fallback",
				accountType: "Free",
				priority:    10,
				quota:       5,
				status:      "正常",
			},
		},
		behavior: compatClientBehavior{
			officialGenerateErrors: map[string]error{
				"token-limited": errors.New("backend-api failed: HTTP 429 too many requests"),
			},
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(`{"prompt":"test prompt","response_format":"b64_json"}`))
	req.Header.Set("Authorization", "Bearer "+server.cfg.App.APIKey)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	if len(recorder.callSequence) != 2 {
		t.Fatalf("call sequence = %v, want two attempts", recorder.callSequence)
	}
	if !strings.Contains(recorder.callSequence[0], "token-limited") || !strings.Contains(recorder.callSequence[1], "token-fallback") {
		t.Fatalf("call sequence = %v, want limited then fallback", recorder.callSequence)
	}

	limitedAccount, err := server.getStore().GetAccountByToken("token-limited")
	if err != nil {
		t.Fatalf("GetAccountByToken(limited) returned error: %v", err)
	}
	if limitedAccount.Status != "限流" {
		t.Fatalf("limited account status = %q, want %q", limitedAccount.Status, "限流")
	}
	if limitedAccount.Quota != 0 {
		t.Fatalf("limited account quota = %d, want 0", limitedAccount.Quota)
	}
}

func TestStudioResponsesRateLimitedAccountRetriesWithNextAccount(t *testing.T) {
	server, recorder := newImageModeCompatTestServerWithOptions(t, imageModeCompatScenario{
		imageMode:   "studio",
		accountType: "Plus",
		freeRoute:   "legacy",
		freeModel:   "auto",
		paidRoute:   "responses",
		paidModel:   "gpt-5.4-mini",
	}, compatTestServerOptions{
		accounts: []compatSeedAccount{
			{
				fileName:    "limited-paid.json",
				accessToken: "token-limited-paid",
				accountType: "Plus",
				priority:    100,
				quota:       5,
				status:      "正常",
			},
			{
				fileName:    "fallback-paid.json",
				accessToken: "token-fallback-paid",
				accountType: "Plus",
				priority:    10,
				quota:       5,
				status:      "正常",
			},
		},
		behavior: compatClientBehavior{
			responsesGenerateErrors: map[string]error{
				"token-limited-paid": errors.New("responses failed: HTTP 429 too many requests"),
			},
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(`{"prompt":"test prompt","size":"2560x1440","quality":"high","response_format":"b64_json"}`))
	req.Header.Set("Authorization", "Bearer "+server.cfg.App.APIKey)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	if recorder.lastFactory != "responses" {
		t.Fatalf("last factory = %q, want %q", recorder.lastFactory, "responses")
	}
	if len(recorder.callSequence) != 2 {
		t.Fatalf("call sequence = %v, want two attempts", recorder.callSequence)
	}
	if !strings.Contains(recorder.callSequence[0], "token-limited-paid") || !strings.Contains(recorder.callSequence[1], "token-fallback-paid") {
		t.Fatalf("call sequence = %v, want limited then fallback responses account", recorder.callSequence)
	}

	limitedAccount, err := server.getStore().GetAccountByToken("token-limited-paid")
	if err != nil {
		t.Fatalf("GetAccountByToken(limited paid) returned error: %v", err)
	}
	if limitedAccount.Status != "限流" {
		t.Fatalf("limited paid account status = %q, want %q", limitedAccount.Status, "限流")
	}
	if limitedAccount.Quota != 0 {
		t.Fatalf("limited paid account quota = %d, want 0", limitedAccount.Quota)
	}
}

func TestStudioPaidResolutionFallsBackOutsideSelectedFreeOnlyGroup(t *testing.T) {
	server, recorder := newImageModeCompatTestServerWithOptions(t, imageModeCompatScenario{
		imageMode:   "studio",
		accountType: "Plus",
		freeRoute:   "legacy",
		freeModel:   "auto",
		paidRoute:   "responses",
		paidModel:   "gpt-5.4-mini",
	}, compatTestServerOptions{
		accounts: []compatSeedAccount{
			{
				fileName:    "free-1.json",
				accessToken: "token-free-1",
				accountType: "Free",
				priority:    10,
				quota:       5,
				status:      "正常",
			},
			{
				fileName:    "free-2.json",
				accessToken: "token-free-2",
				accountType: "Free",
				priority:    9,
				quota:       5,
				status:      "正常",
			},
			{
				fileName:    "paid-1.json",
				accessToken: "token-paid-1",
				accountType: "Plus",
				priority:    8,
				quota:       5,
				status:      "正常",
			},
		},
	})

	policyHeader := base64.RawURLEncoding.EncodeToString([]byte(`{
		"enabled": true,
		"sortMode": "imported_at",
		"groupSize": 2,
		"enabledGroupIndexes": [0],
		"reserveMode": "daily_first_seen_percent",
		"reservePercent": 20
	}`))

	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(`{"prompt":"test prompt","size":"2560x1440","quality":"high","response_format":"b64_json"}`))
	req.Header.Set("Authorization", "Bearer "+server.cfg.App.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(imageAccountPolicyHeader, policyHeader)

	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if recorder.lastFactory != "responses" {
		t.Fatalf("last factory = %q, want responses", recorder.lastFactory)
	}
	if got := recorder.callSequence[len(recorder.callSequence)-1]; !strings.Contains(got, "token-paid-1") {
		t.Fatalf("call sequence = %v, want paid fallback selected", recorder.callSequence)
	}
	entries := server.reqLogs.list(1)
	if len(entries) != 1 {
		t.Fatalf("log entries = %d, want 1", len(entries))
	}
	if entries[0].RoutingPolicyApplied {
		t.Fatalf("expected fallback outside selected groups to skip policy-applied log flag, got %#v", entries[0])
	}
}
