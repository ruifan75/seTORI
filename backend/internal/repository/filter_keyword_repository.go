package repository

import (
	"database/sql"
	"fmt"

	"github.com/ruifan75/setori/internal/models"
)

type FilterKeywordRepository struct {
	db *sql.DB
}

func NewFilterKeywordRepository(db *sql.DB) *FilterKeywordRepository {
	return &FilterKeywordRepository{db: db}
}

// FindAll はすべての filter keyword を取得する。
func (r *FilterKeywordRepository) FindAll() ([]models.FilterKeyword, error) {
	query := `SELECT id, keyword, type, created_at FROM filter_keywords ORDER BY type, keyword`

	rows, err := r.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("query filter keywords: %w", err)
	}
	defer rows.Close()

	var keywords []models.FilterKeyword
	for rows.Next() {
		var kw models.FilterKeyword
		if err := rows.Scan(&kw.ID, &kw.Keyword, &kw.Type, &kw.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan filter keyword: %w", err)
		}
		keywords = append(keywords, kw)
	}

	return keywords, nil
}

// Create は filter keyword を追加する。
func (r *FilterKeywordRepository) Create(keyword, keywordType string) (*models.FilterKeyword, error) {
	query := `INSERT INTO filter_keywords (keyword, type) VALUES ($1, $2) RETURNING id, keyword, type, created_at`

	var kw models.FilterKeyword
	err := r.db.QueryRow(query, keyword, keywordType).Scan(&kw.ID, &kw.Keyword, &kw.Type, &kw.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create filter keyword: %w", err)
	}

	return &kw, nil
}

// Delete はフィルターキーワードを削除する。
func (r *FilterKeywordRepository) Delete(id int) error {
	result, err := r.db.Exec(`DELETE FROM filter_keywords WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete filter keyword: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("filter keyword not found")
	}

	return nil
}
