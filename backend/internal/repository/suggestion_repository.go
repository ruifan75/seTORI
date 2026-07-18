package repository

import (
	"database/sql"
	"fmt"

	"github.com/google/uuid"
	"github.com/ruifan75/setori/internal/models"
)

// SuggestionRepository は edit_suggestions（閲覧モードからの修正提案）を扱う。
type SuggestionRepository struct {
	db *sql.DB
}

func NewSuggestionRepository(db *sql.DB) *SuggestionRepository {
	return &SuggestionRepository{db: db}
}

// Create は提案を1件登録し、生成された行を返す。
func (r *SuggestionRepository) Create(s *models.EditSuggestion) (*models.EditSuggestion, error) {
	err := r.db.QueryRow(`
		INSERT INTO edit_suggestions (target_type, target_id, target_label, before_data, after_data, note)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at, status`,
		s.TargetType, s.TargetID, s.TargetLabel, s.BeforeData, s.AfterData, s.Note).
		Scan(&s.ID, &s.CreatedAt, &s.Status)
	if err != nil {
		return nil, fmt.Errorf("create suggestion: %w", err)
	}
	return s, nil
}

// List は status で絞った提案一覧をページングして返す。status が空なら全件。
func (r *SuggestionRepository) List(status string, limit, offset int) ([]models.EditSuggestion, int, error) {
	where := ""
	args := []any{}
	if status != "" {
		where = "WHERE status = $1"
		args = append(args, status)
	}

	var total int
	if err := r.db.QueryRow("SELECT COUNT(*) FROM edit_suggestions "+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count suggestions: %w", err)
	}

	query := fmt.Sprintf(`
		SELECT id, target_type, target_id, target_label, before_data, after_data, note, status, created_at, reviewed_at
		FROM edit_suggestions
		%s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d`, where, len(args)+1, len(args)+2)
	args = append(args, limit, offset)

	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list suggestions: %w", err)
	}
	defer rows.Close()

	var out []models.EditSuggestion
	for rows.Next() {
		var s models.EditSuggestion
		if err := rows.Scan(&s.ID, &s.TargetType, &s.TargetID, &s.TargetLabel,
			&s.BeforeData, &s.AfterData, &s.Note, &s.Status, &s.CreatedAt, &s.ReviewedAt); err != nil {
			return nil, 0, fmt.Errorf("scan suggestion: %w", err)
		}
		out = append(out, s)
	}
	return out, total, rows.Err()
}

// FindByID は提案を1件取得する。見つからなければ nil。
func (r *SuggestionRepository) FindByID(id uuid.UUID) (*models.EditSuggestion, error) {
	var s models.EditSuggestion
	err := r.db.QueryRow(`
		SELECT id, target_type, target_id, target_label, before_data, after_data, note, status, created_at, reviewed_at
		FROM edit_suggestions WHERE id = $1`, id).
		Scan(&s.ID, &s.TargetType, &s.TargetID, &s.TargetLabel,
			&s.BeforeData, &s.AfterData, &s.Note, &s.Status, &s.CreatedAt, &s.ReviewedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find suggestion: %w", err)
	}
	return &s, nil
}

// UpdateStatus は提案のステータスを更新し reviewed_at を現在時刻にする。
func (r *SuggestionRepository) UpdateStatus(id uuid.UUID, status string) error {
	_, err := r.db.Exec(`UPDATE edit_suggestions SET status = $2, reviewed_at = NOW() WHERE id = $1`, id, status)
	if err != nil {
		return fmt.Errorf("update suggestion status: %w", err)
	}
	return nil
}

// CountPending は未処理（pending）の提案件数を返す（バッジ表示用）。
func (r *SuggestionRepository) CountPending() (int, error) {
	var n int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM edit_suggestions WHERE status = 'pending'`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count pending suggestions: %w", err)
	}
	return n, nil
}
