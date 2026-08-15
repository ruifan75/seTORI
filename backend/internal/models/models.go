package models

import (
	"database/sql"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// Singer 歌手/VTuber
type Singer struct {
	ID          string         `json:"id"`           // YouTube Channel ID
	Name        string         `json:"name"`         // 表示名
	EnglishName sql.NullString `json:"english_name"` // 英語名
	PhotoURL    sql.NullString `json:"photo_url"`    // アバター URL
	// 事務所は「Holodex の意見」と「こちらの判断」を分けて持つ。表示に使うのは
	// OrganizationOverride を優先した実効値（EffectiveOrganization）。
	Organization         sql.NullString `json:"organization"`              // Holodex が返した所属（同期のたびに更新される）
	OrganizationOverride sql.NullString `json:"organization_override"`     // 手動指定。NULL なら Organization を使う
	OrganizationName     sql.NullString `json:"organization_name"`         // 実効値の表示名（organizations.display_name）
	OrganizationUnaffil  bool           `json:"organization_unaffiliated"` // 実効値が「所属なし」を意味する分類か
	MetadataSource       string         `json:"metadata_source"`           // holodex / youtube
	IsHidden             bool           `json:"is_hidden"`                 // チャンネル一覧から外す（詳細ページは閲覧可）
	CreatedAt            time.Time      `json:"created_at"`
	UpdatedAt            time.Time      `json:"updated_at"`
}

// Organization 事務所。
// Key は取り込み時の生の値（Holodex の org）で、画面に出すのは DisplayName。
// 分けてあるので、表示を直しても取り込み時の値は壊れない。
type Organization struct {
	Key         string `json:"key"`
	DisplayName string `json:"display_name"`
	SortOrder   int    `json:"sort_order"`
	// IsUnaffiliated は「所属なし」を意味する分類（Holodex の Independents など）。
	// 一覧では所属が無いチャンネルと同じ組にまとめ、バッジも出さない。
	IsUnaffiliated bool      `json:"is_unaffiliated"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// EffectiveOrganization は表示・グループ分けに使う事務所キーを返す。
// 手動指定があればそれを、無ければ Holodex の値を使う。
func (s Singer) EffectiveOrganization() sql.NullString {
	if s.OrganizationOverride.Valid && s.OrganizationOverride.String != "" {
		return s.OrganizationOverride
	}
	return s.Organization
}

// Song 楽曲 Master
type Song struct {
	ID                    uuid.UUID      `json:"id"`
	Name                  string         `json:"name"`                    // 楽曲名
	NameReading           sql.NullString `json:"name_reading"`            // 読み（平仮名）
	OriginalArtist        string         `json:"original_artist"`         // 原曲アーティスト
	OriginalArtistReading sql.NullString `json:"original_artist_reading"` // 原曲アーティストの読み
	Arts                  sql.NullString `json:"arts"`                    // ジャケット画像 URL
	CreatedAt             time.Time      `json:"created_at"`
	UpdatedAt             time.Time      `json:"updated_at"`
}

// Artist 原曲アーティスト（正規化テーブル）。
// songs.original_artist（表示テキスト）と name で対応し、読みはここで一元管理する。
type Artist struct {
	ID          uuid.UUID      `json:"id"`
	Name        string         `json:"name"`
	NameReading sql.NullString `json:"name_reading"`
	// Aliases は同一人物の別名義（松任谷由実 に対する 荒井由実）。
	// 照合時に各名前を本体へ寄せるために使う（docs/SONG_MATCHING.md）。
	Aliases   []string  `json:"aliases,omitempty"`
	SongCount int       `json:"song_count"` // JOIN で補完
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ArtistReference は楽曲・歌唱 API に埋め込む安定したアーティスト参照。
type ArtistReference struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// SongITunes は楽曲の iTunes ID。
type SongITunes struct {
	ID             uuid.UUID      `json:"id"`
	SongID         uuid.UUID      `json:"song_id"`
	ITunesID       int64          `json:"itunes_id"`
	CollectionName sql.NullString `json:"collection_name"` // アルバム名
	Country        sql.NullString `json:"country"`         // 国コード
	IsPrimary      bool           `json:"is_primary"`
	CreatedAt      time.Time      `json:"created_at"`
}

// Stream は歌枠の配信。
type Stream struct {
	ID              string         `json:"id"` // YouTube Video ID
	Title           string         `json:"title"`
	StreamDate      time.Time      `json:"stream_date"`
	DurationSeconds sql.NullInt32  `json:"duration_seconds"`
	ThumbnailURL    sql.NullString `json:"thumbnail_url"`
	HolodexData     []byte         `json:"holodex_data"` // JSONB - Holodex songs data
	HolodexHash     sql.NullString `json:"holodex_hash"`
	CommentRaw      []byte         `json:"comment_raw"` // JSONB - Raw comment list
	// CommentSongsAnalyzedAt は解析（抽出＋正規化）を最後に走らせた時刻。
	// FindByID でのみ読む（一覧では使わないので他の SELECT には含めていない）。
	CommentSongsAnalyzedAt sql.NullTime
	CommentSongs           []byte `json:"comment_songs"` // JSONB - Parsed songs (undeduped)
	// チャプター経路（3 つ目の入力元）。FindByID でのみ読む。
	// ChapterRaw が NULL は「まだ調べていない」、[] は「調べたが章節が無い」で意味が違う。
	ChapterRaw   []byte    `json:"chapter_raw"`   // JSONB - yt-dlp が返した章節
	ChapterSongs []byte    `json:"chapter_songs"` // JSONB - 章節から抽出した楽曲
	IsProcessed  bool      `json:"is_processed"`  // 処理済み
	IsHidden               bool      `json:"is_hidden"`     // 初回登録時に判定し、その後は手動編集のみ
	CreatedAt              time.Time `json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
}

// StreamSearchFilters 配信検索で組み合わせられる条件。
// StreamTagIDs / PerformanceTagIDs は、それぞれ指定した全タグを持つ配信に絞り込む。
type StreamSearchFilters struct {
	Query             string
	OwnerID           string
	ParticipantIDs    []string
	VocalistIDs       []string
	StreamTagIDs      []string
	PerformanceTagIDs []string
}

// Performance は歌唱記録。
type Performance struct {
	ID            uuid.UUID      `json:"id"`
	StreamID      string         `json:"stream_id"`
	SongID        uuid.UUID      `json:"song_id"`
	StartSeconds  int            `json:"start_seconds"`
	EndSeconds    int            `json:"end_seconds"`
	OrderIndex    int            `json:"order_index"`
	HolodexSongID uuid.NullUUID  `json:"holodex_song_id"`
	CustomTags    pq.StringArray `json:"custom_tags"`
	CreatedAt     time.Time      `json:"created_at"`

	// 終了時間の由来と確認状態（詳細は docs/DATA_COMPLETION.md）。
	// 2 つは直交する：拍手検出が出した値を人が見て承認すれば
	// EndSource="chat" かつ EndConfirmed=true になる。
	EndSource    string `json:"end_source"`
	EndConfirmed bool   `json:"end_confirmed"`
}

// PerformanceTag は歌唱バージョンのタグ。
type PerformanceTag struct {
	ID          string    `json:"id"` // タグ ID
	DisplayName string    `json:"display_name"`
	Color       string    `json:"color"`
	CreatedAt   time.Time `json:"created_at"`
}

// StreamTag は配信種別のタグ。
type StreamTag struct {
	ID          string    `json:"id"` // タグ ID
	DisplayName string    `json:"display_name"`
	Color       string    `json:"color"`
	CreatedAt   time.Time `json:"created_at"`
}

// FilterKeyword フィルターキーワード
type FilterKeyword struct {
	ID        int       `json:"id"`
	Keyword   string    `json:"keyword"`
	Type      string    `json:"type"` // "filter" or "keep"
	CreatedAt time.Time `json:"created_at"`
}

// TagKeywordRule タイトルの文字列マッチで stream_tag を自動付与するルール
type TagKeywordRule struct {
	ID        int       `json:"id"`
	TagID     string    `json:"tag_id"`
	Keyword   string    `json:"keyword"`
	CreatedAt time.Time `json:"created_at"`
}

// AIProvider は AI プロバイダーの設定（OpenAI 互換エンドポイント）。
type AIProvider struct {
	ID             int       `json:"id"`
	Name           string    `json:"name"`
	BaseURL        string    `json:"base_url"`
	Model          string    `json:"model"`
	APIKey         string    `json:"-"` // API key は出力しない
	Enabled        bool      `json:"enabled"`
	Priority       int       `json:"priority"`
	TimeoutSeconds int       `json:"timeout_seconds"` // AI 呼び出し 1 回のタイムアウト秒数（プロバイダーごとに指定可能）
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// Role 権限セット（RBAC）。permissions は権限キーの配列で '*' は全権限。
type Role struct {
	ID          uuid.UUID      `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Permissions pq.StringArray `json:"permissions"`
	IsSystem    bool           `json:"is_system"` // 組み込みロール（削除不可）
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

// User は利用者。
type User struct {
	ID            uuid.UUID  `json:"id"`
	Username      string     `json:"username"`
	DisplayName   string     `json:"display_name"`
	Email         *string    `json:"email"` // 自助登録・外部連携で設定。管理者が作った旧アカウントは null
	EmailVerified bool       `json:"email_verified"`
	PasswordHash  string     `json:"-"` // パスワードハッシュは出力しない。外部アカウントのみの利用者は空
	RoleID        uuid.UUID  `json:"role_id"`
	RoleName      string     `json:"role"`        // roles.name（表示用、JOIN で補完）
	Permissions   []string   `json:"permissions"` // role の permissions（認証時に補完）
	IsActive      bool       `json:"is_active"`
	LastLogin     *time.Time `json:"last_login"` // 未ログインなら null
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// OAuthIdentity 外部アカウント（Google / X / Discord …）との紐付け。
// provider を値で持つので、対応を増やしてもスキーマは変わらない。
type OAuthIdentity struct {
	ID             uuid.UUID `json:"id"`
	UserID         uuid.UUID `json:"user_id"`
	Provider       string    `json:"provider"`
	ProviderUserID string    `json:"provider_user_id"`
	Email          *string   `json:"email"`
	DisplayName    *string   `json:"display_name"`
	AvatarURL      *string   `json:"avatar_url"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// Playlist 利用者のプレイリスト。
// visibility: private（本人のみ）/ unlisted（share_slug を知る人のみ）/ public（公開）
type Playlist struct {
	ID          uuid.UUID `json:"id"`
	UserID      uuid.UUID `json:"user_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Visibility  string    `json:"visibility"`
	ShareSlug   string    `json:"share_slug"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// PlaylistItem プレイリストの1項目＝1歌唱記録（performances）。
type PlaylistItem struct {
	ID            uuid.UUID `json:"id"`
	PlaylistID    uuid.UUID `json:"playlist_id"`
	PerformanceID uuid.UUID `json:"performance_id"`
	Position      int       `json:"position"`
	AddedAt       time.Time `json:"added_at"`
}

// プレイリストの公開範囲
const (
	PlaylistPrivate  = "private"
	PlaylistUnlisted = "unlisted"
	PlaylistPublic   = "public"
)

// EditSuggestion 閲覧モードからの修正提案。管理者が承認/却下する。
type EditSuggestion struct {
	ID         uuid.UUID `json:"id"`
	TargetType string    `json:"target_type"` // song / artist / performance / stream
	TargetID   uuid.UUID `json:"target_id"`
	// TargetKey は UUID で表せない対象の識別子（配信の YouTube 動画 ID）。UUID 対象では空。
	// 対象の同一性は (TargetType, TargetID, TargetKey) で判断する。
	TargetKey   string `json:"target_key"`
	TargetLabel string `json:"target_label"`
	Kind        string `json:"kind"` // field（フィールドの差し替え）/ perf.missing（未登録曲の追加）
	BeforeData  []byte `json:"-"`    // JSONB（生バイト、ハンドラで json.RawMessage として出力）
	AfterData   []byte `json:"-"`
	Payload     []byte `json:"-"` // kind 固有の追加情報（field では未使用）
	Note        string `json:"note"`
	Status      string `json:"status"` // pending / approved / rejected / conflict

	// 提案者。匿名投稿を許すため NULL 可。CreatedByName は削除後も残る表示名スナップショット。
	CreatedBy     *uuid.UUID `json:"created_by"`
	CreatedByName string     `json:"created_by_name"`
	ClientHint    string     `json:"-"` // 匿名提案の同一性の手がかり（IP ハッシュ）。外部へは出さない。

	ReviewedBy *uuid.UUID `json:"reviewed_by"`
	ReviewNote string     `json:"review_note"`
	CreatedAt  time.Time  `json:"created_at"`
	ReviewedAt *time.Time `json:"reviewed_at"`
}

// Session Bearer トークンのセッション。DB には token の SHA-256 ハッシュのみ保存する。
type Session struct {
	TokenHash string    `json:"-"`
	UserID    uuid.UUID `json:"user_id"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}
