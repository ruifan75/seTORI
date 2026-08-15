package repository

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/ruifan75/setori/internal/models"
)

type TagRepository struct {
	db *sql.DB
}

func NewTagRepository(db *sql.DB) *TagRepository {
	return &TagRepository{db: db}
}

// ========== Stream Tags ==========

// FindAllStreamTags はすべての stream tag を取得する。
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

// CreateStreamTag は stream tag を追加する。
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

// DeleteStreamTag は stream tag を削除する。
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

// FindAllPerformanceTags はすべての performance tag を取得する。
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

// CreatePerformanceTag は performance tag を追加する。
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

// DeletePerformanceTag は performance tag を削除する。
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

// ========== Tag Keyword Rules（タイトルへの自動タグ付け規則）==========

// FindAllTagKeywordRules はすべての自動タグ付け規則を取得する。
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

// CreateTagKeywordRule は自動タグ付け規則を追加する（tag_id は既存の stream_tag に限る）。
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

// DeleteTagKeywordRule は自動タグ付け規則を削除する。
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

// ========== タグ漏れ（解析キャッシュ vs 歌唱） ==========
//
// コメント / Holodex の解析キャッシュは正規化のときに演奏バージョンのタグを付けるが、
// そのタグが歌唱に付くのは「編集フォームへ取り込んで保存した」ときだけ。取り込む前に
// 人が手で作った歌唱、規則を足す前に保存した歌唱、タグ ID の語彙がずれていた時期の
// 歌唱では、キャッシュにタグがあるのに歌唱には無い、という差が残る。
//
// 差分は派生値なので保存しない（毎回計算する）。保存するのは否定だけで、
// それが performance_tag_checks（migration 047）。

// TagGapRow はタグ漏れ 1 件（歌唱 1 つにつき 1 行）。
type TagGapRow struct {
	PerformanceID uuid.UUID `json:"performance_id"`
	StreamID      string    `json:"stream_id"`
	StreamTitle   string    `json:"stream_title"`
	StartSeconds  int       `json:"start_seconds"`
	SongID        uuid.UUID `json:"song_id"`
	SongName      string    `json:"song_name"`
	SongArtist    string    `json:"song_artist"`
	CurrentTags   []string  `json:"current_tags"`
	MissingTags   []string  `json:"missing_tags"`
	Sources       []string  `json:"sources"`     // comment / holodex
	CachedName    string    `json:"cached_name"` // 解析側の曲名（判断材料。原文のバージョン表記が残っている）
	// NameMatches は解析側の曲名と歌唱の曲名が同じものを指していそうか。
	// false は「同じ時刻に別の曲が登録されている」＝そのタグがこの歌唱のものとは限らない、の合図。
	NameMatches bool `json:"name_matches"`
}

// FindTagGaps は解析キャッシュにあって歌唱に無いタグを返す。
//
// 突き合わせは (配信, 開始秒 ±30) で、いちばん近い歌唱だけを相手にする
// （人が開始時刻を微調整していても拾えるように幅を持たせてあるが、
// 幅の中に複数あるとメドレーで隣の曲に付けてしまうため）。
//
// 時刻が同じでも曲が違うことがある（人が曲を差し替えた／コメントの曲名が誤り）。
// そこは機械では裁けないので落とさず、name_matches を付けて人に見せる。
func (r *TagRepository) FindTagGaps(limit int) ([]TagGapRow, error) {
	if limit <= 0 || limit > 1000 {
		limit = 300
	}
	rows, err := r.db.Query(`
		WITH cached AS (
			SELECT s.id AS stream_id, (e->>'start')::int AS start_sec,
			       COALESCE(e->>'name', '') AS name,
			       ARRAY(SELECT jsonb_array_elements_text(e->'tags')) AS tags,
			       'comment' AS src
			FROM streams s CROSS JOIN LATERAL jsonb_array_elements(s.comment_songs) e
			WHERE e ? 'tags' AND jsonb_array_length(e->'tags') > 0
		  UNION ALL
			SELECT s.id, (e->>'start_seconds')::int,
			       COALESCE(e->>'name', ''),
			       ARRAY(SELECT jsonb_array_elements_text(e->'tags')),
			       'holodex'
			FROM streams s CROSS JOIN LATERAL jsonb_array_elements(s.holodex_songs_normalized) e
			WHERE e ? 'tags' AND jsonb_array_length(e->'tags') > 0
		),
		perf AS (
			SELECT p.id, p.stream_id, p.start_seconds, so.name AS song_name,
			       COALESCE(array_agg(ppt.tag_id) FILTER (WHERE ppt.tag_id IS NOT NULL), '{}') AS tags
			FROM performances p
			JOIN songs so ON so.id = p.song_id
			LEFT JOIN performance_performance_tags ppt ON ppt.performance_id = p.id
			GROUP BY p.id, so.name
		),
		paired AS (
			SELECT DISTINCT ON (c.src, c.stream_id, c.start_sec)
			       pf.id AS performance_id, pf.tags AS perf_tags,
			       c.src, c.name AS cached_name, c.tags AS cached_tags,
			       -- 空白を落とした部分一致で「同じ曲を指していそうか」を見る。
			       -- 解析側の曲名にはバージョン表記が残る（"幾億光年 piano ver."）ので完全一致では見られない。
			       (position(lower(regexp_replace(pf.song_name, '[[:space:]　]', '', 'g'))
			                 in lower(regexp_replace(c.name, '[[:space:]　]', '', 'g'))) > 0
			        OR position(lower(regexp_replace(c.name, '[[:space:]　]', '', 'g'))
			                 in lower(regexp_replace(pf.song_name, '[[:space:]　]', '', 'g'))) > 0) AS name_matches
			FROM cached c
			JOIN perf pf ON pf.stream_id = c.stream_id AND abs(pf.start_seconds - c.start_sec) <= 30
			ORDER BY c.src, c.stream_id, c.start_sec, abs(pf.start_seconds - c.start_sec)
		),
		gaps AS (
			SELECT pr.performance_id, pr.src, pr.cached_name, pr.name_matches, m.tag_id
			FROM paired pr
			CROSS JOIN LATERAL (
				SELECT t AS tag_id FROM unnest(pr.cached_tags) t
				WHERE NOT (t = ANY(pr.perf_tags))
				  AND NOT EXISTS (
				      SELECT 1 FROM performance_tag_checks k
				      WHERE k.performance_id = pr.performance_id AND k.tag_id = t)
			) m
		)
		SELECT g.performance_id, p.stream_id, st.title, p.start_seconds,
		       p.song_id, so.name, so.original_artist,
		       COALESCE((SELECT array_agg(ppt.tag_id ORDER BY ppt.tag_id)
		                 FROM performance_performance_tags ppt
		                 WHERE ppt.performance_id = g.performance_id), '{}') AS current_tags,
		       array_agg(DISTINCT g.tag_id) AS missing_tags,
		       array_agg(DISTINCT g.src) AS sources,
		       min(g.cached_name) AS cached_name,
		       bool_or(g.name_matches) AS name_matches
		FROM gaps g
		JOIN performances p ON p.id = g.performance_id
		JOIN streams st ON st.id = p.stream_id
		JOIN songs so ON so.id = p.song_id
		GROUP BY g.performance_id, p.stream_id, st.title, st.stream_date,
		         p.start_seconds, p.song_id, so.name, so.original_artist
		ORDER BY st.stream_date DESC NULLS LAST, p.start_seconds
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("find tag gaps: %w", err)
	}
	defer rows.Close()

	out := []TagGapRow{}
	for rows.Next() {
		var x TagGapRow
		var current, missing, sources pq.StringArray
		if err := rows.Scan(&x.PerformanceID, &x.StreamID, &x.StreamTitle, &x.StartSeconds,
			&x.SongID, &x.SongName, &x.SongArtist, &current, &missing, &sources,
			&x.CachedName, &x.NameMatches); err != nil {
			return nil, fmt.Errorf("scan tag gap: %w", err)
		}
		x.CurrentTags = current
		x.MissingTags = missing
		x.Sources = sources
		if x.CurrentTags == nil {
			x.CurrentTags = []string{}
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// TagGapDismissalRow は「このタグは付けない」と判断した記録 1 件。
type TagGapDismissalRow struct {
	PerformanceID uuid.UUID `json:"performance_id"`
	TagID         string    `json:"tag_id"`
	StreamID      string    `json:"stream_id"`
	StreamTitle   string    `json:"stream_title"`
	StartSeconds  int       `json:"start_seconds"`
	SongName      string    `json:"song_name"`
	CheckedBy     string    `json:"checked_by"`
	CheckedAt     time.Time `json:"checked_at"`
}

// ListTagGapDismissals は無視した組を新しい順に返す。
//
// 無視は一覧からその組を消し続けるので、見えないと誤って無視したものを戻せない
// （song_identity_checks を管理画面から見直せるようにしてあるのと同じ理由）。
func (r *TagRepository) ListTagGapDismissals(limit int) ([]TagGapDismissalRow, error) {
	if limit <= 0 || limit > 1000 {
		limit = 300
	}
	rows, err := r.db.Query(`
		SELECT k.performance_id, k.tag_id, p.stream_id, st.title, p.start_seconds, so.name,
		       COALESCE(NULLIF(u.display_name, ''), u.username, ''), k.checked_at
		FROM performance_tag_checks k
		JOIN performances p ON p.id = k.performance_id
		JOIN streams st ON st.id = p.stream_id
		JOIN songs so ON so.id = p.song_id
		LEFT JOIN users u ON u.id = k.checked_by
		ORDER BY k.checked_at DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list tag gap dismissals: %w", err)
	}
	defer rows.Close()

	out := []TagGapDismissalRow{}
	for rows.Next() {
		var x TagGapDismissalRow
		if err := rows.Scan(&x.PerformanceID, &x.TagID, &x.StreamID, &x.StreamTitle,
			&x.StartSeconds, &x.SongName, &x.CheckedBy, &x.CheckedAt); err != nil {
			return nil, fmt.Errorf("scan tag gap dismissal: %w", err)
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// DismissTagGap は「この歌唱にこのタグは付けない」を記録する（既にあれば押し直すだけ）。
func (r *TagRepository) DismissTagGap(performanceID uuid.UUID, tagID string, checkedBy *uuid.UUID) error {
	_, err := r.db.Exec(`
		INSERT INTO performance_tag_checks (performance_id, tag_id, checked_by)
		VALUES ($1, $2, $3)
		ON CONFLICT (performance_id, tag_id)
		DO UPDATE SET checked_by = EXCLUDED.checked_by, checked_at = NOW()`,
		performanceID, tagID, checkedBy)
	if err != nil {
		return fmt.Errorf("dismiss tag gap: %w", err)
	}
	return nil
}

// UndismissTagGap は無視を取り消す（次の計算からまた一覧に出る）。
func (r *TagRepository) UndismissTagGap(performanceID uuid.UUID, tagID string) error {
	res, err := r.db.Exec(`DELETE FROM performance_tag_checks WHERE performance_id = $1 AND tag_id = $2`,
		performanceID, tagID)
	if err != nil {
		return fmt.Errorf("undismiss tag gap: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return fmt.Errorf("該当する記録が見つかりません")
	}
	return nil
}
