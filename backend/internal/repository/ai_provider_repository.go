package repository

import (
	"database/sql"
	"fmt"

	"github.com/ruifan75/setori/internal/models"
)

type AIProviderRepository struct {
	db *sql.DB
}

func NewAIProviderRepository(db *sql.DB) *AIProviderRepository {
	return &AIProviderRepository{db: db}
}

const aiProviderColumns = "id, name, base_url, model, api_key, enabled, priority, created_at, updated_at"

func scanAIProvider(rows *sql.Rows) (models.AIProvider, error) {
	var p models.AIProvider
	err := rows.Scan(&p.ID, &p.Name, &p.BaseURL, &p.Model, &p.APIKey, &p.Enabled, &p.Priority, &p.CreatedAt, &p.UpdatedAt)
	return p, err
}

// FindAll 取得所有 provider（依優先序）
func (r *AIProviderRepository) FindAll() ([]models.AIProvider, error) {
	rows, err := r.db.Query("SELECT " + aiProviderColumns + " FROM ai_providers ORDER BY priority ASC, id ASC")
	if err != nil {
		return nil, fmt.Errorf("query ai providers: %w", err)
	}
	defer rows.Close()

	var providers []models.AIProvider
	for rows.Next() {
		p, err := scanAIProvider(rows)
		if err != nil {
			return nil, fmt.Errorf("scan ai provider: %w", err)
		}
		providers = append(providers, p)
	}
	return providers, rows.Err()
}

// FindEnabled 取得啟用中的 provider（依優先序）
func (r *AIProviderRepository) FindEnabled() ([]models.AIProvider, error) {
	rows, err := r.db.Query("SELECT " + aiProviderColumns + " FROM ai_providers WHERE enabled = TRUE ORDER BY priority ASC, id ASC")
	if err != nil {
		return nil, fmt.Errorf("query enabled ai providers: %w", err)
	}
	defer rows.Close()

	var providers []models.AIProvider
	for rows.Next() {
		p, err := scanAIProvider(rows)
		if err != nil {
			return nil, fmt.Errorf("scan ai provider: %w", err)
		}
		providers = append(providers, p)
	}
	return providers, rows.Err()
}

// FindByID 取得單一 provider
func (r *AIProviderRepository) FindByID(id int) (*models.AIProvider, error) {
	var p models.AIProvider
	err := r.db.QueryRow("SELECT "+aiProviderColumns+" FROM ai_providers WHERE id = $1", id).
		Scan(&p.ID, &p.Name, &p.BaseURL, &p.Model, &p.APIKey, &p.Enabled, &p.Priority, &p.CreatedAt, &p.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find ai provider: %w", err)
	}
	return &p, nil
}

// Create 建立 provider
func (r *AIProviderRepository) Create(p *models.AIProvider) error {
	query := `
		INSERT INTO ai_providers (name, base_url, model, api_key, enabled, priority)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at, updated_at`
	err := r.db.QueryRow(query, p.Name, p.BaseURL, p.Model, p.APIKey, p.Enabled, p.Priority).
		Scan(&p.ID, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create ai provider: %w", err)
	}
	return nil
}

// Update 更新 provider
func (r *AIProviderRepository) Update(p *models.AIProvider) error {
	query := `
		UPDATE ai_providers
		SET name = $2, base_url = $3, model = $4, api_key = $5, enabled = $6, priority = $7, updated_at = NOW()
		WHERE id = $1`
	_, err := r.db.Exec(query, p.ID, p.Name, p.BaseURL, p.Model, p.APIKey, p.Enabled, p.Priority)
	if err != nil {
		return fmt.Errorf("update ai provider: %w", err)
	}
	return nil
}

// Delete 刪除 provider
func (r *AIProviderRepository) Delete(id int) error {
	_, err := r.db.Exec("DELETE FROM ai_providers WHERE id = $1", id)
	if err != nil {
		return fmt.Errorf("delete ai provider: %w", err)
	}
	return nil
}
