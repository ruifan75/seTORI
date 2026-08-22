package repository

import (
	"database/sql"
	"fmt"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/ruifan75/setori/internal/models"
	"github.com/ruifan75/setori/pkg/songmatch"
)

// SongMatchRepository は楽曲の照合キー（song_match_keys）と
// 統合候補（song_merge_candidates）を扱う。
//
// キーの計算は pkg/songmatch にあり、ここは保存と索引引きに徹する。
type SongMatchRepository struct {
	db *sql.DB
}

func NewSongMatchRepository(db *sql.DB) *SongMatchRepository {
	return &SongMatchRepository{db: db}
}

// ---------- 照合キーの保守 ----------

// execer は *sql.DB でも *sql.Tx でも受けられるようにするための最小の口。
type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

// upsertSongMatchKey は 1 曲ぶんの照合キーを書く。
// 楽曲を書き換えるすべての経路から呼べるよう、レシーバを持たない形にしてある
// （SongRepository の Create/Update もこれを使う）。
func upsertSongMatchKey(db execer, songID uuid.UUID, name, artist string) error {
	ak := songmatch.ParseArtist(artist)
	// アーティストが空だと Tokens は nil で、pq.Array(nil) は NULL になる
	// （artist_tokens は NOT NULL）。曲名だけ分かってアーティストが不明、は
	// コメント解析では普通に起きるので、空配列に倒しておく。
	tokens := ak.Tokens
	if tokens == nil {
		tokens = []string{}
	}
	_, err := db.Exec(`
		INSERT INTO song_match_keys
			(song_id, name_key, artist_primary, artist_tokens, rules_version, src_name, src_artist, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())
		ON CONFLICT (song_id) DO UPDATE SET
			name_key = EXCLUDED.name_key,
			artist_primary = EXCLUDED.artist_primary,
			artist_tokens = EXCLUDED.artist_tokens,
			rules_version = EXCLUDED.rules_version,
			src_name = EXCLUDED.src_name,
			src_artist = EXCLUDED.src_artist,
			updated_at = NOW()`,
		songID, songmatch.TitleKey(name), ak.Primary, pq.Array(tokens), songmatch.RulesVersion, name, artist)
	if err != nil {
		return fmt.Errorf("upsert song match key: %w", err)
	}
	return nil
}

// Upsert は 1 曲ぶんの照合キーを作り直す。
func (r *SongMatchRepository) Upsert(songID uuid.UUID, name, artist string) error {
	return upsertSongMatchKey(r.db, songID, name, artist)
}

// queryExecer は照合キーの一括更新に必要な口（*sql.DB / *sql.Tx が満たす）。
type queryExecer interface {
	execer
	Query(query string, args ...any) (*sql.Rows, error)
}

// refreshSongMatchKeysByArtist は指定アーティストに紐づく楽曲のキーを作り直す。
// アーティストの改名・統合は songs.original_artist を直接書き換えるので、
// そのトランザクションの中から呼んでキーを追随させる。
func refreshSongMatchKeysByArtist(db queryExecer, artistID uuid.UUID) error {
	rows, err := db.Query(`
		SELECT s.id, s.name, s.original_artist
		FROM songs s JOIN song_artists sa ON sa.song_id = s.id
		WHERE sa.artist_id = $1`, artistID)
	if err != nil {
		return fmt.Errorf("list songs for artist: %w", err)
	}
	type pending struct {
		id           uuid.UUID
		name, artist string
	}
	var todo []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.id, &p.name, &p.artist); err != nil {
			rows.Close()
			return fmt.Errorf("scan song for artist: %w", err)
		}
		todo = append(todo, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, p := range todo {
		if err := upsertSongMatchKey(db, p.id, p.name, p.artist); err != nil {
			return err
		}
	}
	return nil
}

// RebuildStale は次のいずれかに当てはまる楽曲のキーを作り直す。
//
//  1. キーがまだ無い（新規導入・復元直後）
//  2. 規則の版が古い（songmatch.RulesVersion を上げた）
//  3. 計算元の曲名・アーティストが songs 側と食い違う（別経路で書き換わった）
//
// 3 があるおかげで「キー更新を呼び忘れた経路」があっても再起動で直る。
// 起動時に呼ぶ。返り値は作り直した件数。
func (r *SongMatchRepository) RebuildStale() (int, error) {
	rows, err := r.db.Query(`
		SELECT s.id, s.name, s.original_artist
		FROM songs s
		LEFT JOIN song_match_keys k ON k.song_id = s.id
		WHERE k.song_id IS NULL
		   OR k.rules_version <> $1
		   OR k.src_name <> s.name
		   OR k.src_artist <> s.original_artist`, songmatch.RulesVersion)
	if err != nil {
		return 0, fmt.Errorf("list stale match keys: %w", err)
	}
	defer rows.Close()

	type pending struct {
		id     uuid.UUID
		name   string
		artist string
	}
	var todo []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.id, &p.name, &p.artist); err != nil {
			return 0, fmt.Errorf("scan stale match key: %w", err)
		}
		todo = append(todo, p)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate stale match keys: %w", err)
	}

	for _, p := range todo {
		if err := r.Upsert(p.id, p.name, p.artist); err != nil {
			return 0, err
		}
	}
	return len(todo), nil
}

// ---------- 照合 ----------

// KeyedSong は楽曲と、その保存済み照合キー。
type KeyedSong struct {
	Song      models.Song
	NameKey   string
	ArtistKey songmatch.ArtistKey
}

const keyedSongColumns = `s.id, s.name, s.name_reading, s.original_artist, s.original_artist_reading,
	s.arts, s.created_at, s.updated_at, k.name_key, k.artist_primary, k.artist_tokens`

func scanKeyedSongs(rows *sql.Rows) ([]KeyedSong, error) {
	var out []KeyedSong
	for rows.Next() {
		var ks KeyedSong
		var tokens pq.StringArray
		if err := rows.Scan(
			&ks.Song.ID, &ks.Song.Name, &ks.Song.NameReading, &ks.Song.OriginalArtist,
			&ks.Song.OriginalArtistReading, &ks.Song.Arts, &ks.Song.CreatedAt, &ks.Song.UpdatedAt,
			&ks.NameKey, &ks.ArtistKey.Primary, &tokens,
		); err != nil {
			return nil, fmt.Errorf("scan keyed song: %w", err)
		}
		ks.ArtistKey.Tokens = []string(tokens)
		out = append(out, ks)
	}
	return out, rows.Err()
}

// FindByNameKey は曲名キーが一致する楽曲をすべて返す。
// 曲名は強いキー（実測 820 曲で衝突 3 組）なので、これが照合の入口になる。
// 複数返ったときは呼び出し側がアーティストで絞る。
func (r *SongMatchRepository) FindByNameKey(nameKey string) ([]KeyedSong, error) {
	if nameKey == "" {
		return nil, nil
	}
	rows, err := r.db.Query(`
		SELECT `+keyedSongColumns+`
		FROM song_match_keys k JOIN songs s ON s.id = k.song_id
		WHERE k.name_key = $1
		ORDER BY s.created_at`, nameKey)
	if err != nil {
		return nil, fmt.Errorf("find by name key: %w", err)
	}
	defer rows.Close()
	return scanKeyedSongs(rows)
}

// FindByNameKeyPrefix は「このキーで始まる」楽曲を返す。
//
// 深昏睡 → 深昏睡deepcoma、革命道中 → 革命道中ontheway のように、
// コメントの表記が DB のキーの接頭辞になっている型を拾う。
// **これ単体では精度が出ない**（ダーリン → ダーリンダンス のような別曲も混ざる。
// 実測で同じ曲を指すのは 2 割）。AI か人が判断する前提の候補抽出専用。
//
// LIKE の右側にだけワイルドカードを置くので name_key の btree 索引が効く。
func (r *SongMatchRepository) FindByNameKeyPrefix(nameKey string, limit int) ([]KeyedSong, error) {
	if nameKey == "" || limit <= 0 {
		return nil, nil
	}
	rows, err := r.db.Query(`
		SELECT `+keyedSongColumns+`
		FROM song_match_keys k JOIN songs s ON s.id = k.song_id
		WHERE k.name_key LIKE $1 || '%' AND k.name_key <> $1
		ORDER BY length(k.name_key), s.created_at
		LIMIT $2`, nameKey, limit)
	if err != nil {
		return nil, fmt.Errorf("find by name key prefix: %w", err)
	}
	defer rows.Close()
	return scanKeyedSongs(rows)
}

// FindSimilarByName は曲名キーが完全一致しないときの保険。
// trigram で近い曲名を拾い、下位ティアの候補として返す（自動採用はしない）。
func (r *SongMatchRepository) FindSimilarByName(name string, threshold float64, limit int) ([]KeyedSong, error) {
	if name == "" {
		return nil, nil
	}
	rows, err := r.db.Query(`
		SELECT `+keyedSongColumns+`
		FROM song_match_keys k JOIN songs s ON s.id = k.song_id
		WHERE similarity(s.name, $1) >= $2
		ORDER BY similarity(s.name, $1) DESC
		LIMIT $3`, name, threshold, limit)
	if err != nil {
		return nil, fmt.Errorf("find similar by name: %w", err)
	}
	defer rows.Close()
	return scanKeyedSongs(rows)
}

// ---------- 統合候補 ----------

// RecordMergeCandidate は「新しく作った曲が既存曲と似ている」ことを記録する。
// 同じ組が既にあれば何もしない（更新もしない：最初に気づいた時点の score を残す）。
func (r *SongMatchRepository) RecordMergeCandidate(newSongID, existingSongID uuid.UUID, score float64, reason string) error {
	_, err := r.recordMergeCandidate(newSongID, existingSongID, score, reason, "create")
	return err
}

// recordMergeCandidate は候補を積む。返り値は実際に新しく積んだかどうか。
func (r *SongMatchRepository) recordMergeCandidate(newSongID, existingSongID uuid.UUID, score float64, reason, origin string) (bool, error) {
	if newSongID == existingSongID {
		return false, nil
	}
	// 逆向きに既にある組は作らない。順序は「どちらが新しいか」を表すだけで、
	// 同じ 2 曲についての判断が 2 行になると却下しても片方が残ってしまう。
	var exists bool
	if err := r.db.QueryRow(`
		SELECT EXISTS(SELECT 1 FROM song_merge_candidates
		WHERE (new_song_id = $1 AND existing_song_id = $2)
		   OR (new_song_id = $2 AND existing_song_id = $1))`, newSongID, existingSongID).Scan(&exists); err != nil {
		return false, fmt.Errorf("check merge candidate: %w", err)
	}
	if exists {
		return false, nil
	}
	res, err := r.db.Exec(`
		INSERT INTO song_merge_candidates (new_song_id, existing_song_id, score, reason, origin)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT ON CONSTRAINT song_merge_candidates_pair_unique DO NOTHING`,
		newSongID, existingSongID, score, reason, origin)
	if err != nil {
		return false, fmt.Errorf("record merge candidate: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// ScanDuplicateTitles は既存データを走査し、曲名キーが同じ楽曲の組を候補に積む。
//
// 取り込み時の検出（origin='create'）は「これから作る曲」しか見ないので、
// 導入前から DB にあった重複は誰にも気づかれない。走査はその穴を埋める。
// 却下済み・統合済みの組は行が残っているので蒸し返さない。
// 返り値は新しく積んだ組数。
func (r *SongMatchRepository) ScanDuplicateTitles() (int, error) {
	rows, err := r.db.Query(`
		SELECT k.name_key, k.song_id
		FROM song_match_keys k
		WHERE k.name_key IN (
			SELECT name_key FROM song_match_keys GROUP BY name_key HAVING COUNT(*) > 1
		)
		ORDER BY k.name_key, k.song_id`)
	if err != nil {
		return 0, fmt.Errorf("scan duplicate titles: %w", err)
	}
	defer rows.Close()

	groups := map[string][]uuid.UUID{}
	var order []string
	for rows.Next() {
		var key string
		var id uuid.UUID
		if err := rows.Scan(&key, &id); err != nil {
			return 0, fmt.Errorf("scan duplicate row: %w", err)
		}
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], id)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	added := 0
	for _, key := range order {
		ids := groups[key]
		for i := 0; i < len(ids); i++ {
			for j := i + 1; j < len(ids); j++ {
				ok, err := r.recordMergeCandidate(ids[j], ids[i], 0.5, "same_title", "scan")
				if err != nil {
					return added, err
				}
				if ok {
					added++
				}
			}
		}
	}
	return added, nil
}

// ScanSong は全件走査で AI に見せる最小の情報。
type ScanSong struct {
	ID             uuid.UUID
	Name           string
	OriginalArtist string
}

// ListAllForScan は登録曲を全件返す（AI の全件走査用）。
// 走査は滅多に回さないので、ページングせず一度に読む。
func (r *SongMatchRepository) ListAllForScan() ([]ScanSong, error) {
	rows, err := r.db.Query(`SELECT id, name, original_artist FROM songs ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("list songs for scan: %w", err)
	}
	defer rows.Close()
	var out []ScanSong
	for rows.Next() {
		var x ScanSong
		if err := rows.Scan(&x.ID, &x.Name, &x.OriginalArtist); err != nil {
			return nil, fmt.Errorf("scan song: %w", err)
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// RecordScanCandidate は走査で見つけた組を候補に積む（origin='scan'）。
// 既にある組（却下済みを含む）は積み直さない ── 蒸し返すと同じ判断を何度もさせることになる。
func (r *SongMatchRepository) RecordScanCandidate(a, b uuid.UUID, score float64, reason string) (bool, error) {
	return r.recordMergeCandidate(a, b, score, reason, "scan")
}

// SetMergeVerdict は AI の見立てを候補に書き込む。統合の実行はしない。
func (r *SongMatchRepository) SetMergeVerdict(id uuid.UUID, v MergeVerdict) error {
	_, err := r.db.Exec(`
		UPDATE song_merge_candidates SET
			same_composition = $2, same_arrangement = $3, recommendation = $4,
			role_new = NULLIF($5, ''), role_existing = NULLIF($6, ''),
			verdict_note = NULLIF($7, ''), verdict_source = $8, verdict_at = NOW()
		WHERE id = $1`,
		id, v.SameComposition, v.SameArrangement, v.Recommendation,
		v.RoleNew, v.RoleExisting, v.Note, v.Source)
	if err != nil {
		return fmt.Errorf("set merge verdict: %w", err)
	}
	return nil
}

// ListUnjudgedCandidates は未判定の候補を返す（AI に聞く対象）。
func (r *SongMatchRepository) ListUnjudgedCandidates(limit int) ([]MergeCandidate, error) {
	if limit <= 0 {
		limit = 30
	}
	rows, err := r.db.Query(mergeCandidateSelect+`
		WHERE c.status = 'open' AND c.verdict_at IS NULL
		ORDER BY c.created_at
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list unjudged candidates: %w", err)
	}
	defer rows.Close()
	return scanMergeCandidates(rows)
}

// MergeCandidate は統合候補 1 件（両側の楽曲情報つき）。
type MergeCandidate struct {
	ID           uuid.UUID
	Score        float64
	Reason       string
	Status       string
	Origin       string // create（取り込み時に気づいた） | scan（既存データの走査）
	NewSong      models.Song
	ExistingSong models.Song
	PerfCountNew int
	PerfCountOld int
	ItunesNew    []int64
	ItunesOld    []int64

	// AI の見立て（未判定なら Verdict.At がゼロ値）
	Verdict MergeVerdict
}

// MergeVerdict は「この 2 曲は何なのか」についての判定。
// 統合するかどうかの決定ではない（それは人がする）。
type MergeVerdict struct {
	SameComposition *bool
	SameArrangement *bool
	Recommendation  string // merge | keep_separate
	RoleNew         string
	RoleExisting    string
	Note            string
	Source          string
	At              sql.NullTime
}

// ListOpenMergeCandidates は未処理の統合候補を新しい順で返す。
func (r *SongMatchRepository) ListOpenMergeCandidates(limit int) ([]MergeCandidate, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.db.Query(mergeCandidateSelect+`
		WHERE c.status = 'open'
		ORDER BY
			CASE c.recommendation WHEN 'merge' THEN 0 WHEN 'keep_separate' THEN 2 ELSE 1 END,
			c.score DESC, c.created_at DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list merge candidates: %w", err)
	}
	defer rows.Close()
	return scanMergeCandidates(rows)
}

// mergeCandidateSelect は候補一覧の共通 SELECT。
// iTunes ID を一緒に引くのは、編曲の違いを人が判断する材料
// （収録アルバム・再生時間）をフロントが引けるようにするため。
// **件数は秘匿を濾す。** この SELECT は公開の GET /api/songs/{id}/merge-candidates と
// 編集者向けの一覧で共用されるが、件数だけは公開側に合わせて常に濾す
// ── 編集者は歌唱一覧そのものを見られるので、ここで秘匿分を数えても得られる情報は無く、
// 逆に濾さないと公開側で「詳細の件数」と食い違う。
var mergeCandidateSelect = `
	SELECT c.id, c.score, c.reason, c.status, c.origin,
	       n.id, n.name, n.name_reading, n.original_artist, n.original_artist_reading, n.arts, n.created_at, n.updated_at,
	       e.id, e.name, e.name_reading, e.original_artist, e.original_artist_reading, e.arts, e.created_at, e.updated_at,
	       (SELECT COUNT(*) FROM performances p JOIN streams st ON st.id = p.stream_id
	         WHERE p.song_id = n.id AND st.is_hidden = FALSE AND ` + NotRestricted("st") + `),
	       (SELECT COUNT(*) FROM performances p JOIN streams st ON st.id = p.stream_id
	         WHERE p.song_id = e.id AND st.is_hidden = FALSE AND ` + NotRestricted("st") + `),
	       ARRAY(SELECT si.itunes_id FROM song_itunes si WHERE si.song_id = n.id ORDER BY si.is_primary DESC),
	       ARRAY(SELECT si.itunes_id FROM song_itunes si WHERE si.song_id = e.id ORDER BY si.is_primary DESC),
	       c.same_composition, c.same_arrangement, COALESCE(c.recommendation, ''),
	       COALESCE(c.role_new, ''), COALESCE(c.role_existing, ''),
	       COALESCE(c.verdict_note, ''), COALESCE(c.verdict_source, ''), c.verdict_at
	FROM song_merge_candidates c
	JOIN songs n ON n.id = c.new_song_id
	JOIN songs e ON e.id = c.existing_song_id`

func scanMergeCandidates(rows *sql.Rows) ([]MergeCandidate, error) {
	var out []MergeCandidate
	for rows.Next() {
		var c MergeCandidate
		var itunesNew, itunesOld pq.Int64Array
		if err := rows.Scan(
			&c.ID, &c.Score, &c.Reason, &c.Status, &c.Origin,
			&c.NewSong.ID, &c.NewSong.Name, &c.NewSong.NameReading, &c.NewSong.OriginalArtist,
			&c.NewSong.OriginalArtistReading, &c.NewSong.Arts, &c.NewSong.CreatedAt, &c.NewSong.UpdatedAt,
			&c.ExistingSong.ID, &c.ExistingSong.Name, &c.ExistingSong.NameReading, &c.ExistingSong.OriginalArtist,
			&c.ExistingSong.OriginalArtistReading, &c.ExistingSong.Arts, &c.ExistingSong.CreatedAt, &c.ExistingSong.UpdatedAt,
			&c.PerfCountNew, &c.PerfCountOld, &itunesNew, &itunesOld,
			&c.Verdict.SameComposition, &c.Verdict.SameArrangement, &c.Verdict.Recommendation,
			&c.Verdict.RoleNew, &c.Verdict.RoleExisting,
			&c.Verdict.Note, &c.Verdict.Source, &c.Verdict.At,
		); err != nil {
			return nil, fmt.Errorf("scan merge candidate: %w", err)
		}
		c.ItunesNew = []int64(itunesNew)
		c.ItunesOld = []int64(itunesOld)
		out = append(out, c)
	}
	return out, rows.Err()
}

// CountOpenMergeCandidates は未処理件数（バッジ表示用）。
func (r *SongMatchRepository) CountOpenMergeCandidates() (int, error) {
	var n int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM song_merge_candidates WHERE status = 'open'`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count merge candidates: %w", err)
	}
	return n, nil
}

// FindOpenMergeCandidatesForSong は特定の楽曲に紐づく未処理候補を返す（楽曲詳細で出す用）。
func (r *SongMatchRepository) FindOpenMergeCandidatesForSong(songID uuid.UUID) ([]MergeCandidate, error) {
	rows, err := r.db.Query(mergeCandidateSelect+`
		WHERE c.status = 'open' AND (c.new_song_id = $1 OR c.existing_song_id = $1)
		ORDER BY c.score DESC`, songID)
	if err != nil {
		return nil, fmt.Errorf("find merge candidates for song: %w", err)
	}
	defer rows.Close()
	return scanMergeCandidates(rows)
}

// SetMergeCandidateStatus は候補を処理済みにする（resolved / dismissed）。
func (r *SongMatchRepository) SetMergeCandidateStatus(id uuid.UUID, status string) error {
	res, err := r.db.Exec(`
		UPDATE song_merge_candidates SET status = $2, resolved_at = NOW()
		WHERE id = $1 AND status = 'open'`, id, status)
	if err != nil {
		return fmt.Errorf("update merge candidate: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ResolveCandidatesForMergedSong は楽曲が統合されたときに、その曲が絡む候補を閉じる。
// 楽曲行は ON DELETE CASCADE で消えるが、統合先が残る側の候補も畳んでおく。
func (r *SongMatchRepository) ResolveCandidatesForMergedSong(sourceID, targetID uuid.UUID) error {
	_, err := r.db.Exec(`
		UPDATE song_merge_candidates SET status = 'resolved', resolved_at = NOW()
		WHERE status = 'open'
		  AND (new_song_id IN ($1, $2) OR existing_song_id IN ($1, $2))`, sourceID, targetID)
	if err != nil {
		return fmt.Errorf("resolve merge candidates: %w", err)
	}
	return nil
}
