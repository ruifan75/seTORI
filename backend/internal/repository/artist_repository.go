package repository

import (
	"database/sql"
	"fmt"
	"unicode"

	"github.com/google/uuid"
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
func (r *ArtistRepository) ListWithCounts(limit, offset int, search string) ([]models.Artist, int, error) {
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

	query := fmt.Sprintf(`
		SELECT a.id, a.name, a.name_reading, a.created_at, a.updated_at,
		       COUNT(sa.song_id) AS song_count
		FROM artists a
		LEFT JOIN song_artists sa ON sa.artist_id = a.id
		%s
		GROUP BY a.id
		ORDER BY song_count DESC, a.name ASC
		LIMIT $%d OFFSET $%d`, where, len(args)+1, len(args)+2)
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

// UpdateReading は読み仮名を更新する。
func (r *ArtistRepository) UpdateReading(id uuid.UUID, reading string) error {
	_, err := r.db.Exec(`UPDATE artists SET name_reading = NULLIF($2, ''), updated_at = NOW() WHERE id = $1`, id, reading)
	if err != nil {
		return fmt.Errorf("update artist reading: %w", err)
	}
	return nil
}

// FindSongsByArtist はアーティストに紐づく楽曲を演唱回数付きで返す。
func (r *ArtistRepository) FindSongsByArtist(artistID uuid.UUID, limit, offset int) ([]models.Song, map[uuid.UUID]int, int, error) {
	var total int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM song_artists WHERE artist_id = $1`, artistID).Scan(&total); err != nil {
		return nil, nil, 0, fmt.Errorf("count artist songs: %w", err)
	}

	rows, err := r.db.Query(`
		SELECT s.id, s.name, s.name_reading, s.original_artist, s.original_artist_reading, s.arts,
		       s.created_at, s.updated_at,
		       (SELECT COUNT(*) FROM performances p JOIN streams st ON st.id = p.stream_id
		        WHERE p.song_id = s.id AND st.is_hidden = FALSE) AS perf_count
		FROM songs s
		JOIN song_artists sa ON sa.song_id = s.id
		WHERE sa.artist_id = $1
		ORDER BY perf_count DESC, s.name ASC
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
