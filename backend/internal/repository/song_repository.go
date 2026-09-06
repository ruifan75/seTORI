package repository

import (
	"database/sql"
	"fmt"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/ruifan75/setori/internal/models"
	"github.com/ruifan75/setori/pkg/util"
)

type SongRepository struct {
	db *sql.DB
}

func NewSongRepository(db *sql.DB) *SongRepository {
	return &SongRepository{db: db}
}

// songListBody は楽曲一覧の FROM 〜 ORDER BY を組み立てる。
// sort: "name"(既定) / "artist" / "performances"、dir: asc|desc。
// where は "" か "WHERE ..."（プレースホルダの番号は呼び出し側の責任）。
//
// **FROM と ORDER BY を対で持つ。** 歌唱数順のときだけ集計を JOIN し、ORDER BY が
// その別名を参照するので、片方だけ変えると SQL エラーになる（一覧の SELECT で
// 同じ形の取り違えを 2 度やっている。streamListQuery の注記を参照）。
//
// **相関サブクエリで数えない。** 以前は曲ごとに `(SELECT COUNT(*) ...)` を回しており、
// 秘匿判定が曲 × 配信の組ごとに評価されていた。手元 922 曲で推定 cost 224,711 ＝
// 既定の jit_above_cost=100000 を越え、ページ要求のたびに JIT のコンパイル費を
// 払っていた（issue #30）。配信ごとに 1 回だけ数えて JOIN すると 16,410 まで下がり、
// JIT は発火しない（実測 44.6ms → 9.0ms）。
func songListBody(sort, dir, where string) string {
	from := "FROM songs"
	var order string

	switch sort {
	case "artist":
		order = nameSortOrderDir("original_artist", "original_artist_reading", dir)
	case "performances":
		from += `
			LEFT JOIN (
			    SELECT p.song_id, COUNT(*) AS n
			    FROM performances p
			    JOIN streams st ON st.id = p.stream_id
			    WHERE st.is_hidden = FALSE AND ` + NotRestricted("st") + `
			    GROUP BY p.song_id
			) visible ON visible.song_id = songs.id`
		// 歌唱が 1 件も無い曲は JOIN で NULL になる。COALESCE を外すと
		// NULLS FIRST/LAST の既定に左右されて 0 件の曲が先頭に来る。
		order = fmt.Sprintf("COALESCE(visible.n, 0) %s, ", dirOr(dir, "desc")) +
			nameSortOrder("name", "name_reading")
	default:
		order = nameSortOrderDir("name", "name_reading", dir)
	}

	if where != "" {
		from += "\n\t\t\t" + where
	}
	return from + "\n\t\t\tORDER BY " + order
}

// FindAll はすべての楽曲を取得する（ページング、検索、並び替え対応）。
func (r *SongRepository) FindAll(limit, offset int, search, sort, dir string) ([]models.Song, int, error) {
	var total int
	var rows *sql.Rows
	var err error

	if search != "" {
		// pg_trgm であいまい検索する
		countQuery := `
			SELECT COUNT(*) FROM songs
			WHERE name ILIKE $1 OR original_artist ILIKE $1 OR name_reading ILIKE $1`
		searchPattern := "%" + search + "%"
		err = r.db.QueryRow(countQuery, searchPattern).Scan(&total)
		if err != nil {
			return nil, 0, fmt.Errorf("count songs: %w", err)
		}

		query := `
			SELECT songs.id, songs.name, songs.name_reading, songs.original_artist,
			       songs.original_artist_reading, songs.arts, songs.created_at, songs.updated_at
			` + songListBody(sort, dir,
			"WHERE songs.name ILIKE $1 OR songs.original_artist ILIKE $1 OR songs.name_reading ILIKE $1") + `
			LIMIT $2 OFFSET $3`
		rows, err = r.db.Query(query, searchPattern, limit, offset)
	} else {
		err = r.db.QueryRow("SELECT COUNT(*) FROM songs").Scan(&total)
		if err != nil {
			return nil, 0, fmt.Errorf("count songs: %w", err)
		}

		query := `
			SELECT songs.id, songs.name, songs.name_reading, songs.original_artist,
			       songs.original_artist_reading, songs.arts, songs.created_at, songs.updated_at
			` + songListBody(sort, dir, "") + `
			LIMIT $1 OFFSET $2`
		rows, err = r.db.Query(query, limit, offset)
	}

	if err != nil {
		return nil, 0, fmt.Errorf("query songs: %w", err)
	}
	defer rows.Close()

	var songs []models.Song
	for rows.Next() {
		var s models.Song
		err := rows.Scan(&s.ID, &s.Name, &s.NameReading, &s.OriginalArtist,
			&s.OriginalArtistReading, &s.Arts, &s.CreatedAt, &s.UpdatedAt)
		if err != nil {
			return nil, 0, fmt.Errorf("scan song: %w", err)
		}
		songs = append(songs, s)
	}

	return songs, total, nil
}

// FindByID は ID で楽曲を取得する。
func (r *SongRepository) FindByID(id uuid.UUID) (*models.Song, error) {
	query := `
		SELECT id, name, name_reading, original_artist, original_artist_reading, arts, created_at, updated_at
		FROM songs WHERE id = $1`

	var s models.Song
	err := r.db.QueryRow(query, id).Scan(
		&s.ID, &s.Name, &s.NameReading, &s.OriginalArtist,
		&s.OriginalArtistReading, &s.Arts, &s.CreatedAt, &s.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find song by id: %w", err)
	}
	return &s, nil
}

// FindByNameAndArtist は曲名とアーティストで検索する（正規化時の重複確認用）。
// まず完全一致で比較し、見つからなければ lower + trim であいまい比較する（大文字小文字や空白の差を吸収）。
// Go 側で先に NFKC 正規化を行う（Ⅱ→II など Unicode の差を吸収）。
func (r *SongRepository) FindByNameAndArtist(name, artist string) (*models.Song, error) {
	// 完全一致で比較する
	exactQuery := `
		SELECT id, name, name_reading, original_artist, original_artist_reading, arts, created_at, updated_at
		FROM songs WHERE name = $1 AND original_artist = $2`

	var s models.Song
	err := r.db.QueryRow(exactQuery, name, artist).Scan(
		&s.ID, &s.Name, &s.NameReading, &s.OriginalArtist,
		&s.OriginalArtistReading, &s.Arts, &s.CreatedAt, &s.UpdatedAt)
	if err == nil {
		return &s, nil
	}
	if err != sql.ErrNoRows {
		return nil, fmt.Errorf("find song by name and artist: %w", err)
	}

	// フォールバック：NFKC 正規化 + lower + trim によるあいまい比較
	normalizedName := util.NormalizeUnicode(name)
	normalizedArtist := util.NormalizeUnicode(artist)

	fuzzyQuery := `
		SELECT id, name, name_reading, original_artist, original_artist_reading, arts, created_at, updated_at
		FROM songs
		WHERE lower(replace(trim(normalize(name, NFKC)), ' ', '')) = lower(replace(trim($1), ' ', ''))
		  AND lower(replace(trim(normalize(original_artist, NFKC)), ' ', '')) = lower(replace(trim($2), ' ', ''))`

	err = r.db.QueryRow(fuzzyQuery, normalizedName, normalizedArtist).Scan(
		&s.ID, &s.Name, &s.NameReading, &s.OriginalArtist,
		&s.OriginalArtistReading, &s.Arts, &s.CreatedAt, &s.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find song by name and artist (fuzzy): %w", err)
	}
	return &s, nil
}

// Create は新しい楽曲を作成する。
func (r *SongRepository) Create(s *models.Song) error {
	s.ID = uuid.New()
	query := `
		INSERT INTO songs (id, name, name_reading, original_artist, original_artist_reading, arts)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING created_at, updated_at`

	err := r.db.QueryRow(query, s.ID, s.Name, s.NameReading, s.OriginalArtist,
		s.OriginalArtistReading, s.Arts).Scan(&s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create song: %w", err)
	}
	// 照合キーは楽曲と同時に作る。サービス層に任せると呼び忘れた経路のぶんだけ
	// 「検索に出てこない曲」が生まれるので、ここで面倒を見る。
	if err := upsertSongMatchKey(r.db, s.ID, s.Name, s.OriginalArtist); err != nil {
		return err
	}
	return nil
}

// Update は楽曲を更新する。
func (r *SongRepository) Update(s *models.Song) error {
	query := `
		UPDATE songs
		SET name = $2, name_reading = $3, original_artist = $4,
		    original_artist_reading = $5, arts = $6, updated_at = NOW()
		WHERE id = $1
		RETURNING updated_at`

	err := r.db.QueryRow(query, s.ID, s.Name, s.NameReading, s.OriginalArtist,
		s.OriginalArtistReading, s.Arts).Scan(&s.UpdatedAt)
	if err != nil {
		return fmt.Errorf("update song: %w", err)
	}
	if err := upsertSongMatchKey(r.db, s.ID, s.Name, s.OriginalArtist); err != nil {
		return err
	}
	return nil
}

// Delete は楽曲を削除する。
func (r *SongRepository) Delete(id uuid.UUID) error {
	_, err := r.db.Exec("DELETE FROM songs WHERE id = $1", id)
	if err != nil {
		return fmt.Errorf("delete song: %w", err)
	}
	return nil
}

// GetPerformanceCount は楽曲の歌唱回数を取得する（非表示・秘匿でない配信だけを集計）。
// **件数も秘匿の対象。** 一覧から歌唱を落としても、件数が合わなければ存在が漏れる。
func (r *SongRepository) GetPerformanceCount(songID uuid.UUID) (int, error) {
	var count int
	err := r.db.QueryRow(`
		SELECT COUNT(*)
		FROM performances p
		JOIN streams st ON p.stream_id = st.id
		WHERE p.song_id = $1 AND st.is_hidden = FALSE AND `+NotRestricted("st")+`
	`, songID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("get performance count: %w", err)
	}
	return count, nil
}

// HasAnyPerformance は歌唱が 1 件でもあるかを返す。**濾さない。**
//
// `GetPerformanceCount` は非表示・秘匿を落とすが、削除の可否はそれで決められない
// ── 見えない歌唱も `performances.song_id` の参照なので、消そうとすれば
// `ON DELETE RESTRICT` に当たる。画面の「0 件」を信じて削除を試すと、
// 理由の分からない失敗になる。
//
// **件数は返さない。** 見えている件数は既に画面へ出ているので隠す意味が無いが、
// 隠している歌唱を数に混ぜると、そこから存在が漏れる（§2 で count も濾している
// のと同じ理由）。削除を止めるのに必要なのは「あるか」だけ。
func (r *SongRepository) HasAnyPerformance(songID uuid.UUID) (bool, error) {
	var exists bool
	err := r.db.QueryRow(
		`SELECT EXISTS (SELECT 1 FROM performances WHERE song_id = $1)`, songID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check song performances: %w", err)
	}
	return exists, nil
}

// GetPerformanceCounts は複数楽曲の歌唱回数を一括取得し（非表示でない配信だけを集計）、N+1 を避ける。
func (r *SongRepository) GetPerformanceCounts(songIDs []uuid.UUID) (map[uuid.UUID]int, error) {
	counts := make(map[uuid.UUID]int, len(songIDs))
	if len(songIDs) == 0 {
		return counts, nil
	}

	ids := make([]string, len(songIDs))
	for i, id := range songIDs {
		ids[i] = id.String()
	}

	query := `
		SELECT p.song_id, COUNT(*)
		FROM performances p
		JOIN streams st ON p.stream_id = st.id
		WHERE p.song_id = ANY($1::uuid[]) AND st.is_hidden = FALSE AND ` + NotRestricted("st") + `
		GROUP BY p.song_id`

	rows, err := r.db.Query(query, pq.Array(ids))
	if err != nil {
		return nil, fmt.Errorf("get performance counts: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id uuid.UUID
		var c int
		if err := rows.Scan(&id, &c); err != nil {
			return nil, fmt.Errorf("scan performance count: %w", err)
		}
		counts[id] = c
	}
	return counts, rows.Err()
}

// FindByItunesID は iTunes ID で楽曲を検索する。
func (r *SongRepository) FindByItunesID(itunesID int64) (*models.Song, error) {
	query := `
		SELECT s.id, s.name, s.name_reading, s.original_artist, s.original_artist_reading, s.arts, s.created_at, s.updated_at
		FROM songs s
		JOIN song_itunes si ON s.id = si.song_id
		WHERE si.itunes_id = $1
		LIMIT 1`

	var s models.Song
	err := r.db.QueryRow(query, itunesID).Scan(
		&s.ID, &s.Name, &s.NameReading, &s.OriginalArtist,
		&s.OriginalArtistReading, &s.Arts, &s.CreatedAt, &s.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find song by itunes id: %w", err)
	}
	return &s, nil
}

// SearchSimilar は trigram で類似楽曲を検索する（AI 正規化候補用）。
func (r *SongRepository) SearchSimilar(name string, limit int) ([]models.Song, error) {
	query := `
		SELECT id, name, name_reading, original_artist, original_artist_reading, arts, created_at, updated_at,
		       similarity(name, $1) AS sim
		FROM songs
		WHERE similarity(name, $1) > 0.3
		ORDER BY sim DESC
		LIMIT $2`

	rows, err := r.db.Query(query, name, limit)
	if err != nil {
		return nil, fmt.Errorf("search similar songs: %w", err)
	}
	defer rows.Close()

	var songs []models.Song
	for rows.Next() {
		var s models.Song
		var sim float64
		err := rows.Scan(&s.ID, &s.Name, &s.NameReading, &s.OriginalArtist,
			&s.OriginalArtistReading, &s.Arts, &s.CreatedAt, &s.UpdatedAt, &sim)
		if err != nil {
			return nil, fmt.Errorf("scan similar song: %w", err)
		}
		songs = append(songs, s)
	}

	return songs, nil
}

// MergeSong は統合元楽曲のすべての performance を統合先へ移し、統合元を削除する。
func (r *SongRepository) MergeSong(sourceSongID, targetSongID uuid.UUID) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// 1. すべての performance の song_id を統合元から統合先へ更新する
	updateQuery := `
		UPDATE performances 
		SET song_id = $1 
		WHERE song_id = $2`
	_, err = tx.Exec(updateQuery, targetSongID, sourceSongID)
	if err != nil {
		return fmt.Errorf("update performances: %w", err)
	}

	// 2. 統合元楽曲と関連する song_itunes を削除する
	// song_itunes には ON DELETE CASCADE があるため自動で削除される
	deleteQuery := `DELETE FROM songs WHERE id = $1`
	_, err = tx.Exec(deleteQuery, sourceSongID)
	if err != nil {
		return fmt.Errorf("delete source song: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

// ListMissingNameReadings は曲名読みが未整備（曲名に漢字を含み、読みが空 or 読みに漢字が残る）
// な楽曲を返す（AI 読み補完の対象抽出）。
func (r *SongRepository) ListMissingNameReadings(limit int) ([]models.Song, error) {
	rows, err := r.db.Query(`SELECT id, name, name_reading FROM songs ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list songs for readings: %w", err)
	}
	defer rows.Close()

	var out []models.Song
	for rows.Next() {
		var s models.Song
		if err := rows.Scan(&s.ID, &s.Name, &s.NameReading); err != nil {
			return nil, fmt.Errorf("scan song: %w", err)
		}
		reading := ""
		if s.NameReading.Valid {
			reading = s.NameReading.String
		}
		if ContainsHan(s.Name) && (reading == "" || ContainsHan(reading)) {
			out = append(out, s)
			if len(out) >= limit {
				break
			}
		}
	}
	return out, rows.Err()
}

// ListAllReadings は全楽曲の id/name/name_reading を返す（読みデータのエクスポート用）。
func (r *SongRepository) ListAllReadings() ([]models.Song, error) {
	rows, err := r.db.Query(`SELECT id, name, name_reading FROM songs ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list song readings: %w", err)
	}
	defer rows.Close()

	var out []models.Song
	for rows.Next() {
		var s models.Song
		if err := rows.Scan(&s.ID, &s.Name, &s.NameReading); err != nil {
			return nil, fmt.Errorf("scan song reading: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// UpdateNameReading は曲名読みのみを更新する。
func (r *SongRepository) UpdateNameReading(id uuid.UUID, reading string) error {
	_, err := r.db.Exec(`UPDATE songs SET name_reading = NULLIF($2, ''), updated_at = NOW() WHERE id = $1`, id, reading)
	if err != nil {
		return fmt.Errorf("update song name reading: %w", err)
	}
	return nil
}
