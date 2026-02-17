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
