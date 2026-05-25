package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"chatgpt2api/internal/imageassets"
	"chatgpt2api/internal/imagehistory"
)

type imageAssetView struct {
	ID              string   `json:"id"`
	Title           string   `json:"title"`
	Prompt          string   `json:"prompt"`
	RevisedPrompt   string   `json:"revisedPrompt,omitempty"`
	Mode            string   `json:"mode"`
	Model           string   `json:"model"`
	CreatedAt       string   `json:"createdAt"`
	UpdatedAt       string   `json:"updatedAt,omitempty"`
	ConversationID  string   `json:"conversationId,omitempty"`
	TurnID          string   `json:"turnId,omitempty"`
	ImageID         string   `json:"imageId,omitempty"`
	Status          string   `json:"status,omitempty"`
	ImageURL        string   `json:"imageUrl,omitempty"`
	Filename        string   `json:"filename,omitempty"`
	MIMEType        string   `json:"mimeType,omitempty"`
	SizeBytes       int64    `json:"sizeBytes,omitempty"`
	SHA256          string   `json:"sha256,omitempty"`
	StorageKind     string   `json:"storageKind,omitempty"`
	SourceKind      string   `json:"sourceKind,omitempty"`
	OriginalURL     string   `json:"originalUrl,omitempty"`
	FileID          string   `json:"fileId,omitempty"`
	GenID           string   `json:"genId,omitempty"`
	SourceAccountID string   `json:"sourceAccountId,omitempty"`
	Category        string   `json:"category,omitempty"`
	Tags            []string `json:"tags,omitempty"`
	Note            string   `json:"note,omitempty"`
	Favorite        bool     `json:"favorite,omitempty"`
}

type imageAssetTagStatView struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}

type imageAssetCategoryStatView struct {
	Category string `json:"category"`
	Count    int    `json:"count"`
}

func (s *Server) handleListImageAssets(w http.ResponseWriter, r *http.Request) {
	store, err := imageassets.NewStore(s.cfg.RootDir())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	defer store.Close()

	if err := s.syncImageAssetsFromHistory(r, store); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}

	query := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("query")))
	category := strings.TrimSpace(r.URL.Query().Get("category"))
	tag := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("tag")))
	favoriteOnly := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("favorite")), "true")
	limit := parseImageAssetIntQuery(r, "limit", 48)
	offset := parseImageAssetIntQuery(r, "offset", 0)
	sort := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("sort")))

	result, err := store.ListPage(r.Context(), imageassets.FilterOptions{
		Query:        query,
		Category:     category,
		Tag:          tag,
		FavoriteOnly: favoriteOnly,
		Limit:        limit,
		Offset:       offset,
		Sort:         sort,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}

	filtered := make([]imageAssetView, 0, len(result.Items))
	categories := map[string]struct{}{}
	tags := map[string]struct{}{}

	for _, item := range result.Items {
		view := buildImageAssetView(item)
		filtered = append(filtered, view)
		if strings.TrimSpace(view.Category) != "" {
			categories[view.Category] = struct{}{}
		}
		for _, itemTag := range view.Tags {
			tags[itemTag] = struct{}{}
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items":      filtered,
		"categories": mapKeys(categories),
		"tags":       mapKeys(tags),
		"total":      result.Total,
		"hasMore":    result.HasMore,
		"limit":      result.Limit,
		"offset":     result.Offset,
		"nextOffset": result.NextOffset,
		"sort":       sort,
	})
}

func (s *Server) handleUpdateImageAsset(w http.ResponseWriter, r *http.Request) {
	store, err := imageassets.NewStore(s.cfg.RootDir())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	defer store.Close()

	var body imageassets.MetadataPatch
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request body"})
		return
	}

	item, err := store.UpdateMetadata(r.Context(), r.PathValue("id"), body)
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(strings.ToLower(err.Error()), "not found") {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]any{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"item": buildImageAssetView(*item)})
}

func (s *Server) handleBulkUpdateImageAssets(w http.ResponseWriter, r *http.Request) {
	store, err := imageassets.NewStore(s.cfg.RootDir())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	defer store.Close()

	var body imageassets.BulkMetadataPatch
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request body"})
		return
	}

	items, err := store.UpdateMetadataBatch(r.Context(), body)
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(strings.ToLower(err.Error()), "required") || strings.Contains(strings.ToLower(err.Error()), "not found") {
			status = http.StatusBadRequest
		}
		writeJSON(w, status, map[string]any{"error": err.Error()})
		return
	}

	result := make([]imageAssetView, 0, len(items))
	for _, item := range items {
		result = append(result, buildImageAssetView(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": result})
}

func (s *Server) handleImageAssetStats(w http.ResponseWriter, r *http.Request) {
	store, err := imageassets.NewStore(s.cfg.RootDir())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	defer store.Close()

	tagStats, err := store.TagStats(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	categoryStats, err := store.CategoryStats(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}

	tagItems := make([]imageAssetTagStatView, 0, len(tagStats))
	for _, item := range tagStats {
		tagItems = append(tagItems, imageAssetTagStatView{Tag: item.Tag, Count: item.Count})
	}
	categoryItems := make([]imageAssetCategoryStatView, 0, len(categoryStats))
	for _, item := range categoryStats {
		categoryItems = append(categoryItems, imageAssetCategoryStatView{
			Category: item.Category,
			Count:    item.Count,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"tags":       tagItems,
		"categories": categoryItems,
	})
}

func (s *Server) handleManageImageAssetTags(w http.ResponseWriter, r *http.Request) {
	store, err := imageassets.NewStore(s.cfg.RootDir())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	defer store.Close()

	var body struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request body"})
		return
	}

	switch r.Method {
	case http.MethodPatch:
		if err := store.RenameTag(r.Context(), body.From, body.To); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
	case http.MethodDelete:
		if err := store.DeleteTag(r.Context(), body.From); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleManageImageAssetCategories(w http.ResponseWriter, r *http.Request) {
	store, err := imageassets.NewStore(s.cfg.RootDir())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	defer store.Close()

	var body struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request body"})
		return
	}

	switch r.Method {
	case http.MethodPatch:
		if err := store.RenameCategory(r.Context(), body.From, body.To); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
	case http.MethodDelete:
		if err := store.DeleteCategory(r.Context(), body.From); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleSyncImageAssets(w http.ResponseWriter, r *http.Request) {
	store, err := imageassets.NewStore(s.cfg.RootDir())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	defer store.Close()

	var body struct {
		Items []imageassets.Asset `json:"items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request body"})
		return
	}
	items, err := store.SaveMany(r.Context(), body.Items)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	result := make([]imageAssetView, 0, len(items))
	for _, item := range items {
		result = append(result, buildImageAssetView(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": result})
}

func (s *Server) syncImageAssetsFromHistory(r *http.Request, store *imageassets.Store) error {
	if store == nil {
		return nil
	}
	historyStore, err := imagehistory.NewStore(s.cfg)
	if err != nil {
		return err
	}
	defer historyStore.Close()

	conversations, err := historyStore.List(r.Context())
	if err != nil {
		return err
	}
	if len(conversations) == 0 {
		return nil
	}

	assets := make([]imageassets.Asset, 0)
	for _, conversation := range conversations {
		assets = append(assets, s.extractImageAssetsFromConversation(conversation)...)
	}
	_, err = store.SaveMany(r.Context(), assets)
	return err
}

func (s *Server) extractImageAssetsFromConversation(conversation imagehistory.Conversation) []imageassets.Asset {
	turns := conversation.Turns
	if len(turns) == 0 {
		turns = []imagehistory.Turn{{
			ID:        conversation.ID + "-legacy",
			Title:     conversation.Title,
			Mode:      conversation.Mode,
			Prompt:    conversation.Prompt,
			Model:     conversation.Model,
			Count:     conversation.Count,
			Images:    conversation.Images,
			CreatedAt: conversation.CreatedAt,
			Status:    conversation.Status,
			Error:     conversation.Error,
		}}
	}

	items := make([]imageassets.Asset, 0)
	for _, turn := range turns {
		title := strings.TrimSpace(turn.Title)
		if title == "" {
			title = summarizePrompt(turn.Prompt)
		}
		for index, image := range turn.Images {
			if strings.TrimSpace(image.URL) == "" && strings.TrimSpace(image.B64JSON) == "" {
				continue
			}
			assetID := buildImageAssetID(conversation.ID, turn.ID, image.ID, index)
			asset := imageassets.Asset{
				ID:              assetID,
				Title:           title,
				Prompt:          strings.TrimSpace(turn.Prompt),
				RevisedPrompt:   strings.TrimSpace(image.RevisedPrompt),
				Mode:            strings.TrimSpace(turn.Mode),
				Model:           strings.TrimSpace(turn.Model),
				CreatedAt:       firstNonEmpty(strings.TrimSpace(turn.CreatedAt), strings.TrimSpace(conversation.CreatedAt)),
				ConversationID:  strings.TrimSpace(conversation.ID),
				TurnID:          strings.TrimSpace(turn.ID),
				ImageID:         strings.TrimSpace(image.ID),
				Status:          strings.TrimSpace(image.Status),
				ImageURL:        strings.TrimSpace(image.URL),
				ImageB64JSON:    strings.TrimSpace(image.B64JSON),
				FileID:          strings.TrimSpace(image.FileID),
				GenID:           strings.TrimSpace(image.GenID),
				SourceAccountID: strings.TrimSpace(image.SourceAccountID),
			}
			asset = s.enrichImageAssetFileMetadata(asset)
			items = append(items, asset)
		}
	}
	return items
}

func (s *Server) enrichImageAssetFileMetadata(asset imageassets.Asset) imageassets.Asset {
	if s == nil || s.cfg == nil {
		return asset
	}
	info := imageassets.InspectStoredImageURL(
		s.cfg.ResolvePath(s.cfg.Storage.ImageDir),
		imageassets.CandidateImageDirs(s.cfg.RootDir(), s.cfg.ResolvePath(s.cfg.Storage.ImageDir)),
		asset.ImageURL,
	)
	if info.Filename == "" {
		return asset
	}
	asset.Filename = info.Filename
	asset.MIMEType = info.MIMEType
	asset.SizeBytes = info.SizeBytes
	asset.SHA256 = info.SHA256
	asset.StorageKind = info.StorageKind
	if asset.SourceKind == "" {
		asset.SourceKind = info.SourceKind
	}
	if asset.OriginalURL == "" {
		asset.OriginalURL = info.OriginalURL
	}
	return asset
}

func buildImageAssetID(conversationID, turnID, imageID string, index int) string {
	if strings.TrimSpace(imageID) != "" {
		return cleanImageAssetID(strings.Join([]string{
			strings.TrimSpace(conversationID),
			strings.TrimSpace(turnID),
			strings.TrimSpace(imageID),
		}, "::"))
	}
	return cleanImageAssetID(fmt.Sprintf("%s::%s::image-%d", strings.TrimSpace(conversationID), strings.TrimSpace(turnID), index))
}

func cleanImageAssetID(value string) string {
	cleaned := strings.TrimSpace(value)
	cleaned = strings.ReplaceAll(cleaned, "/", "-")
	cleaned = strings.ReplaceAll(cleaned, "\\", "-")
	return cleaned
}

func summarizePrompt(prompt string) string {
	cleaned := strings.TrimSpace(prompt)
	if cleaned == "" {
		return "未命名图片"
	}
	runes := []rune(cleaned)
	if len(runes) <= 32 {
		return cleaned
	}
	return string(runes[:32]) + "..."
}

func buildImageAssetView(item imageassets.Asset) imageAssetView {
	return imageAssetView{
		ID:              item.ID,
		Title:           item.Title,
		Prompt:          item.Prompt,
		RevisedPrompt:   item.RevisedPrompt,
		Mode:            item.Mode,
		Model:           item.Model,
		CreatedAt:       item.CreatedAt,
		UpdatedAt:       item.UpdatedAt,
		ConversationID:  item.ConversationID,
		TurnID:          item.TurnID,
		ImageID:         item.ImageID,
		Status:          item.Status,
		ImageURL:        normalizeImageAssetURL(item.ImageURL),
		Filename:        item.Filename,
		MIMEType:        item.MIMEType,
		SizeBytes:       item.SizeBytes,
		SHA256:          item.SHA256,
		StorageKind:     item.StorageKind,
		SourceKind:      item.SourceKind,
		OriginalURL:     item.OriginalURL,
		FileID:          item.FileID,
		GenID:           item.GenID,
		SourceAccountID: item.SourceAccountID,
		Category:        item.Category,
		Tags:            append([]string(nil), item.Tags...),
		Note:            item.Note,
		Favorite:        item.Favorite,
	}
}

func normalizeImageAssetURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "/") {
		return trimmed
	}
	index := strings.Index(trimmed, "/v1/files/image/")
	if index >= 0 {
		return trimmed[index:]
	}
	return trimmed
}

func mapKeys(values map[string]struct{}) []string {
	items := make([]string, 0, len(values))
	for value := range values {
		if strings.TrimSpace(value) != "" {
			items = append(items, value)
		}
	}
	sort.Strings(items)
	return items
}

func parseImageAssetIntQuery(r *http.Request, key string, fallback int) int {
	if r == nil {
		return fallback
	}
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return fallback
	}
	var value int
	if _, err := fmt.Sscanf(raw, "%d", &value); err != nil {
		return fallback
	}
	return value
}
