package repository

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/ruifan75/setori/internal/models"
)

// SuggestionRepository は edit_suggestions（閲覧モードからの修正提案）を扱う。
type SuggestionRepository struct {
	db *sql.DB
}

func NewSuggestionRepository(db *sql.DB) *SuggestionRepository {
	return &SuggestionRepository{db: db}
}

// suggestionColumns は SELECT 句とスキャン順を1か所に揃えるための共通定義。
const suggestionColumns = `id, target_type, target_id, target_key, target_label, kind,
	before_data, after_data, payload, note, status,
	created_by, created_by_name, client_hint, reviewed_by, review_note,
	created_at, reviewed_at`

func scanSuggestion(scan func(...any) error) (models.EditSuggestion, error) {
	var s models.EditSuggestion
	err := scan(&s.ID, &s.TargetType, &s.TargetID, &s.TargetKey, &s.TargetLabel, &s.Kind,
		&s.BeforeData, &s.AfterData, &s.Payload, &s.Note, &s.Status,
		&s.CreatedBy, &s.CreatedByName, &s.ClientHint, &s.ReviewedBy, &s.ReviewNote,
		&s.CreatedAt, &s.ReviewedAt)
	return s, err
}

// Create は提案を1件登録し、生成された行を返す。
func (r *SuggestionRepository) Create(s *models.EditSuggestion) (*models.EditSuggestion, error) {
	if len(s.Payload) == 0 {
		s.Payload = []byte("{}")
	}
	err := r.db.QueryRow(`
		INSERT INTO edit_suggestions
			(target_type, target_id, target_key, target_label, kind, before_data, after_data, payload, note,
			 created_by, created_by_name, client_hint)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING id, created_at, status`,
		s.TargetType, s.TargetID, s.TargetKey, s.TargetLabel, s.Kind, s.BeforeData, s.AfterData, s.Payload, s.Note,
		s.CreatedBy, s.CreatedByName, s.ClientHint).
		Scan(&s.ID, &s.CreatedAt, &s.Status)
	if err != nil {
		return nil, fmt.Errorf("create suggestion: %w", err)
	}
	return s, nil
}

// List は status / kind で絞った提案一覧をページングして返す。どちらも空なら全件。
func (r *SuggestionRepository) List(status, kind string, limit, offset int) ([]models.EditSuggestion, int, error) {
	var conds []string
	args := []any{}
	if status != "" {
		args = append(args, status)
		conds = append(conds, fmt.Sprintf("status = $%d", len(args)))
	}
	if kind != "" {
		args = append(args, kind)
		conds = append(conds, fmt.Sprintf("kind = $%d", len(args)))
	}
	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}

	var total int
	if err := r.db.QueryRow("SELECT COUNT(*) FROM edit_suggestions "+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count suggestions: %w", err)
	}

	query := fmt.Sprintf(`
		SELECT %s
		FROM edit_suggestions
		%s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d`, suggestionColumns, where, len(args)+1, len(args)+2)
	args = append(args, limit, offset)

	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list suggestions: %w", err)
	}
	defer rows.Close()

	var out []models.EditSuggestion
	for rows.Next() {
		s, err := scanSuggestion(rows.Scan)
		if err != nil {
			return nil, 0, fmt.Errorf("scan suggestion: %w", err)
		}
		out = append(out, s)
	}
	return out, total, rows.Err()
}

// TargetGroup は同一対象に集まった提案の1グループ。
// 対象の同一性は (TargetType, TargetID, TargetKey) で見る（配信は UUID を持たないため）。
type TargetGroup struct {
	TargetType  string
	TargetID    uuid.UUID
	TargetKey   string
	TargetLabel string
	Suggestions []models.EditSuggestion
}

// ListGroupedByTarget は status で絞った提案を「対象ごと」にまとめて返す。
// ページングの単位はグループ。同じ歌唱に届いた通報を1枚のカードで捌けるようにするため、
// グループが分断されないようページングは対象単位で行う。
//
// 返るグループは「最も新しい提案が新しい順」、グループ内は投稿の古い順。
// kind が空でなければその種別だけを返す（レビュー画面の種別の絞り込み用）。
func (r *SuggestionRepository) ListGroupedByTarget(status, kind string, limit, offset int) ([]TargetGroup, int, error) {
	var conds []string
	args := []any{}
	if status != "" {
		args = append(args, status)
		conds = append(conds, fmt.Sprintf("status = $%d", len(args)))
	}
	if kind != "" {
		args = append(args, kind)
		conds = append(conds, fmt.Sprintf("kind = $%d", len(args)))
	}
	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}

	// グループ総数（＝対象の種類数）
	var total int
	countQuery := fmt.Sprintf(
		`SELECT COUNT(*) FROM (SELECT 1 FROM edit_suggestions %s GROUP BY target_type, target_id, target_key) g`, where)
	if err := r.db.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count suggestion groups: %w", err)
	}

	// 先に対象を決めてから、その対象の提案だけを引く（グループがページ境界で割れないように）
	pageQuery := fmt.Sprintf(`
		SELECT target_type, target_id, target_key, MAX(created_at) AS latest
		FROM edit_suggestions
		%s
		GROUP BY target_type, target_id, target_key
		ORDER BY latest DESC
		LIMIT $%d OFFSET $%d`, where, len(args)+1, len(args)+2)

	pageArgs := append(append([]any{}, args...), limit, offset)
	rows, err := r.db.Query(pageQuery, pageArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("list suggestion groups: %w", err)
	}
	type key struct {
		targetType string
		targetID   uuid.UUID
		targetKey  string
	}
	var order []key
	for rows.Next() {
		var k key
		var latest time.Time
		if err := rows.Scan(&k.targetType, &k.targetID, &k.targetKey, &latest); err != nil {
			rows.Close()
			return nil, 0, fmt.Errorf("scan suggestion group: %w", err)
		}
		order = append(order, k)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if len(order) == 0 {
		return nil, total, nil
	}

	// UUID と文字列キーの両方で絞る。両方に合致しても別グループの行が混ざりうるが、
	// 下の byKey で3つ組の完全一致だけを拾うので問題ない。
	ids := make([]string, 0, len(order))
	keys := make([]string, 0, len(order))
	for _, k := range order {
		ids = append(ids, k.targetID.String())
		keys = append(keys, k.targetKey)
	}

	detailArgs := []any{pq.Array(ids), pq.Array(keys)}
	detailWhere := "WHERE target_id = ANY($1::uuid[]) AND target_key = ANY($2::varchar[])"
	if status != "" {
		detailArgs = append(detailArgs, status)
		detailWhere += fmt.Sprintf(" AND status = $%d", len(detailArgs))
	}
	if kind != "" {
		detailArgs = append(detailArgs, kind)
		detailWhere += fmt.Sprintf(" AND kind = $%d", len(detailArgs))
	}
	detailRows, err := r.db.Query(
		`SELECT `+suggestionColumns+` FROM edit_suggestions `+detailWhere+` ORDER BY created_at ASC`, detailArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("list grouped suggestions: %w", err)
	}
	defer detailRows.Close()

	byKey := make(map[key][]models.EditSuggestion, len(order))
	for detailRows.Next() {
		s, err := scanSuggestion(detailRows.Scan)
		if err != nil {
			return nil, 0, fmt.Errorf("scan suggestion: %w", err)
		}
		k := key{s.TargetType, s.TargetID, s.TargetKey}
		byKey[k] = append(byKey[k], s)
	}
	if err := detailRows.Err(); err != nil {
		return nil, 0, err
	}

	groups := make([]TargetGroup, 0, len(order))
	for _, k := range order {
		items := byKey[k]
		if len(items) == 0 {
			continue // 取得の合間に処理された（レビュー済みになった）
		}
		groups = append(groups, TargetGroup{
			TargetType:  k.targetType,
			TargetID:    k.targetID,
			TargetKey:   k.targetKey,
			TargetLabel: items[len(items)-1].TargetLabel, // 最新の提案時点の表示名
			Suggestions: items,
		})
	}
	return groups, total, nil
}

// ListByCreator は指定した利用者が出した提案をページングして返す（status が空なら全件）。
// 「自分の提案」画面で、取り下げや結果の確認に使う。
func (r *SuggestionRepository) ListByCreator(userID uuid.UUID, status string, limit, offset int) ([]models.EditSuggestion, int, error) {
	where := "WHERE created_by = $1"
	args := []any{userID}
	if status != "" {
		where += " AND status = $2"
		args = append(args, status)
	}

	var total int
	if err := r.db.QueryRow("SELECT COUNT(*) FROM edit_suggestions "+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count own suggestions: %w", err)
	}

	query := fmt.Sprintf(`
		SELECT %s
		FROM edit_suggestions
		%s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d`, suggestionColumns, where, len(args)+1, len(args)+2)
	args = append(args, limit, offset)

	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list own suggestions: %w", err)
	}
	defer rows.Close()

	var out []models.EditSuggestion
	for rows.Next() {
		s, err := scanSuggestion(rows.Scan)
		if err != nil {
			return nil, 0, fmt.Errorf("scan suggestion: %w", err)
		}
		out = append(out, s)
	}
	return out, total, rows.Err()
}

// FindPendingTimingByTarget は自動適用の判定に使う。
// 同一対象について、ログイン済みユーザーが出した未処理（pending）の提案を古い順で返す。
func (r *SuggestionRepository) FindPendingTimingByTarget(targetType string, targetID uuid.UUID) ([]models.EditSuggestion, error) {
	rows, err := r.db.Query(`SELECT `+suggestionColumns+`
		FROM edit_suggestions
		WHERE target_type = $1 AND target_id = $2 AND status = 'pending'
		  AND kind = 'field' AND created_by IS NOT NULL
		ORDER BY created_at ASC`, targetType, targetID)
	if err != nil {
		return nil, fmt.Errorf("find pending timing suggestions: %w", err)
	}
	defer rows.Close()

	var out []models.EditSuggestion
	for rows.Next() {
		s, err := scanSuggestion(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("scan suggestion: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// FindMissingSongsByStream は配信に積まれた perf.missing 提案をすべて返す（status は問わない）。
//
// 重複の判定に使う。**却下済みも返すのが要点** ── 却下を覚える先がここしかないので、
// 処理済みを除くと「却下したものが次の一括実行でまた出る」が直らない。
// 配信あたりの件数はせいぜい数十なので、突き合わせ（開始秒・曲名キー）は呼び出し側で行う。
func (r *SuggestionRepository) FindMissingSongsByStream(streamID string) ([]models.EditSuggestion, error) {
	rows, err := r.db.Query(`SELECT `+suggestionColumns+`
		FROM edit_suggestions
		WHERE kind = 'perf.missing' AND target_key = $1
		ORDER BY created_at ASC`, streamID)
	if err != nil {
		return nil, fmt.Errorf("find missing song suggestions: %w", err)
	}
	defer rows.Close()

	var out []models.EditSuggestion
	for rows.Next() {
		s, err := scanSuggestion(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("scan suggestion: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// FindByID は提案を1件取得する。見つからなければ nil。
func (r *SuggestionRepository) FindByID(id uuid.UUID) (*models.EditSuggestion, error) {
	row := r.db.QueryRow(`SELECT `+suggestionColumns+` FROM edit_suggestions WHERE id = $1`, id)
	s, err := scanSuggestion(row.Scan)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find suggestion: %w", err)
	}
	return &s, nil
}

// Delete は提案を1件削除する（取り下げ）。
// 却下と違い履歴を残さない：本人が「やっぱり無し」と引っ込めただけのものを
// レビュー履歴に積むと、実際の判断の記録が埋もれるため。
func (r *SuggestionRepository) Delete(id uuid.UUID) error {
	if _, err := r.db.Exec(`DELETE FROM edit_suggestions WHERE id = $1`, id); err != nil {
		return fmt.Errorf("delete suggestion: %w", err)
	}
	return nil
}

// UpdateStatus は提案のステータスを更新し、レビュー者・理由・時刻を記録する。
// reviewer は未ログイン経路では nil（現状レビューは要権限なので通常は入る）。
func (r *SuggestionRepository) UpdateStatus(id uuid.UUID, status string, reviewer *uuid.UUID, note string) error {
	_, err := r.db.Exec(`
		UPDATE edit_suggestions
		SET status = $2, reviewed_by = $3, review_note = $4, reviewed_at = NOW()
		WHERE id = $1`, id, status, reviewer, note)
	if err != nil {
		return fmt.Errorf("update suggestion status: %w", err)
	}
	return nil
}

// CountPending は未処理（pending）の提案件数を返す（バッジ表示用）。
// 衝突（conflict）も人手の判断待ちなので同じバッジに含める。
func (r *SuggestionRepository) CountPending() (int, error) {
	var n int
	if err := r.db.QueryRow(
		`SELECT COUNT(*) FROM edit_suggestions WHERE status IN ('pending', 'conflict')`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count pending suggestions: %w", err)
	}
	return n, nil
}

// CountRecentBy は直近 windowSeconds 秒間に同じ提案者から投稿された件数を返す。
// createdBy が nil のときは clientHint（匿名）で数える。投稿の rate limit 判定に使う。
func (r *SuggestionRepository) CountRecentBy(createdBy *uuid.UUID, clientHint string, windowSeconds int) (int, error) {
	var (
		n   int
		err error
	)
	if createdBy != nil {
		err = r.db.QueryRow(`
			SELECT COUNT(*) FROM edit_suggestions
			WHERE created_by = $1 AND created_at > NOW() - ($2 || ' seconds')::interval`,
			*createdBy, windowSeconds).Scan(&n)
	} else {
		if clientHint == "" {
			return 0, nil // 手がかりが無ければ数えようがない（判定はスキップ）
		}
		err = r.db.QueryRow(`
			SELECT COUNT(*) FROM edit_suggestions
			WHERE created_by IS NULL AND client_hint = $1
			  AND created_at > NOW() - ($2 || ' seconds')::interval`,
			clientHint, windowSeconds).Scan(&n)
	}
	if err != nil {
		return 0, fmt.Errorf("count recent suggestions: %w", err)
	}
	return n, nil
}
