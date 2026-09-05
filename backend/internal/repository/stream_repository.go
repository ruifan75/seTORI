package repository

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/lib/pq"
	"github.com/ruifan75/setori/internal/models"
	"github.com/ruifan75/setori/pkg/util"
)

type StreamRepository struct {
	db *sql.DB
}

func NewStreamRepository(db *sql.DB) *StreamRepository {
	return &StreamRepository{db: db}
}

// FindAll はすべての歌枠を取得する（ページング対応、既定では非表示を除外）。
func (r *StreamRepository) FindAll(limit, offset int, includeHidden bool, sort, dir string) ([]models.Stream, int, error) {
	var total int
	var countQuery string
	if includeHidden {
		countQuery = "SELECT COUNT(*) FROM streams"
	} else {
		countQuery = "SELECT COUNT(*) FROM streams WHERE is_hidden = FALSE"
	}
	err := r.db.QueryRow(countQuery).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count streams: %w", err)
	}

	// 既定は配信日の新しい順。"title" 指定でタイトルの五十音順。
	order := "stream_date " + dirOr(dir, "desc")
	if sort == "title" {
		order = nameSortOrderDir("title", "''", dir)
	}

	var query string
	if includeHidden {
		query = streamListQuery("streams", `
			ORDER BY `+order+`
			LIMIT $1 OFFSET $2`)
	} else {
		query = streamListQuery("streams", `
			WHERE is_hidden = FALSE
			ORDER BY `+order+`
			LIMIT $1 OFFSET $2`)
	}

	rows, err := r.db.Query(query, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("query streams: %w", err)
	}
	defer rows.Close()

	var streams []models.Stream
	for rows.Next() {
		var s models.Stream
		s, err := scanStreamRow(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scan stream: %w", err)
		}
		streams = append(streams, s)
	}

	return streams, total, nil
}

// streamListColumns は一覧系が読む列。**scanStreamRow と対で使うこと。**
//
// 実効的な秘匿（EffectiveRestrictedExpr）は SELECT でしか計算されないので、
// 列を書き並べる方式だと 1 か所書き忘れても**コンパイルは通り、Scan で初めて落ちる**
// （実際 FindBySingerID がそうなっていて、歌手配下の一覧が 500 になっていた）。
// 定数と scanner を対にして、書き忘れる余地を無くしてある。
func streamListColumns(alias string) string {
	p := alias + "."
	return p + "id, " + p + "title, " + p + "stream_date, " + p + "duration_seconds, " + p + "thumbnail_url, " +
		p + "holodex_data, " + p + "holodex_hash, " + p + "comment_raw, " + p + "comment_songs, " +
		p + "is_processed, " + p + "is_hidden, " + p + "restriction_override, " +
		p + "created_at, " + p + "updated_at, " + EffectiveRestrictedExpr(alias)
}

// streamListQuery は一覧系の SELECT を丸ごと組み立てる。呼び出し側が書くのは
// FROM 以降（JOIN / WHERE / ORDER BY / LIMIT）だけ。
//
// **列を継ぎ足す口を呼び出し側に残さない**のが目的。列名を並べる方式をやめて
// streamListColumns に集約したあとも、FindByTagID と SearchStreams が
// `streamListColumns(...) + ", " + EffectiveRestrictedExpr(...)` と書いて
// 17 列 vs 16 引数になり、タグ別一覧と配信検索が常に 500 を返していた
// ── 列の「書き忘れ」は塞いだが「継ぎ足し」は塞げていなかった。
// SELECT ごと持てば、列をいじるには必ずこの対を通る。
func streamListQuery(alias, rest string) string {
	from := "FROM streams"
	if alias != "streams" {
		from += " " + alias
	}
	return "SELECT " + streamListColumns(alias) + "\n" + from + "\n" + rest
}

// scanStreamRow は streamListColumns で引いた 1 行を読む。
func scanStreamRow(row interface{ Scan(...any) error }) (models.Stream, error) {
	var s models.Stream
	err := row.Scan(&s.ID, &s.Title, &s.StreamDate, &s.DurationSeconds,
		&s.ThumbnailURL, &s.HolodexData, &s.HolodexHash, &s.CommentRaw, &s.CommentSongs,
		&s.IsProcessed, &s.IsHidden, &s.RestrictionOverride,
		&s.CreatedAt, &s.UpdatedAt, &s.IsRestrictedEffective)
	return s, err
}

// FindByID は動画 ID で歌枠を取得する。
func (r *StreamRepository) FindByID(id string) (*models.Stream, error) {
	query := `
		SELECT id, title, stream_date, duration_seconds, thumbnail_url, holodex_data, holodex_hash, comment_raw, comment_songs, comment_songs_analyzed_at, chapter_raw, chapter_songs, is_processed, is_hidden, restriction_override, holodex_uploaded_at, holodex_upload_unknown, availability, playable_in_embed, availability_checked_at, created_at, updated_at,
		       ` + EffectiveRestrictedExpr("streams") + ` AS is_restricted_effective
		FROM streams WHERE id = $1`

	var s models.Stream
	err := r.db.QueryRow(query, id).Scan(
		&s.ID, &s.Title, &s.StreamDate, &s.DurationSeconds,
		&s.ThumbnailURL, &s.HolodexData, &s.HolodexHash, &s.CommentRaw, &s.CommentSongs, &s.CommentSongsAnalyzedAt,
		&s.ChapterRaw, &s.ChapterSongs, &s.IsProcessed, &s.IsHidden, &s.RestrictionOverride, &s.HolodexUploadedAt, &s.HolodexUploadUnknown,
		&s.Availability, &s.PlayableInEmbed, &s.AvailabilityCheckedAt, &s.CreatedAt, &s.UpdatedAt, &s.IsRestrictedEffective)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find stream by id: %w", err)
	}
	return &s, nil
}

// Create は新しい歌枠を作成する。
func (r *StreamRepository) Create(s *models.Stream) error {
	query := `
		INSERT INTO streams (id, title, stream_date, duration_seconds, thumbnail_url, holodex_data, holodex_hash)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING created_at, updated_at`

	err := r.db.QueryRow(query, s.ID, s.Title, s.StreamDate, s.DurationSeconds,
		s.ThumbnailURL, s.HolodexData, s.HolodexHash).
		Scan(&s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create stream: %w", err)
	}
	return nil
}

// UpdateMetadata は利用者が編集できる metadata フィールドだけを更新し、大きな JSONB（holodex_data / comment_*）には触れない。
// 通常の情報更新で問題を含む可能性のある JSONB データを書き戻さないため。
// restrictionOverride は人の裁定（NULL＝未裁定）。**検出の members_only タグは書かない** ──
// 人が触った事実と、検出が言っていることは別の列に残す。
func (r *StreamRepository) UpdateMetadata(id string, title string, streamDate time.Time, isProcessed, isHidden bool, restrictionOverride sql.NullBool) error {
	query := `
		UPDATE streams
		SET title = $2, stream_date = $3, is_processed = $4, is_hidden = $5, restriction_override = $6, updated_at = NOW()
		WHERE id = $1
		RETURNING updated_at`

	var updatedAt time.Time
	err := r.db.QueryRow(query, id, title, streamDate, isProcessed, isHidden, restrictionOverride).Scan(&updatedAt)
	if err != nil {
		return fmt.Errorf("update stream metadata: %w", err)
	}
	return nil
}

// UpdateStatus は歌枠の状態（処理済み／非表示）を更新する。
func (r *StreamRepository) UpdateStatus(id string, isProcessed, isHidden bool) error {
	query := `UPDATE streams SET is_processed = $2, is_hidden = $3, updated_at = NOW() WHERE id = $1`
	_, err := r.db.Exec(query, id, isProcessed, isHidden)
	if err != nil {
		return fmt.Errorf("update stream status: %w", err)
	}
	return nil
}

// Upsert は歌枠を作成または更新する（Holodex 同期用）。
func (r *StreamRepository) Upsert(s *models.Stream) error {
	query := `
		INSERT INTO streams (id, title, stream_date, duration_seconds, thumbnail_url, holodex_data, holodex_hash, is_hidden)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (id) DO UPDATE SET
			title = EXCLUDED.title,
			stream_date = EXCLUDED.stream_date,
			duration_seconds = EXCLUDED.duration_seconds,
			thumbnail_url = EXCLUDED.thumbnail_url,
			holodex_data = EXCLUDED.holodex_data,
			holodex_hash = EXCLUDED.holodex_hash,
			updated_at = NOW()
		RETURNING created_at, updated_at`

	err := r.db.QueryRow(query, s.ID, s.Title, s.StreamDate, s.DurationSeconds,
		s.ThumbnailURL, s.HolodexData, s.HolodexHash, s.IsHidden).
		Scan(&s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return fmt.Errorf("upsert stream: %w", err)
	}
	return nil
}

// Delete は歌枠を削除する。
func (r *StreamRepository) Delete(id string) error {
	_, err := r.db.Exec("DELETE FROM streams WHERE id = $1", id)
	if err != nil {
		return fmt.Errorf("delete stream: %w", err)
	}
	return nil
}

// FindWithoutCommentSongs は comment_raw を持つすべての配信を取得する（backfill／再生成用）。
func (r *StreamRepository) FindWithoutCommentSongs() ([]models.Stream, error) {
	query := streamListQuery("streams", `
		WHERE comment_raw IS NOT NULL AND comment_raw != 'null'`)

	rows, err := r.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("query streams without comment songs: %w", err)
	}
	defer rows.Close()

	var streams []models.Stream
	for rows.Next() {
		var s models.Stream
		s, err := scanStreamRow(rows)
		if err != nil {
			return nil, fmt.Errorf("scan stream: %w", err)
		}
		streams = append(streams, s)
	}

	return streams, nil
}

// FindIDsWithCommentSongs は comment_songs を持つすべての歌枠 ID を取得する（拍手 end の backfill 用）。
func (r *StreamRepository) FindIDsWithCommentSongs() ([]string, error) {
	rows, err := r.db.Query(`
		SELECT id FROM streams
		WHERE comment_songs IS NOT NULL AND comment_songs::text NOT IN ('null', '[]')
		ORDER BY stream_date DESC`)
	if err != nil {
		return nil, fmt.Errorf("query streams with comment songs: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// CommentHashRow は comment_songs_hash 補正用の最小行（id + 現在の comment_raw + 保存済み hash）。
type CommentHashRow struct {
	ID         string
	CommentRaw []byte
	Hash       sql.NullString
	// CommentSongs は保存済みの抽出結果そのもの。**空配列も含めて渡す。**
	// キャッシュ判定は decode 後に 1 曲以上あることまで見るので、
	// 「空の結果 + 現行 hash」の行も再抽出される。行ごと落とすとそれを数え落とす。
	CommentSongs []byte
}

// FindCommentHashRows は comment_songs を持つ全歌回の
// (id, comment_raw, comment_songs_hash, comment_songs) を返す。
// 抽出キャッシュの世代を数える監査に使う（BackfillCommentSongsHashes）。
// 以前はここから hash を書き換えていたが、canonical に抽出規則の版が混ざってからは
// **数えるだけ**（旧規則の結果へ現行版の hash を貼ると誤りが固定されるため）。
func (r *StreamRepository) FindCommentHashRows() ([]CommentHashRow, error) {
	// **空配列を除外しない。** キャッシュ判定は decode 後に 1 曲以上あることまで要求するので、
	// comment_songs = '[]' の行も次の解析で再抽出される。ここで落とすと監査から消える。
	rows, err := r.db.Query(`
		SELECT id, comment_raw, comment_songs_hash, comment_songs FROM streams
		WHERE comment_songs IS NOT NULL`)
	if err != nil {
		return nil, fmt.Errorf("query comment hash rows: %w", err)
	}
	defer rows.Close()

	var out []CommentHashRow
	for rows.Next() {
		var row CommentHashRow
		if err := rows.Scan(&row.ID, &row.CommentRaw, &row.Hash, &row.CommentSongs); err != nil {
			return nil, fmt.Errorf("scan comment hash row: %w", err)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// UpdateCommentSongsHash は comment_songs_hash のみを書き換える（comment_songs / comment_raw は不変）。
func (r *StreamRepository) UpdateCommentSongsHash(id, hash string) error {
	_, err := r.db.Exec(`UPDATE streams SET comment_songs_hash = $2 WHERE id = $1`, id, hash)
	if err != nil {
		return fmt.Errorf("update comment_songs_hash: %w", err)
	}
	return nil
}

// FindByDateRange は日付範囲で歌枠を取得する。
func (r *StreamRepository) FindByDateRange(start, end time.Time) ([]models.Stream, error) {
	query := streamListQuery("streams", `
		WHERE stream_date >= $1 AND stream_date <= $2
		ORDER BY stream_date DESC`)

	rows, err := r.db.Query(query, start, end)
	if err != nil {
		return nil, fmt.Errorf("query streams by date range: %w", err)
	}
	defer rows.Close()

	var streams []models.Stream
	for rows.Next() {
		var s models.Stream
		s, err := scanStreamRow(rows)
		if err != nil {
			return nil, fmt.Errorf("scan stream: %w", err)
		}
		streams = append(streams, s)
	}

	return streams, nil
}

// GetTags は歌枠に付いたすべてのタグを取得する。
func (r *StreamRepository) GetTags(streamID string) ([]models.StreamTag, error) {
	query := `
		SELECT st.id, st.display_name, st.color, st.created_at
		FROM stream_tags st
		JOIN stream_stream_tags sst ON st.id = sst.tag_id
		WHERE sst.stream_id = $1`

	rows, err := r.db.Query(query, streamID)
	if err != nil {
		return nil, fmt.Errorf("get stream tags: %w", err)
	}
	defer rows.Close()

	var tags []models.StreamTag
	for rows.Next() {
		var t models.StreamTag
		err := rows.Scan(&t.ID, &t.DisplayName, &t.Color, &t.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("scan stream tag: %w", err)
		}
		tags = append(tags, t)
	}

	return tags, nil
}

// GetTagsForStreams は複数の歌枠のタグを一括取得し、N+1 を避ける。
func (r *StreamRepository) GetTagsForStreams(streamIDs []string) (map[string][]models.StreamTag, error) {
	result := make(map[string][]models.StreamTag, len(streamIDs))
	if len(streamIDs) == 0 {
		return result, nil
	}

	query := `
		SELECT sst.stream_id, st.id, st.display_name, st.color, st.created_at
		FROM stream_tags st
		JOIN stream_stream_tags sst ON st.id = sst.tag_id
		WHERE sst.stream_id = ANY($1)`

	rows, err := r.db.Query(query, pq.Array(streamIDs))
	if err != nil {
		return nil, fmt.Errorf("get tags for streams: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var streamID string
		var t models.StreamTag
		if err := rows.Scan(&streamID, &t.ID, &t.DisplayName, &t.Color, &t.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan stream tag: %w", err)
		}
		result[streamID] = append(result[streamID], t)
	}
	return result, rows.Err()
}

// GetSingersForStreams は複数の歌枠の参加者とチャンネル所有者を一括取得し、N+1 を避ける。
func (r *StreamRepository) GetSingersForStreams(streamIDs []string) (participants map[string][]models.Singer, owners map[string]*models.Singer, err error) {
	participants = make(map[string][]models.Singer, len(streamIDs))
	owners = make(map[string]*models.Singer, len(streamIDs))
	if len(streamIDs) == 0 {
		return participants, owners, nil
	}

	query := `
		SELECT ss.stream_id, ss.is_owner, s.id, s.name, s.english_name, s.photo_url, COALESCE(s.organization_override, s.organization), o.display_name, COALESCE(o.is_unaffiliated, FALSE), s.metadata_source, s.created_at, s.updated_at
		FROM singers s
		LEFT JOIN organizations o ON COALESCE(s.organization_override, s.organization) = o.key
		JOIN stream_singers ss ON s.id = ss.singer_id
		WHERE ss.stream_id = ANY($1)`

	rows, err := r.db.Query(query, pq.Array(streamIDs))
	if err != nil {
		return nil, nil, fmt.Errorf("get singers for streams: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var streamID string
		var isOwner bool
		var sg models.Singer
		if err := rows.Scan(&streamID, &isOwner, &sg.ID, &sg.Name, &sg.EnglishName,
			&sg.PhotoURL, &sg.Organization, &sg.OrganizationName, &sg.OrganizationUnaffil, &sg.MetadataSource, &sg.CreatedAt, &sg.UpdatedAt); err != nil {
			return nil, nil, fmt.Errorf("scan stream singer: %w", err)
		}
		participants[streamID] = append(participants[streamID], sg)
		if isOwner {
			owner := sg
			owners[streamID] = &owner
		}
	}
	return participants, owners, rows.Err()
}

// AddTag は歌枠にタグを追加する。
func (r *StreamRepository) AddTag(streamID, tagID string) error {
	query := `INSERT INTO stream_stream_tags (stream_id, tag_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`
	_, err := r.db.Exec(query, streamID, tagID)
	if err != nil {
		return fmt.Errorf("add stream tag: %w", err)
	}
	return nil
}

// RemoveTag は歌枠からタグを外す。
func (r *StreamRepository) RemoveTag(streamID, tagID string) error {
	_, err := r.db.Exec("DELETE FROM stream_stream_tags WHERE stream_id = $1 AND tag_id = $2", streamID, tagID)
	if err != nil {
		return fmt.Errorf("remove stream tag: %w", err)
	}
	return nil
}

// SetInitialVisibility は新規配信のタグ付け完了後に、初回の表示判定を確定する。
// 呼び出し側は既存配信には使わない。以後の表示状態は手動編集だけが変更する。
func (r *StreamRepository) SetInitialVisibility(streamID string, hidden bool) error {
	_, err := r.db.Exec(`
		UPDATE streams
		SET is_hidden = $2, updated_at = NOW()
		WHERE id = $1
		  AND is_hidden IS DISTINCT FROM $2`, streamID, hidden)
	if err != nil {
		return fmt.Errorf("set initial stream visibility: %w", err)
	}
	return nil
}

// ErrStreamNotFound は対象の配信が存在しないこと。UPDATE は存在しない ID でも
// error にならないので、行数を見て返す側でこれを使う。
var ErrStreamNotFound = errors.New("stream not found")

// MarkHolodexUploadAttempt は Holodex への**送信を試みた**時刻を記録する
// （外部コピーの台帳。migration 054）。
//
// **「送信済み」ではない。** 呼ぶのは PUT の前で、実際に届いたかは分からない
// （通信で失敗することも、Holodex 側が拒むこともある）。安全側に倒すための記録なので、
// 過剰に残るぶんには構わない。
//
// **最新の 1 回だけ持つ。** 履歴を持たないのは、この値を「いつ送ったか」ではなく
// 「外部にコピーがあるかもしれない」の旗として使うため。
// なお**再送しても向こうが最新になるとは限らない** ── 既に iTunes ID がある曲は
// PUT せず飛ばし、削除の呼び出しも無いので、こちらで直した内容が向こうに古いまま残りうる。
func (r *StreamRepository) MarkHolodexUploadAttempt(streamID string) error {
	_, err := r.db.Exec(`UPDATE streams SET holodex_uploaded_at = NOW() WHERE id = $1`, streamID)
	if err != nil {
		return fmt.Errorf("mark holodex uploaded: %w", err)
	}
	return nil
}

// MarkMembersOnly は会限の検出を記録する（members_only タグを付ける）。
//
// **新規同期にも効かせるために要る。** yt-dlp が availability を取りに行くのは
// 限られた経路だけなので、それを待つ間このデータは公開側に置かれてしまう。
// 会限と分かる材料（Holodex の topic / タイトル規則）は同期の時点で揃っている。
//
// **付けるだけで外さない。** 外せるのは人だけ（「これは会限ではない」という
// 事実の訂正）。「会限だが公開してよい」は restriction_override で表す。
func (r *StreamRepository) MarkMembersOnly(streamID string) error {
	_, err := r.db.Exec(`
		INSERT INTO stream_stream_tags (stream_id, tag_id) VALUES ($1, $2)
		ON CONFLICT (stream_id, tag_id) DO NOTHING`, streamID, MembersOnlyTagID)
	if err != nil {
		return fmt.Errorf("mark members only: %w", err)
	}
	return nil
}

// FindByTagID は指定タグが付いた配信を新しい順で返す（タグ検索用、非表示は除外）。
func (r *StreamRepository) FindByTagID(tagID string, limit, offset int) ([]models.Stream, int, error) {
	var total int
	err := r.db.QueryRow(`
		SELECT COUNT(*)
		FROM streams s
		JOIN stream_stream_tags sst ON sst.stream_id = s.id
		WHERE sst.tag_id = $1 AND s.is_hidden = FALSE`, tagID).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count streams by tag: %w", err)
	}

	rows, err := r.db.Query(streamListQuery("s", `
		JOIN stream_stream_tags sst ON sst.stream_id = s.id
		WHERE sst.tag_id = $1 AND s.is_hidden = FALSE
		ORDER BY s.stream_date DESC
		LIMIT $2 OFFSET $3`), tagID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("query streams by tag: %w", err)
	}
	defer rows.Close()

	var streams []models.Stream
	for rows.Next() {
		var s models.Stream
		s, err := scanStreamRow(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scan stream: %w", err)
		}
		streams = append(streams, s)
	}
	return streams, total, rows.Err()
}

// FindStreamsForBatch は一括分析の対象配信（id/title のみ）を mode と（任意の）歌手で
// 絞り込んで古い順に返す。singerID が空なら全チャンネルが対象。
//
// mode 別の対象範囲（いずれも comment_raw あり）:
//   - "unanalyzed"           : 分析結果（comment_songs）が一度も無い配信のみ
//   - "unprocessed"/"refresh": 未処理（ユーザー未確認）の配信すべて
//   - "reanalyze"            : comment_raw を持つ配信すべて（分析済みも対象。force で作り直す）
//
// singerID 指定時は stream_singers を EXISTS で絞る（owner / 参加者どちらでも一致）。
//
// hidden は非表示配信の扱い。nil=両方 / false=非表示を除く（既定）/ true=非表示だけ。
// **既定を「除く」に据え置くのは、通常の運用で雑談・ゲーム配信を毎回 AI にかけないため。**
// 非表示を回すのは抽出規則を変えた後の棚卸しという別の作業で、そのときだけ明示的に選ぶ
// ── 非表示に残っていた抽出結果は構造フィルタ（2026-08-05）と grouped 経路（2026-08-07）が
// 入る前のもので、現在の規則なら落ちる行を大量に含んでいる。
func (r *StreamRepository) FindStreamsForBatch(mode, singerID string, hidden *bool) ([]models.Stream, error) {
	// comment_raw に中身があるものだけを対象にする。
	//
	// `IS NOT NULL AND != 'null'` では**空配列 `[]` が通ってしまう**。migration 014 が
	// SQL NULL と JSON null を `[]` へ正規化したので、本番では隠し 720 本のうち 344 本が
	// これに当たる。通すと「保存済みの入力を processing し直す」つもりの実行が、
	// 半分は遠隔からの再取得に化ける（失敗すると 3 回試行 × 90 秒の冷却が積み上がる）。
	//
	// refresh だけは例外。あちらは RefreshCommentRaw で取り直すのが役目なので、
	// 中身が無い配信こそ対象に入れる必要がある。
	// refresh は raw の有無を条件にしない。取り直すのが役目なので、まだ何も入っていない
	// 配信こそ対象に入れる必要がある。`comment_raw IS NOT NULL` を残すと、
	// migration 014 の正規化を受けていない**新しく同期された配信は SQL NULL のまま**
	// （streams.comment_raw に default は無く、Upsert もこの列を指定しない）なので、
	// 「中身が無い配信こそ再取得する」という説明と食い違う。
	where := "TRUE"
	if mode != "refresh" {
		where = "jsonb_typeof(comment_raw) = 'array' AND jsonb_array_length(comment_raw) > 0"
	}
	if hidden != nil {
		if *hidden {
			where = "is_hidden = TRUE AND " + where
		} else {
			where = "is_hidden = FALSE AND " + where
		}
	}
	switch mode {
	case "unanalyzed":
		where += " AND (comment_songs IS NULL OR comment_songs = 'null')"
	case "unprocessed", "refresh":
		where += " AND is_processed = FALSE"
	case "reanalyze":
		// 追加条件なし：comment_raw を持つ配信すべてを再分析対象にする
	default:
		return nil, fmt.Errorf("unknown batch mode: %s", mode)
	}

	args := []any{}
	if singerID != "" {
		args = append(args, singerID)
		where += fmt.Sprintf(" AND EXISTS (SELECT 1 FROM stream_singers ss WHERE ss.stream_id = s.id AND ss.singer_id = $%d)", len(args))
	}

	rows, err := r.db.Query(`SELECT s.id, s.title FROM streams s WHERE `+where+` ORDER BY s.stream_date ASC`, args...)
	if err != nil {
		return nil, fmt.Errorf("query batch streams: %w", err)
	}
	defer rows.Close()

	var streams []models.Stream
	for rows.Next() {
		var s models.Stream
		if err := rows.Scan(&s.ID, &s.Title); err != nil {
			return nil, fmt.Errorf("scan stream: %w", err)
		}
		streams = append(streams, s)
	}
	return streams, rows.Err()
}

// SearchStreams は配信元・参加者・ボーカル・タグを AND で組み合わせて検索する。
// StreamTagIDs / PerformanceTagIDs の各配列内も AND 条件。
// 検索は明示的な操作なので、非表示の配信も対象に含める。
func (r *StreamRepository) SearchStreams(filters models.StreamSearchFilters, limit, offset int) ([]models.Stream, int, error) {
	where := "WHERE TRUE"
	args := []any{}
	i := 1
	if filters.Query != "" {
		where += fmt.Sprintf(" AND s.title ILIKE '%%' || $%d || '%%'", i)
		args = append(args, filters.Query)
		i++
	}
	if filters.OwnerID != "" {
		where += fmt.Sprintf(" AND EXISTS (SELECT 1 FROM stream_singers ss WHERE ss.stream_id = s.id AND ss.singer_id = $%d AND ss.is_owner = TRUE)", i)
		args = append(args, filters.OwnerID)
		i++
	}
	if len(filters.ParticipantIDs) > 0 {
		where += fmt.Sprintf(" AND (SELECT COUNT(DISTINCT ss.singer_id) FROM stream_singers ss WHERE ss.stream_id = s.id AND ss.singer_id = ANY($%d)) = %d", i, len(filters.ParticipantIDs))
		args = append(args, pq.Array(filters.ParticipantIDs))
		i++
	}
	// **歌手・歌唱タグでの絞り込みは「誰が何を歌ったか」そのもの**なので、
	// 秘匿された配信は突き合わせの対象から外す。配信のタイトルは公開してよいが、
	// 「この配信でこの人が歌った」は伏せている中身の一部。
	if len(filters.VocalistIDs) > 0 {
		where += fmt.Sprintf(" AND (SELECT COUNT(DISTINCT ps.singer_id) FROM performances p JOIN performance_singers ps ON ps.performance_id = p.id JOIN streams st ON st.id = p.stream_id WHERE p.stream_id = s.id AND "+NotRestricted("st")+" AND ps.singer_id = ANY($%d)) = %d", i, len(filters.VocalistIDs))
		args = append(args, pq.Array(filters.VocalistIDs))
		i++
	}
	if len(filters.StreamTagIDs) > 0 {
		where += fmt.Sprintf(" AND (SELECT COUNT(DISTINCT sst.tag_id) FROM stream_stream_tags sst WHERE sst.stream_id = s.id AND sst.tag_id = ANY($%d)) = %d", i, len(filters.StreamTagIDs))
		args = append(args, pq.Array(filters.StreamTagIDs))
		i++
	}
	if len(filters.PerformanceTagIDs) > 0 {
		where += fmt.Sprintf(" AND (SELECT COUNT(DISTINCT ppt.tag_id) FROM performances p JOIN performance_performance_tags ppt ON ppt.performance_id = p.id JOIN streams st ON st.id = p.stream_id WHERE p.stream_id = s.id AND "+NotRestricted("st")+" AND ppt.tag_id = ANY($%d)) = %d", i, len(filters.PerformanceTagIDs))
		args = append(args, pq.Array(filters.PerformanceTagIDs))
		i++
	}

	var total int
	if err := r.db.QueryRow("SELECT COUNT(*) FROM streams s "+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count searched streams: %w", err)
	}

	query := fmt.Sprintf(streamListQuery("s", `
		%s
		ORDER BY s.stream_date DESC
		LIMIT $%d OFFSET $%d`), where, i, i+1)
	args = append(args, limit, offset)

	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("search streams: %w", err)
	}
	defer rows.Close()

	var streams []models.Stream
	for rows.Next() {
		var s models.Stream
		s, err := scanStreamRow(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scan stream: %w", err)
		}
		streams = append(streams, s)
	}
	return streams, total, rows.Err()
}

// SearchByTitle はタイトル部分一致（大小無視）で配信を検索する（グローバル検索用）。
// 非表示の配信も対象に含める（検索は明示的な操作のため）。
func (r *StreamRepository) SearchByTitle(query string, limit int) ([]models.Stream, error) {
	rows, err := r.db.Query(`
		SELECT id, title, stream_date, thumbnail_url, is_processed, is_hidden
		FROM streams
		WHERE title ILIKE '%' || $1 || '%'
		ORDER BY stream_date DESC
		LIMIT $2`, query, limit)
	if err != nil {
		return nil, fmt.Errorf("search streams by title: %w", err)
	}
	defer rows.Close()

	var streams []models.Stream
	for rows.Next() {
		var s models.Stream
		if err := rows.Scan(&s.ID, &s.Title, &s.StreamDate, &s.ThumbnailURL, &s.IsProcessed, &s.IsHidden); err != nil {
			return nil, fmt.Errorf("scan stream search result: %w", err)
		}
		streams = append(streams, s)
	}
	return streams, rows.Err()
}

// applyTagRulesSQL は tag_keyword_rules を配信タイトルへ ILIKE 部分一致で照合し、
// 一致したタグを stream_stream_tags に「追加」する（既存タグは消さない・冪等）。
const applyTagRulesSQL = `
	INSERT INTO stream_stream_tags (stream_id, tag_id)
	SELECT s.id, r.tag_id
	FROM streams s
	JOIN tag_keyword_rules r ON s.title ILIKE '%' || r.keyword || '%'`

// ApplyTagRulesToStream は 1 配信に自動タグ付け規則を適用し、追加したタグ数を返す。
func (r *StreamRepository) ApplyTagRulesToStream(streamID string) (int64, error) {
	res, err := r.db.Exec(applyTagRulesSQL+`
		WHERE s.id = $1
		ON CONFLICT (stream_id, tag_id) DO NOTHING`, streamID)
	if err != nil {
		return 0, fmt.Errorf("apply tag rules to stream: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// ApplyTagRulesToAll は全配信に自動タグ付け規則を適用し、追加したタグの総数を返す（バックフィル用）。
func (r *StreamRepository) ApplyTagRulesToAll() (int64, error) {
	res, err := r.db.Exec(applyTagRulesSQL + `
		ON CONFLICT (stream_id, tag_id) DO NOTHING`)
	if err != nil {
		return 0, fmt.Errorf("apply tag rules to all: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// ========== 分析キャッシュ（comment / holodex の正規化結果。由来のハッシュをキーにする） ==========

// SaveCommentRaw は comment_raw を更新し、内容が変わった場合は古いコメントから作られた分析キャッシュも消す。
func (r *StreamRepository) SaveCommentRaw(id string, raw []byte) error {
	raw = util.SanitizeJSONB(raw)
	_, err := r.db.Exec(`
		UPDATE streams
		SET comment_songs = CASE WHEN comment_raw IS DISTINCT FROM $2 THEN NULL ELSE comment_songs END,
		    comment_songs_hash = CASE WHEN comment_raw IS DISTINCT FROM $2 THEN NULL ELSE comment_songs_hash END,
		    comment_raw = $2,
		    updated_at = NOW()
		WHERE id = $1`, id, raw)
	if err != nil {
		return fmt.Errorf("save comment raw: %w", err)
	}
	return nil
}

// GetCommentSongsHash は comment_songs の計算元となった comment_raw のハッシュを取得する（キャッシュの有効性判定用）。
func (r *StreamRepository) GetCommentSongsHash(id string) (sql.NullString, error) {
	var h sql.NullString
	err := r.db.QueryRow(`SELECT comment_songs_hash FROM streams WHERE id = $1`, id).Scan(&h)
	if err == sql.ErrNoRows {
		return sql.NullString{}, nil
	}
	if err != nil {
		return sql.NullString{}, fmt.Errorf("get comment songs hash: %w", err)
	}
	return h, nil
}

// SaveCommentSongs は分析後の comment_songs と由来のハッシュを書き込む（この 2 列だけを変更）。
// UpdateCommentSongs は解析結果の欄だけを書き換える。hash は触らない。
//
// hash が指すのは「どの comment_raw から作ったか」なので、抽出をやり直していない
// 更新（拍手 end の付与など）では据え置くのが正しい。ここで動かすと次回の解析が
// キャッシュを外して AI を呼ぶ。
//
// 行まるごとの更新（旧 Update）を置き換えたもの。あちらは title / holodex_data /
// comment_raw / is_hidden まで巻き込んで書き戻すので、呼び出し側が完全な行を
// 持っていることが暗黙の前提になっていた。部分的に組み立てた model を渡すと
// comment_songs が黙って空になる ── 失敗してもエラーもログも出ない形だった。
func (r *StreamRepository) UpdateCommentSongs(id string, songs []byte) error {
	_, err := r.db.Exec(`UPDATE streams SET comment_songs = $2, updated_at = NOW() WHERE id = $1`,
		id, util.SanitizeJSONB(songs))
	if err != nil {
		return fmt.Errorf("update comment songs: %w", err)
	}
	return nil
}

// SaveCommentSongs は抽出結果を書き込む。**分析に使った comment_raw がまだ入っているときだけ**書く。
//
// 書き込まなかった場合は (false, nil) を返す。
//
// ガードが要る理由：分析は AI を挟むので数秒〜数十秒かかる。その間に Holodex 同期や
// コメント再取得が comment_raw を差し替えると、無条件に書けば
// 「raw は新しい内容、comment_songs と hash は古い内容から作ったもの」という組み合わせが残る。
// 次の分析で hash が合わずに作り直されるので永続的な破損にはならないが、それまでの間、
// 配信詳細と検索の応答は hash を確認せずにその古い結果を返す。
//
// 比較は SaveCommentRaw と同じく JSONB の意味的な比較（IS NOT DISTINCT FROM）に任せる。
// バイト列ではないので、JSONB 正規化による表記の違いは無視される。
// comment_raw は最大 12kB・平均 671 バイトなので、投げ直す費用は無視できる。
func (r *StreamRepository) SaveCommentSongs(id string, songs []byte, hash string, rawWhenAnalyzed []byte) (bool, error) {
	songs = util.SanitizeJSONB(songs)
	rawWhenAnalyzed = util.SanitizeJSONB(rawWhenAnalyzed)
	// comment_songs_analyzed_at は**ここでだけ**刻む。updated_at では代用できない
	// （毎日回る Holodex 同期が全配信の updated_at を今日に押し上げる）。
	res, err := r.db.Exec(`UPDATE streams
		SET comment_songs = $2, comment_songs_hash = $3, comment_songs_analyzed_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND comment_raw IS NOT DISTINCT FROM $4`, id, songs, hash, rawWhenAnalyzed)
	if err != nil {
		return false, fmt.Errorf("save comment songs: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("save comment songs: %w", err)
	}
	return n > 0, nil
}

// SaveChapterRaw は chapter_raw を更新し、内容が変わった場合は古い章節から作られた
// 抽出キャッシュも消す（SaveCommentRaw と同じ約束）。
func (r *StreamRepository) SaveChapterRaw(id string, raw []byte) error {
	raw = util.SanitizeJSONB(raw)
	_, err := r.db.Exec(`
		UPDATE streams
		SET chapter_songs = CASE WHEN chapter_raw IS DISTINCT FROM $2 THEN NULL ELSE chapter_songs END,
		    chapter_songs_hash = CASE WHEN chapter_raw IS DISTINCT FROM $2 THEN NULL ELSE chapter_songs_hash END,
		    chapter_raw = $2,
		    updated_at = NOW()
		WHERE id = $1`, id, raw)
	if err != nil {
		return fmt.Errorf("save chapter raw: %w", err)
	}
	return nil
}

// GetChapterSongsHash は chapter_songs の計算元 chapter_raw のハッシュを取得する。
func (r *StreamRepository) GetChapterSongsHash(id string) (sql.NullString, error) {
	var h sql.NullString
	err := r.db.QueryRow(`SELECT chapter_songs_hash FROM streams WHERE id = $1`, id).Scan(&h)
	if err == sql.ErrNoRows {
		return sql.NullString{}, nil
	}
	if err != nil {
		return sql.NullString{}, fmt.Errorf("get chapter songs hash: %w", err)
	}
	return h, nil
}

// SaveChapterSongs は抽出結果と由来のハッシュを書き込む（この 3 列だけを変更）。
func (r *StreamRepository) SaveChapterSongs(id string, songs []byte, hash string) error {
	songs = util.SanitizeJSONB(songs)
	_, err := r.db.Exec(`UPDATE streams
		SET chapter_songs = $2, chapter_songs_hash = $3, chapter_songs_analyzed_at = NOW(), updated_at = NOW()
		WHERE id = $1`, id, songs, hash)
	if err != nil {
		return fmt.Errorf("save chapter songs: %w", err)
	}
	return nil
}

// FindIDsWithoutChapterRaw はチャプターを**まだ調べていない**配信の ID を返す（backfill 用）。
// 空配列（＝調べたが章節が無い）は対象に含めない。含めると毎回全配信を取り直すことになる。
func (r *StreamRepository) FindIDsWithoutChapterRaw() ([]string, error) {
	rows, err := r.db.Query(`
		SELECT id FROM streams
		WHERE is_hidden = FALSE AND chapter_raw IS NULL
		ORDER BY stream_date DESC`)
	if err != nil {
		return nil, fmt.Errorf("query streams without chapter_raw: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan stream id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// SaveAvailability は yt-dlp から拾った再生可否を保存する。
//
// **checked_at は必ず一緒に書く。** これが NULL のままだと未調査と区別できず、
// 「調べたが値が取れなかった」配信を毎回調べ直すことになる。
func (r *StreamRepository) SaveAvailability(id string, availability sql.NullString, playableInEmbed sql.NullBool) error {
	// **1 文にまとめる。** 更新とタグ付けが別々の Exec だと、間で失敗したときに
	// 「availability は subscriber_only なのに会限の印が無い」＝公開側に置かれた行が残る。
	// data-modifying CTE なら同じ文なので、片方だけ成立することがない。
	//
	// 会限と分かったら**検出の側だけ**記録する。restriction_override（人の裁定）は
	// 触らない ── 触ると「公開してよい」と決めた配信が、次の取得で伏せ直される。
	// 外しもしない（public は「反証が無かった」という弱い結論なので、
	// それで秘匿を解くと誤って公開する）。
	//
	// **COALESCE を外さないこと。** availability が NULL のとき比較結果も NULL になり、
	// SQL の三値論理では WHERE NULL は偽でも真でもなく行が消える。以前この列が
	// NOT NULL だった頃は制約違反で本番が落ちた。
	var updated int
	err := r.db.QueryRow(`
		WITH upd AS (
		    UPDATE streams
		    SET availability = $2, playable_in_embed = $3, availability_checked_at = NOW()
		    WHERE id = $1
		    RETURNING id
		), tagged AS (
		    INSERT INTO stream_stream_tags (stream_id, tag_id)
		    SELECT upd.id, $4 FROM upd
		    WHERE COALESCE($2 = 'subscriber_only', FALSE)
		    ON CONFLICT (stream_id, tag_id) DO NOTHING
		)
		SELECT COUNT(*) FROM upd`, id, availability, playableInEmbed, MembersOnlyTagID).Scan(&updated)
	if err != nil {
		return fmt.Errorf("save availability: %w", err)
	}
	// **行数を見る。** 存在しない ID でも UPDATE は error にならないので、
	// 確認しないと「記録できた」と答えてしまう（saved の契約は「DB に記録できたか」）。
	if updated == 0 {
		return ErrStreamNotFound
	}
	return nil
}

// FindIDsNeedingAvailability は再生可否を調べる対象の ID を返す（backfill 用）。
//
// **FindIDsWithoutChapterRaw と違い、非表示配信を除かない。** 会限の歌枠はどれも
// 非表示に置かれているので、除くと判定したい配信がまるごと対象から落ちる
// （チャプター側は「表示中の配信のセットリストを埋める」のが目的なので除いて正しい）。
//
// includeWeak を立てると、**弱い判定で確定している行も対象に戻す**。
// `public` は yt-dlp が「反証が無かった」ときに出す既定の結論であって、
// 公開だと確かめた証拠ではない（`_availability` は 5 つの材料が全部揃えば public を返し、
// 会限かどうかを決める badge が取れなかった場合も「揃った」と数える）。
// つまり **会限の配信が public と記録されうる**。害は「プレイヤーを描いて 150 で失敗する」
// ＝この機能を入れる前と同じ挙動だが、記録済みだと二度と調べ直さないのが問題なので、
// 調べ直す口をここに用意しておく。
func (r *StreamRepository) FindIDsNeedingAvailability(includeWeak bool) ([]string, error) {
	// 弱い判定＝「public かつ埋め込み可」。subscriber_only / 埋め込み不可 / 取得不可は
	// どれも積極的な発見なので、調べ直す理由が無い。
	where := "availability_checked_at IS NULL"
	if includeWeak {
		where += " OR (availability = 'public' AND playable_in_embed IS TRUE)"
	}
	rows, err := r.db.Query(`
		SELECT id FROM streams
		WHERE ` + where + `
		ORDER BY stream_date DESC`)
	if err != nil {
		return nil, fmt.Errorf("query streams without availability: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan stream id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// GetHolodexSongsCache はキャッシュ済みの Holodex 正規化結果と由来の holodex_hash を取得する。
func (r *StreamRepository) GetHolodexSongsCache(id string) (normalized []byte, hash sql.NullString, err error) {
	var n []byte
	var h sql.NullString
	err = r.db.QueryRow(`SELECT holodex_songs_normalized, holodex_songs_hash FROM streams WHERE id = $1`, id).Scan(&n, &h)
	if err == sql.ErrNoRows {
		return nil, sql.NullString{}, nil
	}
	if err != nil {
		return nil, sql.NullString{}, fmt.Errorf("get holodex songs cache: %w", err)
	}
	return n, h, nil
}

// SaveHolodexSongs は Holodex 正規化結果と由来の holodex_hash をキャッシュへ書き込む（この 2 列だけを変更）。
func (r *StreamRepository) SaveHolodexSongs(id string, normalized []byte, hash string) error {
	normalized = util.SanitizeJSONB(normalized)
	_, err := r.db.Exec(`UPDATE streams SET holodex_songs_normalized = $2, holodex_songs_hash = $3, updated_at = NOW() WHERE id = $1`, id, normalized, hash)
	if err != nil {
		return fmt.Errorf("save holodex songs: %w", err)
	}
	return nil
}

// CheckHashChanged は Holodex データが変更されたか確認する。
func (r *StreamRepository) CheckHashChanged(id, newHash string) (bool, error) {
	var currentHash sql.NullString
	err := r.db.QueryRow("SELECT holodex_hash FROM streams WHERE id = $1", id).Scan(&currentHash)
	if err == sql.ErrNoRows {
		return true, nil // 新しいデータ
	}
	if err != nil {
		return false, fmt.Errorf("check hash: %w", err)
	}

	if !currentHash.Valid {
		return true, nil
	}

	return currentHash.String != newHash, nil
}

// ========== Stream Singers ==========

// GetSingers はこの配信に参加したすべての歌手を取得する。
func (r *StreamRepository) GetSingers(streamID string) ([]models.Singer, error) {
	query := `
		SELECT s.id, s.name, s.english_name, s.photo_url, COALESCE(s.organization_override, s.organization), o.display_name, COALESCE(o.is_unaffiliated, FALSE), s.metadata_source, s.created_at, s.updated_at
		FROM singers s
		LEFT JOIN organizations o ON COALESCE(s.organization_override, s.organization) = o.key
		JOIN stream_singers ss ON s.id = ss.singer_id
		WHERE ss.stream_id = $1`

	rows, err := r.db.Query(query, streamID)
	if err != nil {
		return nil, fmt.Errorf("get stream singers: %w", err)
	}
	defer rows.Close()

	var singers []models.Singer
	for rows.Next() {
		var s models.Singer
		err := rows.Scan(&s.ID, &s.Name, &s.EnglishName, &s.PhotoURL, &s.Organization, &s.OrganizationName, &s.OrganizationUnaffil, &s.MetadataSource, &s.CreatedAt, &s.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("scan singer: %w", err)
		}
		singers = append(singers, s)
	}

	return singers, nil
}

// GetChannelOwner はこの配信のチャンネル所有者を取得する。
func (r *StreamRepository) GetChannelOwner(streamID string) (*models.Singer, error) {
	query := `
		SELECT s.id, s.name, s.english_name, s.photo_url, COALESCE(s.organization_override, s.organization), o.display_name, COALESCE(o.is_unaffiliated, FALSE), s.metadata_source, s.created_at, s.updated_at
		FROM singers s
		LEFT JOIN organizations o ON COALESCE(s.organization_override, s.organization) = o.key
		JOIN stream_singers ss ON s.id = ss.singer_id
		WHERE ss.stream_id = $1 AND ss.is_owner = TRUE
		LIMIT 1`

	var s models.Singer
	err := r.db.QueryRow(query, streamID).Scan(&s.ID, &s.Name, &s.EnglishName, &s.PhotoURL, &s.Organization, &s.OrganizationName, &s.OrganizationUnaffil, &s.MetadataSource, &s.CreatedAt, &s.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil // チャンネル所有者が設定されていない
	}
	if err != nil {
		return nil, fmt.Errorf("get channel owner: %w", err)
	}
	return &s, nil
}

// AddSinger は配信に参加者を追加する。
func (r *StreamRepository) AddSinger(streamID, singerID string, isOwner bool) error {
	query := `INSERT INTO stream_singers (stream_id, singer_id, is_owner) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`
	_, err := r.db.Exec(query, streamID, singerID, isOwner)
	if err != nil {
		return fmt.Errorf("add stream singer: %w", err)
	}
	return nil
}

// RemoveSinger は配信から参加者を外す。
func (r *StreamRepository) RemoveSinger(streamID, singerID string) error {
	_, err := r.db.Exec("DELETE FROM stream_singers WHERE stream_id = $1 AND singer_id = $2", streamID, singerID)
	if err != nil {
		return fmt.Errorf("remove stream singer: %w", err)
	}
	return nil
}

// SetSingers は配信の参加者を設定する（既存値を置換）。
func (r *StreamRepository) SetSingers(streamID string, singerIDs []string, ownerID string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// 既存の参加者を削除する
	_, err = tx.Exec("DELETE FROM stream_singers WHERE stream_id = $1", streamID)
	if err != nil {
		return fmt.Errorf("delete existing singers: %w", err)
	}

	// 新しい参加者を追加する
	for _, singerID := range singerIDs {
		isOwner := singerID == ownerID
		_, err = tx.Exec(
			"INSERT INTO stream_singers (stream_id, singer_id, is_owner) VALUES ($1, $2, $3)",
			streamID, singerID, isOwner,
		)
		if err != nil {
			return fmt.Errorf("insert singer: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

// ClearTags は配信のタグをすべて消す。
func (r *StreamRepository) ClearTags(streamID string) error {
	_, err := r.db.Exec("DELETE FROM stream_stream_tags WHERE stream_id = $1", streamID)
	if err != nil {
		return fmt.Errorf("clear stream tags: %w", err)
	}
	return nil
}

// SetTags は配信のタグを設定する（既存値を置換）。
func (r *StreamRepository) SetTags(streamID string, tagIDs []string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// 既存のタグを削除する
	_, err = tx.Exec("DELETE FROM stream_stream_tags WHERE stream_id = $1", streamID)
	if err != nil {
		return fmt.Errorf("delete existing tags: %w", err)
	}

	// 新しいタグを追加する
	for _, tagID := range tagIDs {
		_, err = tx.Exec(
			"INSERT INTO stream_stream_tags (stream_id, tag_id) VALUES ($1, $2) ON CONFLICT DO NOTHING",
			streamID, tagID,
		)
		if err != nil {
			return fmt.Errorf("insert tag: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

// StreamFilter は歌枠を絞り込むためのオプション。
type StreamFilter struct {
	ProcessedOnly *bool // nil=すべて, true=処理済みのみ, false=未処理のみ
	HiddenFilter  *bool // nil=すべて, true=非表示のみ, false=非表示を除外（既定）
}

// FindBySingerID は歌手が参加した歌枠を取得する（ページングと絞り込み対応）。
func (r *StreamRepository) FindBySingerID(singerID string, limit, offset int, filter *StreamFilter) ([]models.Stream, int, error) {
	// WHERE 条件を組み立てる
	whereClause := "ss.singer_id = $1"
	args := []interface{}{singerID}
	argIndex := 2

	if filter != nil {
		if filter.ProcessedOnly != nil {
			whereClause += fmt.Sprintf(" AND s.is_processed = $%d", argIndex)
			args = append(args, *filter.ProcessedOnly)
			argIndex++
		}
		if filter.HiddenFilter != nil {
			if *filter.HiddenFilter {
				// 非表示だけを見る
				whereClause += fmt.Sprintf(" AND s.is_hidden = $%d", argIndex)
				args = append(args, true)
			} else {
				// 非表示を除外する
				whereClause += fmt.Sprintf(" AND s.is_hidden = $%d", argIndex)
				args = append(args, false)
			}
			argIndex++
		}
	}

	var total int
	countQuery := fmt.Sprintf(`
		SELECT COUNT(DISTINCT s.id)
		FROM streams s
		JOIN stream_singers ss ON s.id = ss.stream_id
		WHERE %s
	`, whereClause)
	err := r.db.QueryRow(countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count streams: %w", err)
	}

	query := fmt.Sprintf(`
		SELECT s.id, s.title, s.stream_date, s.duration_seconds, s.thumbnail_url, s.holodex_data, s.holodex_hash, s.is_processed, s.is_hidden, s.restriction_override, s.created_at, s.updated_at, `+EffectiveRestrictedExpr("s")+`
		FROM streams s
		JOIN stream_singers ss ON s.id = ss.stream_id
		WHERE %s
		ORDER BY s.stream_date DESC
		LIMIT $%d OFFSET $%d`, whereClause, argIndex, argIndex+1)

	args = append(args, limit, offset)
	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query streams: %w", err)
	}
	defer rows.Close()

	var streams []models.Stream
	for rows.Next() {
		var s models.Stream
		err := rows.Scan(&s.ID, &s.Title, &s.StreamDate, &s.DurationSeconds,
			&s.ThumbnailURL, &s.HolodexData, &s.HolodexHash, &s.IsProcessed, &s.IsHidden, &s.RestrictionOverride, &s.CreatedAt, &s.UpdatedAt, &s.IsRestrictedEffective)
		if err != nil {
			return nil, 0, fmt.Errorf("scan stream: %w", err)
		}
		streams = append(streams, s)
	}

	return streams, total, nil
}

// FindStreamsNeedingCommentRefresh はコメントを取り直す価値がある配信の ID を返す。
//
// **歌単は配信が終わったあとに貼られることが多い。** 同期は新規のときしか
// コメントを取り直さない（`holodex_service.go` の `existing == nil || forceUpdate`）ので、
// 取り直さなければ自動処理は何度走らせても同じ入力を見続けることになる。
//
// 一方で全配信を取り直すと、その本数ぶん外部 API を叩くことになる。そこで
// 「取り直しても結果が変わりうるもの」だけに絞る：
//
//	歌単が空          … 既にある歌単には触らない約束なので、それ以外は取り直しても何も起きない
//	非表示でない      … 雑談・ゲームまで対象にしない（本番では 731 → 14 まで減る）
//	終了から days 日以内 … 古い配信に今さら歌単が貼られることは稀
//	未来日でない        … 予約配信を配信前から取りに行かない
//
// 取り直しは外部 API を叩く（`GetVideoComments` は YouTube → Holodex の
// 2 リクエストになりうるので、1 本につき 1 回とは限らない）。コメントが空の
// 配信はこの実行の中で複数回取りに行くこともあるが、
// **飛ばして取りこぼすよりは安い**という判断。
//
// singerIDs が空なら全チャンネル。**所有者で絞る**（ゲスト参加しただけの
// 他人の配信まで対象にしない。FindStreamsForFill と同じ）。
// justSynced はこの実行の同期で入ってきた配信。**それだけでは除外しない** ──
// 同期のコメント取得は失敗してもログだけで、その配信は「新規」として返る。
// 「新規だから取得済み」と決めつけると、取得に失敗した配信を黙って飛ばすことになる。
// **実際にコメントが入っているものだけ**を除外する（＝取得できた証拠がある）。
func (r *StreamRepository) FindStreamsNeedingCommentRefresh(singerIDs []string, days int, justSynced []string) ([]string, error) {
	if days < 1 {
		days = 30
	}
	query := `
		SELECT s.id
		FROM streams s
		WHERE s.is_hidden = FALSE
		  -- **上限も要る。** 予約配信（未来日）は「終了から N 日以内」に入って
		  -- しまい、配信が始まる前から定期的にコメントを取りに行くことになる。
		  -- SyncChannel は past しか取らないが、SyncVideo 等で先に登録されうる。
		  AND s.stream_date <= NOW()
		  AND s.stream_date > NOW() - ($1 || ' days')::INTERVAL
		  AND NOT EXISTS (SELECT 1 FROM performances p WHERE p.stream_id = s.id)`
	args := []any{days}
	if len(singerIDs) > 0 {
		query += `
		  AND EXISTS (SELECT 1 FROM stream_singers ss
		              WHERE ss.stream_id = s.id AND ss.is_owner AND ss.singer_id = ANY($2))`
		args = append(args, pq.Array(singerIDs))
	}
	if len(justSynced) > 0 {
		// 同期で入ってきて、**かつ実際にコメントが入った**ものだけ除外する。
		// 空のまま（取得失敗・コメントがまだ無い）なら取り直す ── そちらは
		// 再取得で結果が変わりうるし、飛ばすと今回の実行から漏れる。
		// **COALESCE を外さないこと。** comment_raw が SQL NULL のとき
		// `jsonb_typeof(NULL)` は NULL なので、条件全体が
		// `TRUE AND NULL AND NULL` = NULL になり、`NOT NULL` も NULL。
		// WHERE では NULL は不成立なので、**同期時にコメント取得が失敗した
		// 新規配信こそが除外される** ── 意図の正反対で、しかも失敗にも数えられない。
		// （本番で `false OR NULL = NULL` を踏んだのと同じ罠。CLAUDE.md §2）
		query += fmt.Sprintf(`
		  AND NOT COALESCE(s.id = ANY($%d)
		           AND jsonb_typeof(s.comment_raw) = 'array'
		           AND jsonb_array_length(s.comment_raw) > 0, FALSE)`, len(args)+1)
		args = append(args, pq.Array(justSynced))
	}
	query += `
		ORDER BY s.stream_date DESC`

	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query streams needing comment refresh: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan stream id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// FindStreamsForFill は一括セットリスト作成の対象を返す。
//
// 一括プレ分析（FindStreamsForBatch）とは対象が違う。あちらは comment_raw を持つ配信だが、
// こちらは**楽曲の入力元を持つ配信**（Holodex の曲か、解析済みのコメント）が対象になる。
//
//	unprocessed … まだ歌唱が 1 つも無い配信だけ。既にあるものは触らない
//	force       … 入力元を持つ配信すべて。既存と食い違う分は人の審査へ回す
//
// 範囲は singerIDs で絞る（空なら全チャンネル）。**既定はチャンネルの所有者**で、
// includeCollabs を立てたときだけ「参加した歌回」まで広がる。
// 以前は参加者で絞っていたので、あるチャンネルを選んだつもりが、そのチャンネルが
// ゲスト参加しただけの他人の配信まで対象になっていた。
func (r *StreamRepository) FindStreamsForFill(mode string, singerIDs []string, includeCollabs bool) ([]models.Stream, error) {
	// jsonb_typeof で配列だけを見る。comment_songs には JSON のスカラー 'null' が入っている
	// 行があり、jsonb_array_length に直接渡すと「cannot get array length of a scalar」で落ちる。
	//
	// comment_raw しか無い（まだ抽出していない）配信も対象に入れる。読み込みの側が
	// 未解析なら抽出から走らせるので、「コメントはあるがまだ解析していない」を
	// 一括の対象外にしておく理由が無い。
	//
	// **ただし「非空の配列」で絞ること。** `IS NOT NULL AND != 'null'` では `[]` が通り、
	// 「保存済みの入力を処理し直す」つもりの実行が遠隔からの再取得に化ける
	// （CLAUDE.md §6.1。FindStreamsForBatch は直っていたが、こちらは残っていた）。
	// 実測（2026-09-05、本番）：これで通っていた 9 本はすべて会限で `comment_raw = []`。
	// コメントは API key では読めない（403 forbidden。**配額ではなく権限**）ので、
	// 毎時間 403 を叩いては何も得られずに終わっていた。
	// チャプターは**取得済みで章節がある**配信だけを対象にする。yt-dlp は一括の中で
	// 呼ばない約束（1 本あたり数秒かかる）なので、まだ調べていない配信を入れても
	// 読み込みの側が空を返すだけになる。先に POST /api/chapters/backfill を回すこと。
	where := `s.is_hidden = FALSE AND (
		(jsonb_typeof(s.holodex_data->'songs') = 'array' AND jsonb_array_length(s.holodex_data->'songs') > 0)
		OR (jsonb_typeof(s.comment_songs) = 'array' AND jsonb_array_length(s.comment_songs) > 0)
		OR (jsonb_typeof(s.comment_raw) = 'array' AND jsonb_array_length(s.comment_raw) > 0)
		OR (jsonb_typeof(s.chapter_raw) = 'array' AND jsonb_array_length(s.chapter_raw) > 0))`
	if mode == "unprocessed" {
		where += " AND NOT EXISTS (SELECT 1 FROM performances p WHERE p.stream_id = s.id)"
		// **人が「処理した」と言った配信は触らない。**
		//
		// ここでの unprocessed は「歌唱記録が無い」という意味で、`is_processed` 列とは
		// 別物（名前だけ衝突している）。見ていなかったので、**人の結論が仕組みに
		// 伝わっていなかった** ── 確認して「この配信に歌は無い」と判断しても、
		// 歌唱が 0 件なら毎回やり直していた。
		//
		// 実測（2026-09-05、本番）：対象 14 本のうち 10 本が会限で、コメントは
		// API key では読めない（403 forbidden。配額ではなく権限）。入力源が全部空なので
		// 歌唱は永久に 0 件のまま、毎時間 403 を叩き続けていた（issue #41）。
		//
		// force モードには付けない。あちらは「全部もう一度考え直す」ための明示的な口で、
		// 人が意図して回すもの。
		where += " AND NOT s.is_processed"
	}

	args := []any{}
	if len(singerIDs) > 0 {
		args = append(args, pq.Array(singerIDs))
		ownerOnly := " AND ss.is_owner"
		if includeCollabs {
			ownerOnly = ""
		}
		where += fmt.Sprintf(
			" AND EXISTS (SELECT 1 FROM stream_singers ss WHERE ss.stream_id = s.id AND ss.singer_id = ANY($%d)%s)",
			len(args), ownerOnly)
	}

	rows, err := r.db.Query(`SELECT s.id, s.title FROM streams s WHERE `+where+` ORDER BY s.stream_date ASC`, args...)
	if err != nil {
		return nil, fmt.Errorf("query fill streams: %w", err)
	}
	defer rows.Close()

	var streams []models.Stream
	for rows.Next() {
		var s models.Stream
		if err := rows.Scan(&s.ID, &s.Title); err != nil {
			return nil, fmt.Errorf("scan stream: %w", err)
		}
		streams = append(streams, s)
	}
	return streams, rows.Err()
}
