package imageassets

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const defaultAssetSQLitePath = "data/image-assets.db"

type Asset struct {
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
	ImageB64JSON    string   `json:"imageB64Json,omitempty"`
	FileID          string   `json:"fileId,omitempty"`
	GenID           string   `json:"genId,omitempty"`
	SourceAccountID string   `json:"sourceAccountId,omitempty"`
	Category        string   `json:"category,omitempty"`
	Tags            []string `json:"tags,omitempty"`
	Note            string   `json:"note,omitempty"`
	Favorite        bool     `json:"favorite,omitempty"`
}

type MetadataPatch struct {
	Category *string   `json:"category,omitempty"`
	Tags     *[]string `json:"tags,omitempty"`
	Note     *string   `json:"note,omitempty"`
	Favorite *bool     `json:"favorite,omitempty"`
}

type BulkMetadataPatch struct {
	IDs      []string  `json:"ids"`
	Category *string   `json:"category,omitempty"`
	Tags     *[]string `json:"tags,omitempty"`
	Note     *string   `json:"note,omitempty"`
	Favorite *bool     `json:"favorite,omitempty"`
}

type FilterOptions struct {
	Query        string
	Category     string
	Tag          string
	FavoriteOnly bool
	Limit        int
	Offset       int
	Sort         string
}

type TagStat struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}

type CategoryStat struct {
	Category string `json:"category"`
	Count    int    `json:"count"`
}

type ListResult struct {
	Items      []Asset
	HasMore    bool
	Total      int
	Limit      int
	Offset     int
	NextOffset int
}

type Store struct {
	db *sql.DB
}

type sortMode string

const (
	sortCreatedDesc sortMode = "created_desc"
	sortCreatedAsc  sortMode = "created_asc"
	sortUpdatedDesc sortMode = "updated_desc"
	sortTitleAsc    sortMode = "title_asc"
	sortTitleDesc   sortMode = "title_desc"
	sortFavorite    sortMode = "favorite"
)

func NewStore(rootDir string) (*Store, error) {
	dbPath := resolveAssetSQLitePath(rootDir)
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	store := &Store{db: db}
	if err := store.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func resolveAssetSQLitePath(rootDir string) string {
	trimmed := strings.TrimSpace(rootDir)
	if trimmed == "" {
		trimmed = "."
	}
	return filepath.Join(trimmed, filepath.FromSlash(defaultAssetSQLitePath))
}

func (s *Store) init() error {
	if s == nil || s.db == nil {
		return fmt.Errorf("asset store is unavailable")
	}
	if err := s.resetIncompatibleSchema(); err != nil {
		return err
	}
	statements := []string{
		`CREATE TABLE IF NOT EXISTS image_assets (
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
		);`,
		`CREATE TABLE IF NOT EXISTS image_asset_tags (
			asset_id TEXT NOT NULL,
			tag TEXT NOT NULL,
			PRIMARY KEY(asset_id, tag),
			FOREIGN KEY(asset_id) REFERENCES image_assets(id) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS idx_image_assets_created_at ON image_assets(created_at DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_image_assets_category ON image_assets(category);`,
		`CREATE INDEX IF NOT EXISTS idx_image_assets_favorite ON image_assets(favorite, created_at DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_image_asset_tags_tag ON image_asset_tags(tag);`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS image_assets_fts USING fts5(
			asset_id UNINDEXED,
			title,
			prompt,
			revised_prompt,
			note,
			tokenize = 'unicode61'
		);`,
	}
	for _, statement := range statements {
		if _, err := s.db.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) resetIncompatibleSchema() error {
	if s == nil || s.db == nil {
		return fmt.Errorf("asset store is unavailable")
	}
	compatible, err := s.hasCompatibleAssetSchema()
	if err != nil {
		return err
	}
	if compatible {
		return nil
	}
	statements := []string{
		`DROP TABLE IF EXISTS image_asset_tags;`,
		`DROP TABLE IF EXISTS image_assets_fts;`,
		`DROP TABLE IF EXISTS image_assets;`,
	}
	for _, statement := range statements {
		if _, err := s.db.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) hasCompatibleAssetSchema() (bool, error) {
	if s == nil || s.db == nil {
		return false, fmt.Errorf("asset store is unavailable")
	}
	exists, err := tableExists(s.db, "image_assets")
	if err != nil || !exists {
		return !exists, err
	}
	assetColumns, err := tableColumns(s.db, "image_assets")
	if err != nil {
		return false, err
	}
	requiredAssetColumns := []string{
		"id",
		"title",
		"prompt",
		"revised_prompt",
		"mode",
		"model",
		"created_at",
		"updated_at",
		"conversation_id",
		"turn_id",
		"image_id",
		"status",
		"image_url",
		"image_b64_json",
		"file_id",
		"gen_id",
		"source_account_id",
		"category",
		"note",
		"favorite",
	}
	if !containsColumns(assetColumns, requiredAssetColumns) {
		return false, nil
	}

	tagExists, err := tableExists(s.db, "image_asset_tags")
	if err != nil || !tagExists {
		return !tagExists, err
	}
	tagColumns, err := tableColumns(s.db, "image_asset_tags")
	if err != nil {
		return false, err
	}
	if !containsColumns(tagColumns, []string{"asset_id", "tag"}) {
		return false, nil
	}

	ftsExists, err := tableExists(s.db, "image_assets_fts")
	if err != nil || !ftsExists {
		return !ftsExists, err
	}
	ftsColumns, err := tableColumns(s.db, "image_assets_fts")
	if err != nil {
		return false, err
	}
	if !containsColumns(ftsColumns, []string{"asset_id", "title", "prompt", "revised_prompt", "note"}) {
		return false, nil
	}
	return true, nil
}

func tableExists(db *sql.DB, name string) (bool, error) {
	var count int
	err := db.QueryRow(
		`SELECT COUNT(1) FROM sqlite_master WHERE type = 'table' AND name = ?`,
		strings.TrimSpace(name),
	).Scan(&count)
	return count > 0, err
}

func tableColumns(db *sql.DB, tableName string) ([]string, error) {
	name := strings.TrimSpace(tableName)
	if name == "" {
		return nil, fmt.Errorf("table name is required")
	}
	rows, err := db.Query(fmt.Sprintf(`PRAGMA table_info("%s")`, strings.ReplaceAll(name, `"`, `""`)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := []string{}
	for rows.Next() {
		var (
			cid        int
			columnName string
			columnType string
			notNull    int
			defaultVal sql.NullString
			primaryKey int
		)
		if err := rows.Scan(&cid, &columnName, &columnType, &notNull, &defaultVal, &primaryKey); err != nil {
			return nil, err
		}
		result = append(result, strings.ToLower(strings.TrimSpace(columnName)))
	}
	return result, rows.Err()
}

func containsColumns(columns []string, required []string) bool {
	for _, name := range required {
		if !slices.Contains(columns, strings.ToLower(strings.TrimSpace(name))) {
			return false
		}
	}
	return true
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) List(ctx context.Context) ([]Asset, error) {
	return s.ListFiltered(ctx, FilterOptions{})
}

func (s *Store) ListFiltered(ctx context.Context, filter FilterOptions) ([]Asset, error) {
	result, err := s.ListPage(ctx, filter)
	if err != nil {
		return nil, err
	}
	return result.Items, nil
}

func (s *Store) ListPage(ctx context.Context, filter FilterOptions) (ListResult, error) {
	if s == nil || s.db == nil {
		return ListResult{Items: []Asset{}}, nil
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 48
	}
	if limit > 200 {
		limit = 200
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}
	orderBy := normalizeSortMode(filter.Sort).orderByClause()
	query := `
		SELECT DISTINCT
			a.id, a.title, a.prompt, a.revised_prompt, a.mode, a.model, a.created_at, a.updated_at,
			a.conversation_id, a.turn_id, a.image_id, a.status, a.image_url, a.image_b64_json,
			a.file_id, a.gen_id, a.source_account_id, a.category, a.note, a.favorite
		FROM image_assets a`
	joins := []string{}
	conditions := []string{}
	args := []any{}

	if cleanedTag := strings.TrimSpace(filter.Tag); cleanedTag != "" {
		joins = append(joins, `JOIN image_asset_tags ft ON ft.asset_id = a.id`)
		conditions = append(conditions, `ft.tag = ?`)
		args = append(args, cleanedTag)
	}
	if cleanedQuery := strings.TrimSpace(filter.Query); cleanedQuery != "" {
		searchConditionParts := []string{
			`a.title LIKE ? ESCAPE '\'`,
			`a.prompt LIKE ? ESCAPE '\'`,
			`a.revised_prompt LIKE ? ESCAPE '\'`,
			`a.note LIKE ? ESCAPE '\'`,
			`a.category LIKE ? ESCAPE '\'`,
			`EXISTS (SELECT 1 FROM image_asset_tags search_tags WHERE search_tags.asset_id = a.id AND search_tags.tag LIKE ? ESCAPE '\')`,
		}
		searchArgs := make([]any, 0, len(searchConditionParts)+1)
		likePattern := buildLikePattern(cleanedQuery)
		for range searchConditionParts {
			searchArgs = append(searchArgs, likePattern)
		}
		if ftsQuery := buildFTSMatchQuery(cleanedQuery); ftsQuery != "" {
			searchConditionParts = append(
				[]string{`EXISTS (SELECT 1 FROM image_assets_fts fts WHERE fts.asset_id = a.id AND fts.image_assets_fts MATCH ?)`},
				searchConditionParts...,
			)
			searchArgs = append([]any{ftsQuery}, searchArgs...)
		}
		conditions = append(conditions, "("+strings.Join(searchConditionParts, " OR ")+")")
		args = append(args, searchArgs...)
	}
	if cleanedCategory := strings.TrimSpace(filter.Category); cleanedCategory != "" {
		conditions = append(conditions, `a.category = ?`)
		args = append(args, cleanedCategory)
	}
	if filter.FavoriteOnly {
		conditions = append(conditions, `a.favorite = 1`)
	}
	if len(joins) > 0 {
		query += "\n" + strings.Join(joins, "\n")
	}
	if len(conditions) > 0 {
		query += "\nWHERE " + strings.Join(conditions, " AND ")
	}
	countQuery := "SELECT COUNT(DISTINCT a.id) FROM image_assets a"
	if len(joins) > 0 {
		countQuery += "\n" + strings.Join(joins, "\n")
	}
	if len(conditions) > 0 {
		countQuery += "\nWHERE " + strings.Join(conditions, " AND ")
	}

	var total int
	if err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return ListResult{}, err
	}

	query += "\nORDER BY " + orderBy + "\nLIMIT ? OFFSET ?"
	args = append(args, limit+1, offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return ListResult{}, err
	}
	defer rows.Close()

	result := []Asset{}
	for rows.Next() {
		item, err := scanAssetRow(rows)
		if err != nil {
			return ListResult{}, err
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return ListResult{}, err
	}
	if err := s.attachTags(ctx, result); err != nil {
		return ListResult{}, err
	}
	hasMore := len(result) > limit
	if hasMore {
		result = result[:limit]
	}
	nextOffset := offset + len(result)
	if !hasMore {
		nextOffset = offset
	}
	return ListResult{
		Items:      result,
		HasMore:    hasMore,
		Total:      total,
		Limit:      limit,
		Offset:     offset,
		NextOffset: nextOffset,
	}, nil
}

func (s *Store) Get(ctx context.Context, id string) (*Asset, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT id, title, prompt, revised_prompt, mode, model, created_at, updated_at,
		       conversation_id, turn_id, image_id, status, image_url, image_b64_json,
		       file_id, gen_id, source_account_id, category, note, favorite
		FROM image_assets
		WHERE id = ?`, cleanID(id))
	item, err := scanAssetRow(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	items := []Asset{item}
	if err := s.attachTags(ctx, items); err != nil {
		return nil, err
	}
	return &items[0], nil
}

func (s *Store) Save(ctx context.Context, asset Asset) (*Asset, error) {
	normalized, err := normalizeAsset(asset)
	if err != nil {
		return nil, err
	}
	if current, err := s.Get(ctx, normalized.ID); err == nil && current != nil {
		if normalized.Category == "" {
			normalized.Category = current.Category
		}
		if len(normalized.Tags) == 0 {
			normalized.Tags = append([]string(nil), current.Tags...)
		}
		if normalized.Note == "" {
			normalized.Note = current.Note
		}
		if !normalized.Favorite {
			normalized.Favorite = current.Favorite
		}
	} else if err != nil {
		return nil, err
	}
	if err := s.save(ctx, normalized); err != nil {
		return nil, err
	}
	return &normalized, nil
}

func (s *Store) SaveMany(ctx context.Context, items []Asset) ([]Asset, error) {
	result := make([]Asset, 0, len(items))
	for _, item := range items {
		saved, err := s.Save(ctx, item)
		if err != nil {
			return nil, err
		}
		if saved != nil {
			result = append(result, *saved)
		}
	}
	sortAssets(result)
	return result, nil
}

func (s *Store) UpdateMetadata(ctx context.Context, id string, patch MetadataPatch) (*Asset, error) {
	current, err := s.Get(ctx, cleanID(id))
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, fmt.Errorf("asset not found")
	}
	next := *current
	if patch.Category != nil {
		next.Category = strings.TrimSpace(*patch.Category)
	}
	if patch.Tags != nil {
		next.Tags = normalizeTags(*patch.Tags)
	}
	if patch.Note != nil {
		next.Note = strings.TrimSpace(*patch.Note)
	}
	if patch.Favorite != nil {
		next.Favorite = *patch.Favorite
	}
	if err := s.save(ctx, next); err != nil {
		return nil, err
	}
	return &next, nil
}

func (s *Store) UpdateMetadataBatch(ctx context.Context, patch BulkMetadataPatch) ([]Asset, error) {
	ids := normalizeIDs(patch.IDs)
	if len(ids) == 0 {
		return nil, fmt.Errorf("asset ids are required")
	}
	result := make([]Asset, 0, len(ids))
	for _, id := range ids {
		item, err := s.UpdateMetadata(ctx, id, MetadataPatch{
			Category: patch.Category,
			Tags:     patch.Tags,
			Note:     patch.Note,
			Favorite: patch.Favorite,
		})
		if err != nil {
			return nil, err
		}
		if item != nil {
			result = append(result, *item)
		}
	}
	sortAssets(result)
	return result, nil
}

func (s *Store) ReferencedFiles(ctx context.Context) (map[string]struct{}, error) {
	items, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	files := map[string]struct{}{}
	for _, item := range items {
		if filename := filenameFromAssetURL(item.ImageURL); filename != "" {
			files[filename] = struct{}{}
		}
	}
	return files, nil
}

func (s *Store) save(ctx context.Context, asset Asset) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("asset store is unavailable")
	}
	normalized, err := normalizeAsset(asset)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO image_assets (
			id, title, prompt, revised_prompt, mode, model, created_at, updated_at,
			conversation_id, turn_id, image_id, status, image_url, image_b64_json,
			file_id, gen_id, source_account_id, category, note, favorite
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			title = excluded.title,
			prompt = excluded.prompt,
			revised_prompt = excluded.revised_prompt,
			mode = excluded.mode,
			model = excluded.model,
			created_at = excluded.created_at,
			updated_at = excluded.updated_at,
			conversation_id = excluded.conversation_id,
			turn_id = excluded.turn_id,
			image_id = excluded.image_id,
			status = excluded.status,
			image_url = excluded.image_url,
			image_b64_json = excluded.image_b64_json,
			file_id = excluded.file_id,
			gen_id = excluded.gen_id,
			source_account_id = excluded.source_account_id,
			category = excluded.category,
			note = excluded.note,
			favorite = excluded.favorite`,
		normalized.ID,
		normalized.Title,
		normalized.Prompt,
		normalized.RevisedPrompt,
		normalized.Mode,
		normalized.Model,
		normalized.CreatedAt,
		normalized.UpdatedAt,
		normalized.ConversationID,
		normalized.TurnID,
		normalized.ImageID,
		normalized.Status,
		normalized.ImageURL,
		normalized.ImageB64JSON,
		normalized.FileID,
		normalized.GenID,
		normalized.SourceAccountID,
		normalized.Category,
		normalized.Note,
		boolToInt(normalized.Favorite),
	)
	if err != nil {
		return err
	}

	if _, err = tx.ExecContext(ctx, `DELETE FROM image_assets_fts WHERE asset_id = ?`, normalized.ID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(
		ctx,
		`INSERT INTO image_assets_fts(asset_id, title, prompt, revised_prompt, note) VALUES(?, ?, ?, ?, ?)`,
		normalized.ID,
		normalized.Title,
		normalized.Prompt,
		normalized.RevisedPrompt,
		normalized.Note,
	); err != nil {
		return err
	}

	if _, err = tx.ExecContext(ctx, `DELETE FROM image_asset_tags WHERE asset_id = ?`, normalized.ID); err != nil {
		return err
	}
	for _, tag := range normalized.Tags {
		if _, err = tx.ExecContext(ctx, `INSERT INTO image_asset_tags(asset_id, tag) VALUES(?, ?)`, normalized.ID, tag); err != nil {
			return err
		}
	}
	err = tx.Commit()
	return err
}

func (s *Store) attachTags(ctx context.Context, items []Asset) error {
	if len(items) == 0 {
		return nil
	}
	tagMap := map[string][]string{}
	rows, err := s.db.QueryContext(ctx, `SELECT asset_id, tag FROM image_asset_tags ORDER BY tag ASC`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var assetID string
		var tag string
		if err := rows.Scan(&assetID, &tag); err != nil {
			return err
		}
		tagMap[assetID] = append(tagMap[assetID], tag)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for index := range items {
		items[index].Tags = append([]string(nil), tagMap[items[index].ID]...)
	}
	return nil
}

func (s *Store) TagStats(ctx context.Context) ([]TagStat, error) {
	if s == nil || s.db == nil {
		return []TagStat{}, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT tag, COUNT(*) AS count
		FROM image_asset_tags
		GROUP BY tag
		ORDER BY count DESC, tag ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []TagStat{}
	for rows.Next() {
		var item TagStat
		if err := rows.Scan(&item.Tag, &item.Count); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) CategoryStats(ctx context.Context) ([]CategoryStat, error) {
	if s == nil || s.db == nil {
		return []CategoryStat{}, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT category, COUNT(*) AS count
		FROM image_assets
		WHERE category <> ''
		GROUP BY category
		ORDER BY count DESC, category ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []CategoryStat{}
	for rows.Next() {
		var item CategoryStat
		if err := rows.Scan(&item.Category, &item.Count); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) RenameTag(ctx context.Context, from, to string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("asset store is unavailable")
	}
	fromTag := strings.TrimSpace(from)
	toTag := strings.TrimSpace(to)
	if fromTag == "" || toTag == "" {
		return fmt.Errorf("from and to tags are required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	rows, err := tx.QueryContext(ctx, `SELECT asset_id FROM image_asset_tags WHERE tag = ?`, fromTag)
	if err != nil {
		return err
	}
	assetIDs := []string{}
	for rows.Next() {
		var assetID string
		if err := rows.Scan(&assetID); err != nil {
			_ = rows.Close()
			return err
		}
		assetIDs = append(assetIDs, assetID)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM image_asset_tags WHERE tag = ?`, fromTag); err != nil {
		return err
	}
	for _, assetID := range assetIDs {
		if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO image_asset_tags(asset_id, tag) VALUES(?, ?)`, assetID, toTag); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) DeleteTag(ctx context.Context, tag string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("asset store is unavailable")
	}
	cleaned := strings.TrimSpace(tag)
	if cleaned == "" {
		return fmt.Errorf("tag is required")
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM image_asset_tags WHERE tag = ?`, cleaned)
	return err
}

func (s *Store) RenameCategory(ctx context.Context, from, to string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("asset store is unavailable")
	}
	fromCategory := strings.TrimSpace(from)
	toCategory := strings.TrimSpace(to)
	if fromCategory == "" || toCategory == "" {
		return fmt.Errorf("from and to categories are required")
	}
	_, err := s.db.ExecContext(ctx, `UPDATE image_assets SET category = ?, updated_at = ? WHERE category = ?`, toCategory, time.Now().UTC().Format(time.RFC3339Nano), fromCategory)
	return err
}

func (s *Store) DeleteCategory(ctx context.Context, category string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("asset store is unavailable")
	}
	cleaned := strings.TrimSpace(category)
	if cleaned == "" {
		return fmt.Errorf("category is required")
	}
	_, err := s.db.ExecContext(ctx, `UPDATE image_assets SET category = '', updated_at = ? WHERE category = ?`, time.Now().UTC().Format(time.RFC3339Nano), cleaned)
	return err
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanAssetRow(scanner rowScanner) (Asset, error) {
	var item Asset
	var favorite int
	err := scanner.Scan(
		&item.ID,
		&item.Title,
		&item.Prompt,
		&item.RevisedPrompt,
		&item.Mode,
		&item.Model,
		&item.CreatedAt,
		&item.UpdatedAt,
		&item.ConversationID,
		&item.TurnID,
		&item.ImageID,
		&item.Status,
		&item.ImageURL,
		&item.ImageB64JSON,
		&item.FileID,
		&item.GenID,
		&item.SourceAccountID,
		&item.Category,
		&item.Note,
		&favorite,
	)
	if err != nil {
		return Asset{}, err
	}
	item.Favorite = favorite != 0
	return item, nil
}

func normalizeAsset(asset Asset) (Asset, error) {
	asset.ID = cleanID(asset.ID)
	if asset.ID == "" {
		return Asset{}, fmt.Errorf("asset id is required")
	}
	asset.Title = strings.TrimSpace(asset.Title)
	asset.Prompt = strings.TrimSpace(asset.Prompt)
	asset.RevisedPrompt = strings.TrimSpace(asset.RevisedPrompt)
	asset.Mode = strings.TrimSpace(asset.Mode)
	asset.Model = strings.TrimSpace(asset.Model)
	asset.ConversationID = cleanID(asset.ConversationID)
	asset.TurnID = cleanID(asset.TurnID)
	asset.ImageID = cleanID(asset.ImageID)
	asset.Status = strings.TrimSpace(asset.Status)
	asset.ImageURL = strings.TrimSpace(asset.ImageURL)
	asset.ImageB64JSON = strings.TrimSpace(asset.ImageB64JSON)
	asset.FileID = strings.TrimSpace(asset.FileID)
	asset.GenID = strings.TrimSpace(asset.GenID)
	asset.SourceAccountID = strings.TrimSpace(asset.SourceAccountID)
	asset.Category = strings.TrimSpace(asset.Category)
	asset.Tags = normalizeTags(asset.Tags)
	asset.Note = strings.TrimSpace(asset.Note)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if strings.TrimSpace(asset.CreatedAt) == "" {
		asset.CreatedAt = now
	}
	asset.UpdatedAt = now
	return asset, nil
}

func normalizeTags(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
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

func normalizeIDs(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		cleaned := cleanID(value)
		if cleaned == "" {
			continue
		}
		if _, ok := seen[cleaned]; ok {
			continue
		}
		seen[cleaned] = struct{}{}
		result = append(result, cleaned)
	}
	return result
}

func cleanID(id string) string {
	cleaned := strings.TrimSpace(id)
	cleaned = strings.ReplaceAll(cleaned, "/", "-")
	cleaned = strings.ReplaceAll(cleaned, "\\", "-")
	return cleaned
}

func normalizeSortMode(value string) sortMode {
	switch sortMode(strings.TrimSpace(strings.ToLower(value))) {
	case sortCreatedAsc:
		return sortCreatedAsc
	case sortUpdatedDesc:
		return sortUpdatedDesc
	case sortTitleAsc:
		return sortTitleAsc
	case sortTitleDesc:
		return sortTitleDesc
	case sortFavorite:
		return sortFavorite
	default:
		return sortCreatedDesc
	}
}

func (m sortMode) orderByClause() string {
	switch m {
	case sortCreatedAsc:
		return "a.created_at ASC, a.favorite DESC, a.updated_at DESC, a.id ASC"
	case sortUpdatedDesc:
		return "a.updated_at DESC, a.favorite DESC, a.created_at DESC, a.id ASC"
	case sortTitleAsc:
		return "LOWER(a.title) ASC, a.favorite DESC, a.created_at DESC, a.id ASC"
	case sortTitleDesc:
		return "LOWER(a.title) DESC, a.favorite DESC, a.created_at DESC, a.id ASC"
	case sortFavorite:
		return "a.favorite DESC, a.updated_at DESC, a.created_at DESC, a.id ASC"
	default:
		return "a.created_at DESC, a.favorite DESC, a.updated_at DESC, a.id ASC"
	}
}

func sortAssets(items []Asset) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].CreatedAt != items[j].CreatedAt {
			return items[i].CreatedAt > items[j].CreatedAt
		}
		if items[i].UpdatedAt != items[j].UpdatedAt {
			return items[i].UpdatedAt > items[j].UpdatedAt
		}
		return items[i].ID < items[j].ID
	})
}

func filenameFromAssetURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	index := strings.LastIndex(trimmed, "/v1/files/image/")
	if index >= 0 {
		return filepath.Base(trimmed[index+len("/v1/files/image/"):])
	}
	return ""
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func buildLikePattern(value string) string {
	replacer := strings.NewReplacer(
		`\`, `\\`,
		`%`, `\%`,
		`_`, `\_`,
	)
	return "%" + replacer.Replace(strings.TrimSpace(value)) + "%"
}

func buildFTSMatchQuery(value string) string {
	terms := strings.Fields(strings.TrimSpace(value))
	if len(terms) == 0 {
		return ""
	}
	parts := make([]string, 0, len(terms))
	for _, term := range terms {
		cleaned := strings.TrimSpace(term)
		if cleaned == "" {
			continue
		}
		parts = append(parts, `"`+strings.ReplaceAll(cleaned, `"`, `""`)+`"`)
	}
	return strings.Join(parts, " ")
}
