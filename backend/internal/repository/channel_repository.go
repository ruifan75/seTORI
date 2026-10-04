package repository

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/lib/pq"

	"github.com/ruifan75/setori/internal/models"
)

type ChannelRepository struct {
	db *sql.DB
}

func NewChannelRepository(db *sql.DB) *ChannelRepository {
	return &ChannelRepository{db: db}
}

// effectiveOrg は表示・グループ分けに使う事務所キーの SQL 式。
// 手動指定（organization_override）があればそれ、無ければ Holodex の値。
const effectiveOrg = `COALESCE(s.organization_override, s.organization)`

// channelColumns は歌手の全カラム（SELECT と scanChannel で対にして使う）。
// organization は Holodex の値、organization_override は手動指定で、
// JOIN しているのは実効値のほう。呼び出し側は必ず channelFrom で組み立てる。
const channelColumns = `s.id, s.name, s.english_name, s.photo_url,
	s.organization, s.organization_override, o.display_name,
	COALESCE(o.is_unaffiliated, FALSE),
	s.metadata_source, s.is_hidden, s.members_only_policy, s.auto_fill_enabled,
	s.created_at, s.updated_at`

// channelFrom は singers と organizations を結んだ FROM 句。
// 事務所は任意なので LEFT JOIN（所属なしのチャンネルを落とさない）。
const channelFrom = `FROM channels s LEFT JOIN organizations o ON ` + effectiveOrg + ` = o.key`

// scanChannel は channelColumns の並びで1行読む。
func scanChannel(row interface{ Scan(...any) error }) (models.Channel, error) {
	var s models.Channel
	err := row.Scan(&s.ID, &s.Name, &s.EnglishName, &s.PhotoURL,
		&s.Organization, &s.OrganizationOverride, &s.OrganizationName, &s.OrganizationUnaffil,
		&s.MetadataSource, &s.IsHidden, &s.MembersOnlyPolicy, &s.AutoFillEnabled,
		&s.CreatedAt, &s.UpdatedAt)
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
// hidden は（includeHidden のとき）非表示チャンネルの総数。一覧で区の見出しに出す。
func (r *ChannelRepository) FindAll(limit, offset int, sort, dir string, includeHidden bool) (channels []models.Channel, total, hidden int, err error) {
	where := hiddenClause(includeHidden, "WHERE")

	err = r.db.QueryRow("SELECT COUNT(*), COUNT(*) FILTER (WHERE s.is_hidden) FROM channels s"+where).Scan(&total, &hidden)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("count singers: %w", err)
	}

	order := channelListOrder(sort, dir, includeHidden)

	query := `
		SELECT ` + channelColumns + `
		` + channelFrom + where + `
		ORDER BY ` + order + `
		LIMIT $1 OFFSET $2`

	rows, err := r.db.Query(query, limit, offset)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("query singers: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		s, err := scanChannel(rows)
		if err != nil {
			return nil, 0, 0, fmt.Errorf("scan singer: %w", err)
		}
		channels = append(channels, s)
	}

	return channels, total, hidden, rows.Err()
}

// channelListOrder は一覧（ページングあり）の並び順。
//
// 既定は名前の五十音順。"organization" 指定で事務所順（名前を第2キー）。
// 事務所は表示名と並び順で並べる（key の文字列順ではない）。所属なしは最後。
//
// **非表示を含めるときは `is_hidden` を第 1 キーにする**（issue #65）。表示中を先に、
// 非表示を後ろに。ページングがあるので画面側で分けると 2 ページ目以降で区が割れる。
// 手元で 152 件中 151 件が非表示で、混ぜて並べると表示中の 1 件が埋もれていた。
// 事務所順を選んだときも先に 2 区へ割れる（区の中で事務所順）。
func channelListOrder(sort, dir string, includeHidden bool) string {
	order := nameSortOrderDir("s.name", "''", dir)
	if sort == "organization" {
		order = organizationGroupOrder(normDir(dir)) + ", " + nameSortOrder("s.name", "''")
	}
	if includeHidden {
		order = "s.is_hidden ASC, " + order
	}
	return order
}

// FindHiddenByName は非表示チャンネルを名前順で返す（事務所別表示の「非表示」区）。
//
// 事務所では組まない。非表示の区を事務所別にすると、畳んだ中がまた 18 段になる。
// **呼び出し側が権限を確かめること**（content:edit のときだけ呼ぶ）。
func (r *ChannelRepository) FindHiddenByName() ([]models.Channel, error) {
	rows, err := r.db.Query(`
		SELECT ` + channelColumns + `
		` + channelFrom + ` WHERE s.is_hidden
		ORDER BY ` + nameSortOrder("s.name", "''"))
	if err != nil {
		return nil, fmt.Errorf("query hidden singers: %w", err)
	}
	defer rows.Close()

	var channels []models.Channel
	for rows.Next() {
		s, err := scanChannel(rows)
		if err != nil {
			return nil, fmt.Errorf("scan hidden singer: %w", err)
		}
		channels = append(channels, s)
	}
	return channels, rows.Err()
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
//
// **表示中のチャンネルだけを返す**（issue #65）。非表示は事務所の組へ混ぜず、
// `FindHiddenByName` で別に引く ── 混ぜると事務所の組 19 個のうち 18 個が
// 中身が全部非表示になり、表示中のものを探すのがいちばん難しくなっていた。
func (r *ChannelRepository) FindAllGrouped() ([]models.Channel, error) {
	query := `
		SELECT ` + channelColumns + `
		` + channelFrom + hiddenClause(false, "WHERE") + `
		ORDER BY ` + organizationGroupOrder("ASC") + `,
			` + nameSortOrder("s.name", "''")
	// 所属なし扱いの組は複数の key（NULL と Independents など）が混ざるが、
	// 上の並びで最後にまとまるので、service 側で 1 つの組に束ねられる。

	rows, err := r.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("query singers grouped: %w", err)
	}
	defer rows.Close()

	var channels []models.Channel
	for rows.Next() {
		s, err := scanChannel(rows)
		if err != nil {
			return nil, fmt.Errorf("scan singer: %w", err)
		}
		channels = append(channels, s)
	}

	return channels, nil
}

// SetHidden はチャンネル一覧での表示/非表示を切り替える。
// メタデータの更新経路（Update / UpdateManualMetadata）と分けているのは、
// Holodex 管理チャンネルでもこのフラグだけは切り替えられる必要があるため。
// 戻り値は対象が存在したか。
func (r *ChannelRepository) SetHidden(id string, hidden bool) (bool, error) {
	res, err := r.db.Exec(
		"UPDATE channels SET is_hidden = $2, updated_at = NOW() WHERE id = $1", id, hidden)
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
func (r *ChannelRepository) SetMembersOnlyPolicy(id, policy string) (bool, error) {
	var val sql.NullString
	if policy != "" {
		val = sql.NullString{String: policy, Valid: true}
	}
	res, err := r.db.Exec(`UPDATE channels SET members_only_policy = $2, updated_at = NOW() WHERE id = $1`, id, val)
	if err != nil {
		return false, fmt.Errorf("set members only policy: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("set members only policy rows: %w", err)
	}
	return n > 0, nil
}

// CountMembersOnlyByOwner は所有者ごとの会限配信の本数を返す（0 本のチャンネルは含まない）。
// onlyIDs を渡すとそのチャンネルだけを数える（詳細ページ用。1 件のために全件を
// 集計しないため）。空なら全チャンネル。
//
// **一覧の SQL に相関サブクエリを足さない。** 1 回のクエリで全チャンネル分をまとめて引き、
// 呼び出し側で突き合わせる ── 148 チャンネルに対して N+1 を作らないため。
func (r *ChannelRepository) CountMembersOnlyByOwner(onlyIDs ...string) (map[string]int, error) {
	where := "ss.is_owner AND " + MembersOnlyDetectedExpr("s")
	args := []any{}
	if len(onlyIDs) > 0 {
		where += " AND ss.channel_id = ANY($1)"
		args = append(args, pq.Array(onlyIDs))
	}

	rows, err := r.db.Query(`
		SELECT ss.channel_id, COUNT(*)
		FROM stream_channels ss
		JOIN streams s ON s.id = ss.stream_id
		WHERE `+where+`
		GROUP BY ss.channel_id`, args...)
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

// SetAutoFill は自動処理の対象かを切り替える。戻り値は対象が存在したか。
func (r *ChannelRepository) SetAutoFill(id string, enabled bool) (bool, error) {
	res, err := r.db.Exec(
		"UPDATE channels SET auto_fill_enabled = $2, updated_at = NOW() WHERE id = $1", id, enabled)
	if err != nil {
		return false, fmt.Errorf("set auto fill: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("set auto fill rows: %w", err)
	}
	return n > 0, nil
}

// FindAutoFillTargets は自動処理に登録されたチャンネルを返す。
// **読むのは今のところ一覧 API だけ**（定期実行器は issue #35 の ③）。
//
// **非表示チャンネルも含める。** `is_hidden` は一覧に載せるかどうかの旗で、
// 処理してよいかとは別の軸（CLAUDE.md §2）。隠してあるチャンネルの歌単を
// 作りたい、という組み合わせは普通にある。
func (r *ChannelRepository) FindAutoFillTargets() ([]models.Channel, error) {
	rows, err := r.db.Query(`
		SELECT ` + channelColumns + `
		` + channelFrom + `
		WHERE s.auto_fill_enabled
		ORDER BY ` + nameSortOrder("s.name", "''"))
	if err != nil {
		return nil, fmt.Errorf("query auto fill targets: %w", err)
	}
	defer rows.Close()

	var channels []models.Channel
	for rows.Next() {
		sg, err := scanChannel(rows)
		if err != nil {
			return nil, fmt.Errorf("scan auto fill target: %w", err)
		}
		channels = append(channels, sg)
	}
	return channels, rows.Err()
}

// SetOrganizationOverride は Holodex の分類を手動で上書きする（空文字なら上書きを解除）。
//
// Holodex の値（organization）は触らない。同期は今後もそちらを更新し続けるので、
// 上書きを外せば最新の Holodex 分類に戻る。メタデータの更新経路と分けているのは、
// これが Holodex のメタデータではなく seTORI 側の判断であり、
// Holodex 管理チャンネルでも設定できる必要があるため。
// 戻り値は対象が存在したか。
func (r *ChannelRepository) SetOrganizationOverride(id, org string) (bool, error) {
	override := sql.NullString{String: org, Valid: strings.TrimSpace(org) != ""}
	if err := r.ensureOrganization(override); err != nil {
		return false, err
	}

	res, err := r.db.Exec(
		"UPDATE channels SET organization_override = $2, updated_at = NOW() WHERE id = $1",
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
func (r *ChannelRepository) FindByID(id string) (*models.Channel, error) {
	query := `
		SELECT ` + channelColumns + `
		` + channelFrom + ` WHERE s.id = $1`

	s, err := scanChannel(r.db.QueryRow(query, id))
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
// channels.organization は organizations への FK なので、Holodex が今まで見たことのない
// org を返した瞬間に取り込みが FK 違反で落ちる。それを避けるため、書き込み経路の入口で
// 必ず通す。表示名は key と同じもので作っておき、あとから管理画面で直す
// （「知らない事務所だから取り込まない」は選ばない ─ 登録は止めず、人の確認は後で受ける）。
//
// 呼び出し側で忘れると本番で初めて落ちる種類の不具合なので、
// service ではなくこのリポジトリの書き込みメソッド側に置いてある。
func (r *ChannelRepository) ensureOrganization(org sql.NullString) error {
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
// ChannelOrigin を渡すほうが正しい。
func (r *ChannelRepository) Create(s *models.Channel) error {
	if err := r.ensureOrganization(s.Organization); err != nil {
		return err
	}
	query := `
		INSERT INTO channels (id, name, english_name, photo_url, organization, metadata_source)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING created_at, updated_at`

	source := normalizeChannelMetadataSource(s.MetadataSource)
	err := r.db.QueryRow(query, s.ID, s.Name, s.EnglishName, s.PhotoURL, s.Organization, source).
		Scan(&s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create singer: %w", err)
	}
	s.MetadataSource = source
	return nil
}

// Update は歌手を更新する。
func (r *ChannelRepository) Update(s *models.Channel) error {
	if err := r.ensureOrganization(s.Organization); err != nil {
		return err
	}
	query := `
		UPDATE channels
		SET name = $2, english_name = $3, photo_url = $4, organization = $5, metadata_source = $6, updated_at = NOW()
		WHERE id = $1
		RETURNING updated_at`

	source := normalizeChannelMetadataSource(s.MetadataSource)
	err := r.db.QueryRow(query, s.ID, s.Name, s.EnglishName, s.PhotoURL, s.Organization, source).
		Scan(&s.UpdatedAt)
	if err != nil {
		return fmt.Errorf("update singer: %w", err)
	}
	s.MetadataSource = source
	return nil
}

// ChannelOrigin は Upsert が**行を新規作成するとき**の既定の可視性を決める。
// 既存行には効かない（後述）。
//
// 同期は「人が名指ししたチャンネル」と「その副産物として流れ込んだチャンネル」の
// 両方を作る。後者はコラボ相手・mention 先で、一覧に出したいものではない。
// 実際、既定が false だった頃の本番は 148 件中 147 件を手で隠していた＝既定が逆だった。
//
// bool ではなく型にしてあるのは、呼び出し側で `Upsert(singer, true)` と書かれても
// どちらの意味か読めないため。引数を必須にしているのは、
// **新しい呼び出し元が origin を決めずにはコンパイルできないようにする**ため。
type ChannelOrigin int

const (
	// ChannelRequested … 人がそのチャンネルを名指しで追加・同期した。既定で一覧に出す。
	ChannelRequested ChannelOrigin = iota
	// ChannelDiscovered … 配信の同期に付随して見つかった（所有者・mention）。既定で非表示。
	// 編集者は一覧の include_hidden で見つけられる。
	ChannelDiscovered
)

func (o ChannelOrigin) hiddenOnInsert() bool { return o == ChannelDiscovered }

// Upsert は歌手を作成または更新する（Holodex 同期用）。
//
// **is_hidden を書くのは INSERT のときだけで、既存行では意図的に触らない。**
// 同期は繰り返し走るので、conflict 側で書き戻すと手動で非表示にしたチャンネルが
// 次の同期で一覧に戻ってしまう。ON CONFLICT の SET に is_hidden を足さないこと。
func (r *ChannelRepository) Upsert(s *models.Channel, origin ChannelOrigin) error {
	if err := r.ensureOrganization(s.Organization); err != nil {
		return err
	}
	query := `
		INSERT INTO channels (id, name, english_name, photo_url, organization, metadata_source, is_hidden)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (id) DO UPDATE SET
			name = EXCLUDED.name,
			english_name = EXCLUDED.english_name,
			photo_url = EXCLUDED.photo_url,
			organization = EXCLUDED.organization,
			metadata_source = EXCLUDED.metadata_source,
			updated_at = NOW()
		RETURNING created_at, updated_at, is_hidden`

	source := normalizeChannelMetadataSource(s.MetadataSource)
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
func (r *ChannelRepository) UpdateManualMetadata(s *models.Channel) error {
	query := `
		UPDATE channels
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
func (r *ChannelRepository) Delete(id string) error {
	_, err := r.db.Exec("DELETE FROM channels WHERE id = $1", id)
	if err != nil {
		return fmt.Errorf("delete singer: %w", err)
	}
	return nil
}

// GetStreamCount は歌手が参加した配信数を取得する（非表示でない配信だけを集計）。
func (r *ChannelRepository) GetStreamCount(channelID string) (int, error) {
	var count int
	err := r.db.QueryRow(`
		SELECT COUNT(DISTINCT ss.stream_id)
		FROM stream_channels ss
		JOIN streams st ON ss.stream_id = st.id
		WHERE ss.channel_id = $1 AND st.is_hidden = FALSE
	`, channelID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count streams: %w", err)
	}
	return count, nil
}

// GetPerformanceCount は歌手の歌唱数を取得する（非表示・秘匿でない配信だけを集計）。
// **件数も秘匿の対象**（一覧から落としても件数が合わなければ存在が漏れる）。
func (r *ChannelRepository) GetPerformanceCount(channelID string, access ViewerAccess) (int, error) {
	var count int
	err := r.db.QueryRow(`
		SELECT COUNT(*)
		FROM performance_singers ps
		JOIN performances p ON ps.performance_id = p.id
		JOIN streams st ON p.stream_id = st.id
		WHERE ps.singer_id = $1 AND `+DiscoverableFor("st", access)+`
	`, channelID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count performances: %w", err)
	}
	return count, nil
}

// Search は歌手を検索する。
// 非表示チャンネルも返す：名前で探すのは「そのチャンネルを見に行く」意図の操作で、
// 詳細ページ自体は非表示でも開けるため、ここで隠すと辿り着く手段だけを塞ぐことになる。
func (r *ChannelRepository) Search(query string, limit int) ([]models.Channel, error) {
	sqlQuery := `
		SELECT ` + channelColumns + `
		` + channelFrom + `
		WHERE s.name ILIKE $1 OR s.english_name ILIKE $1
		ORDER BY s.name ASC
		LIMIT $2`

	searchPattern := "%" + query + "%"
	rows, err := r.db.Query(sqlQuery, searchPattern, limit)
	if err != nil {
		return nil, fmt.Errorf("search singers: %w", err)
	}
	defer rows.Close()

	var channels []models.Channel
	for rows.Next() {
		s, err := scanChannel(rows)
		if err != nil {
			return nil, fmt.Errorf("scan singer: %w", err)
		}
		channels = append(channels, s)
	}

	return channels, nil
}

func normalizeChannelMetadataSource(source string) string {
	if source == "" {
		return "holodex"
	}
	return source
}
