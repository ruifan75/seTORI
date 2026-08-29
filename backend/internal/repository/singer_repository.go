package repository

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/ruifan75/setori/internal/models"
)

type SingerRepository struct {
	db *sql.DB
}

func NewSingerRepository(db *sql.DB) *SingerRepository {
	return &SingerRepository{db: db}
}

// effectiveOrg は表示・グループ分けに使う事務所キーの SQL 式。
// 手動指定（organization_override）があればそれ、無ければ Holodex の値。
const effectiveOrg = `COALESCE(s.organization_override, s.organization)`

// singerColumns は歌手の全カラム（SELECT と scanSinger で対にして使う）。
// organization は Holodex の値、organization_override は手動指定で、
// JOIN しているのは実効値のほう。呼び出し側は必ず singerFrom で組み立てる。
const singerColumns = `s.id, s.name, s.english_name, s.photo_url,
	s.organization, s.organization_override, o.display_name,
	COALESCE(o.is_unaffiliated, FALSE),
	s.metadata_source, s.is_hidden, s.members_only_policy, s.created_at, s.updated_at`

// singerFrom は singers と organizations を結んだ FROM 句。
// 事務所は任意なので LEFT JOIN（所属なしのチャンネルを落とさない）。
const singerFrom = `FROM singers s LEFT JOIN organizations o ON ` + effectiveOrg + ` = o.key`

// scanSinger は singerColumns の並びで1行読む。
func scanSinger(row interface{ Scan(...any) error }) (models.Singer, error) {
	var s models.Singer
	err := row.Scan(&s.ID, &s.Name, &s.EnglishName, &s.PhotoURL,
		&s.Organization, &s.OrganizationOverride, &s.OrganizationName, &s.OrganizationUnaffil,
		&s.MetadataSource, &s.IsHidden, &s.MembersOnlyPolicy, &s.CreatedAt, &s.UpdatedAt)
	return s, err
}

// hiddenClause は非表示チャンネルを除く WHERE 句を返す（includeHidden なら空）。
func hiddenClause(includeHidden bool, keyword string) string {
	if includeHidden {
		return ""
	}
	return " " + keyword + " s.is_hidden = FALSE"
}

// FindAll はすべての歌手を取得する。includeHidden=false なら非表示チャンネルを除く。
func (r *SingerRepository) FindAll(limit, offset int, sort, dir string, includeHidden bool) ([]models.Singer, int, error) {
	where := hiddenClause(includeHidden, "WHERE")

	var total int
	err := r.db.QueryRow("SELECT COUNT(*) FROM singers s" + where).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count singers: %w", err)
	}

	// 既定は名前の五十音順。"organization" 指定で事務所順（名前を第2キー）。
	// 事務所は表示名と並び順で並べる（key の文字列順ではない）。所属なしは最後。
	order := nameSortOrderDir("s.name", "''", dir)
	if sort == "organization" {
		order = organizationGroupOrder(normDir(dir)) + ", " + nameSortOrder("s.name", "''")
	}

	query := `
		SELECT ` + singerColumns + `
		` + singerFrom + where + `
		ORDER BY ` + order + `
		LIMIT $1 OFFSET $2`

	rows, err := r.db.Query(query, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("query singers: %w", err)
	}
	defer rows.Close()

	var singers []models.Singer
	for rows.Next() {
		s, err := scanSinger(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scan singer: %w", err)
		}
		singers = append(singers, s)
	}

	return singers, total, nil
}

// organizationGroupOrder は事務所グループの並び順を返す。
// 所属なしは常に最後で、それ以外は organizations.sort_order → 表示名の五十音順。
// key の文字列順にしないのは、表示名を直しても並びが変わらないと直感に反するため。
//
// 「所属なし」には 2 種類が入る：事務所が未設定のものと、Holodex の Independents の
// ように無所属を意味する分類（is_unaffiliated）。別の事実だが同じ組に見せる。
func organizationGroupOrder(dir string) string {
	return unaffiliatedLast + ` ASC,
		o.sort_order ` + dir + `,
		` + nameSortOrderDir("o.display_name", "''", dir)
}

// unaffiliatedLast は「所属なし扱いなら 1、それ以外は 0」を返す式（並び替え用）。
const unaffiliatedLast = `CASE WHEN ` + effectiveOrg + ` IS NULL OR COALESCE(o.is_unaffiliated, FALSE) THEN 1 ELSE 0 END`

// FindAllGrouped は事務所別表示用に全件を「事務所 → 名前（五十音）」順で返す。
// グループを跨ぐページ送りは意味を成さないため、ここではページングしない。
// 所属なし（NULL）は最後にまとめる。
func (r *SingerRepository) FindAllGrouped(includeHidden bool) ([]models.Singer, error) {
	query := `
		SELECT ` + singerColumns + `
		` + singerFrom + hiddenClause(includeHidden, "WHERE") + `
		ORDER BY ` + organizationGroupOrder("ASC") + `,
			` + nameSortOrder("s.name", "''")
	// 所属なし扱いの組は複数の key（NULL と Independents など）が混ざるが、
	// 上の並びで最後にまとまるので、service 側で 1 つの組に束ねられる。

	rows, err := r.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("query singers grouped: %w", err)
	}
	defer rows.Close()

	var singers []models.Singer
	for rows.Next() {
		s, err := scanSinger(rows)
		if err != nil {
			return nil, fmt.Errorf("scan singer: %w", err)
		}
		singers = append(singers, s)
	}

	return singers, nil
}

// SetHidden はチャンネル一覧での表示/非表示を切り替える。
// メタデータの更新経路（Update / UpdateManualMetadata）と分けているのは、
// Holodex 管理チャンネルでもこのフラグだけは切り替えられる必要があるため。
// 戻り値は対象が存在したか。
func (r *SingerRepository) SetHidden(id string, hidden bool) (bool, error) {
	res, err := r.db.Exec(
		"UPDATE singers SET is_hidden = $2, updated_at = NOW() WHERE id = $1", id, hidden)
	if err != nil {
		return false, fmt.Errorf("set singer hidden: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("set singer hidden: %w", err)
	}
	return affected > 0, nil
}

// SetMembersOnlyPolicy は会限セットリストの公開可否を設定する（migration 056）。
//
// policy が空文字なら NULL（未確認）へ戻す。**NULL と 'deny' は実効的には同じ**
// （どちらも伏せる）が、「まだ訊いていない」と「訊いて断られた」を区別するために分ける
// ── 未確認のチャンネルを一覧したいときに要る。
func (r *SingerRepository) SetMembersOnlyPolicy(id, policy string) (bool, error) {
	var val sql.NullString
	if policy != "" {
		val = sql.NullString{String: policy, Valid: true}
	}
	res, err := r.db.Exec(`UPDATE singers SET members_only_policy = $2, updated_at = NOW() WHERE id = $1`, id, val)
	if err != nil {
		return false, fmt.Errorf("set members only policy: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("set members only policy rows: %w", err)
	}
	return n > 0, nil
}

// MembersOnlyDetectedExpr は「その配信が会限らしいか」の**検出**を表す式。
//
// **実効判定（EffectiveRestrictedExpr）とは別物。** あちらは「歌単を伏せるか」で、
// チャンネル方針が allow なら false になる。こちらは「会限かどうか」という事実なので、
// 公開してよいと決めた後も真のまま ── 方針を設定する画面は、方針が allow でも
// 「このチャンネルは会限を 85 本持っている」と出す必要がある。
//
// 検出の材料を変えるときはここ 1 か所（issue #32 で members_only タグへ移す予定）。
func MembersOnlyDetectedExpr(alias string) string {
	return alias + ".is_restricted"
}

// CountMembersOnlyByOwner は所有者ごとの会限配信の本数を返す（0 本のチャンネルは含まない）。
//
// **一覧の SQL に相関サブクエリを足さない。** 1 回のクエリで全チャンネル分をまとめて引き、
// 呼び出し側で突き合わせる ── 148 チャンネルに対して N+1 を作らないため。
func (r *SingerRepository) CountMembersOnlyByOwner() (map[string]int, error) {
	rows, err := r.db.Query(`
		SELECT ss.singer_id, COUNT(*)
		FROM stream_singers ss
		JOIN streams s ON s.id = ss.stream_id
		WHERE ss.is_owner AND ` + MembersOnlyDetectedExpr("s") + `
		GROUP BY ss.singer_id`)
	if err != nil {
		return nil, fmt.Errorf("count members only by owner: %w", err)
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, fmt.Errorf("scan members only count: %w", err)
		}
		counts[id] = n
	}
	return counts, rows.Err()
}

// SetOrganizationOverride は Holodex の分類を手動で上書きする（空文字なら上書きを解除）。
//
// Holodex の値（organization）は触らない。同期は今後もそちらを更新し続けるので、
// 上書きを外せば最新の Holodex 分類に戻る。メタデータの更新経路と分けているのは、
// これが Holodex のメタデータではなく seTORI 側の判断であり、
// Holodex 管理チャンネルでも設定できる必要があるため。
// 戻り値は対象が存在したか。
func (r *SingerRepository) SetOrganizationOverride(id, org string) (bool, error) {
	override := sql.NullString{String: org, Valid: strings.TrimSpace(org) != ""}
	if err := r.ensureOrganization(override); err != nil {
		return false, err
	}

	res, err := r.db.Exec(
		"UPDATE singers SET organization_override = $2, updated_at = NOW() WHERE id = $1",
		id, override)
	if err != nil {
		return false, fmt.Errorf("set organization override: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("set organization override: %w", err)
	}
	return affected > 0, nil
}

// FindByID はチャンネル ID で歌手を取得する。
func (r *SingerRepository) FindByID(id string) (*models.Singer, error) {
	query := `
		SELECT ` + singerColumns + `
		` + singerFrom + ` WHERE s.id = $1`

	s, err := scanSinger(r.db.QueryRow(query, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find singer by id: %w", err)
	}
	return &s, nil
}

// ensureOrganization は書き込み前に事務所の行を用意する。
//
// singers.organization は organizations への FK なので、Holodex が今まで見たことのない
// org を返した瞬間に取り込みが FK 違反で落ちる。それを避けるため、書き込み経路の入口で
// 必ず通す。表示名は key と同じもので作っておき、あとから管理画面で直す
// （「知らない事務所だから取り込まない」は選ばない ─ 登録は止めず、人の確認は後で受ける）。
//
// 呼び出し側で忘れると本番で初めて落ちる種類の不具合なので、
// service ではなくこのリポジトリの書き込みメソッド側に置いてある。
func (r *SingerRepository) ensureOrganization(org sql.NullString) error {
	key := strings.TrimSpace(org.String)
	if !org.Valid || key == "" {
		return nil
	}
	_, err := r.db.Exec(`
		INSERT INTO organizations (key, display_name)
		VALUES ($1, $1)
		ON CONFLICT (key) DO NOTHING`, key)
	if err != nil {
		return fmt.Errorf("ensure organization %q: %w", key, err)
	}
	return nil
}

// Create は新しい歌手を作成する。
//
// **現在この関数に呼び出し元は無い**（Update も同じ）。歌手を作る経路は
// すべて Holodex 同期を通り、Upsert に集約されている。
// 復活させるときは is_hidden の既定を決めること ── ここは列に入れていないので
// DB default の false（＝一覧に出る）になる。同期経由で作るなら Upsert に
// SingerOrigin を渡すほうが正しい。
func (r *SingerRepository) Create(s *models.Singer) error {
	if err := r.ensureOrganization(s.Organization); err != nil {
		return err
	}
	query := `
		INSERT INTO singers (id, name, english_name, photo_url, organization, metadata_source)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING created_at, updated_at`

	source := normalizeSingerMetadataSource(s.MetadataSource)
	err := r.db.QueryRow(query, s.ID, s.Name, s.EnglishName, s.PhotoURL, s.Organization, source).
		Scan(&s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create singer: %w", err)
	}
	s.MetadataSource = source
	return nil
}

// Update は歌手を更新する。
func (r *SingerRepository) Update(s *models.Singer) error {
	if err := r.ensureOrganization(s.Organization); err != nil {
		return err
	}
	query := `
		UPDATE singers
		SET name = $2, english_name = $3, photo_url = $4, organization = $5, metadata_source = $6, updated_at = NOW()
		WHERE id = $1
		RETURNING updated_at`

	source := normalizeSingerMetadataSource(s.MetadataSource)
	err := r.db.QueryRow(query, s.ID, s.Name, s.EnglishName, s.PhotoURL, s.Organization, source).
		Scan(&s.UpdatedAt)
	if err != nil {
		return fmt.Errorf("update singer: %w", err)
	}
	s.MetadataSource = source
	return nil
}

// SingerOrigin は Upsert が**行を新規作成するとき**の既定の可視性を決める。
// 既存行には効かない（後述）。
//
// 同期は「人が名指ししたチャンネル」と「その副産物として流れ込んだチャンネル」の
// 両方を作る。後者はコラボ相手・mention 先で、一覧に出したいものではない。
// 実際、既定が false だった頃の本番は 148 件中 147 件を手で隠していた＝既定が逆だった。
//
// bool ではなく型にしてあるのは、呼び出し側で `Upsert(singer, true)` と書かれても
// どちらの意味か読めないため。引数を必須にしているのは、
// **新しい呼び出し元が origin を決めずにはコンパイルできないようにする**ため。
type SingerOrigin int

const (
	// SingerRequested … 人がそのチャンネルを名指しで追加・同期した。既定で一覧に出す。
	SingerRequested SingerOrigin = iota
	// SingerDiscovered … 配信の同期に付随して見つかった（所有者・mention）。既定で非表示。
	// 編集者は一覧の include_hidden で見つけられる。
	SingerDiscovered
)

func (o SingerOrigin) hiddenOnInsert() bool { return o == SingerDiscovered }

// Upsert は歌手を作成または更新する（Holodex 同期用）。
//
// **is_hidden を書くのは INSERT のときだけで、既存行では意図的に触らない。**
// 同期は繰り返し走るので、conflict 側で書き戻すと手動で非表示にしたチャンネルが
// 次の同期で一覧に戻ってしまう。ON CONFLICT の SET に is_hidden を足さないこと。
func (r *SingerRepository) Upsert(s *models.Singer, origin SingerOrigin) error {
	if err := r.ensureOrganization(s.Organization); err != nil {
		return err
	}
	query := `
		INSERT INTO singers (id, name, english_name, photo_url, organization, metadata_source, is_hidden)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (id) DO UPDATE SET
			name = EXCLUDED.name,
			english_name = EXCLUDED.english_name,
			photo_url = EXCLUDED.photo_url,
			organization = EXCLUDED.organization,
			metadata_source = EXCLUDED.metadata_source,
			updated_at = NOW()
		RETURNING created_at, updated_at, is_hidden`

	source := normalizeSingerMetadataSource(s.MetadataSource)
	err := r.db.QueryRow(query, s.ID, s.Name, s.EnglishName, s.PhotoURL, s.Organization, source, origin.hiddenOnInsert()).
		Scan(&s.CreatedAt, &s.UpdatedAt, &s.IsHidden)
	if err != nil {
		return fmt.Errorf("upsert singer: %w", err)
	}
	s.MetadataSource = source
	return nil
}

// UpdateManualMetadata updates user-editable metadata without changing the source.
// organization は**意図的に触らない**。事務所の書き込み口は 2 つだけに保つ：
//
//	organization          … Holodex 同期だけが書く（外部の事実）
//	organization_override … SetOrganizationOverride だけが書く（こちらの判断）
//
// ここからも書けるようにすると、同じ列を 2 経路が別の意味で更新することになり、
// 「同期で戻る値」と「戻らない値」が混ざって追えなくなる。
func (r *SingerRepository) UpdateManualMetadata(s *models.Singer) error {
	query := `
		UPDATE singers
		SET name = $2, english_name = $3, photo_url = $4, updated_at = NOW()
		WHERE id = $1
		RETURNING created_at, updated_at`

	err := r.db.QueryRow(query, s.ID, s.Name, s.EnglishName, s.PhotoURL).
		Scan(&s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return fmt.Errorf("update singer metadata: %w", err)
	}
	return nil
}

// Delete は歌手を削除する。
func (r *SingerRepository) Delete(id string) error {
	_, err := r.db.Exec("DELETE FROM singers WHERE id = $1", id)
	if err != nil {
		return fmt.Errorf("delete singer: %w", err)
	}
	return nil
}

// GetStreamCount は歌手が参加した配信数を取得する（非表示でない配信だけを集計）。
func (r *SingerRepository) GetStreamCount(singerID string) (int, error) {
	var count int
	err := r.db.QueryRow(`
		SELECT COUNT(DISTINCT ss.stream_id)
		FROM stream_singers ss
		JOIN streams st ON ss.stream_id = st.id
		WHERE ss.singer_id = $1 AND st.is_hidden = FALSE
	`, singerID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count streams: %w", err)
	}
	return count, nil
}

// GetPerformanceCount は歌手の歌唱数を取得する（非表示・秘匿でない配信だけを集計）。
// **件数も秘匿の対象**（一覧から落としても件数が合わなければ存在が漏れる）。
func (r *SingerRepository) GetPerformanceCount(singerID string) (int, error) {
	var count int
	err := r.db.QueryRow(`
		SELECT COUNT(*)
		FROM performance_singers ps
		JOIN performances p ON ps.performance_id = p.id
		JOIN streams st ON p.stream_id = st.id
		WHERE ps.singer_id = $1 AND st.is_hidden = FALSE AND `+NotRestricted("st")+`
	`, singerID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count performances: %w", err)
	}
	return count, nil
}

// Search は歌手を検索する。
// 非表示チャンネルも返す：名前で探すのは「そのチャンネルを見に行く」意図の操作で、
// 詳細ページ自体は非表示でも開けるため、ここで隠すと辿り着く手段だけを塞ぐことになる。
func (r *SingerRepository) Search(query string, limit int) ([]models.Singer, error) {
	sqlQuery := `
		SELECT ` + singerColumns + `
		` + singerFrom + `
		WHERE s.name ILIKE $1 OR s.english_name ILIKE $1
		ORDER BY s.name ASC
		LIMIT $2`

	searchPattern := "%" + query + "%"
	rows, err := r.db.Query(sqlQuery, searchPattern, limit)
	if err != nil {
		return nil, fmt.Errorf("search singers: %w", err)
	}
	defer rows.Close()

	var singers []models.Singer
	for rows.Next() {
		s, err := scanSinger(rows)
		if err != nil {
			return nil, fmt.Errorf("scan singer: %w", err)
		}
		singers = append(singers, s)
	}

	return singers, nil
}

// FindByOrganization は事務所別に歌手を取得する。
func (r *SingerRepository) FindByOrganization(org string) ([]models.Singer, error) {
	query := `
		SELECT ` + singerColumns + `
		` + singerFrom + `
		WHERE ` + effectiveOrg + ` = $1
		ORDER BY s.name ASC`

	rows, err := r.db.Query(query, org)
	if err != nil {
		return nil, fmt.Errorf("query singers by org: %w", err)
	}
	defer rows.Close()

	var singers []models.Singer
	for rows.Next() {
		s, err := scanSinger(rows)
		if err != nil {
			return nil, fmt.Errorf("scan singer: %w", err)
		}
		singers = append(singers, s)
	}

	return singers, nil
}

func normalizeSingerMetadataSource(source string) string {
	if source == "" {
		return "holodex"
	}
	return source
}
