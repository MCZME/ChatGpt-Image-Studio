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

type MetadataPatch struct {
	Title    *string   `json:"title,omitempty"`
	Category *string   `json:"category,omitempty"`
	Tags     *[]string `json:"tags,omitempty"`
	Note     *string   `json:"note,omitempty"`
	Favorite *bool     `json:"favorite,omitempty"`
}

type BulkMetadataPatch struct {
	IDs      []string  `json:"ids"`
	Title    *string   `json:"title,omitempty"`
	Category *string   `json:"category,omitempty"`
	Tags     *[]string `json:"tags,omitempty"`
	Note     *string   `json:"note,omitempty"`
	Favorite *bool     `json:"favorite,omitempty"`
}

type DeleteAutoOptions struct {
	ConversationID string
	TurnID         string
	ImageID        string
	Filename       string
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

type assetColumnSpec struct {
	Name       string
	SQL        string
	Required   bool
	CreateOnly bool
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

var assetColumnSpecs = []assetColumnSpec{
	{Name: "id", SQL: "id TEXT PRIMARY KEY", Required: true, CreateOnly: true},
	{Name: "title", SQL: "title TEXT NOT NULL DEFAULT ''", Required: true},
	{Name: "prompt", SQL: "prompt TEXT NOT NULL DEFAULT ''", Required: true},
	{Name: "revised_prompt", SQL: "revised_prompt TEXT NOT NULL DEFAULT ''", Required: true},
	{Name: "mode", SQL: "mode TEXT NOT NULL DEFAULT ''", Required: true},
	{Name: "model", SQL: "model TEXT NOT NULL DEFAULT ''", Required: true},
	{Name: "created_at", SQL: "created_at TEXT NOT NULL DEFAULT ''", Required: true},
	{Name: "updated_at", SQL: "updated_at TEXT NOT NULL DEFAULT ''", Required: true},
	{Name: "conversation_id", SQL: "conversation_id TEXT NOT NULL DEFAULT ''", Required: true},
	{Name: "turn_id", SQL: "turn_id TEXT NOT NULL DEFAULT ''", Required: true},
	{Name: "image_id", SQL: "image_id TEXT NOT NULL DEFAULT ''", Required: true},
	{Name: "status", SQL: "status TEXT NOT NULL DEFAULT ''", Required: true},
	{Name: "image_url", SQL: "image_url TEXT NOT NULL DEFAULT ''", Required: true},
	{Name: "image_b64_json", SQL: "image_b64_json TEXT NOT NULL DEFAULT ''", Required: true},
	{Name: "filename", SQL: "filename TEXT NOT NULL DEFAULT ''", Required: true},
	{Name: "mime_type", SQL: "mime_type TEXT NOT NULL DEFAULT ''", Required: true},
	{Name: "size_bytes", SQL: "size_bytes INTEGER NOT NULL DEFAULT 0", Required: true},
	{Name: "sha256", SQL: "sha256 TEXT NOT NULL DEFAULT ''", Required: true},
	{Name: "storage_kind", SQL: "storage_kind TEXT NOT NULL DEFAULT ''", Required: true},
	{Name: "source_kind", SQL: "source_kind TEXT NOT NULL DEFAULT ''", Required: true},
	{Name: "original_url", SQL: "original_url TEXT NOT NULL DEFAULT ''", Required: true},
	{Name: "file_id", SQL: "file_id TEXT NOT NULL DEFAULT ''", Required: true},
	{Name: "gen_id", SQL: "gen_id TEXT NOT NULL DEFAULT ''", Required: true},
	{Name: "source_account_id", SQL: "source_account_id TEXT NOT NULL DEFAULT ''", Required: true},
	{Name: "category", SQL: "category TEXT NOT NULL DEFAULT ''", Required: true},
	{Name: "note", SQL: "note TEXT NOT NULL DEFAULT ''", Required: true},
	{Name: "favorite", SQL: "favorite INTEGER NOT NULL DEFAULT 0", Required: true},
}

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
	if err := s.ensureAssetSchema(); err != nil {
		return err
	}
	if err := s.ensureAuxiliarySchemas(); err != nil {
		return err
	}
	statements := []string{
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
		`CREATE TABLE IF NOT EXISTS image_asset_deletions (
			asset_id TEXT PRIMARY KEY,
			deleted_at TEXT NOT NULL DEFAULT ''
		);`,
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

func (s *Store) ensureAuxiliarySchemas() error {
	tagExists, err := tableExists(s.db, "image_asset_tags")
	if err != nil {
		return err
	}
	if tagExists {
		tagColumns, err := tableColumns(s.db, "image_asset_tags")
		if err != nil {
			return err
		}
		if !containsColumns(tagColumns, []string{"asset_id", "tag"}) {
			if _, err := s.db.Exec(`DROP TABLE IF EXISTS image_asset_tags`); err != nil {
				return err
			}
		}
	}

	ftsExists, err := tableExists(s.db, "image_assets_fts")
	if err != nil {
		return err
	}
	if ftsExists {
		ftsColumns, err := tableColumns(s.db, "image_assets_fts")
		if err != nil {
			return err
		}
		if !containsColumns(ftsColumns, []string{"asset_id", "title", "prompt", "revised_prompt", "note"}) {
			if _, err := s.db.Exec(`DROP TABLE IF EXISTS image_assets_fts`); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Store) ensureAssetSchema() error {
	exists, err := tableExists(s.db, "image_assets")
	if err != nil {
		return err
	}
	if !exists {
		_, err := s.db.Exec(`CREATE TABLE image_assets (` + assetCreateColumnsSQL() + `);`)
		return err
	}

	columns, err := tableColumnDetails(s.db, "image_assets")
	if err != nil {
		return err
	}
	if _, ok := columns["id"]; !ok {
		return fmt.Errorf("incompatible image_assets schema: missing id column")
	}
	if hasBlockingUnknownColumns(columns) {
		if err := s.rebuildAssetTable(columns); err != nil {
			return err
		}
		columns, err = tableColumnDetails(s.db, "image_assets")
		if err != nil {
			return err
		}
	}
	for _, spec := range assetColumnSpecs {
		if _, ok := columns[spec.Name]; ok {
			continue
		}
		if spec.CreateOnly {
			return fmt.Errorf("incompatible image_assets schema: missing %s column", spec.Name)
		}
		if _, err := s.db.Exec(`ALTER TABLE image_assets ADD COLUMN ` + spec.SQL); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) rebuildAssetTable(columns map[string]tableColumnDetail) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err = tx.Exec(`DROP TABLE IF EXISTS image_assets_fts`); err != nil {
		return err
	}
	if _, err = tx.Exec(`CREATE TABLE image_assets_new (` + assetCreateColumnsSQL() + `);`); err != nil {
		return err
	}

	names := make([]string, 0, len(assetColumnSpecs))
	expressions := make([]string, 0, len(assetColumnSpecs))
	for _, spec := range assetColumnSpecs {
		names = append(names, quoteIdentifier(spec.Name))
		expressions = append(expressions, assetMigrationExpression(spec, columns))
	}
	if _, err = tx.Exec(
		`INSERT OR IGNORE INTO image_assets_new (` + strings.Join(names, ", ") + `) SELECT ` + strings.Join(expressions, ", ") + ` FROM image_assets`,
	); err != nil {
		return err
	}
	if _, err = tx.Exec(`DROP TABLE image_assets`); err != nil {
		return err
	}
	if _, err = tx.Exec(`ALTER TABLE image_assets_new RENAME TO image_assets`); err != nil {
		return err
	}
	err = tx.Commit()
	return err
}

func assetCreateColumnsSQL() string {
	parts := make([]string, 0, len(assetColumnSpecs))
	for _, spec := range assetColumnSpecs {
		parts = append(parts, spec.SQL)
	}
	return strings.Join(parts, ",\n")
}

func assetMigrationExpression(spec assetColumnSpec, columns map[string]tableColumnDetail) string {
	if _, ok := columns[spec.Name]; ok {
		quoted := quoteIdentifier(spec.Name)
		switch spec.Name {
		case "favorite", "size_bytes":
			return "COALESCE(" + quoted + ", 0)"
		default:
			return "COALESCE(CAST(" + quoted + " AS TEXT), '')"
		}
	}
	switch spec.Name {
	case "favorite", "size_bytes":
		return "0"
	default:
		return "''"
	}
}

func hasBlockingUnknownColumns(columns map[string]tableColumnDetail) bool {
	known := map[string]struct{}{}
	for _, spec := range assetColumnSpecs {
		known[spec.Name] = struct{}{}
	}
	for name, column := range columns {
		if _, ok := known[name]; ok {
			continue
		}
		if column.PrimaryKey == 0 && column.NotNull != 0 && !column.Default.Valid {
			return true
		}
	}
	return false
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
	details, err := tableColumnDetails(db, tableName)
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, len(details))
	for name := range details {
		result = append(result, name)
	}
	return result, nil
}

type tableColumnDetail struct {
	Name       string
	NotNull    int
	Default    sql.NullString
	PrimaryKey int
}

func tableColumnDetails(db *sql.DB, tableName string) (map[string]tableColumnDetail, error) {
	name := strings.TrimSpace(tableName)
	if name == "" {
		return nil, fmt.Errorf("table name is required")
	}
	rows, err := db.Query(fmt.Sprintf(`PRAGMA table_info("%s")`, strings.ReplaceAll(name, `"`, `""`)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := map[string]tableColumnDetail{}
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
		normalized := strings.ToLower(strings.TrimSpace(columnName))
		result[normalized] = tableColumnDetail{
			Name:       normalized,
			NotNull:    notNull,
			Default:    defaultVal,
			PrimaryKey: primaryKey,
		}
	}
	return result, rows.Err()
}

func quoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(strings.TrimSpace(name), `"`, `""`) + `"`
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
			a.filename, a.mime_type, a.size_bytes, a.sha256, a.storage_kind, a.source_kind, a.original_url,
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
		       filename, mime_type, size_bytes, sha256, storage_kind, source_kind, original_url,
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
	if shouldRespectAssetDeletion(normalized) {
		deleted, err := s.deletedIDs(ctx, []string{normalized.ID})
		if err != nil {
			return nil, err
		}
		if _, ok := deleted[normalized.ID]; ok {
			return nil, nil
		}
	}
	if current, err := s.Get(ctx, normalized.ID); err == nil && current != nil {
		if shouldPreserveStoredTitle(normalized, *current) {
			normalized.Title = current.Title
		}
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
		if normalized.Filename == "" {
			normalized.Filename = current.Filename
		}
		if normalized.MIMEType == "" {
			normalized.MIMEType = current.MIMEType
		}
		if normalized.SizeBytes == 0 {
			normalized.SizeBytes = current.SizeBytes
		}
		if normalized.SHA256 == "" {
			normalized.SHA256 = current.SHA256
		}
		if normalized.StorageKind == "" {
			normalized.StorageKind = current.StorageKind
		}
		if normalized.SourceKind == "" {
			normalized.SourceKind = current.SourceKind
		}
		if normalized.OriginalURL == "" {
			normalized.OriginalURL = current.OriginalURL
		}
	} else if err != nil {
		return nil, err
	}
	if err := s.save(ctx, normalized); err != nil {
		return nil, err
	}
	return &normalized, nil
}

func shouldPreserveStoredTitle(incoming, current Asset) bool {
	if strings.TrimSpace(current.Title) == "" {
		return false
	}
	if strings.TrimSpace(incoming.Title) == "" {
		return true
	}
	if strings.TrimSpace(incoming.Title) == strings.TrimSpace(current.Title) {
		return false
	}
	return incoming.ConversationID != "" && incoming.TurnID != ""
}

func (s *Store) SaveMany(ctx context.Context, items []Asset) ([]Asset, error) {
	result := make([]Asset, 0, len(items))
	skipDeleted := map[string]struct{}{}
	idsToCheck := []string{}
	for _, item := range items {
		normalized, err := normalizeAsset(item)
		if err != nil {
			return nil, err
		}
		if shouldRespectAssetDeletion(normalized) {
			idsToCheck = append(idsToCheck, normalized.ID)
		}
	}
	if len(idsToCheck) > 0 {
		var err error
		skipDeleted, err = s.deletedIDs(ctx, idsToCheck)
		if err != nil {
			return nil, err
		}
	}
	for _, item := range items {
		normalized, err := normalizeAsset(item)
		if err != nil {
			return nil, err
		}
		if _, ok := skipDeleted[normalized.ID]; ok {
			continue
		}
		saved, err := s.Save(ctx, normalized)
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
	if patch.Title != nil {
		next.Title = strings.TrimSpace(*patch.Title)
	}
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
			Title:    patch.Title,
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

func (s *Store) Delete(ctx context.Context, id string) (*Asset, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("asset store is unavailable")
	}
	cleanedID := cleanID(id)
	if cleanedID == "" {
		return nil, fmt.Errorf("asset id is required")
	}
	current, err := s.Get(ctx, cleanedID)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, fmt.Errorf("asset not found")
	}
	if err := s.deleteAsset(ctx, *current, shouldRememberAssetDeletion(*current)); err != nil {
		return nil, err
	}
	return current, nil
}

func (s *Store) DeleteBatch(ctx context.Context, ids []string) ([]Asset, error) {
	normalizedIDs := normalizeIDs(ids)
	if len(normalizedIDs) == 0 {
		return nil, fmt.Errorf("asset ids are required")
	}
	result := make([]Asset, 0, len(normalizedIDs))
	for _, id := range normalizedIDs {
		item, err := s.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		if item == nil {
			return nil, fmt.Errorf("asset not found")
		}
		result = append(result, *item)
	}
	for _, item := range result {
		if err := s.deleteAsset(ctx, item, shouldRememberAssetDeletion(item)); err != nil {
			return nil, err
		}
	}
	sortAssets(result)
	return result, nil
}

func (s *Store) DeleteExisting(ctx context.Context, ids []string) ([]Asset, error) {
	normalizedIDs := normalizeIDs(ids)
	if len(normalizedIDs) == 0 {
		return []Asset{}, nil
	}
	result := make([]Asset, 0, len(normalizedIDs))
	for _, id := range normalizedIDs {
		item, err := s.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		if item == nil {
			continue
		}
		result = append(result, *item)
	}
	for _, item := range result {
		if err := s.deleteAsset(ctx, item, shouldRememberAssetDeletion(item)); err != nil {
			return nil, err
		}
	}
	sortAssets(result)
	return result, nil
}

func (s *Store) DeleteAutoAsset(ctx context.Context, options DeleteAutoOptions) error {
	if s == nil || s.db == nil {
		return nil
	}
	conditions := []string{}
	args := []any{}
	if conversationID := cleanID(options.ConversationID); conversationID != "" {
		conditions = append(conditions, `conversation_id = ?`)
		args = append(args, conversationID)
	}
	if turnID := cleanID(options.TurnID); turnID != "" {
		conditions = append(conditions, `turn_id = ?`)
		args = append(args, turnID)
	}
	if imageID := cleanID(options.ImageID); imageID != "" {
		conditions = append(conditions, `image_id = ?`)
		args = append(args, imageID)
	}
	if filename := filepath.Base(strings.TrimSpace(options.Filename)); filename != "" && filename != "." {
		conditions = append(conditions, `(filename = ? OR image_url LIKE ?)`)
		args = append(args, filename, "%"+filename)
	}
	if len(conditions) == 0 {
		return nil
	}

	query := `DELETE FROM image_assets
		WHERE ` + strings.Join(conditions, " AND ") + `
		  AND favorite = 0
		  AND category = ''
		  AND note = ''
		  AND NOT EXISTS (
			SELECT 1 FROM image_asset_tags tags
			WHERE tags.asset_id = image_assets.id
		  )`
	_, err := s.db.ExecContext(ctx, query, args...)
	return err
}

func (s *Store) DeleteAutoAssets(ctx context.Context, options []DeleteAutoOptions) error {
	for _, option := range options {
		if err := s.DeleteAutoAsset(ctx, option); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) deletedIDs(ctx context.Context, ids []string) (map[string]struct{}, error) {
	if s == nil || s.db == nil || len(ids) == 0 {
		return map[string]struct{}{}, nil
	}
	result := map[string]struct{}{}
	for _, id := range normalizeIDs(ids) {
		var assetID string
		err := s.db.QueryRowContext(ctx, `SELECT asset_id FROM image_asset_deletions WHERE asset_id = ?`, id).Scan(&assetID)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return nil, err
		}
		result[assetID] = struct{}{}
	}
	return result, nil
}

func (s *Store) deleteAsset(ctx context.Context, asset Asset, rememberDeletion bool) (err error) {
	if s == nil || s.db == nil {
		return fmt.Errorf("asset store is unavailable")
	}
	cleanedID := cleanID(asset.ID)
	if cleanedID == "" {
		return fmt.Errorf("asset id is required")
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

	if _, err = tx.ExecContext(ctx, `DELETE FROM image_asset_tags WHERE asset_id = ?`, cleanedID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM image_assets_fts WHERE asset_id = ?`, cleanedID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM image_assets WHERE id = ?`, cleanedID); err != nil {
		return err
	}
	if rememberDeletion {
		if _, err = tx.ExecContext(
			ctx,
			`INSERT INTO image_asset_deletions(asset_id, deleted_at) VALUES(?, ?)
			 ON CONFLICT(asset_id) DO UPDATE SET deleted_at = excluded.deleted_at`,
			cleanedID,
			time.Now().UTC().Format(time.RFC3339Nano),
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ReferencedFiles(ctx context.Context) (map[string]struct{}, error) {
	items, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	files := map[string]struct{}{}
	for _, item := range items {
		if filename := filepath.Base(strings.TrimSpace(item.Filename)); filename != "" && filename != "." {
			files[filename] = struct{}{}
			continue
		}
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
			filename, mime_type, size_bytes, sha256, storage_kind, source_kind, original_url,
			file_id, gen_id, source_account_id, category, note, favorite
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
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
			filename = excluded.filename,
			mime_type = excluded.mime_type,
			size_bytes = excluded.size_bytes,
			sha256 = excluded.sha256,
			storage_kind = excluded.storage_kind,
			source_kind = excluded.source_kind,
			original_url = excluded.original_url,
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
		normalized.Filename,
		normalized.MIMEType,
		normalized.SizeBytes,
		normalized.SHA256,
		normalized.StorageKind,
		normalized.SourceKind,
		normalized.OriginalURL,
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
		&item.Filename,
		&item.MIMEType,
		&item.SizeBytes,
		&item.SHA256,
		&item.StorageKind,
		&item.SourceKind,
		&item.OriginalURL,
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
	if item.Filename == "" {
		item.Filename = FilenameFromImageURL(item.ImageURL)
	}
	if item.MIMEType == "" {
		item.MIMEType = DetectImageMIME(nil, "", item.Filename)
	}
	if item.StorageKind == "" && item.Filename != "" {
		item.StorageKind = "local"
	}
	if item.SourceKind == "" && item.Filename != "" {
		item.SourceKind = sourceKindFromFilename(item.Filename)
	}
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
	asset.Filename = filepath.Base(strings.TrimSpace(asset.Filename))
	if asset.Filename == "." {
		asset.Filename = ""
	}
	if asset.Filename == "" {
		asset.Filename = FilenameFromImageURL(asset.ImageURL)
	}
	asset.MIMEType = DetectImageMIME(nil, asset.MIMEType, asset.Filename)
	asset.SHA256 = strings.ToLower(strings.TrimSpace(asset.SHA256))
	asset.StorageKind = strings.ToLower(strings.TrimSpace(asset.StorageKind))
	if asset.StorageKind == "" && asset.Filename != "" {
		asset.StorageKind = "local"
	}
	asset.SourceKind = strings.ToLower(strings.TrimSpace(asset.SourceKind))
	if asset.SourceKind == "" && asset.Filename != "" {
		asset.SourceKind = sourceKindFromFilename(asset.Filename)
	}
	asset.OriginalURL = strings.TrimSpace(asset.OriginalURL)
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

func shouldRememberAssetDeletion(asset Asset) bool {
	return strings.TrimSpace(asset.ConversationID) != "" ||
		strings.TrimSpace(asset.TurnID) != "" ||
		strings.TrimSpace(asset.ImageID) != ""
}

func shouldRespectAssetDeletion(asset Asset) bool {
	if strings.TrimSpace(asset.SourceKind) == "import" || strings.TrimSpace(asset.Mode) == "import" {
		return false
	}
	return shouldRememberAssetDeletion(asset)
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
