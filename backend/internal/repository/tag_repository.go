package repository

import (
	"database/sql"
	"fmt"

	"github.com/ruifan75/setori/internal/models"
)

type TagRepository struct {
	db *sql.DB
}

func NewTagRepository(db *sql.DB) *TagRepository {
	return &TagRepository{db: db}
}

// ========== Stream Tags ==========

// FindAllStreamTags 取得所有 stream tags
func (r *TagRepository) FindAllStreamTags() ([]models.StreamTag, error) {
	rows, err := r.db.Query(`SELECT id, display_name, color, created_at FROM stream_tags ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("query stream tags: %w", err)
	}
	defer rows.Close()

	var tags []models.StreamTag
	for rows.Next() {
		var t models.StreamTag
		if err := rows.Scan(&t.ID, &t.DisplayName, &t.Color, &t.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan stream tag: %w", err)
		}
		tags = append(tags, t)
	}
	return tags, nil
}

// CreateStreamTag 新增 stream tag
func (r *TagRepository) CreateStreamTag(id, displayName, color string) (*models.StreamTag, error) {
	var t models.StreamTag
	err := r.db.QueryRow(
		`INSERT INTO stream_tags (id, display_name, color) VALUES ($1, $2, $3) RETURNING id, display_name, color, created_at`,
		id, displayName, color,
	).Scan(&t.ID, &t.DisplayName, &t.Color, &t.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create stream tag: %w", err)
	}
	return &t, nil
}

// DeleteStreamTag 刪除 stream tag
func (r *TagRepository) DeleteStreamTag(id string) error {
	result, err := r.db.Exec(`DELETE FROM stream_tags WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete stream tag: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("stream tag not found")
	}
	return nil
}

// ========== Performance Tags ==========

// FindAllPerformanceTags 取得所有 performance tags
func (r *TagRepository) FindAllPerformanceTags() ([]models.PerformanceTag, error) {
	rows, err := r.db.Query(`SELECT id, display_name, color, created_at FROM performance_tags ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("query performance tags: %w", err)
	}
	defer rows.Close()

	var tags []models.PerformanceTag
	for rows.Next() {
		var t models.PerformanceTag
		if err := rows.Scan(&t.ID, &t.DisplayName, &t.Color, &t.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan performance tag: %w", err)
		}
		tags = append(tags, t)
	}
	return tags, nil
}

// CreatePerformanceTag 新增 performance tag
func (r *TagRepository) CreatePerformanceTag(id, displayName, color string) (*models.PerformanceTag, error) {
	var t models.PerformanceTag
	err := r.db.QueryRow(
		`INSERT INTO performance_tags (id, display_name, color) VALUES ($1, $2, $3) RETURNING id, display_name, color, created_at`,
		id, displayName, color,
	).Scan(&t.ID, &t.DisplayName, &t.Color, &t.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create performance tag: %w", err)
	}
	return &t, nil
}

// DeletePerformanceTag 刪除 performance tag
func (r *TagRepository) DeletePerformanceTag(id string) error {
	result, err := r.db.Exec(`DELETE FROM performance_tags WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete performance tag: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("performance tag not found")
	}
	return nil
}

// TagWithCount タグ + 使用件数（グローバル検索の結果表示用）
type TagWithCount struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Color       string `json:"color"`
	Count       int    `json:"count"`
}

// SearchStreamTags は id / 表示名の部分一致で配信タグを検索し、使用件数付きで返す。
func (r *TagRepository) SearchStreamTags(query string, limit int) ([]TagWithCount, error) {
	rows, err := r.db.Query(`
		SELECT t.id, t.display_name, COALESCE(t.color, ''),
		       (SELECT COUNT(*) FROM stream_stream_tags sst
		        JOIN streams s ON s.id = sst.stream_id
		        WHERE sst.tag_id = t.id AND s.is_hidden = FALSE) AS cnt
		FROM stream_tags t
		WHERE t.id ILIKE '%' || $1 || '%' OR t.display_name ILIKE '%' || $1 || '%'
		ORDER BY cnt DESC, t.id
		LIMIT $2`, query, limit)
	if err != nil {
		return nil, fmt.Errorf("search stream tags: %w", err)
	}
	defer rows.Close()

	tags := make([]TagWithCount, 0)
	for rows.Next() {
		var t TagWithCount
		if err := rows.Scan(&t.ID, &t.DisplayName, &t.Color, &t.Count); err != nil {
			return nil, fmt.Errorf("scan stream tag: %w", err)
		}
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

// SearchPerformanceTags は id / 表示名の部分一致で演出タグを検索し、使用件数付きで返す。
func (r *TagRepository) SearchPerformanceTags(query string, limit int) ([]TagWithCount, error) {
	rows, err := r.db.Query(`
		SELECT t.id, t.display_name, COALESCE(t.color, ''),
		       (SELECT COUNT(*) FROM performance_performance_tags ppt
		        JOIN performances p ON p.id = ppt.performance_id
		        JOIN streams s ON s.id = p.stream_id
		        WHERE ppt.tag_id = t.id AND s.is_hidden = FALSE) AS cnt
		FROM performance_tags t
		WHERE t.id ILIKE '%' || $1 || '%' OR t.display_name ILIKE '%' || $1 || '%'
		ORDER BY cnt DESC, t.id
		LIMIT $2`, query, limit)
	if err != nil {
		return nil, fmt.Errorf("search performance tags: %w", err)
	}
	defer rows.Close()

	tags := make([]TagWithCount, 0)
	for rows.Next() {
		var t TagWithCount
		if err := rows.Scan(&t.ID, &t.DisplayName, &t.Color, &t.Count); err != nil {
			return nil, fmt.Errorf("scan performance tag: %w", err)
		}
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

// ========== Tag Keyword Rules（標題自動標籤規則）==========

// FindAllTagKeywordRules 取得所有自動標籤規則
func (r *TagRepository) FindAllTagKeywordRules() ([]models.TagKeywordRule, error) {
	rows, err := r.db.Query(`SELECT id, tag_id, keyword, created_at FROM tag_keyword_rules ORDER BY tag_id, keyword`)
	if err != nil {
		return nil, fmt.Errorf("query tag keyword rules: %w", err)
	}
	defer rows.Close()

	rules := make([]models.TagKeywordRule, 0)
	for rows.Next() {
		var rule models.TagKeywordRule
		if err := rows.Scan(&rule.ID, &rule.TagID, &rule.Keyword, &rule.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan tag keyword rule: %w", err)
		}
		rules = append(rules, rule)
	}
	return rules, rows.Err()
}

// CreateTagKeywordRule 新增自動標籤規則（tag_id 必須是既有 stream_tag）
func (r *TagRepository) CreateTagKeywordRule(tagID, keyword string) (*models.TagKeywordRule, error) {
	var rule models.TagKeywordRule
	err := r.db.QueryRow(
		`INSERT INTO tag_keyword_rules (tag_id, keyword) VALUES ($1, $2)
		 RETURNING id, tag_id, keyword, created_at`,
		tagID, keyword,
	).Scan(&rule.ID, &rule.TagID, &rule.Keyword, &rule.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create tag keyword rule: %w", err)
	}
	return &rule, nil
}

// DeleteTagKeywordRule 刪除自動標籤規則
func (r *TagRepository) DeleteTagKeywordRule(id int) error {
	result, err := r.db.Exec(`DELETE FROM tag_keyword_rules WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete tag keyword rule: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("tag keyword rule not found")
	}
	return nil
}
