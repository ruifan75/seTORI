package repository

import (
	"database/sql"
	"fmt"

	"github.com/ruifan75/setori/internal/models"
)

type SingerRepository struct {
	db *sql.DB
}

func NewSingerRepository(db *sql.DB) *SingerRepository {
	return &SingerRepository{db: db}
}

// FindAll 取得所有演唱者
func (r *SingerRepository) FindAll(limit, offset int) ([]models.Singer, int, error) {
	var total int
	err := r.db.QueryRow("SELECT COUNT(*) FROM singers").Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count singers: %w", err)
	}

	query := `
		SELECT id, name, english_name, photo_url, organization, created_at, updated_at
		FROM singers
		ORDER BY name ASC
		LIMIT $1 OFFSET $2`

	rows, err := r.db.Query(query, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("query singers: %w", err)
	}
	defer rows.Close()

	var singers []models.Singer
	for rows.Next() {
		var s models.Singer
		err := rows.Scan(&s.ID, &s.Name, &s.EnglishName, &s.PhotoURL,
			&s.Organization, &s.CreatedAt, &s.UpdatedAt)
		if err != nil {
			return nil, 0, fmt.Errorf("scan singer: %w", err)
		}
		singers = append(singers, s)
	}

	return singers, total, nil
}

// FindByID 根據 Channel ID 取得演唱者
func (r *SingerRepository) FindByID(id string) (*models.Singer, error) {
	query := `
		SELECT id, name, english_name, photo_url, organization, created_at, updated_at
		FROM singers WHERE id = $1`

	var s models.Singer
	err := r.db.QueryRow(query, id).Scan(
		&s.ID, &s.Name, &s.EnglishName, &s.PhotoURL,
		&s.Organization, &s.CreatedAt, &s.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find singer by id: %w", err)
	}
	return &s, nil
}

// Create 建立新演唱者
func (r *SingerRepository) Create(s *models.Singer) error {
	query := `
		INSERT INTO singers (id, name, english_name, photo_url, organization)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING created_at, updated_at`

	err := r.db.QueryRow(query, s.ID, s.Name, s.EnglishName, s.PhotoURL, s.Organization).
		Scan(&s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create singer: %w", err)
	}
	return nil
}

// Update 更新演唱者
func (r *SingerRepository) Update(s *models.Singer) error {
	query := `
		UPDATE singers
		SET name = $2, english_name = $3, photo_url = $4, organization = $5, updated_at = NOW()
		WHERE id = $1
		RETURNING updated_at`

	err := r.db.QueryRow(query, s.ID, s.Name, s.EnglishName, s.PhotoURL, s.Organization).
		Scan(&s.UpdatedAt)
	if err != nil {
		return fmt.Errorf("update singer: %w", err)
	}
	return nil
}

// Upsert 建立或更新演唱者（用於 Holodex 同步）
func (r *SingerRepository) Upsert(s *models.Singer) error {
	query := `
		INSERT INTO singers (id, name, english_name, photo_url, organization)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (id) DO UPDATE SET
			name = EXCLUDED.name,
			english_name = EXCLUDED.english_name,
			photo_url = EXCLUDED.photo_url,
			organization = EXCLUDED.organization,
			updated_at = NOW()
		RETURNING created_at, updated_at`

	err := r.db.QueryRow(query, s.ID, s.Name, s.EnglishName, s.PhotoURL, s.Organization).
		Scan(&s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return fmt.Errorf("upsert singer: %w", err)
	}
	return nil
}

// Delete 刪除演唱者
func (r *SingerRepository) Delete(id string) error {
	_, err := r.db.Exec("DELETE FROM singers WHERE id = $1", id)
	if err != nil {
		return fmt.Errorf("delete singer: %w", err)
	}
	return nil
}

// GetStreamCount 取得演唱者參與的直播數量（只計算非隱藏的 Stream）
func (r *SingerRepository) GetStreamCount(singerID string) (int, error) {
	var count int
	err := r.db.QueryRow(`
		SELECT COUNT(DISTINCT ss.stream_id)
		FROM stream_singers ss
		JOIN streams st ON ss.stream_id = st.id
		WHERE ss.singer_id = $1 AND st.is_hidden = FALSE
	`, singerID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count streams: %w", err)
	}
	return count, nil
}

// GetPerformanceCount 取得演唱者的演出數量（只計算非隱藏的 Stream）
func (r *SingerRepository) GetPerformanceCount(singerID string) (int, error) {
	var count int
	err := r.db.QueryRow(`
		SELECT COUNT(*)
		FROM performance_singers ps
		JOIN performances p ON ps.performance_id = p.id
		JOIN streams st ON p.stream_id = st.id
		WHERE ps.singer_id = $1 AND st.is_hidden = FALSE
	`, singerID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count performances: %w", err)
	}
	return count, nil
}

// Search 搜尋演唱者
func (r *SingerRepository) Search(query string, limit int) ([]models.Singer, error) {
	sqlQuery := `
		SELECT id, name, english_name, photo_url, organization, created_at, updated_at
		FROM singers
		WHERE name ILIKE $1 OR english_name ILIKE $1
		ORDER BY name ASC
		LIMIT $2`

	searchPattern := "%" + query + "%"
	rows, err := r.db.Query(sqlQuery, searchPattern, limit)
	if err != nil {
		return nil, fmt.Errorf("search singers: %w", err)
	}
	defer rows.Close()

	var singers []models.Singer
	for rows.Next() {
		var s models.Singer
		err := rows.Scan(&s.ID, &s.Name, &s.EnglishName, &s.PhotoURL,
			&s.Organization, &s.CreatedAt, &s.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("scan singer: %w", err)
		}
		singers = append(singers, s)
	}

	return singers, nil
}

// FindByOrganization 根據組織取得演唱者
func (r *SingerRepository) FindByOrganization(org string) ([]models.Singer, error) {
	query := `
		SELECT id, name, english_name, photo_url, organization, created_at, updated_at
		FROM singers
		WHERE organization = $1
		ORDER BY name ASC`

	rows, err := r.db.Query(query, org)
	if err != nil {
		return nil, fmt.Errorf("query singers by org: %w", err)
	}
	defer rows.Close()

	var singers []models.Singer
	for rows.Next() {
		var s models.Singer
		err := rows.Scan(&s.ID, &s.Name, &s.EnglishName, &s.PhotoURL,
			&s.Organization, &s.CreatedAt, &s.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("scan singer: %w", err)
		}
		singers = append(singers, s)
	}

	return singers, nil
}
