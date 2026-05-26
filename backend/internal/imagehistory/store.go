package imagehistory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"chatgpt2api/internal/config"
	"chatgpt2api/internal/imageassets"

	"github.com/redis/go-redis/v9"
	_ "modernc.org/sqlite"
)

const (
	defaultHistoryDir = "data/image_history"
	defaultAssetMIME  = "image/png"
)

type SourceImage struct {
	ID       string             `json:"id"`
	Role     string             `json:"role"`
	Name     string             `json:"name"`
	DataURL  string             `json:"dataUrl,omitempty"`
	URL      string             `json:"url,omitempty"`
	Category string             `json:"category,omitempty"`
	Tags     []string           `json:"tags,omitempty"`
	Source   *ImageSourceOrigin `json:"source,omitempty"`
}

type ImageSourceOrigin struct {
	Type      string                       `json:"type,omitempty"`
	Confirmed bool                         `json:"confirmed,omitempty"`
	URL       string                       `json:"url,omitempty"`
	FilePath  string                       `json:"filePath,omitempty"`
	Gallery   *ImageSourceGalleryReference `json:"gallery,omitempty"`
}

type ImageSourceGalleryReference struct {
	AssetID        string `json:"assetId,omitempty"`
	Index          *int   `json:"index,omitempty"`
	ConversationID string `json:"conversationId,omitempty"`
	TurnID         string `json:"turnId,omitempty"`
	ImageID        string `json:"imageId,omitempty"`
}

func buildAssetSourceImages(items []SourceImage) []imageassets.AssetSourceImage {
	if len(items) == 0 {
		return nil
	}
	result := make([]imageassets.AssetSourceImage, 0, len(items))
	for _, item := range items {
		origin := buildAssetSourceOrigin(item.Source, item.URL)
		reference := imageassets.AssetSourceImage{
			ID:       cleanID(item.ID),
			Role:     strings.TrimSpace(item.Role),
			Name:     strings.TrimSpace(item.Name),
			URL:      strings.TrimSpace(item.URL),
			Category: strings.TrimSpace(item.Category),
			Tags:     normalizeStringList(item.Tags),
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

func buildAssetSourceOrigin(origin *ImageSourceOrigin, fallbackURL string) *imageassets.AssetSourceOrigin {
	normalized := normalizeSourceImageOrigin(origin, fallbackURL)
	if normalized == nil {
		return nil
	}
	result := &imageassets.AssetSourceOrigin{
		Type:      normalized.Type,
		Confirmed: normalized.Confirmed,
		URL:       normalized.URL,
		FilePath:  normalized.FilePath,
	}
	if normalized.Gallery != nil {
		result.Gallery = &imageassets.AssetSourceGalleryReference{
			AssetID:        normalized.Gallery.AssetID,
			Index:          normalized.Gallery.Index,
			ConversationID: normalized.Gallery.ConversationID,
			TurnID:         normalized.Gallery.TurnID,
			ImageID:        normalized.Gallery.ImageID,
		}
	}
	return result
}

type Image struct {
	ID              string `json:"id"`
	Status          string `json:"status,omitempty"`
	B64JSON         string `json:"b64_json,omitempty"`
	URL             string `json:"url,omitempty"`
	RevisedPrompt   string `json:"revised_prompt,omitempty"`
	FileID          string `json:"file_id,omitempty"`
	GenID           string `json:"gen_id,omitempty"`
	ConversationID  string `json:"conversation_id,omitempty"`
	ParentMessageID string `json:"parent_message_id,omitempty"`
	SourceAccountID string `json:"source_account_id,omitempty"`
	Error           string `json:"error,omitempty"`
}

type Turn struct {
	ID           string        `json:"id"`
	Title        string        `json:"title"`
	Mode         string        `json:"mode"`
	Prompt       string        `json:"prompt"`
	Model        string        `json:"model"`
	Count        int           `json:"count"`
	Size         string        `json:"size,omitempty"`
	Quality      string        `json:"quality,omitempty"`
	Scale        string        `json:"scale,omitempty"`
	Category     string        `json:"category,omitempty"`
	Tags         []string      `json:"tags,omitempty"`
	SourceImages []SourceImage `json:"sourceImages,omitempty"`
	Images       []Image       `json:"images"`
	CreatedAt    string        `json:"createdAt"`
	Status       string        `json:"status"`
	Error        string        `json:"error,omitempty"`
}

type Conversation struct {
	ID           string        `json:"id"`
	Title        string        `json:"title"`
	Mode         string        `json:"mode"`
	Prompt       string        `json:"prompt"`
	Model        string        `json:"model"`
	Count        int           `json:"count"`
	Size         string        `json:"size,omitempty"`
	Quality      string        `json:"quality,omitempty"`
	Scale        string        `json:"scale,omitempty"`
	Category     string        `json:"category,omitempty"`
	Tags         []string      `json:"tags,omitempty"`
	SourceImages []SourceImage `json:"sourceImages,omitempty"`
	Images       []Image       `json:"images"`
	CreatedAt    string        `json:"createdAt"`
	Status       string        `json:"status"`
	Error        string        `json:"error,omitempty"`
	Turns        []Turn        `json:"turns,omitempty"`
}

type Store struct {
	backend    backend
	imageDir   string
	rootDir    string
	assetStore *imageassets.Store
}

type backend interface {
	Init() error
	Close() error
	List(ctx context.Context) ([]Conversation, error)
	Get(ctx context.Context, id string) (*Conversation, error)
	Save(ctx context.Context, conversation Conversation) error
	Delete(ctx context.Context, id string) error
	Clear(ctx context.Context) error
}

func NewStore(cfg *config.Config) (*Store, error) {
	imageDir := cfg.ResolvePath(cfg.Storage.ImageDir)
	var storage backend
	switch strings.ToLower(strings.TrimSpace(cfg.Storage.Backend)) {
	case "sqlite":
		storage = &sqliteBackend{path: cfg.ResolvePath(cfg.Storage.SQLitePath)}
	case "redis":
		storage = &redisBackend{
			client: redis.NewClient(&redis.Options{
				Addr:     strings.TrimSpace(cfg.Storage.RedisAddr),
				Password: cfg.Storage.RedisPassword,
				DB:       cfg.Storage.RedisDB,
			}),
			prefix: firstNonEmpty(cfg.Storage.RedisPrefix, "chatgpt2api:studio") + ":image_history",
		}
	default:
		storage = &fileBackend{dir: cfg.ResolvePath(defaultHistoryDir)}
	}
	if err := storage.Init(); err != nil {
		_ = storage.Close()
		return nil, err
	}
	if err := os.MkdirAll(imageDir, 0o755); err != nil {
		_ = storage.Close()
		return nil, err
	}
	assetStore, err := imageassets.NewStore(cfg.RootDir())
	if err != nil {
		_ = storage.Close()
		return nil, err
	}
	return &Store{backend: storage, imageDir: imageDir, rootDir: cfg.RootDir(), assetStore: assetStore}, nil
}

func (s *Store) Close() error {
	if s == nil || s.backend == nil {
		return nil
	}
	var closeErr error
	if err := s.backend.Close(); err != nil {
		closeErr = err
	}
	if s.assetStore != nil {
		if err := s.assetStore.Close(); err != nil && closeErr == nil {
			closeErr = err
		}
	}
	return closeErr
}

func (s *Store) List(ctx context.Context) ([]Conversation, error) {
	items, err := s.backend.List(ctx)
	if err != nil {
		return nil, err
	}
	sortConversations(items)
	return items, nil
}

func (s *Store) Get(ctx context.Context, id string) (*Conversation, error) {
	return s.backend.Get(ctx, cleanID(id))
}

func (s *Store) Save(ctx context.Context, conversation Conversation) (*Conversation, error) {
	normalized, err := s.normalizeConversation(conversation)
	if err != nil {
		return nil, err
	}
	if err := s.backend.Save(ctx, normalized); err != nil {
		return nil, err
	}
	if err := s.syncAssets(ctx, normalized); err != nil {
		return nil, err
	}
	return &normalized, nil
}

func (s *Store) Delete(ctx context.Context, id string) error {
	current, err := s.backend.Get(ctx, cleanID(id))
	if err != nil || current == nil {
		return err
	}
	candidateFiles := collectConversationImageFiles(*current)
	candidateAssets := collectConversationAssetDeletes(*current)
	if err := s.backend.Delete(ctx, cleanID(id)); err != nil {
		return err
	}
	if s.assetStore != nil {
		if err := s.assetStore.DeleteAutoAssets(ctx, candidateAssets); err != nil {
			return err
		}
	}
	return s.cleanupCandidateFiles(ctx, candidateFiles)
}

func (s *Store) Clear(ctx context.Context) error {
	items, err := s.backend.List(ctx)
	if err != nil {
		return err
	}
	candidateFiles := map[string]struct{}{}
	candidateAssets := []imageassets.DeleteAutoOptions{}
	for _, item := range items {
		mergeFileSets(candidateFiles, collectConversationImageFiles(item))
		candidateAssets = append(candidateAssets, collectConversationAssetDeletes(item)...)
	}
	if err := s.backend.Clear(ctx); err != nil {
		return err
	}
	if s.assetStore != nil {
		if err := s.assetStore.DeleteAutoAssets(ctx, candidateAssets); err != nil {
			return err
		}
	}
	return s.cleanupCandidateFiles(ctx, candidateFiles)
}

func (s *Store) syncAssets(ctx context.Context, conversation Conversation) error {
	if s == nil || s.assetStore == nil {
		return nil
	}
	assets := s.extractImageAssets(conversation)
	if len(assets) == 0 {
		return nil
	}
	_, err := s.assetStore.SaveMany(ctx, assets)
	return err
}

func (s *Store) normalizeConversation(conversation Conversation) (Conversation, error) {
	conversation.ID = cleanID(conversation.ID)
	if conversation.ID == "" {
		return Conversation{}, fmt.Errorf("conversation id is required")
	}
	if conversation.CreatedAt == "" {
		conversation.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if len(conversation.Turns) == 0 {
		conversation.Turns = []Turn{{
			ID:           conversation.ID + "-legacy",
			Title:        conversation.Title,
			Mode:         conversation.Mode,
			Prompt:       conversation.Prompt,
			Model:        conversation.Model,
			Count:        conversation.Count,
			Size:         conversation.Size,
			Quality:      conversation.Quality,
			Scale:        conversation.Scale,
			Category:     conversation.Category,
			Tags:         append([]string(nil), conversation.Tags...),
			SourceImages: conversation.SourceImages,
			Images:       conversation.Images,
			CreatedAt:    conversation.CreatedAt,
			Status:       conversation.Status,
			Error:        conversation.Error,
		}}
	}
	for turnIndex := range conversation.Turns {
		turn := &conversation.Turns[turnIndex]
		if turn.ID == "" {
			turn.ID = fmt.Sprintf("%s-%d", conversation.ID, turnIndex)
		}
		if turn.CreatedAt == "" {
			turn.CreatedAt = conversation.CreatedAt
		}
		turn.Category = strings.TrimSpace(turn.Category)
		turn.Tags = normalizeStringList(turn.Tags)
		for sourceIndex := range turn.SourceImages {
			source := &turn.SourceImages[sourceIndex]
			if source.ID == "" {
				source.ID = fmt.Sprintf("%s-source-%d", turn.ID, sourceIndex)
			}
			source.Category = strings.TrimSpace(source.Category)
			source.Tags = normalizeStringList(source.Tags)
			source.Source = normalizeSourceImageOrigin(source.Source, source.URL)
			if source.URL == "" && strings.TrimSpace(source.DataURL) != "" {
				url, err := s.saveDataURLAsset(source.DataURL, "source", source.Name)
				if err != nil {
					return Conversation{}, err
				}
				source.URL = url
				source.DataURL = ""
			}
		}
		for imageIndex := range turn.Images {
			image := &turn.Images[imageIndex]
			if image.ID == "" {
				image.ID = fmt.Sprintf("%s-image-%d", turn.ID, imageIndex)
			}
			if image.URL == "" && strings.TrimSpace(image.B64JSON) != "" {
				url, err := s.saveBase64Asset(image.B64JSON, "result", defaultAssetMIME)
				if err != nil {
					return Conversation{}, err
				}
				image.URL = url
				image.B64JSON = ""
			}
			if image.Status == "" {
				if image.URL != "" {
					image.Status = "success"
				} else {
					image.Status = "loading"
				}
			}
		}
	}
	latest := conversation.Turns[len(conversation.Turns)-1]
	conversation.Title = latest.Title
	conversation.Mode = latest.Mode
	conversation.Prompt = latest.Prompt
	conversation.Model = latest.Model
	conversation.Count = latest.Count
	conversation.Size = latest.Size
	conversation.Quality = latest.Quality
	conversation.Scale = latest.Scale
	conversation.Category = latest.Category
	conversation.Tags = normalizeStringList(latest.Tags)
	conversation.SourceImages = latest.SourceImages
	conversation.Images = latest.Images
	conversation.CreatedAt = latest.CreatedAt
	conversation.Status = latest.Status
	conversation.Error = latest.Error
	return conversation, nil
}

func (s *Store) saveDataURLAsset(raw, kind, name string) (string, error) {
	info, err := imageassets.SaveImageDataURL(s.imageDir, raw, imageassets.FileSaveOptions{
		SourceKind:   kind,
		OriginalName: name,
	})
	return info.URL, err
}

func (s *Store) saveBase64Asset(raw, kind, mimeType string) (string, error) {
	info, err := imageassets.SaveImageBase64(s.imageDir, raw, imageassets.FileSaveOptions{
		SourceKind: kind,
		MIMEType:   firstNonEmpty(mimeType, defaultAssetMIME),
	})
	return info.URL, err
}

func collectConversationImageFiles(conversation Conversation) map[string]struct{} {
	files := map[string]struct{}{}
	collectSourceFiles := func(items []SourceImage) {
		for _, item := range items {
			if filename := filenameFromImageURL(item.URL); filename != "" {
				files[filename] = struct{}{}
			}
		}
	}
	collectResultFiles := func(items []Image) {
		for _, item := range items {
			if filename := filenameFromImageURL(item.URL); filename != "" {
				files[filename] = struct{}{}
			}
		}
	}
	collectSourceFiles(conversation.SourceImages)
	collectResultFiles(conversation.Images)
	for _, turn := range conversation.Turns {
		collectSourceFiles(turn.SourceImages)
		collectResultFiles(turn.Images)
	}
	return files
}

func collectConversationAssetDeletes(conversation Conversation) []imageassets.DeleteAutoOptions {
	result := []imageassets.DeleteAutoOptions{}
	turns := conversation.Turns
	if len(turns) == 0 {
		turns = []Turn{{
			ID:        conversation.ID + "-legacy",
			Images:    conversation.Images,
			CreatedAt: conversation.CreatedAt,
		}}
	}
	for _, turn := range turns {
		for _, image := range turn.Images {
			filename := filenameFromImageURL(image.URL)
			if filename == "" && image.ID == "" {
				continue
			}
			result = append(result, imageassets.DeleteAutoOptions{
				ConversationID: conversation.ID,
				TurnID:         turn.ID,
				ImageID:        image.ID,
				Filename:       filename,
			})
		}
	}
	return result
}

func filenameFromImageURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	index := strings.LastIndex(trimmed, "/v1/files/image/")
	if index >= 0 {
		return filepath.Base(trimmed[index+len("/v1/files/image/"):])
	}
	return filepath.Base(trimmed)
}

func mergeFileSets(target map[string]struct{}, source map[string]struct{}) {
	for key := range source {
		target[key] = struct{}{}
	}
}

func (s *Store) cleanupCandidateFiles(ctx context.Context, candidates map[string]struct{}) error {
	if len(candidates) == 0 {
		return nil
	}
	remainingItems, err := s.backend.List(ctx)
	if err != nil {
		return err
	}
	stillReferenced := map[string]struct{}{}
	for _, item := range remainingItems {
		mergeFileSets(stillReferenced, collectConversationImageFiles(item))
	}
	if s.assetStore != nil {
		assetFiles, err := s.assetStore.ReferencedFiles(ctx)
		if err != nil {
			return err
		}
		mergeFileSets(stillReferenced, assetFiles)
	}
	for filename := range candidates {
		if _, exists := stillReferenced[filename]; exists {
			continue
		}
		for _, dir := range imageassets.CandidateImageDirs(s.rootDir, s.imageDir) {
			path := filepath.Join(dir, filepath.Base(filename))
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
	}
	return nil
}

func (s *Store) extractImageAssets(conversation Conversation) []imageassets.Asset {
	turns := conversation.Turns
	if len(turns) == 0 {
		turns = []Turn{{
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

	result := make([]imageassets.Asset, 0)
	for _, turn := range turns {
		title := strings.TrimSpace(turn.Title)
		if title == "" {
			title = summarizeAssetPrompt(turn.Prompt)
		}
		for index, image := range turn.Images {
			if strings.TrimSpace(image.URL) == "" && strings.TrimSpace(image.B64JSON) == "" {
				continue
			}
			asset := imageassets.Asset{
				ID:              buildImageAssetID(conversation.ID, turn.ID, image.ID, index),
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
				SourceImages:    buildAssetSourceImages(turn.SourceImages),
			}
			asset = s.enrichAssetFileMetadata(asset)
			result = append(result, asset)
		}
	}
	return result
}

func (s *Store) enrichAssetFileMetadata(asset imageassets.Asset) imageassets.Asset {
	if s == nil {
		return asset
	}
	info := imageassets.InspectStoredImageURL(
		s.imageDir,
		imageassets.CandidateImageDirs(s.rootDir, s.imageDir),
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
	if trimmed := cleanID(imageID); trimmed != "" {
		return cleanID(strings.Join([]string{cleanID(conversationID), cleanID(turnID), trimmed}, "::"))
	}
	return cleanID(fmt.Sprintf("%s::%s::image-%d", cleanID(conversationID), cleanID(turnID), index))
}

func summarizeAssetPrompt(prompt string) string {
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

func sortConversations(items []Conversation) {
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].CreatedAt > items[j].CreatedAt
	})
}

func cleanID(id string) string {
	return strings.ReplaceAll(strings.TrimSpace(id), "/", "-")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func normalizeStringList(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	result := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func normalizeSourceImageOrigin(origin *ImageSourceOrigin, fallbackURL string) *ImageSourceOrigin {
	if origin == nil {
		if trimmed := strings.TrimSpace(fallbackURL); trimmed != "" {
			return &ImageSourceOrigin{
				Type:      "url",
				Confirmed: true,
				URL:       trimmed,
			}
		}
		return nil
	}
	copy := *origin
	copy.Type = strings.TrimSpace(copy.Type)
	copy.URL = strings.TrimSpace(copy.URL)
	copy.FilePath = strings.TrimSpace(copy.FilePath)
	if copy.Gallery != nil {
		gallery := *copy.Gallery
		gallery.AssetID = cleanID(gallery.AssetID)
		gallery.ConversationID = cleanID(gallery.ConversationID)
		gallery.TurnID = cleanID(gallery.TurnID)
		gallery.ImageID = cleanID(gallery.ImageID)
		if gallery.AssetID == "" && gallery.ConversationID == "" && gallery.TurnID == "" && gallery.ImageID == "" && gallery.Index == nil {
			copy.Gallery = nil
		} else {
			copy.Gallery = &gallery
		}
	}
	if copy.Type == "" {
		switch {
		case copy.Gallery != nil:
			copy.Type = "gallery"
		case copy.FilePath != "":
			copy.Type = "file"
		case copy.URL != "":
			copy.Type = "url"
		case strings.TrimSpace(fallbackURL) != "":
			copy.Type = "url"
			copy.URL = strings.TrimSpace(fallbackURL)
			copy.Confirmed = true
		}
	}
	switch copy.Type {
	case "gallery":
		if copy.Gallery == nil {
			return nil
		}
	case "file":
		if copy.FilePath == "" {
			return nil
		}
	case "url":
		if copy.URL == "" {
			if trimmed := strings.TrimSpace(fallbackURL); trimmed != "" {
				copy.URL = trimmed
				copy.Confirmed = true
			} else {
				return nil
			}
		}
	default:
		if copy.Gallery == nil && copy.FilePath == "" && copy.URL == "" {
			return nil
		}
	}
	return &copy
}

type fileBackend struct {
	dir string
}

func (b *fileBackend) Init() error {
	return os.MkdirAll(b.dir, 0o755)
}

func (b *fileBackend) Close() error {
	return nil
}

func (b *fileBackend) List(ctx context.Context) ([]Conversation, error) {
	entries, err := os.ReadDir(b.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []Conversation{}, nil
		}
		return nil, err
	}
	result := make([]Conversation, 0, len(entries))
	for _, entry := range entries {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		conversation, err := b.read(filepath.Join(b.dir, entry.Name()))
		if err != nil {
			continue
		}
		result = append(result, conversation)
	}
	return result, nil
}

func (b *fileBackend) Get(_ context.Context, id string) (*Conversation, error) {
	conversation, err := b.read(b.path(id))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	return &conversation, nil
}

func (b *fileBackend) Save(_ context.Context, conversation Conversation) error {
	if err := os.MkdirAll(b.dir, 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(conversation, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	tmp := b.path(conversation.ID) + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, b.path(conversation.ID))
}

func (b *fileBackend) Delete(_ context.Context, id string) error {
	err := os.Remove(b.path(id))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (b *fileBackend) Clear(_ context.Context) error {
	entries, err := os.ReadDir(b.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		if err := os.Remove(filepath.Join(b.dir, entry.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func (b *fileBackend) read(path string) (Conversation, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Conversation{}, err
	}
	var conversation Conversation
	if err := json.Unmarshal(raw, &conversation); err != nil {
		return Conversation{}, err
	}
	return conversation, nil
}

func (b *fileBackend) path(id string) string {
	return filepath.Join(b.dir, cleanID(id)+".json")
}

type sqliteBackend struct {
	path string
	db   *sql.DB
}

func (b *sqliteBackend) Init() error {
	if err := os.MkdirAll(filepath.Dir(b.path), 0o755); err != nil {
		return err
	}
	db, err := sql.Open("sqlite", b.path)
	if err != nil {
		return err
	}
	b.db = db
	_, err = b.db.Exec(`CREATE TABLE IF NOT EXISTS image_conversations (id TEXT PRIMARY KEY, raw_json BLOB NOT NULL, updated_at TEXT NOT NULL);`)
	return err
}

func (b *sqliteBackend) Close() error {
	if b.db == nil {
		return nil
	}
	return b.db.Close()
}

func (b *sqliteBackend) List(_ context.Context) ([]Conversation, error) {
	rows, err := b.db.Query(`SELECT raw_json FROM image_conversations ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Conversation{}
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var conversation Conversation
		if err := json.Unmarshal(raw, &conversation); err != nil {
			continue
		}
		result = append(result, conversation)
	}
	return result, rows.Err()
}

func (b *sqliteBackend) Get(_ context.Context, id string) (*Conversation, error) {
	var raw []byte
	err := b.db.QueryRow(`SELECT raw_json FROM image_conversations WHERE id = ?`, id).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var conversation Conversation
	if err := json.Unmarshal(raw, &conversation); err != nil {
		return nil, err
	}
	return &conversation, nil
}

func (b *sqliteBackend) Save(_ context.Context, conversation Conversation) error {
	raw, err := json.Marshal(conversation)
	if err != nil {
		return err
	}
	_, err = b.db.Exec(
		`INSERT INTO image_conversations(id, raw_json, updated_at) VALUES(?, ?, ?) ON CONFLICT(id) DO UPDATE SET raw_json = excluded.raw_json, updated_at = excluded.updated_at`,
		conversation.ID,
		raw,
		conversation.CreatedAt,
	)
	return err
}

func (b *sqliteBackend) Delete(_ context.Context, id string) error {
	_, err := b.db.Exec(`DELETE FROM image_conversations WHERE id = ?`, id)
	return err
}

func (b *sqliteBackend) Clear(_ context.Context) error {
	_, err := b.db.Exec(`DELETE FROM image_conversations`)
	return err
}

type redisBackend struct {
	client *redis.Client
	prefix string
}

func (b *redisBackend) Init() error {
	return b.client.Ping(context.Background()).Err()
}

func (b *redisBackend) Close() error {
	if b.client == nil {
		return nil
	}
	return b.client.Close()
}

func (b *redisBackend) List(ctx context.Context) ([]Conversation, error) {
	values, err := b.client.HGetAll(ctx, b.key("conversations")).Result()
	if err != nil {
		return nil, err
	}
	result := make([]Conversation, 0, len(values))
	for _, raw := range values {
		var conversation Conversation
		if err := json.Unmarshal([]byte(raw), &conversation); err != nil {
			continue
		}
		result = append(result, conversation)
	}
	return result, nil
}

func (b *redisBackend) Get(ctx context.Context, id string) (*Conversation, error) {
	raw, err := b.client.HGet(ctx, b.key("conversations"), id).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var conversation Conversation
	if err := json.Unmarshal([]byte(raw), &conversation); err != nil {
		return nil, err
	}
	return &conversation, nil
}

func (b *redisBackend) Save(ctx context.Context, conversation Conversation) error {
	raw, err := json.Marshal(conversation)
	if err != nil {
		return err
	}
	return b.client.HSet(ctx, b.key("conversations"), conversation.ID, raw).Err()
}

func (b *redisBackend) Delete(ctx context.Context, id string) error {
	return b.client.HDel(ctx, b.key("conversations"), id).Err()
}

func (b *redisBackend) Clear(ctx context.Context) error {
	return b.client.Del(ctx, b.key("conversations")).Err()
}

func (b *redisBackend) key(name string) string {
	return b.prefix + ":" + name
}
