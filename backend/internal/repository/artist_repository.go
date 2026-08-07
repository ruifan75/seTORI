package repository

import (
	"database/sql"
	"fmt"
	"unicode"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/ruifan75/setori/internal/models"
)

// ArtistRepository は artists / song_artists を扱う。
type ArtistRepository struct {
	db *sql.DB
}

func NewArtistRepository(db *sql.DB) *ArtistRepository {
	return &ArtistRepository{db: db}
}

// ContainsHan は文字列に漢字が含まれるかを返す（読み仮名の妥当性判定に使用）。
// 読みは平仮名/片仮名であるべきで、漢字が含まれていれば「読みが未整備」とみなす。
func ContainsHan(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

// ListWithCounts はアーティスト一覧（曲数付き）を返す。search は名前/読みの部分一致。
// sort は "songs"（曲数順）か "name"（数字→英字→読み順、既定）。dir は asc|desc。
func (r *ArtistRepository) ListWithCounts(limit, offset int, search, sort, dir string) ([]models.Artist, int, error) {
	where := ""
	args := []any{}
	if search != "" {
		where = "WHERE a.name ILIKE '%' || $1 || '%' OR a.name_reading ILIKE '%' || $1 || '%'"
		args = append(args, search)
	}

	var total int
	if err := r.db.QueryRow("SELECT COUNT(*) FROM artists a "+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count artists: %w", err)
	}

	order := nameSortOrderDir("a.name", "a.name_reading", dir)
	if sort == "songs" {
		order = "song_count " + normDir(dir) + ", a.name ASC"
	}
	query := fmt.Sprintf(`
		SELECT a.id, a.name, a.name_reading, a.created_at, a.updated_at,
		       COUNT(sa.song_id) AS song_count
		FROM artists a
		LEFT JOIN song_artists sa ON sa.artist_id = a.id
		%s
		GROUP BY a.id
		ORDER BY %s
		LIMIT $%d OFFSET $%d`, where, order, len(args)+1, len(args)+2)
	args = append(args, limit, offset)

	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list artists: %w", err)
	}
	defer rows.Close()

	var artists []models.Artist
	for rows.Next() {
		var a models.Artist
		if err := rows.Scan(&a.ID, &a.Name, &a.NameReading, &a.CreatedAt, &a.UpdatedAt, &a.SongCount); err != nil {
			return nil, 0, fmt.Errorf("scan artist: %w", err)
		}
		artists = append(artists, a)
	}
	return artists, total, rows.Err()
}

// FindByID はアーティストを曲数付きで取得する。見つからなければ nil。
func (r *ArtistRepository) FindByID(id uuid.UUID) (*models.Artist, error) {
	var a models.Artist
	err := r.db.QueryRow(`
		SELECT a.id, a.name, a.name_reading, a.created_at, a.updated_at,
		       (SELECT COUNT(*) FROM song_artists sa WHERE sa.artist_id = a.id)
		FROM artists a WHERE a.id = $1`, id).
		Scan(&a.ID, &a.Name, &a.NameReading, &a.CreatedAt, &a.UpdatedAt, &a.SongCount)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find artist: %w", err)
	}
	return &a, nil
}

// FindByName は名前でアーティストを取得する。見つからなければ nil。
func (r *ArtistRepository) FindByName(name string) (*models.Artist, error) {
	var a models.Artist
	err := r.db.QueryRow(`SELECT id, name, name_reading, created_at, updated_at, 0 FROM artists WHERE name = $1`, name).
		Scan(&a.ID, &a.Name, &a.NameReading, &a.CreatedAt, &a.UpdatedAt, &a.SongCount)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find artist by name: %w", err)
	}
	return &a, nil
}

// Rename はアーティスト名を変更し、所属する全楽曲の original_artist 表示テキストも
// 連動して更新する（アーティスト単位で表記を一括修正できる）。
// 呼び出し前に新名称が他アーティストと重複しないことを確認すること。
func (r *ArtistRepository) Rename(id uuid.UUID, newName string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`UPDATE artists SET name = $2, updated_at = NOW() WHERE id = $1`, id, newName); err != nil {
		return fmt.Errorf("rename artist: %w", err)
	}
	if _, err := tx.Exec(`
		UPDATE songs SET original_artist = $2, updated_at = NOW()
		WHERE id IN (SELECT song_id FROM song_artists WHERE artist_id = $1)`, id, newName); err != nil {
		return fmt.Errorf("update songs artist text: %w", err)
	}
	return tx.Commit()
}

// MergeArtists は source を target に統合する。
//  1. 両者に同名楽曲がある場合はその楽曲も統合（演唱記録を target 側へ移動、重複は除去）
//  2. 残りの source 楽曲は original_artist テキストを target 名に書き換え
//  3. マッピングを target に付け替え、source アーティストを削除
func (r *ArtistRepository) MergeArtists(sourceID, targetID uuid.UUID) error {
	target, err := r.FindByID(targetID)
	if err != nil {
		return err
	}
	if target == nil {
		return fmt.Errorf("target artist not found")
	}

	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	// 1. 同名楽曲の衝突ペアを検出（source 側の曲名が target 側にも存在する）
	rows, err := tx.Query(`
		SELECT ss.id, ts.id
		FROM song_artists sa
		JOIN songs ss ON ss.id = sa.song_id
		JOIN songs ts ON ts.name = ss.name AND ts.original_artist = $2 AND ts.id <> ss.id
		WHERE sa.artist_id = $1`, sourceID, target.Name)
	if err != nil {
		return fmt.Errorf("find colliding songs: %w", err)
	}
	type pair struct{ src, dst uuid.UUID }
	var pairs []pair
	for rows.Next() {
		var p pair
		if err := rows.Scan(&p.src, &p.dst); err != nil {
			rows.Close()
			return fmt.Errorf("scan colliding songs: %w", err)
		}
		pairs = append(pairs, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	// 衝突した楽曲を統合：演唱記録を移動（同一配信・同一開始秒の重複は削除）→ source 曲を削除
	for _, p := range pairs {
		if _, err := tx.Exec(`
			DELETE FROM performances p1 WHERE p1.song_id = $1 AND EXISTS (
				SELECT 1 FROM performances p2
				WHERE p2.song_id = $2 AND p2.stream_id = p1.stream_id AND p2.start_seconds = p1.start_seconds
			)`, p.src, p.dst); err != nil {
			return fmt.Errorf("dedupe performances: %w", err)
		}
		if _, err := tx.Exec(`UPDATE performances SET song_id = $2 WHERE song_id = $1`, p.src, p.dst); err != nil {
			return fmt.Errorf("move performances: %w", err)
		}
		if _, err := tx.Exec(`DELETE FROM songs WHERE id = $1`, p.src); err != nil {
			return fmt.Errorf("delete merged song: %w", err)
		}
	}

	// 2. 残りの source 楽曲の表示テキストを target 名に統一
	if _, err := tx.Exec(`
		UPDATE songs SET original_artist = $2, original_artist_reading = $3, updated_at = NOW()
		WHERE id IN (SELECT song_id FROM song_artists WHERE artist_id = $1)`,
		sourceID, target.Name, target.NameReading); err != nil {
		return fmt.Errorf("update songs artist text: %w", err)
	}

	// 3. マッピングを付け替え、source を削除
	if _, err := tx.Exec(`
		INSERT INTO song_artists (song_id, artist_id)
		SELECT song_id, $2 FROM song_artists WHERE artist_id = $1
		ON CONFLICT DO NOTHING`, sourceID, targetID); err != nil {
		return fmt.Errorf("remap song artists: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM song_artists WHERE artist_id = $1`, sourceID); err != nil {
		return fmt.Errorf("clear source mappings: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM artists WHERE id = $1`, sourceID); err != nil {
		return fmt.Errorf("delete source artist: %w", err)
	}

	return tx.Commit()
}

// FindSongsByArtist はアーティストに紐づく楽曲を演唱回数付きで返す。
func (r *ArtistRepository) FindSongsByArtist(artistID uuid.UUID, limit, offset int, sort, dir string) ([]models.Song, map[uuid.UUID]int, int, error) {
	var total int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM song_artists WHERE artist_id = $1`, artistID).Scan(&total); err != nil {
		return nil, nil, 0, fmt.Errorf("count artist songs: %w", err)
	}

	// 既定は歌唱回数の多い順。"name" 指定で曲名の五十音順。
	order := "perf_count " + dirOr(dir, "desc") + ", s.name ASC"
	if sort == "name" {
		order = nameSortOrderDir("s.name", "s.name_reading", dir)
	}

	rows, err := r.db.Query(`
		SELECT s.id, s.name, s.name_reading, s.original_artist, s.original_artist_reading, s.arts,
		       s.created_at, s.updated_at,
		       (SELECT COUNT(*) FROM performances p JOIN streams st ON st.id = p.stream_id
		        WHERE p.song_id = s.id AND st.is_hidden = FALSE) AS perf_count
		FROM songs s
		JOIN song_artists sa ON sa.song_id = s.id
		WHERE sa.artist_id = $1
		ORDER BY `+order+`
		LIMIT $2 OFFSET $3`, artistID, limit, offset)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("query artist songs: %w", err)
	}
	defer rows.Close()

	var songs []models.Song
	counts := make(map[uuid.UUID]int)
	for rows.Next() {
		var s models.Song
		var perfCount int
		if err := rows.Scan(&s.ID, &s.Name, &s.NameReading, &s.OriginalArtist, &s.OriginalArtistReading,
			&s.Arts, &s.CreatedAt, &s.UpdatedAt, &perfCount); err != nil {
			return nil, nil, 0, fmt.Errorf("scan artist song: %w", err)
		}
		songs = append(songs, s)
		counts[s.ID] = perfCount
	}
	return songs, counts, total, rows.Err()
}

// FindReferencesBySongIDs は複数楽曲の artist key をまとめて取得する。
func (r *ArtistRepository) FindReferencesBySongIDs(songIDs []uuid.UUID) (map[uuid.UUID][]models.ArtistReference, error) {
	result := make(map[uuid.UUID][]models.ArtistReference, len(songIDs))
	if len(songIDs) == 0 {
		return result, nil
	}

	ids := make([]string, len(songIDs))
	for i, id := range songIDs {
		ids[i] = id.String()
	}
	rows, err := r.db.Query(`
		SELECT sa.song_id, a.id, a.name
		FROM song_artists sa
		JOIN artists a ON a.id = sa.artist_id
		WHERE sa.song_id = ANY($1::uuid[])
		ORDER BY sa.song_id, a.name`, pq.Array(ids))
	if err != nil {
		return nil, fmt.Errorf("find artist references by song ids: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var songID uuid.UUID
		var artist models.ArtistReference
		if err := rows.Scan(&songID, &artist.ID, &artist.Name); err != nil {
			return nil, fmt.Errorf("scan artist reference: %w", err)
		}
		result[songID] = append(result[songID], artist)
	}
	return result, rows.Err()
}

func (r *ArtistRepository) FindReferencesBySongID(songID uuid.UUID) ([]models.ArtistReference, error) {
	bySong, err := r.FindReferencesBySongIDs([]uuid.UUID{songID})
	if err != nil {
		return nil, err
	}
	return bySong[songID], nil
}

// SyncSongArtist は楽曲の original_artist テキストと artists/song_artists を同期する。
// 楽曲の作成・更新時にサービス層から呼ぶ。アーティストは表記そのままで upsert する。
func (r *ArtistRepository) SyncSongArtist(songID uuid.UUID, artistText string) error {
	if artistText == "" {
		return nil
	}
	var artistID uuid.UUID
	err := r.db.QueryRow(`
		INSERT INTO artists (name) VALUES ($1)
		ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
		RETURNING id`, artistText).Scan(&artistID)
	if err != nil {
		return fmt.Errorf("upsert artist: %w", err)
	}

	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM song_artists WHERE song_id = $1`, songID); err != nil {
		return fmt.Errorf("clear song artists: %w", err)
	}
	if _, err := tx.Exec(`INSERT INTO song_artists (song_id, artist_id) VALUES ($1, $2)`, songID, artistID); err != nil {
		return fmt.Errorf("insert song artist: %w", err)
	}
	return tx.Commit()
}

// ListAllReadings は全アーティストの id/name/name_reading を返す（読みデータのエクスポート用）。
func (r *ArtistRepository) ListAllReadings() ([]models.Artist, error) {
	rows, err := r.db.Query(`SELECT id, name, name_reading FROM artists ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list artist readings: %w", err)
	}
	defer rows.Close()

	var out []models.Artist
	for rows.Next() {
		var a models.Artist
		if err := rows.Scan(&a.ID, &a.Name, &a.NameReading); err != nil {
			return nil, fmt.Errorf("scan artist reading: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// UpdateReadingPropagate は読み仮名を更新し、所属楽曲の original_artist_reading にも反映する
// （読みデータの取り込み用。アーティストの読みが正で、楽曲側の表示テキストを追従させる）。
func (r *ArtistRepository) UpdateReadingPropagate(id uuid.UUID, reading string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`UPDATE artists SET name_reading = NULLIF($2, ''), updated_at = NOW() WHERE id = $1`, id, reading); err != nil {
		return fmt.Errorf("update artist reading: %w", err)
	}
	if _, err := tx.Exec(`
		UPDATE songs SET original_artist_reading = NULLIF($2, ''), updated_at = NOW()
		WHERE id IN (SELECT song_id FROM song_artists WHERE artist_id = $1)`, id, reading); err != nil {
		return fmt.Errorf("propagate artist reading: %w", err)
	}
	return tx.Commit()
}

// ListMissingReadings は読みが未設定/不正（漢字を含む名前なのに読みが無い・読みに漢字が残る）
// なアーティストを返す（AI 補完の対象抽出）。
func (r *ArtistRepository) ListMissingReadings(limit int) ([]models.Artist, error) {
	rows, err := r.db.Query(`
		SELECT id, name, name_reading, created_at, updated_at, 0
		FROM artists
		ORDER BY name
	`)
	if err != nil {
		return nil, fmt.Errorf("list artists for readings: %w", err)
	}
	defer rows.Close()

	var out []models.Artist
	for rows.Next() {
		var a models.Artist
		if err := rows.Scan(&a.ID, &a.Name, &a.NameReading, &a.CreatedAt, &a.UpdatedAt, &a.SongCount); err != nil {
			return nil, fmt.Errorf("scan artist: %w", err)
		}
		reading := ""
		if a.NameReading.Valid {
			reading = a.NameReading.String
		}
		// 対象：名前に漢字を含み、読みが空 or 読みにまだ漢字が残っている
		if ContainsHan(a.Name) && (reading == "" || ContainsHan(reading)) {
			out = append(out, a)
			if len(out) >= limit {
				break
			}
		}
	}
	return out, rows.Err()
}
