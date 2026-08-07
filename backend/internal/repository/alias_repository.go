package repository

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/ruifan75/setori/internal/models"
)

// AliasRepository は照合の学習層（楽曲の別表記・アーティストの別名義）を扱う。
type AliasRepository struct {
	db *sql.DB
}

func NewAliasRepository(db *sql.DB) *AliasRepository {
	return &AliasRepository{db: db}
}

// ---------- 楽曲の別表記（song_aliases） ----------

// FindSongAlias は表記から楽曲 ID を引く。無ければ (nil, nil)。
func (r *AliasRepository) FindSongAlias(nameKey, artistKey string) (*uuid.UUID, error) {
	if nameKey == "" {
		return nil, nil
	}
	var id uuid.UUID
	err := r.db.QueryRow(
		`SELECT song_id FROM song_aliases WHERE name_key = $1 AND artist_key = $2`,
		nameKey, artistKey).Scan(&id)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find song alias: %w", err)
	}
	return &id, nil
}

// PutSongAlias は表記 → 楽曲の対応を記録する（既にあれば付け替える）。
func (r *AliasRepository) PutSongAlias(db execer, nameKey, artistKey string, songID uuid.UUID, source string) error {
	if nameKey == "" {
		return nil
	}
	if db == nil {
		db = r.db
	}
	_, err := db.Exec(`
		INSERT INTO song_aliases (name_key, artist_key, song_id, source)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (name_key, artist_key) DO UPDATE
		SET song_id = EXCLUDED.song_id, source = EXCLUDED.source, created_at = NOW()`,
		nameKey, artistKey, songID, source)
	if err != nil {
		return fmt.Errorf("put song alias: %w", err)
	}
	return nil
}

// RepointSongAliases は統合で消える楽曲を指していた別表記を統合先へ付け替える。
// これをしないと、学習済みの表記が楽曲ごと消える（ON DELETE CASCADE）。
func (r *AliasRepository) RepointSongAliases(db execer, fromSongID, toSongID uuid.UUID) error {
	if db == nil {
		db = r.db
	}
	_, err := db.Exec(`UPDATE song_aliases SET song_id = $2 WHERE song_id = $1`, fromSongID, toSongID)
	if err != nil {
		return fmt.Errorf("repoint song aliases: %w", err)
	}
	return nil
}

// SongAliasRow は管理画面に出す学習済みの別表記。
type SongAliasRow struct {
	NameKey   string
	ArtistKey string
	Source    string
	Song      models.Song
}

// ListSongAliases は学習済みの別表記を新しい順で返す。
func (r *AliasRepository) ListSongAliases(limit int) ([]SongAliasRow, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.db.Query(`
		SELECT a.name_key, a.artist_key, a.source,
		       s.id, s.name, s.name_reading, s.original_artist, s.original_artist_reading,
		       s.arts, s.created_at, s.updated_at
		FROM song_aliases a JOIN songs s ON s.id = a.song_id
		ORDER BY a.created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list song aliases: %w", err)
	}
	defer rows.Close()

	var out []SongAliasRow
	for rows.Next() {
		var x SongAliasRow
		if err := rows.Scan(&x.NameKey, &x.ArtistKey, &x.Source,
			&x.Song.ID, &x.Song.Name, &x.Song.NameReading, &x.Song.OriginalArtist,
			&x.Song.OriginalArtistReading, &x.Song.Arts, &x.Song.CreatedAt, &x.Song.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan song alias: %w", err)
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// DeleteSongAlias は学習した対応を取り消す（誤学習の修正）。
func (r *AliasRepository) DeleteSongAlias(nameKey, artistKey string) error {
	res, err := r.db.Exec(`DELETE FROM song_aliases WHERE name_key = $1 AND artist_key = $2`, nameKey, artistKey)
	if err != nil {
		return fmt.Errorf("delete song alias: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ---------- アーティストの別名義（artist_aliases） ----------

// ArtistAliasMember は別名義グループの 1 名。
type ArtistAliasMember struct {
	NameKey     string
	DisplayName string
	Source      string
	Note        string
}

// ArtistAliasGroup は同一人物としてまとめられた名前の集まり。
type ArtistAliasGroup struct {
	GroupID uuid.UUID
	Members []ArtistAliasMember
}

// LoadArtistAliasMap は name_key → グループ代表の name_key を返す。
// 照合のたびに使うので 1 クエリで全部読む（アーティストは数百件規模）。
func (r *AliasRepository) LoadArtistAliasMap() (map[string]string, error) {
	rows, err := r.db.Query(`SELECT name_key, group_id FROM artist_aliases`)
	if err != nil {
		return nil, fmt.Errorf("load artist alias map: %w", err)
	}
	defer rows.Close()

	byGroup := map[uuid.UUID][]string{}
	for rows.Next() {
		var key string
		var gid uuid.UUID
		if err := rows.Scan(&key, &gid); err != nil {
			return nil, fmt.Errorf("scan artist alias: %w", err)
		}
		byGroup[gid] = append(byGroup[gid], key)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 代表は「グループ内で辞書順が最小の name_key」。DB の行順に依存せず
	// 同じ結果になるので、キャッシュを跨いでも判定がぶれない。
	canon := make(map[string]string)
	for _, keys := range byGroup {
		sort.Strings(keys)
		for _, k := range keys {
			canon[k] = keys[0]
		}
	}
	return canon, nil
}

// LinkArtists は 2 つの名前を同一人物として結びつける。
// どちらも未登録なら新しいグループ、片方が既存ならそこへ合流、
// 両方が別グループなら 2 つのグループを 1 つに畳む。
func (r *AliasRepository) LinkArtists(keyA, displayA, keyB, displayB, source, note string) error {
	if keyA == "" || keyB == "" || keyA == keyB {
		return fmt.Errorf("異なる 2 つの名前が必要です")
	}
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	groupOf := func(key string) (uuid.UUID, bool, error) {
		var gid uuid.UUID
		err := tx.QueryRow(`SELECT group_id FROM artist_aliases WHERE name_key = $1`, key).Scan(&gid)
		if err == sql.ErrNoRows {
			return uuid.Nil, false, nil
		}
		if err != nil {
			return uuid.Nil, false, err
		}
		return gid, true, nil
	}

	gidA, okA, err := groupOf(keyA)
	if err != nil {
		return err
	}
	gidB, okB, err := groupOf(keyB)
	if err != nil {
		return err
	}

	var target uuid.UUID
	switch {
	case okA && okB:
		if gidA == gidB {
			return tx.Commit() // 既に同じグループ
		}
		target = gidA
		if _, err := tx.Exec(`UPDATE artist_aliases SET group_id = $1 WHERE group_id = $2`, gidA, gidB); err != nil {
			return fmt.Errorf("merge alias groups: %w", err)
		}
	case okA:
		target = gidA
	case okB:
		target = gidB
	default:
		target = uuid.New()
	}

	insert := `
		INSERT INTO artist_aliases (name_key, group_id, display_name, source, note)
		VALUES ($1, $2, $3, $4, NULLIF($5, ''))
		ON CONFLICT (name_key) DO UPDATE SET group_id = EXCLUDED.group_id`
	if _, err := tx.Exec(insert, keyA, target, artistKeyDisplay(displayA, keyA), source, note); err != nil {
		return fmt.Errorf("insert alias a: %w", err)
	}
	if _, err := tx.Exec(insert, keyB, target, artistKeyDisplay(displayB, keyB), source, note); err != nil {
		return fmt.Errorf("insert alias b: %w", err)
	}
	return tx.Commit()
}

// ListArtistAliasGroups は別名義グループを返す（管理画面用）。
func (r *AliasRepository) ListArtistAliasGroups() ([]ArtistAliasGroup, error) {
	rows, err := r.db.Query(`
		SELECT group_id, name_key, display_name, source, COALESCE(note, '')
		FROM artist_aliases ORDER BY group_id, name_key`)
	if err != nil {
		return nil, fmt.Errorf("list artist alias groups: %w", err)
	}
	defer rows.Close()

	var out []ArtistAliasGroup
	byID := map[uuid.UUID]int{}
	for rows.Next() {
		var gid uuid.UUID
		var m ArtistAliasMember
		if err := rows.Scan(&gid, &m.NameKey, &m.DisplayName, &m.Source, &m.Note); err != nil {
			return nil, fmt.Errorf("scan artist alias group: %w", err)
		}
		idx, ok := byID[gid]
		if !ok {
			out = append(out, ArtistAliasGroup{GroupID: gid})
			idx = len(out) - 1
			byID[gid] = idx
		}
		out[idx].Members = append(out[idx].Members, m)
	}
	return out, rows.Err()
}

// UnlinkArtist はグループから 1 名を外す。
// 残りが 1 名だけになったグループは意味を持たないので畳む。
func (r *AliasRepository) UnlinkArtist(nameKey string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	var gid uuid.UUID
	err = tx.QueryRow(`SELECT group_id FROM artist_aliases WHERE name_key = $1`, nameKey).Scan(&gid)
	if err == sql.ErrNoRows {
		return sql.ErrNoRows
	}
	if err != nil {
		return fmt.Errorf("find alias: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM artist_aliases WHERE name_key = $1`, nameKey); err != nil {
		return fmt.Errorf("delete alias: %w", err)
	}
	if _, err := tx.Exec(`
		DELETE FROM artist_aliases
		WHERE group_id = $1 AND (SELECT COUNT(*) FROM artist_aliases WHERE group_id = $1) < 2`, gid); err != nil {
		return fmt.Errorf("prune alias group: %w", err)
	}
	return tx.Commit()
}

// ---------- 判定履歴（artist_alias_checks） ----------

// AliasPairKey は 2 つの name_key から順序に依存しないキーを作る。
func AliasPairKey(a, b string) string {
	if a > b {
		a, b = b, a
	}
	return a + "|" + b
}

// FindCheckedPairs は既に判定済みの組を返す（同一人物かどうかを問わない）。
// AI に同じことを何度も聞かないために使う。
func (r *AliasRepository) FindCheckedPairs(pairKeys []string) (map[string]bool, error) {
	if len(pairKeys) == 0 {
		return map[string]bool{}, nil
	}
	rows, err := r.db.Query(`SELECT pair_key, same FROM artist_alias_checks WHERE pair_key = ANY($1)`, pq.Array(pairKeys))
	if err != nil {
		return nil, fmt.Errorf("find checked pairs: %w", err)
	}
	defer rows.Close()

	out := map[string]bool{}
	for rows.Next() {
		var k string
		var same bool
		if err := rows.Scan(&k, &same); err != nil {
			return nil, fmt.Errorf("scan checked pair: %w", err)
		}
		out[k] = same
	}
	return out, rows.Err()
}

// RecordCheck は判定結果を残す。same=false も必ず残すこと（再問い合わせの抑止）。
func (r *AliasRepository) RecordCheck(keyA, keyB string, same bool, source, note string) error {
	a, b := keyA, keyB
	if a > b {
		a, b = b, a
	}
	_, err := r.db.Exec(`
		INSERT INTO artist_alias_checks (pair_key, name_key_a, name_key_b, same, source, note)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''))
		ON CONFLICT (pair_key) DO UPDATE
		SET same = EXCLUDED.same, source = EXCLUDED.source, note = EXCLUDED.note, checked_at = NOW()`,
		AliasPairKey(keyA, keyB), a, b, same, source, note)
	if err != nil {
		return fmt.Errorf("record alias check: %w", err)
	}
	return nil
}

// artistKeyDisplay は保存する表示名を整える（空なら畳んだキーで代用）。
func artistKeyDisplay(display, key string) string {
	if d := strings.TrimSpace(display); d != "" {
		return d
	}
	return key
}
