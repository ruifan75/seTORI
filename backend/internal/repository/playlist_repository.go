package repository

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/ruifan75/setori/internal/models"
)

// uuidStrings は uuid の配列を ANY($n::uuid[]) へ渡せる形へ変換する。
func uuidStrings(ids []uuid.UUID) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = id.String()
	}
	return out
}

// PlaylistRepository はプレイリストとその項目を扱う。
// 項目の実体は歌唱記録（performances）なので、明細の取得は PerformanceRepository を再利用する。
type PlaylistRepository struct {
	db   *sql.DB
	perf *PerformanceRepository
}

func NewPlaylistRepository(db *sql.DB, perf *PerformanceRepository) *PlaylistRepository {
	return &PlaylistRepository{db: db, perf: perf}
}

// PlaylistWithMeta は一覧表示用に曲数と所有者名を付けたプレイリスト。
type PlaylistWithMeta struct {
	models.Playlist
	ItemCount int    `json:"item_count"`
	OwnerName string `json:"owner_name"`
}

const playlistSelect = `
	SELECT p.id, p.user_id, p.name, p.description, p.visibility, p.share_slug, p.created_at, p.updated_at
	FROM playlists p`

const playlistSelectWithMeta = `
	SELECT p.id, p.user_id, p.name, p.description, p.visibility, p.share_slug, p.created_at, p.updated_at,
	       (SELECT count(*) FROM playlist_items pi WHERE pi.playlist_id = p.id) AS item_count,
	       COALESCE(NULLIF(u.display_name, ''), u.username) AS owner_name
	FROM playlists p
	JOIN users u ON u.id = p.user_id`

func scanPlaylist(row interface{ Scan(...any) error }) (*models.Playlist, error) {
	var p models.Playlist
	err := row.Scan(&p.ID, &p.UserID, &p.Name, &p.Description, &p.Visibility,
		&p.ShareSlug, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func scanPlaylistWithMeta(row interface{ Scan(...any) error }) (*PlaylistWithMeta, error) {
	var p PlaylistWithMeta
	err := row.Scan(&p.ID, &p.UserID, &p.Name, &p.Description, &p.Visibility,
		&p.ShareSlug, &p.CreatedAt, &p.UpdatedAt, &p.ItemCount, &p.OwnerName)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// generateShareSlug は限定公開 URL 用の推測困難なキーを作る。
func generateShareSlug() (string, error) {
	b := make([]byte, 12) // 24 文字の hex
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate share slug: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// Create はプレイリストを作成する。share_slug は未設定なら自動生成する。
func (r *PlaylistRepository) Create(p *models.Playlist) error {
	if p.ShareSlug == "" {
		slug, err := generateShareSlug()
		if err != nil {
			return err
		}
		p.ShareSlug = slug
	}
	query := `
		INSERT INTO playlists (user_id, name, description, visibility, share_slug)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at, updated_at`
	err := r.db.QueryRow(query, p.UserID, p.Name, p.Description, p.Visibility, p.ShareSlug).
		Scan(&p.ID, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create playlist: %w", err)
	}
	return nil
}

// FindByID は ID で引く。見つからなければ nil。公開範囲の判定は呼び出し側で行う。
func (r *PlaylistRepository) FindByID(id uuid.UUID) (*models.Playlist, error) {
	p, err := scanPlaylist(r.db.QueryRow(playlistSelect+" WHERE p.id = $1", id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find playlist: %w", err)
	}
	return p, nil
}

// FindByShareSlug は限定公開 URL のキーで引く。見つからなければ nil。
func (r *PlaylistRepository) FindByShareSlug(slug string) (*PlaylistWithMeta, error) {
	p, err := scanPlaylistWithMeta(r.db.QueryRow(playlistSelectWithMeta+" WHERE p.share_slug = $1", slug))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find playlist by slug: %w", err)
	}
	return p, nil
}

// FindByIDWithMeta は曲数・所有者名付きで引く。
func (r *PlaylistRepository) FindByIDWithMeta(id uuid.UUID) (*PlaylistWithMeta, error) {
	p, err := scanPlaylistWithMeta(r.db.QueryRow(playlistSelectWithMeta+" WHERE p.id = $1", id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find playlist with meta: %w", err)
	}
	return p, nil
}

func (r *PlaylistRepository) queryPlaylists(query string, args ...interface{}) ([]PlaylistWithMeta, error) {
	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query playlists: %w", err)
	}
	defer rows.Close()

	result := make([]PlaylistWithMeta, 0)
	for rows.Next() {
		p, err := scanPlaylistWithMeta(rows)
		if err != nil {
			return nil, fmt.Errorf("scan playlist: %w", err)
		}
		result = append(result, *p)
	}
	return result, rows.Err()
}

// ListByUser は本人のプレイリストを全て返す（private も含む）。
func (r *PlaylistRepository) ListByUser(userID uuid.UUID) ([]PlaylistWithMeta, error) {
	return r.queryPlaylists(playlistSelectWithMeta+" WHERE p.user_id = $1 ORDER BY p.updated_at DESC", userID)
}

// ListPublic は公開プレイリストを新しい順に返す。unlisted は一覧に出さない。
func (r *PlaylistRepository) ListPublic(limit, offset int) ([]PlaylistWithMeta, int, error) {
	var total int
	if err := r.db.QueryRow("SELECT count(*) FROM playlists WHERE visibility = 'public'").Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count public playlists: %w", err)
	}
	items, err := r.queryPlaylists(
		playlistSelectWithMeta+" WHERE p.visibility = 'public' ORDER BY p.updated_at DESC LIMIT $1 OFFSET $2",
		limit, offset)
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// Update は名称・説明・公開範囲を更新する（所有者の確認は呼び出し側の責任）。
func (r *PlaylistRepository) Update(p *models.Playlist) error {
	res, err := r.db.Exec(`
		UPDATE playlists SET name = $2, description = $3, visibility = $4, updated_at = NOW()
		WHERE id = $1`, p.ID, p.Name, p.Description, p.Visibility)
	if err != nil {
		return fmt.Errorf("update playlist: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return fmt.Errorf("update playlist: not found")
	}
	return nil
}

func (r *PlaylistRepository) Delete(id uuid.UUID) error {
	if _, err := r.db.Exec("DELETE FROM playlists WHERE id = $1", id); err != nil {
		return fmt.Errorf("delete playlist: %w", err)
	}
	return nil
}

// touch は更新日時を進める（項目の増減でも一覧の並びが動くように）。
func (r *PlaylistRepository) touch(playlistID uuid.UUID) error {
	if _, err := r.db.Exec("UPDATE playlists SET updated_at = NOW() WHERE id = $1", playlistID); err != nil {
		return fmt.Errorf("touch playlist: %w", err)
	}
	return nil
}

// ========== 項目 ==========

// AddItem は末尾に歌唱を追加する。既に入っている場合は何もしない（重複を作らない）。
func (r *PlaylistRepository) AddItem(playlistID, performanceID uuid.UUID) error {
	query := `
		INSERT INTO playlist_items (playlist_id, performance_id, position)
		VALUES ($1, $2, (SELECT COALESCE(MAX(position), -1) + 1 FROM playlist_items WHERE playlist_id = $1))
		ON CONFLICT (playlist_id, performance_id) DO NOTHING`
	if _, err := r.db.Exec(query, playlistID, performanceID); err != nil {
		return fmt.Errorf("add playlist item: %w", err)
	}
	return r.touch(playlistID)
}

// RemoveItem は項目を取り除く。残りの position は詰め直す。
func (r *PlaylistRepository) RemoveItem(playlistID, performanceID uuid.UUID) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec("DELETE FROM playlist_items WHERE playlist_id = $1 AND performance_id = $2",
		playlistID, performanceID); err != nil {
		return fmt.Errorf("remove playlist item: %w", err)
	}
	// position の抜けを詰める
	if _, err := tx.Exec(`
		UPDATE playlist_items pi SET position = ordered.rn - 1
		FROM (SELECT id, ROW_NUMBER() OVER (ORDER BY position) AS rn
		      FROM playlist_items WHERE playlist_id = $1) ordered
		WHERE pi.id = ordered.id AND pi.position <> ordered.rn - 1`, playlistID); err != nil {
		return fmt.Errorf("compact playlist positions: %w", err)
	}
	if _, err := tx.Exec("UPDATE playlists SET updated_at = NOW() WHERE id = $1", playlistID); err != nil {
		return fmt.Errorf("touch playlist: %w", err)
	}
	return tx.Commit()
}

// Reorder は与えられた並び順どおりに position を振り直す。
// 指定に含まれない項目は末尾へ回す（同時編集で取りこぼしても項目が消えないように）。
func (r *PlaylistRepository) Reorder(playlistID uuid.UUID, performanceIDs []uuid.UUID) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	for i, pid := range performanceIDs {
		if _, err := tx.Exec(
			"UPDATE playlist_items SET position = $3 WHERE playlist_id = $1 AND performance_id = $2",
			playlistID, pid, i); err != nil {
			return fmt.Errorf("reorder playlist item: %w", err)
		}
	}
	// 指定漏れを末尾へ（既存の相対順は保つ）。rn は 1 起算なので -1 して番号を詰める。
	if _, err := tx.Exec(`
		UPDATE playlist_items pi SET position = $2 + ordered.rn - 1
		FROM (SELECT id, ROW_NUMBER() OVER (ORDER BY position) AS rn
		      FROM playlist_items WHERE playlist_id = $1 AND NOT (performance_id = ANY($3::uuid[]))) ordered
		WHERE pi.id = ordered.id`, playlistID, len(performanceIDs), pq.Array(uuidStrings(performanceIDs))); err != nil {
		return fmt.Errorf("append unlisted playlist items: %w", err)
	}
	if _, err := tx.Exec("UPDATE playlists SET updated_at = NOW() WHERE id = $1", playlistID); err != nil {
		return fmt.Errorf("touch playlist: %w", err)
	}
	return tx.Commit()
}

// ListItems はプレイリストの歌唱を並び順で返す（配信・楽曲・タグ・歌手つき）。
// 非表示配信の歌唱も本人のプレイリストには残す（勝手に消えると混乱するため）。
func (r *PlaylistRepository) ListItems(playlistID uuid.UUID) ([]PerformanceWithDetails, error) {
	query := perfDetailSelect + `
		JOIN playlist_items pi ON pi.performance_id = p.id
		WHERE pi.playlist_id = $1
		ORDER BY pi.position ASC`
	return r.perf.queryPerformanceDetails(query, playlistID)
}

// ContainsPerformance は指定の歌唱が既に入っているかを返す（UI の追加済み表示用）。
func (r *PlaylistRepository) ContainsPerformance(playlistID, performanceID uuid.UUID) (bool, error) {
	var exists bool
	err := r.db.QueryRow(
		"SELECT EXISTS(SELECT 1 FROM playlist_items WHERE playlist_id = $1 AND performance_id = $2)",
		playlistID, performanceID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check playlist item: %w", err)
	}
	return exists, nil
}
