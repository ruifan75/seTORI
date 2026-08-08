package repository

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/lib/pq"

	"github.com/ruifan75/setori/internal/models"
)

type OrganizationRepository struct {
	db *sql.DB
}

func NewOrganizationRepository(db *sql.DB) *OrganizationRepository {
	return &OrganizationRepository{db: db}
}

const organizationColumns = `key, display_name, sort_order, created_at, updated_at`

func scanOrganization(row interface{ Scan(...any) error }) (models.Organization, error) {
	var o models.Organization
	err := row.Scan(&o.Key, &o.DisplayName, &o.SortOrder, &o.CreatedAt, &o.UpdatedAt)
	return o, err
}

// organizationOrder は一覧の並び順。sort_order が小さいほど先、同値なら表示名の五十音順。
// 表示名は日本語が入るので、名前ソートの共通定義（読みキー）を使う。
var organizationOrder = "sort_order ASC, " + nameSortOrder("display_name", "''")

// FindAll は全事務所を並び順で返す。
func (r *OrganizationRepository) FindAll() ([]models.Organization, error) {
	rows, err := r.db.Query(`SELECT ` + organizationColumns + ` FROM organizations ORDER BY ` + organizationOrder)
	if err != nil {
		return nil, fmt.Errorf("query organizations: %w", err)
	}
	defer rows.Close()

	orgs := []models.Organization{}
	for rows.Next() {
		o, err := scanOrganization(rows)
		if err != nil {
			return nil, fmt.Errorf("scan organization: %w", err)
		}
		orgs = append(orgs, o)
	}
	return orgs, nil
}

// FindByKey は1件取得する。見つからなければ (nil, nil)。
func (r *OrganizationRepository) FindByKey(key string) (*models.Organization, error) {
	o, err := scanOrganization(r.db.QueryRow(
		`SELECT `+organizationColumns+` FROM organizations WHERE key = $1`, key))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find organization: %w", err)
	}
	return &o, nil
}

// EnsureExists は取り込み時に呼ぶ。未知の key なら display_name = key で作る。
//
// 「知らない事務所だから取り込まない」は選ばない。song_merge_candidates と同じ考えで、
// 人の確認が要るものは残しておくが、登録そのものは止めない。表示名は後から直せる。
// 既にある行の display_name は上書きしない（人が直した表示名を毎回の同期で戻さないため）。
func (r *OrganizationRepository) EnsureExists(key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil
	}
	_, err := r.db.Exec(`
		INSERT INTO organizations (key, display_name)
		VALUES ($1, $1)
		ON CONFLICT (key) DO NOTHING`, key)
	if err != nil {
		return fmt.Errorf("ensure organization %q: %w", key, err)
	}
	return nil
}

// Create は事務所を手で追加する（Holodex に無い事務所用）。
// key が既にあれば ErrOrganizationExists 相当として重複を呼び出し側へ伝える。
func (r *OrganizationRepository) Create(o *models.Organization) (bool, error) {
	err := r.db.QueryRow(`
		INSERT INTO organizations (key, display_name, sort_order)
		VALUES ($1, $2, $3)
		ON CONFLICT (key) DO NOTHING
		RETURNING created_at, updated_at`,
		o.Key, o.DisplayName, o.SortOrder).Scan(&o.CreatedAt, &o.UpdatedAt)
	if err == sql.ErrNoRows {
		return false, nil // 既に同じ key がある
	}
	if err != nil {
		return false, fmt.Errorf("create organization: %w", err)
	}
	return true, nil
}

// Update は表示名と並び順を更新する。key は取り込み時の値なので変えない。
// 見つからなければ (nil, nil)。
func (r *OrganizationRepository) Update(o *models.Organization) (*models.Organization, error) {
	updated, err := scanOrganization(r.db.QueryRow(`
		UPDATE organizations
		SET display_name = $2, sort_order = $3, updated_at = NOW()
		WHERE key = $1
		RETURNING `+organizationColumns,
		o.Key, o.DisplayName, o.SortOrder))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("update organization: %w", err)
	}
	return &updated, nil
}

// Delete は事務所を消す。所属チャンネルが残っていれば FK（RESTRICT）で失敗するので、
// それを ErrOrganizationInUse として区別できるよう bool で返す。
func (r *OrganizationRepository) Delete(key string) (deleted bool, inUse bool, err error) {
	res, err := r.db.Exec("DELETE FROM organizations WHERE key = $1", key)
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23503" { // foreign_key_violation
			return false, true, nil
		}
		return false, false, fmt.Errorf("delete organization: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, false, fmt.Errorf("delete organization: %w", err)
	}
	return affected > 0, false, nil
}

// CountSingers は事務所に所属するチャンネル数を返す（管理画面の表示・削除前の確認用）。
func (r *OrganizationRepository) CountSingers() (map[string]int, error) {
	rows, err := r.db.Query(`
		SELECT organization, COUNT(*)
		FROM singers
		WHERE organization IS NOT NULL
		GROUP BY organization`)
	if err != nil {
		return nil, fmt.Errorf("count singers by organization: %w", err)
	}
	defer rows.Close()

	counts := map[string]int{}
	for rows.Next() {
		var key string
		var n int
		if err := rows.Scan(&key, &n); err != nil {
			return nil, fmt.Errorf("scan organization count: %w", err)
		}
		counts[key] = n
	}
	return counts, nil
}
