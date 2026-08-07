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
		songID, songmatch.TitleKey(name), ak.Primary, pq.Array(ak.Tokens), songmatch.RulesVersion, name, artist)
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
	if newSongID == existingSongID {
		return nil
	}
	_, err := r.db.Exec(`
		INSERT INTO song_merge_candidates (new_song_id, existing_song_id, score, reason)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT ON CONSTRAINT song_merge_candidates_pair_unique DO NOTHING`,
		newSongID, existingSongID, score, reason)
	if err != nil {
		return fmt.Errorf("record merge candidate: %w", err)
	}
	return nil
}

// MergeCandidate は統合候補 1 件（両側の楽曲情報つき）。
type MergeCandidate struct {
	ID           uuid.UUID
	Score        float64
	Reason       string
	Status       string
	NewSong      models.Song
	ExistingSong models.Song
	PerfCountNew int
	PerfCountOld int
}

// ListOpenMergeCandidates は未処理の統合候補を新しい順で返す。
func (r *SongMatchRepository) ListOpenMergeCandidates(limit int) ([]MergeCandidate, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.db.Query(`
		SELECT c.id, c.score, c.reason, c.status,
		       n.id, n.name, n.name_reading, n.original_artist, n.original_artist_reading, n.arts, n.created_at, n.updated_at,
		       e.id, e.name, e.name_reading, e.original_artist, e.original_artist_reading, e.arts, e.created_at, e.updated_at,
		       (SELECT COUNT(*) FROM performances p WHERE p.song_id = n.id),
		       (SELECT COUNT(*) FROM performances p WHERE p.song_id = e.id)
		FROM song_merge_candidates c
		JOIN songs n ON n.id = c.new_song_id
		JOIN songs e ON e.id = c.existing_song_id
		WHERE c.status = 'open'
		ORDER BY c.created_at DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list merge candidates: %w", err)
	}
	defer rows.Close()

	var out []MergeCandidate
	for rows.Next() {
		var c MergeCandidate
		if err := rows.Scan(
			&c.ID, &c.Score, &c.Reason, &c.Status,
			&c.NewSong.ID, &c.NewSong.Name, &c.NewSong.NameReading, &c.NewSong.OriginalArtist,
			&c.NewSong.OriginalArtistReading, &c.NewSong.Arts, &c.NewSong.CreatedAt, &c.NewSong.UpdatedAt,
			&c.ExistingSong.ID, &c.ExistingSong.Name, &c.ExistingSong.NameReading, &c.ExistingSong.OriginalArtist,
			&c.ExistingSong.OriginalArtistReading, &c.ExistingSong.Arts, &c.ExistingSong.CreatedAt, &c.ExistingSong.UpdatedAt,
			&c.PerfCountNew, &c.PerfCountOld,
		); err != nil {
			return nil, fmt.Errorf("scan merge candidate: %w", err)
		}
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
	rows, err := r.db.Query(`
		SELECT c.id, c.score, c.reason, c.status,
		       n.id, n.name, n.name_reading, n.original_artist, n.original_artist_reading, n.arts, n.created_at, n.updated_at,
		       e.id, e.name, e.name_reading, e.original_artist, e.original_artist_reading, e.arts, e.created_at, e.updated_at,
		       (SELECT COUNT(*) FROM performances p WHERE p.song_id = n.id),
		       (SELECT COUNT(*) FROM performances p WHERE p.song_id = e.id)
		FROM song_merge_candidates c
		JOIN songs n ON n.id = c.new_song_id
		JOIN songs e ON e.id = c.existing_song_id
		WHERE c.status = 'open' AND (c.new_song_id = $1 OR c.existing_song_id = $1)
		ORDER BY c.score DESC`, songID)
	if err != nil {
		return nil, fmt.Errorf("find merge candidates for song: %w", err)
	}
	defer rows.Close()

	var out []MergeCandidate
	for rows.Next() {
		var c MergeCandidate
		if err := rows.Scan(
			&c.ID, &c.Score, &c.Reason, &c.Status,
			&c.NewSong.ID, &c.NewSong.Name, &c.NewSong.NameReading, &c.NewSong.OriginalArtist,
			&c.NewSong.OriginalArtistReading, &c.NewSong.Arts, &c.NewSong.CreatedAt, &c.NewSong.UpdatedAt,
			&c.ExistingSong.ID, &c.ExistingSong.Name, &c.ExistingSong.NameReading, &c.ExistingSong.OriginalArtist,
			&c.ExistingSong.OriginalArtistReading, &c.ExistingSong.Arts, &c.ExistingSong.CreatedAt, &c.ExistingSong.UpdatedAt,
			&c.PerfCountNew, &c.PerfCountOld,
		); err != nil {
			return nil, fmt.Errorf("scan merge candidate: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
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
