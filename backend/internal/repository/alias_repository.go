package repository

import (
	"database/sql"
	"fmt"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// AliasRepository は照合の「記憶」を扱う。持つのは 2 つだけ。
//
//  1. アーティストの別名義（artists.aliases）
//     松任谷由実 と 荒井由実 は文字列では絶対に解けない。しかも一度分かれば
//     その人の全楽曲に効くので、覚えておく価値がある。
//
//  2. 「別人 / 別の曲」という**否定**の記録（artist_alias_checks / song_identity_checks）
//     batch では確信度の高い AI 判定がそのまま performances になるため、人が
//     「これは違う」と消したものを次の force 実行が書き戻してしまう。それを止める。
//
// 肯定は保存しない。楽曲の同一性は AI が読み込みのたびに解き、結果は performances
// として残る。表記 → 楽曲の対応表（song_aliases）は、AI の二段照合に置き換えて廃止した。
type AliasRepository struct {
	db *sql.DB
}

func NewAliasRepository(db *sql.DB) *AliasRepository {
	return &AliasRepository{db: db}
}

// ---------- アーティストの別名義 ----------

// ArtistAliasRow は別名義を持つアーティスト 1 人。
type ArtistAliasRow struct {
	ID      uuid.UUID
	Name    string
	Aliases []string
}

// ListArtistAliases は別名義を持つアーティストだけを返す。
// 照合キーへ畳むのは service 側（畳み方の規則は pkg/songmatch が持つ）。
func (r *AliasRepository) ListArtistAliases() ([]ArtistAliasRow, error) {
	rows, err := r.db.Query(
		`SELECT id, name, aliases FROM artists WHERE array_length(aliases, 1) > 0`)
	if err != nil {
		return nil, fmt.Errorf("list artist aliases: %w", err)
	}
	defer rows.Close()

	var out []ArtistAliasRow
	for rows.Next() {
		var a ArtistAliasRow
		if err := rows.Scan(&a.ID, &a.Name, pq.Array(&a.Aliases)); err != nil {
			return nil, fmt.Errorf("scan artist alias: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// AddArtistAlias は canonical の別名義として alias を足す。
//
// canonical の行が無ければ作る。「曲が 1 つも無い名義は artists に行が無い」ため、
// 別名義の登録が行の有無に左右されないようにしておく必要がある。
func (r *AliasRepository) AddArtistAlias(canonical, alias string) error {
	if canonical == "" || alias == "" || canonical == alias {
		return fmt.Errorf("異なる 2 つの名前が必要です")
	}
	_, err := r.db.Exec(`
		INSERT INTO artists (name, aliases) VALUES ($1, ARRAY[$2]::text[])
		ON CONFLICT (name) DO UPDATE
		SET aliases = (
			SELECT array_agg(DISTINCT x) FROM unnest(artists.aliases || ARRAY[$2]::text[]) AS x
		), updated_at = NOW()`, canonical, alias)
	if err != nil {
		return fmt.Errorf("add artist alias: %w", err)
	}
	return nil
}

// RemoveArtistAlias は誤って登録した別名義を外す。
func (r *AliasRepository) RemoveArtistAlias(canonical, alias string) error {
	_, err := r.db.Exec(
		`UPDATE artists SET aliases = array_remove(aliases, $2), updated_at = NOW() WHERE name = $1`,
		canonical, alias)
	if err != nil {
		return fmt.Errorf("remove artist alias: %w", err)
	}
	return nil
}

// ---------- 否定の記録 ----------

// AliasPairKey は 2 つの名前の組を順序に依らず一意に表す。
func AliasPairKey(a, b string) string {
	if a > b {
		a, b = b, a
	}
	return a + "|" + b
}

// FindArtistRejections は「別人」と記録済みの組を返す。
func (r *AliasRepository) FindArtistRejections(pairKeys []string) (map[string]bool, error) {
	return r.findRejections(`SELECT pair_key FROM artist_alias_checks WHERE NOT same AND pair_key = ANY($1)`, pairKeys)
}

// RecordArtistRejection は「この 2 つは別人」を残す。
func (r *AliasRepository) RecordArtistRejection(keyA, keyB, source, note string) error {
	a, b := keyA, keyB
	if a > b {
		a, b = b, a
	}
	_, err := r.db.Exec(`
		INSERT INTO artist_alias_checks (pair_key, name_key_a, name_key_b, same, source, note)
		VALUES ($1, $2, $3, FALSE, $4, NULLIF($5, ''))
		ON CONFLICT (pair_key) DO UPDATE
		SET same = FALSE, source = EXCLUDED.source, note = EXCLUDED.note, checked_at = NOW()`,
		AliasPairKey(keyA, keyB), a, b, source, note)
	if err != nil {
		return fmt.Errorf("record artist rejection: %w", err)
	}
	return nil
}

// SongIdentityPairKey は「この表記」と「この曲」の組を一意に表す。
func SongIdentityPairKey(nameKey, artistKey string, songID uuid.UUID) string {
	return nameKey + "\x1f" + artistKey + "\x1f" + songID.String()
}

// FindSongRejections は「別の曲」と記録済みの組を返す。
func (r *AliasRepository) FindSongRejections(pairKeys []string) (map[string]bool, error) {
	return r.findRejections(`SELECT pair_key FROM song_identity_checks WHERE NOT same AND pair_key = ANY($1)`, pairKeys)
}

// RecordSongRejection は「この表記はこの曲ではない」を残す。
func (r *AliasRepository) RecordSongRejection(nameKey, artistKey string, songID uuid.UUID, source, note string) error {
	_, err := r.db.Exec(`
		INSERT INTO song_identity_checks (pair_key, name_key, artist_key, song_id, same, source, note)
		VALUES ($1, $2, $3, $4, FALSE, $5, NULLIF($6, ''))
		ON CONFLICT (pair_key) DO UPDATE
		SET same = FALSE, source = EXCLUDED.source, note = EXCLUDED.note, checked_at = NOW()`,
		SongIdentityPairKey(nameKey, artistKey, songID), nameKey, artistKey, songID, source, note)
	if err != nil {
		return fmt.Errorf("record song rejection: %w", err)
	}
	return nil
}

// RepointSongIdentityChecks は統合で消える楽曲を指していた否定を統合先へ付け替える。
// 付け替えないと「この表記はこの曲ではない」が楽曲ごと消え、統合のたびに復活する。
func (r *AliasRepository) RepointSongIdentityChecks(db execer, fromSongID, toSongID uuid.UUID) error {
	if db == nil {
		db = r.db
	}
	// 付け替えると pair_key が衝突しうる（同じ表記について両方の曲を判定済みの場合）。
	// 統合先の判定を正とし、統合元の行は捨てる。
	if _, err := db.Exec(`
		DELETE FROM song_identity_checks a
		WHERE a.song_id = $1 AND EXISTS (
			SELECT 1 FROM song_identity_checks b
			WHERE b.song_id = $2 AND b.name_key = a.name_key AND b.artist_key = a.artist_key)`,
		fromSongID, toSongID); err != nil {
		return fmt.Errorf("prune song identity checks: %w", err)
	}
	if _, err := db.Exec(`
		UPDATE song_identity_checks
		SET song_id = $2, pair_key = name_key || E'\x1f' || artist_key || E'\x1f' || $2::text
		WHERE song_id = $1`, fromSongID, toSongID); err != nil {
		return fmt.Errorf("repoint song identity checks: %w", err)
	}
	return nil
}

func (r *AliasRepository) findRejections(query string, pairKeys []string) (map[string]bool, error) {
	if len(pairKeys) == 0 {
		return map[string]bool{}, nil
	}
	rows, err := r.db.Query(query, pq.Array(pairKeys))
	if err != nil {
		return nil, fmt.Errorf("find rejections: %w", err)
	}
	defer rows.Close()

	out := map[string]bool{}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, fmt.Errorf("scan rejection: %w", err)
		}
		out[k] = true
	}
	return out, rows.Err()
}
