package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"chatgpt2api/internal/imageassets"
	"chatgpt2api/internal/imagehistory"
)

type imageAssetView struct {
	ID              string                         `json:"id"`
	Title           string                         `json:"title"`
	Prompt          string                         `json:"prompt"`
	RevisedPrompt   string                         `json:"revisedPrompt,omitempty"`
	Mode            string                         `json:"mode"`
	Model           string                         `json:"model"`
	CreatedAt       string                         `json:"createdAt"`
	UpdatedAt       string                         `json:"updatedAt,omitempty"`
	ConversationID  string                         `json:"conversationId,omitempty"`
	TurnID          string                         `json:"turnId,omitempty"`
	ImageID         string                         `json:"imageId,omitempty"`
	Status          string                         `json:"status,omitempty"`
	ImageURL        string                         `json:"imageUrl,omitempty"`
	Filename        string                         `json:"filename,omitempty"`
	MIMEType        string                         `json:"mimeType,omitempty"`
	SizeBytes       int64                          `json:"sizeBytes,omitempty"`
	SHA256          string                         `json:"sha256,omitempty"`
	StorageKind     string                         `json:"storageKind,omitempty"`
	SourceKind      string                         `json:"sourceKind,omitempty"`
	OriginalURL     string                         `json:"originalUrl,omitempty"`
	FileID          string                         `json:"fileId,omitempty"`
	GenID           string                         `json:"genId,omitempty"`
	SourceAccountID string                         `json:"sourceAccountId,omitempty"`
	Category        string                         `json:"category,omitempty"`
	Tags            []string                       `json:"tags,omitempty"`
	SourceImages    []imageassets.AssetSourceImage `json:"sourceImages,omitempty"`
	Note            string                         `json:"note,omitempty"`
	Favorite        bool                           `json:"favorite,omitempty"`
}

type imageAssetTagStatView struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}

type imageAssetCategoryStatView struct {
	Category string `json:"category"`
	Count    int    `json:"count"`
}

type imageAssetImportFailureView struct {
	Name  string `json:"name"`
	Error string `json:"error"`
}

type imageAssetCleanupFileView struct {
	Filename string `json:"filename"`
	Path     string `json:"path,omitempty"`
}

type imageAssetCleanupResult struct {
	DryRun          bool                        `json:"dryRun"`
	OrphanFiles     []imageAssetCleanupFileView `json:"orphanFiles"`
	MissingAssets   []imageAssetView            `json:"missingAssets"`
	RemovedFiles    []string                    `json:"removedFiles"`
	RemovedAssetIDs []string                    `json:"removedAssetIds"`
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

func (s *Server) handleGetImageAsset(w http.ResponseWriter, r *http.Request) {
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

	item, err := store.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	if item == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "asset not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"item": buildImageAssetView(*item)})
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

func (s *Server) handleDeleteImageAsset(w http.ResponseWriter, r *http.Request) {
	store, err := imageassets.NewStore(s.cfg.RootDir())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	defer store.Close()

	item, err := store.Delete(r.Context(), r.PathValue("id"))
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(strings.ToLower(err.Error()), "not found") {
			status = http.StatusNotFound
		} else if strings.Contains(strings.ToLower(err.Error()), "required") {
			status = http.StatusBadRequest
		}
		writeJSON(w, status, map[string]any{"error": err.Error()})
		return
	}

	deletedFile := false
	if parseImageAssetBoolQuery(r, "delete_file") {
		removed, err := s.deleteImageAssetFilesIfUnreferenced(r.Context(), store, []imageassets.Asset{*item})
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		deletedFile = len(removed) > 0
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":          true,
		"item":        buildImageAssetView(*item),
		"deletedFile": deletedFile,
	})
}

func (s *Server) handleBulkDeleteImageAssets(w http.ResponseWriter, r *http.Request) {
	store, err := imageassets.NewStore(s.cfg.RootDir())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	defer store.Close()

	var body struct {
		IDs        []string `json:"ids"`
		DeleteFile bool     `json:"deleteFile"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request body"})
		return
	}

	items, err := store.DeleteBatch(r.Context(), body.IDs)
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(strings.ToLower(err.Error()), "required") || strings.Contains(strings.ToLower(err.Error()), "not found") {
			status = http.StatusBadRequest
		}
		writeJSON(w, status, map[string]any{"error": err.Error()})
		return
	}

	deletedFiles := []string{}
	if body.DeleteFile {
		deletedFiles, err = s.deleteImageAssetFilesIfUnreferenced(r.Context(), store, items)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
	}

	result := make([]imageAssetView, 0, len(items))
	for _, item := range items {
		result = append(result, buildImageAssetView(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":           true,
		"items":        result,
		"deletedFiles": deletedFiles,
	})
}

func (s *Server) deleteImageAssetFilesIfUnreferenced(ctx context.Context, store *imageassets.Store, items []imageassets.Asset) ([]string, error) {
	if len(items) == 0 {
		return []string{}, nil
	}
	referenced, err := store.ReferencedFiles(ctx)
	if err != nil {
		return nil, err
	}
	candidates := map[string]struct{}{}
	for _, item := range items {
		filename := imageAssetFilename(item)
		if filename == "" {
			continue
		}
		if _, stillUsed := referenced[filename]; stillUsed {
			continue
		}
		candidates[filename] = struct{}{}
	}
	return s.removeImageAssetFiles(candidates)
}

func (s *Server) cleanupImageAssets(
	ctx context.Context,
	store *imageassets.Store,
	dryRun bool,
	removeOrphanFiles bool,
	removeMissingFileAssets bool,
) (imageAssetCleanupResult, error) {
	result := imageAssetCleanupResult{
		DryRun:          dryRun,
		OrphanFiles:     []imageAssetCleanupFileView{},
		MissingAssets:   []imageAssetView{},
		RemovedFiles:    []string{},
		RemovedAssetIDs: []string{},
	}
	items, err := store.List(ctx)
	if err != nil {
		return result, err
	}
	referenced := map[string]struct{}{}
	for _, item := range items {
		filename := imageAssetFilename(item)
		if filename != "" {
			referenced[filename] = struct{}{}
		}
		if removeMissingFileAssets && filename != "" && !s.imageAssetFileExists(filename) {
			result.MissingAssets = append(result.MissingAssets, buildImageAssetView(item))
		}
	}
	if removeOrphanFiles {
		orphans, err := s.findOrphanImageAssetFiles(referenced)
		if err != nil {
			return result, err
		}
		result.OrphanFiles = orphans
	}
	if dryRun {
		return result, nil
	}
	if removeOrphanFiles {
		filesToRemove := map[string]struct{}{}
		for _, item := range result.OrphanFiles {
			filesToRemove[item.Filename] = struct{}{}
		}
		removed, err := s.removeImageAssetFiles(filesToRemove)
		if err != nil {
			return result, err
		}
		result.RemovedFiles = removed
	}
	if removeMissingFileAssets {
		ids := make([]string, 0, len(result.MissingAssets))
		for _, item := range result.MissingAssets {
			ids = append(ids, item.ID)
		}
		removedItems, err := store.DeleteExisting(ctx, ids)
		if err != nil {
			return result, err
		}
		for _, item := range removedItems {
			result.RemovedAssetIDs = append(result.RemovedAssetIDs, item.ID)
		}
		sort.Strings(result.RemovedAssetIDs)
	}
	return result, nil
}

func (s *Server) findOrphanImageAssetFiles(referenced map[string]struct{}) ([]imageAssetCleanupFileView, error) {
	result := []imageAssetCleanupFileView{}
	seen := map[string]struct{}{}
	for _, dir := range s.imageAssetCandidateDirs() {
		entries, err := os.ReadDir(dir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			filename := filepath.Base(entry.Name())
			if filename == "." || filename == "" || !isImageAssetFilename(filename) {
				continue
			}
			if _, ok := referenced[filename]; ok {
				continue
			}
			path := filepath.Join(dir, filename)
			cleanPath := filepath.Clean(path)
			if _, ok := seen[cleanPath]; ok {
				continue
			}
			seen[cleanPath] = struct{}{}
			result = append(result, imageAssetCleanupFileView{Filename: filename, Path: cleanPath})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Filename == result[j].Filename {
			return result[i].Path < result[j].Path
		}
		return result[i].Filename < result[j].Filename
	})
	return result, nil
}

func (s *Server) removeImageAssetFiles(files map[string]struct{}) ([]string, error) {
	if len(files) == 0 {
		return []string{}, nil
	}
	removed := []string{}
	seenRemoved := map[string]struct{}{}
	for filename := range files {
		base := filepath.Base(strings.TrimSpace(filename))
		if base == "" || base == "." {
			continue
		}
		for _, dir := range s.imageAssetCandidateDirs() {
			path := filepath.Join(dir, base)
			if err := os.Remove(path); err != nil {
				if errors.Is(err, os.ErrNotExist) {
					continue
				}
				return nil, err
			}
			if _, ok := seenRemoved[base]; ok {
				continue
			}
			seenRemoved[base] = struct{}{}
			removed = append(removed, base)
		}
	}
	sort.Strings(removed)
	return removed, nil
}

func (s *Server) imageAssetFileExists(filename string) bool {
	base := filepath.Base(strings.TrimSpace(filename))
	if base == "" || base == "." {
		return false
	}
	for _, dir := range s.imageAssetCandidateDirs() {
		info, err := os.Stat(filepath.Join(dir, base))
		if err == nil && info.Mode().IsRegular() {
			return true
		}
	}
	return false
}

func (s *Server) imageAssetCandidateDirs() []string {
	if s == nil || s.cfg == nil {
		return []string{}
	}
	primaryDir := s.cfg.ResolvePath(s.cfg.Storage.ImageDir)
	return imageassets.CandidateImageDirs(s.cfg.RootDir(), primaryDir)
}

func (s *Server) handleImportImageAssets(w http.ResponseWriter, r *http.Request) {
	reader, err := r.MultipartReader()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid multipart form"})
		return
	}

	store, err := imageassets.NewStore(s.cfg.RootDir())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	defer store.Close()

	imageDir := s.cfg.ResolvePath(s.cfg.Storage.ImageDir)
	maxImageBytes := int64(max(1, s.cfg.App.MaxUploadSizeMB)) << 20
	category := ""
	tags := []string{}
	note := ""
	files := []importedImageAssetPayload{}
	imported := make([]imageAssetView, 0, len(files))
	failures := []imageAssetImportFailureView{}

	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid multipart form"})
			return
		}
		fieldName := strings.TrimSpace(part.FormName())
		filename := strings.TrimSpace(part.FileName())
		if filename == "" {
			value, readErr := readMultipartFieldValue(part, 1<<20)
			if readErr != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": readErr.Error()})
				return
			}
			switch fieldName {
			case "category":
				category = strings.TrimSpace(value)
			case "tags":
				tags = splitImageAssetTags(value)
			case "note":
				note = strings.TrimSpace(value)
			}
			continue
		}
		if !isImageAssetMultipartFileField(fieldName) {
			continue
		}
		name := filename
		if name == "" {
			name = "image"
		}
		payload, tooLarge, readErr := readMultipartPartWithLimit(part, maxImageBytes)
		if tooLarge {
			failures = append(failures, imageAssetImportFailureView{Name: name, Error: "image exceeds max upload size"})
			continue
		}
		if readErr != nil {
			failures = append(failures, imageAssetImportFailureView{Name: name, Error: readErr.Error()})
			continue
		}
		files = append(files, importedImageAssetPayload{
			Name:        name,
			Payload:     payload,
			ContentType: strings.TrimSpace(part.Header.Get("Content-Type")),
		})
	}

	if len(files) == 0 && len(failures) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "at least one image file is required"})
		return
	}

	imported = make([]imageAssetView, 0, len(files))
	for _, file := range files {
		mimeType := detectImportedImageMIME(file.Payload, file.ContentType, file.Name)
		if mimeType == "" {
			failures = append(failures, imageAssetImportFailureView{Name: file.Name, Error: "file is not a supported image"})
			continue
		}
		info, err := imageassets.SaveImageBytes(imageDir, file.Payload, imageassets.FileSaveOptions{
			SourceKind:   "import",
			MIMEType:     mimeType,
			OriginalName: file.Name,
		})
		if err != nil {
			failures = append(failures, imageAssetImportFailureView{Name: file.Name, Error: err.Error()})
			continue
		}
		asset := imageassets.Asset{
			ID:          buildImportedImageAssetID(info.SHA256),
			Title:       importedImageAssetTitle(file.Name),
			Mode:        "import",
			Model:       "external",
			CreatedAt:   time.Now().UTC().Format(time.RFC3339Nano),
			Status:      "success",
			ImageURL:    info.URL,
			Filename:    info.Filename,
			MIMEType:    info.MIMEType,
			SizeBytes:   info.SizeBytes,
			SHA256:      info.SHA256,
			StorageKind: info.StorageKind,
			SourceKind:  info.SourceKind,
			OriginalURL: strings.TrimSpace(file.Name),
			Category:    category,
			Tags:        append([]string(nil), tags...),
			Note:        note,
		}
		saved, err := store.Save(r.Context(), asset)
		if err != nil {
			failures = append(failures, imageAssetImportFailureView{Name: file.Name, Error: err.Error()})
			continue
		}
		imported = append(imported, buildImageAssetView(*saved))
	}

	status := http.StatusOK
	if len(imported) == 0 && len(failures) > 0 {
		status = http.StatusBadRequest
	} else if len(failures) > 0 {
		status = http.StatusMultiStatus
	}
	writeJSON(w, status, map[string]any{
		"items":    imported,
		"imported": len(imported),
		"failed":   failures,
	})
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

func (s *Server) handleCleanupImageAssets(w http.ResponseWriter, r *http.Request) {
	store, err := imageassets.NewStore(s.cfg.RootDir())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	defer store.Close()

	var body struct {
		DryRun                  *bool `json:"dryRun"`
		RemoveOrphanFiles       *bool `json:"removeOrphanFiles"`
		RemoveMissingFileAssets *bool `json:"removeMissingFileAssets"`
	}
	if r.Body != nil {
		decoder := json.NewDecoder(r.Body)
		if err := decoder.Decode(&body); err != nil && !errors.Is(err, io.EOF) {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request body"})
			return
		}
	}

	dryRun := true
	if body.DryRun != nil {
		dryRun = *body.DryRun
	}
	removeOrphanFiles := true
	if body.RemoveOrphanFiles != nil {
		removeOrphanFiles = *body.RemoveOrphanFiles
	}
	removeMissingFileAssets := true
	if body.RemoveMissingFileAssets != nil {
		removeMissingFileAssets = *body.RemoveMissingFileAssets
	}

	result, err := s.cleanupImageAssets(r.Context(), store, dryRun, removeOrphanFiles, removeMissingFileAssets)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, result)
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
			Category:  conversation.Category,
			Tags:      append([]string(nil), conversation.Tags...),
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
				Category:        strings.TrimSpace(turn.Category),
				Tags:            append([]string(nil), turn.Tags...),
				SourceImages:    buildAssetSourceImagesFromHistory(turn.SourceImages),
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

func buildAssetSourceImagesFromHistory(items []imagehistory.SourceImage) []imageassets.AssetSourceImage {
	if len(items) == 0 {
		return nil
	}
	result := make([]imageassets.AssetSourceImage, 0, len(items))
	for _, item := range items {
		origin := buildAssetSourceOriginFromHistory(item.Source, item.URL)
		reference := imageassets.AssetSourceImage{
			ID:       cleanImageAssetID(item.ID),
			Role:     strings.TrimSpace(item.Role),
			Name:     strings.TrimSpace(item.Name),
			URL:      normalizeImageAssetURL(item.URL),
			Category: strings.TrimSpace(item.Category),
			Tags:     append([]string(nil), item.Tags...),
			Origin:   origin,
			Source:   origin,
		}
		if reference.ID == "" && reference.Role == "" && reference.Name == "" && reference.URL == "" && reference.Category == "" && len(reference.Tags) == 0 && reference.Origin == nil {
			continue
		}
		result = append(result, reference)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func buildAssetSourceOriginFromHistory(origin *imagehistory.ImageSourceOrigin, fallbackURL string) *imageassets.AssetSourceOrigin {
	if origin == nil {
		if trimmed := normalizeImageAssetURL(fallbackURL); trimmed != "" {
			return &imageassets.AssetSourceOrigin{
				Type:      "url",
				Confirmed: true,
				URL:       trimmed,
			}
		}
		return nil
	}
	copy := &imageassets.AssetSourceOrigin{
		Type:      strings.TrimSpace(origin.Type),
		Confirmed: origin.Confirmed,
		URL:       strings.TrimSpace(origin.URL),
		FilePath:  strings.TrimSpace(origin.FilePath),
	}
	if origin.Gallery != nil {
		copy.Gallery = &imageassets.AssetSourceGalleryReference{
			AssetID:        cleanImageAssetID(origin.Gallery.AssetID),
			Index:          origin.Gallery.Index,
			ConversationID: cleanImageAssetID(origin.Gallery.ConversationID),
			TurnID:         cleanImageAssetID(origin.Gallery.TurnID),
			ImageID:        cleanImageAssetID(origin.Gallery.ImageID),
		}
	}
	copy.URL = normalizeImageAssetURL(copy.URL)
	if copy.Type == "" {
		switch {
		case copy.Gallery != nil:
			copy.Type = "gallery"
		case copy.FilePath != "":
			copy.Type = "file"
		case copy.URL != "":
			copy.Type = "url"
		}
	}
	return copy
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
		SourceImages:    append([]imageassets.AssetSourceImage(nil), item.SourceImages...),
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

func imageAssetFilename(item imageassets.Asset) string {
	if filename := filepath.Base(strings.TrimSpace(item.Filename)); filename != "" && filename != "." {
		return filename
	}
	return imageassets.FilenameFromImageURL(item.ImageURL)
}

func isImageAssetFilename(filename string) bool {
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(filename)))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".webp", ".gif":
		return true
	default:
		return false
	}
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

func parseImageAssetBoolQuery(r *http.Request, key string) bool {
	if r == nil {
		return false
	}
	raw := strings.TrimSpace(strings.ToLower(r.URL.Query().Get(key)))
	return raw == "1" || raw == "true" || raw == "yes" || raw == "on"
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

func imageAssetMultipartFiles(form *multipart.Form) []*multipart.FileHeader {
	if form == nil {
		return nil
	}
	result := []*multipart.FileHeader{}
	for _, key := range []string{"images", "images[]", "image", "file", "files"} {
		result = append(result, form.File[key]...)
	}
	return result
}

type importedImageAssetPayload struct {
	Name        string
	Payload     []byte
	ContentType string
}

func isImageAssetMultipartFileField(name string) bool {
	switch strings.TrimSpace(name) {
	case "images", "images[]", "image", "file", "files":
		return true
	default:
		return false
	}
}

func readMultipartPartWithLimit(part *multipart.Part, maxBytes int64) ([]byte, bool, error) {
	if part == nil {
		return nil, false, fmt.Errorf("invalid multipart part")
	}
	limited := io.LimitReader(part, maxBytes+1)
	payload, err := io.ReadAll(limited)
	if err != nil {
		return nil, false, err
	}
	if int64(len(payload)) > maxBytes {
		return nil, true, nil
	}
	return payload, false, nil
}

func readMultipartFieldValue(part *multipart.Part, maxBytes int64) (string, error) {
	payload, tooLarge, err := readMultipartPartWithLimit(part, maxBytes)
	if err != nil {
		return "", err
	}
	if tooLarge {
		return "", fmt.Errorf("multipart field exceeds max size")
	}
	return string(payload), nil
}

func buildImportedImageAssetID(sha256 string) string {
	cleaned := strings.ToLower(strings.TrimSpace(sha256))
	if cleaned == "" {
		return cleanImageAssetID(fmt.Sprintf("import::%d", time.Now().UnixNano()))
	}
	return cleanImageAssetID("import::" + cleaned)
}

func importedImageAssetTitle(name string) string {
	base := filepath.Base(strings.TrimSpace(name))
	if base == "." || base == "" {
		return "导入图片"
	}
	ext := filepath.Ext(base)
	title := strings.TrimSpace(strings.TrimSuffix(base, ext))
	if title == "" {
		return base
	}
	return title
}

func splitImageAssetTags(raw string) []string {
	seen := map[string]struct{}{}
	result := []string{}
	for _, value := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '，' || r == '\n' || r == '\t'
	}) {
		cleaned := strings.TrimSpace(value)
		if cleaned == "" {
			continue
		}
		key := strings.ToLower(cleaned)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, cleaned)
	}
	sort.Strings(result)
	return result
}

func detectImportedImageMIME(payload []byte, preferred, name string) string {
	if len(payload) > 0 {
		detected := http.DetectContentType(payload)
		if !strings.HasPrefix(detected, "image/") {
			return ""
		}
		return imageassets.DetectImageMIME(payload, detected, name)
	}
	mimeType := imageassets.DetectImageMIME(payload, preferred, name)
	if !strings.HasPrefix(mimeType, "image/") {
		return ""
	}
	return mimeType
}
