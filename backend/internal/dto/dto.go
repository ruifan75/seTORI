package dto

import (
	"time"

	"github.com/google/uuid"
)

// ========== ページング ==========

type PaginationRequest struct {
	Page  int `json:"page"`
	Limit int `json:"limit"`
}

type PaginationResponse struct {
	Page       int `json:"page"`
	Limit      int `json:"limit"`
	Total      int `json:"total"`
	TotalPages int `json:"total_pages"`
}

// ========== 楽曲 ==========

type SongItunesResponse struct {
	ItunesID       int64   `json:"itunes_id"`
	CollectionName *string `json:"collection_name,omitempty"`
	Country        *string `json:"country,omitempty"`
	IsPrimary      bool    `json:"is_primary"`
}

type ArtistReference struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

type SongResponse struct {
	ID                    uuid.UUID            `json:"id"`
	Name                  string               `json:"name"`
	NameReading           *string              `json:"name_reading,omitempty"`
	OriginalArtist        string               `json:"original_artist"`
	OriginalArtistReading *string              `json:"original_artist_reading,omitempty"`
	Artists               []ArtistReference    `json:"artists"`
	Arts                  *string              `json:"arts,omitempty"`
	PerformanceCount      int                  `json:"performance_count"`
	ItunesIDs             []SongItunesResponse `json:"itunes_ids,omitempty"`
	CreatedAt             time.Time            `json:"created_at"`
	UpdatedAt             time.Time            `json:"updated_at"`
}

type SongListResponse struct {
	Songs      []SongResponse     `json:"songs"`
	Pagination PaginationResponse `json:"pagination"`
}

type CreateSongRequest struct {
	Name                  string       `json:"name"`
	NameReading           *string      `json:"name_reading,omitempty"`
	OriginalArtist        string       `json:"original_artist"`
	OriginalArtistReading *string      `json:"original_artist_reading,omitempty"`
	Arts                  *string      `json:"arts,omitempty"`
	ItunesIds             []ItunesItem `json:"itunes_ids,omitempty"`
}

type ItunesItem struct {
	ItunesID  int64 `json:"itunes_id"`
	IsPrimary bool  `json:"is_primary"`
}

type UpdateSongRequest struct {
	Name                  string       `json:"name"`
	NameReading           *string      `json:"name_reading,omitempty"`
	OriginalArtist        string       `json:"original_artist"`
	OriginalArtistReading *string      `json:"original_artist_reading,omitempty"`
	Arts                  *string      `json:"arts,omitempty"`
	ItunesIds             []ItunesItem `json:"itunes_ids,omitempty"`
}

// ========== 歌手 ==========

type SingerResponse struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	EnglishName *string `json:"english_name,omitempty"`
	PhotoURL    *string `json:"photo_url,omitempty"`
	// Organization は実効値の organizations.key（手動指定があればそれ、無ければ Holodex の値）。
	// 画面に出すのは OrganizationName で、「所属なし」を意味する分類のときは空になる。
	Organization     *string `json:"organization,omitempty"`
	OrganizationName *string `json:"organization_name,omitempty"`
	// 以下は編集画面用。手動指定の有無と、Holodex が何と言っているかを見せるため。
	OrganizationOverride *string `json:"organization_override,omitempty"`
	OrganizationHolodex  *string `json:"organization_holodex,omitempty"`
	MetadataSource       string  `json:"metadata_source"`
	CanEditMetadata      bool    `json:"can_edit_metadata"`
	IsHidden             bool    `json:"is_hidden"`
	// MembersOnlyPolicy は会限セットリストの公開可否（未設定なら省略＝未確認）。
	// 「訊いていない」「断られた」は運用の情報なので、**編集画面のためのもの**。
	MembersOnlyPolicy *string `json:"members_only_policy,omitempty"`
	// MembersOnlyStreamCount は所有する会限配信の本数（0 なら省略）。
	// **方針を出す画面には要る。** 0 本のチャンネルに「会限の公開可否」を訊いても
	// 意味が無く、148 件すべてに「未確認」を出すと本当に訊くべき 2 件が埋もれる。
	// 方針が allow でも本数は数える（「会限かどうか」は事実で、公開可否とは別）。
	MembersOnlyStreamCount int       `json:"members_only_stream_count,omitempty"`
	CreatedAt              time.Time `json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
}

type SingerListResponse struct {
	Singers    []SingerResponse   `json:"singers"`
	Pagination PaginationResponse `json:"pagination"`
}

// SingerGroupResponse は事務所ごとにまとめたチャンネル。
// Organization が空文字の組は「所属なし（個人勢）」を表し、一覧では最後に置く。
// DisplayName は見出しに出す名前（所属なしの組では空）。
type SingerGroupResponse struct {
	Organization string           `json:"organization"`
	DisplayName  string           `json:"display_name"`
	Singers      []SingerResponse `json:"singers"`
}

// ========== 事務所 ==========

type OrganizationResponse struct {
	Key            string    `json:"key"`
	DisplayName    string    `json:"display_name"`
	SortOrder      int       `json:"sort_order"`
	IsUnaffiliated bool      `json:"is_unaffiliated"`
	SingerCount    int       `json:"singer_count"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type OrganizationListResponse struct {
	Organizations []OrganizationResponse `json:"organizations"`
}

// CreateOrganizationRequest は Holodex に無い事務所を手で足すときのもの。
// Key を省略すると DisplayName をそのまま Key にする。
type CreateOrganizationRequest struct {
	Key            string `json:"key"`
	DisplayName    string `json:"display_name"`
	SortOrder      int    `json:"sort_order"`
	IsUnaffiliated bool   `json:"is_unaffiliated"`
}

// UpdateOrganizationRequest は表示名と並び順のみ。
// key は取り込み時の値なので変更できない（変えると Holodex からの取り込みと結びつかなくなる）。
type UpdateOrganizationRequest struct {
	DisplayName    string `json:"display_name"`
	SortOrder      int    `json:"sort_order"`
	IsUnaffiliated bool   `json:"is_unaffiliated"`
}

// UpdateSingerOrganizationRequest は Holodex の分類の手動上書き。
// 空文字（または省略）で上書きを解除し、Holodex の値に戻す。
// メタデータ更新（UpdateSingerRequest）と分けているのは、これが Holodex の
// メタデータではなく seTORI 側の判断で、Holodex 管理チャンネルでも設定できる必要があるため。
type UpdateSingerOrganizationRequest struct {
	Organization string `json:"organization"`
}

// SingerGroupListResponse は事務所別のチャンネル一覧。
// グループを跨いだページ送りは意味を成さないので、ページングせず全件返す。
type SingerGroupListResponse struct {
	Groups []SingerGroupResponse `json:"groups"`
	Total  int                   `json:"total"`
}

// UpdateSingerVisibilityRequest はチャンネルの非表示切り替え。
// 名前などのメタデータ更新（UpdateSingerRequest）と分けているのは、
// メタデータは Holodex 管理チャンネルでは編集できない一方、
// 非表示は seTORI 側の都合なのでどのチャンネルでも切り替えられるため。
// UpdateSingerMembersPolicyRequest は会限セットリストの公開可否。
//
// **ポインタにするのは「フィールドが無い」と「明示的な空文字」を分けるため。**
// 値型だと `{}` や項目名の typo が decode に成功し、既存の allow / deny を
// 黙って未確認へ戻してしまう（deny では「訊いて断られた」という記録が消える）。
type UpdateSingerMembersPolicyRequest struct {
	Policy *string `json:"members_only_policy"`
}

type UpdateSingerVisibilityRequest struct {
	IsHidden bool `json:"is_hidden"`
}

type SingerDetailResponse struct {
	SingerResponse
	StreamCount      int `json:"stream_count"`
	PerformanceCount int `json:"performance_count"`
}

type SingerPerformanceListResponse struct {
	Singer       SingerResponse            `json:"singer"`
	Performances []SongPerformanceResponse `json:"performances"`
	Pagination   PaginationResponse        `json:"pagination"`
}

type CreateSingerRequest struct {
	ID           string  `json:"id"` // YouTube Channel ID / @handle / URL
	Name         string  `json:"name,omitempty"`
	EnglishName  *string `json:"english_name,omitempty"`
	PhotoURL     *string `json:"photo_url,omitempty"`
	Organization *string `json:"organization,omitempty"`
}

type CreateSingerResponse struct {
	Message string `json:"message"`
	ID      string `json:"id"`
	Name    string `json:"name"`
}

// UpdateSingerRequest は Holodex 管理でないチャンネルのメタデータ更新。
// 事務所は含まない（UpdateSingerOrganizationRequest が唯一の窓口）。
type UpdateSingerRequest struct {
	Name        string  `json:"name"`
	EnglishName *string `json:"english_name,omitempty"`
	PhotoURL    *string `json:"photo_url,omitempty"`
}

// ========== 歌枠 ==========

type StreamTagResponse struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Color       string `json:"color"`
}

type StreamResponse struct {
	ID              string              `json:"id"`
	Title           string              `json:"title"`
	StreamDate      string              `json:"stream_date"`
	DurationSeconds *int32              `json:"duration_seconds,omitempty"`
	ThumbnailURL    *string             `json:"thumbnail_url,omitempty"`
	Tags            []StreamTagResponse `json:"tags"`
	Participants    []SingerResponse    `json:"participants"`
	ChannelOwner    *SingerResponse     `json:"channel_owner,omitempty"` // チャンネル所有者
	// IsProcessed は**運用の状態**（セットリストを作り終えたか）。
	// `content:edit` のときだけ載せる ── 閲覧者には意味が無く、
	// 「まだ手を付けていない配信」の一覧を外から作れてしまう。
	// nil＝載せない（omitempty で応答から消える）。
	IsProcessed *bool `json:"is_processed,omitempty"`
	IsHidden    bool  `json:"is_hidden"`
	// IsRestricted は中身を公開してよいか未確認の配信。**閲覧者にも返す**
	// （画面で「セットリストは未公開」と言うために要る）。旗そのものは秘密ではない。
	IsRestricted         bool             `json:"is_restricted"`
	HolodexTimelineSongs []SongSuggestion `json:"holodex_timeline_songs,omitempty"` // holodex_data から解析
	CommentTimelineSongs []CommentSong    `json:"comment_timeline_songs,omitempty"` // comment_songs から解析（分析済みキャッシュ）
	// HasCommentRaw は comment_raw に解析可能なコメントがあるか。
	// **非編集者には省略する**（nil）。false を返すと「非開示」ではなく「入力が無い」に
	// 見えてしまい、応答が実態と食い違う。
	HasCommentRaw        *bool         `json:"has_comment_raw,omitempty"`
	ChapterTimelineSongs []CommentSong `json:"chapter_timeline_songs,omitempty"` // chapter_songs から解析（分析済みキャッシュ）
	// ChapterCount は配信者が付けた章節の数。-1 は「まだ調べていない」で、0（＝調べたが
	// 章節が無い）とは別。同じ見た目にすると取得を試す導線が消える。
	// **非編集者には省略する**（nil）。0 は「調べたが章節が無い」を意味するので、
	// 伏せる代わりに 0 を返すと別の事実を主張することになる。
	ChapterCount *int `json:"chapter_count,omitempty"`
	// CommentSongsAnalyzedAt は解析を最後に走らせた時刻。updated_at では代用できない
	// （毎日回る Holodex 同期が全配信の updated_at を今日に押し上げる）。
	CommentSongsAnalyzedAt *string `json:"comment_songs_analyzed_at,omitempty"`
	// Playability は**閲覧者にも返す**（プレイヤーを描くかどうかの判断に要る）。
	// 会限の動画は YouTube が埋め込みを塞いでいるので、描いてから onError: 150 で
	// 失敗させるより最初から描かないほうがよい。値は dto.Playability* を参照。
	//
	// **詳細（GetByID）でだけ入る。** 一覧・検索の SELECT は元の 3 列を読んでいないので、
	// そこで組み立てると全部 unknown になり、実態と食い違う主張をすることになる。
	// 省略時は「この応答は判定していない」で、受け手は従来どおり描いてよい。
	Playability string `json:"playability,omitempty"`
	// HolodexUploadedAt は Holodex への**送信を試みた**時刻（PUT の前に記録するので
	// 「送信済み」ではない）。**編集者だけ**に返す。
	// 秘匿にしても向こうのコピーは残りうるので、そこに気付ける材料として要る。
	HolodexUploadedAt *string `json:"holodex_uploaded_at,omitempty"`
	// HolodexUploadUnknown は台帳の追跡開始より前から存在する配信。
	// 台帳が空でも「送っていない」とは言えないことを画面へ伝える。**編集者だけ**。
	HolodexUploadUnknown bool `json:"holodex_upload_unknown,omitempty"`
	// 生の値は編集者だけに返す。判断材料であって閲覧者が使うものではない。
	Availability          *string   `json:"availability,omitempty"`
	PlayableInEmbed       *bool     `json:"playable_in_embed,omitempty"`
	AvailabilityCheckedAt *string   `json:"availability_checked_at,omitempty"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

// Playability は「この配信を埋め込みプレイヤーで再生できるか」の判定結果。
//
// 生の availability / playable_in_embed から導く（両方を見る必要がある：
// **`--ignore-no-formats-error` が付いた実行では、視聴できない動画でも
// availability が public で返る**ので、availability 単独では信用できない）。
const (
	// PlayabilityUnknown … まだ調べていない。従来どおりプレイヤーを描く
	// （調べていないことを「再生不可」と読み替えると全配信のプレイヤーが消える）。
	PlayabilityUnknown = "unknown"
	// PlayabilityPlayable … 埋め込み再生できる。
	PlayabilityPlayable = "playable"
	// PlayabilityMembersOnly … 会限。メンバー資格があっても埋め込みでは再生できない。
	PlayabilityMembersOnly = "members_only"
	// PlayabilityEmbedDisabled … 公開だが所有者が埋め込みを切っている（onError: 150 の別の原因）。
	PlayabilityEmbedDisabled = "embed_disabled"
	// PlayabilityUnavailable … 動画情報を取得できない（削除・非公開・権利で降ろされた）。
	PlayabilityUnavailable = "unavailable"
)

type StreamListResponse struct {
	Streams    []StreamResponse   `json:"streams"`
	Pagination PaginationResponse `json:"pagination"`
}

type StreamDetailResponse struct {
	StreamResponse
	Performances []PerformanceResponse `json:"performances"`
}

type UpdateStreamRequest struct {
	Title          *string  `json:"title,omitempty"`
	StreamDate     *string  `json:"stream_date,omitempty"`
	TagIDs         []string `json:"tag_ids,omitempty"`
	ParticipantIDs []string `json:"participant_ids,omitempty"`
	IsProcessed    *bool    `json:"is_processed,omitempty"`
	IsHidden       *bool    `json:"is_hidden,omitempty"`
	// IsRestricted を false にするのが「中身を公開してよい」という人の判断。
	// 自動では下がらない（SaveAvailability は立てるだけ）。
	IsRestricted *bool `json:"is_restricted,omitempty"`
}

// ========== 歌唱 ==========

type PerformanceTagResponse struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Color       string `json:"color"`
}

type PerformanceResponse struct {
	ID             uuid.UUID                `json:"id"`
	StreamID       string                   `json:"stream_id"`
	SongID         uuid.UUID                `json:"song_id"`
	SongName       string                   `json:"song_name"`
	OriginalArtist string                   `json:"original_artist"`
	Artists        []ArtistReference        `json:"artists"`
	Arts           *string                  `json:"arts,omitempty"`
	StartSeconds   int                      `json:"start_seconds"`
	EndSeconds     int                      `json:"end_seconds"`
	OrderIndex     int                      `json:"order_index"`
	Tags           []PerformanceTagResponse `json:"tags"`
	CustomTags     []string                 `json:"custom_tags"`
	Singers        []SingerResponse         `json:"singers"`
	YouTubeURL     string                   `json:"youtube_url"`
	CreatedAt      time.Time                `json:"created_at"`
	// 終了時間の由来と確認状態（docs/DATA_COMPLETION.md）。
	// 編集画面はこれを読んで endSource を復元し、保存時にそのまま送り返す。
	EndSource    string `json:"end_source"`
	EndConfirmed bool   `json:"end_confirmed"`
	// この曲に紐付いている primary な iTunes ID。配信詳細（編集画面）でのみ設定される。
	// 編集カードが「紐付け済みか」を出すのに要る ── 無いと、既に紐付いている曲まで
	// 「iTunes なし」と表示してしまう
	ItunesID *int64 `json:"itunes_id,omitempty"`
	// タグ検索など配信横断の一覧でのみ設定される（配信詳細では省略）
	StreamTitle  string  `json:"stream_title,omitempty"`
	StreamDate   string  `json:"stream_date,omitempty"`
	ThumbnailURL *string `json:"thumbnail_url,omitempty"`
}

// 楽曲詳細ページで使う逆引き。
type SongPerformanceResponse struct {
	ID             uuid.UUID                `json:"id"`
	StreamID       string                   `json:"stream_id"`
	StreamTitle    string                   `json:"stream_title"`
	StreamDate     string                   `json:"stream_date"`
	ThumbnailURL   *string                  `json:"thumbnail_url,omitempty"`
	SongID         uuid.UUID                `json:"song_id,omitempty"`
	SongName       string                   `json:"song_name,omitempty"`
	OriginalArtist string                   `json:"original_artist,omitempty"`
	Artists        []ArtistReference        `json:"artists"`
	StartSeconds   int                      `json:"start_seconds"`
	EndSeconds     int                      `json:"end_seconds"`
	Tags           []PerformanceTagResponse `json:"tags"`
	CustomTags     []string                 `json:"custom_tags"`
	Singers        []SingerResponse         `json:"singers"`
	YouTubeURL     string                   `json:"youtube_url"`
	CreatedAt      time.Time                `json:"created_at"`
	// 終了時間の由来と確認状態（docs/DATA_COMPLETION.md）。
	// 編集画面はこれを読んで endSource を復元し、保存時にそのまま送り返す。
	EndSource    string `json:"end_source"`
	EndConfirmed bool   `json:"end_confirmed"`
}

type SongPerformanceListResponse struct {
	Song         SongResponse              `json:"song"`
	Performances []SongPerformanceResponse `json:"performances"`
	Pagination   PaginationResponse        `json:"pagination"`
}

// ========== Holodex 同期 ==========

type SyncHolodexRequest struct {
	ChannelID   string `json:"channel_id"`
	Limit       int    `json:"limit,omitempty"`        // 同期件数（既定 50）
	ForceUpdate bool   `json:"force_update,omitempty"` // すべてのデータを強制更新
}

type SyncHolodexResponse struct {
	SyncedCount  int      `json:"synced_count"`
	TotalStreams int      `json:"total_streams"` // 処理対象の配信総数
	Processed    int      `json:"processed"`     // 処理済み件数
	NewStreams   []string `json:"new_streams"`
	Updated      []string `json:"updated"`
	Skipped      []string `json:"skipped"`
	InProgress   bool     `json:"in_progress"`       // 処理中か
	Message      string   `json:"message,omitempty"` // 状態メッセージ
}

// ========== Comment 分析 ==========

type CommentSong struct {
	Start              int    `json:"start"`
	End                int    `json:"end"`
	Name               string `json:"name"`            // 抽出した曲名（原文どおり。正規化前）
	OriginalArtist     string `json:"original_artist"` // 抽出した歌手（原文どおり。正規化前）
	OriginalComment    string `json:"original_comment"`
	IsEndTimeEstimated bool   `json:"is_end_time_estimated"`

	// Chat の拍手検出関連（コメントに明記された終了時刻との比較用）
	ChatEnd int `json:"chat_end,omitempty"` // Chat が検出した終了秒数（ある場合）
	EndDiff int `json:"end_diff,omitempty"` // |end - chat_end|。両方に値がある場合のみ設定

	// ↓ 折り込んだ正規化結果（分析時に AI 正規化＋DB 照合まで実行し、ここに保存・再利用する）
	NormalizedName          string   `json:"normalized_name,omitempty"`
	NormalizedNameReading   string   `json:"normalized_name_reading,omitempty"`
	NormalizedArtist        string   `json:"normalized_artist,omitempty"`
	NormalizedArtistReading string   `json:"normalized_artist_reading,omitempty"`
	Tags                    []string `json:"tags,omitempty"`
	Confidence              float64  `json:"confidence,omitempty"`
	// DB 照合（マッチした既存曲）
	MatchedSongID            *string `json:"matched_song_id,omitempty"`
	MatchedSongName          *string `json:"matched_song_name,omitempty"`
	MatchedSongNameReading   *string `json:"matched_song_name_reading,omitempty"`
	MatchedSongArtist        *string `json:"matched_song_artist,omitempty"`
	MatchedSongArtistReading *string `json:"matched_song_artist_reading,omitempty"`
	MatchedSongArtURL        *string `json:"matched_song_art_url,omitempty"`
	MatchedSongItunesID      *int64  `json:"matched_song_itunes_id,omitempty"`
	// 自動採用に届かなかった照合候補（別名義・同名異曲など。UI で選ばせる）
	MatchCandidates []SongMatchCandidate `json:"match_candidates,omitempty"`
	// AI が照合した場合の申し送り（歌手が DB と違うとき）
	ArtistAlias *ArtistAliasProposal `json:"artist_alias,omitempty"`

	// Changes は「抽出したままの値が、どの処理でどう変わったか」の履歴。
	// 画面に「元は X、AI 正規化で Y、DB 照合で Z」と出すためのもの。
	//
	// 保存しない（照合は読み取り時に計算するので、保存しても古くなるだけ）。
	Changes []FieldChange `json:"changes,omitempty"`
}

// FieldChange は 1 つの処理でその欄がどう変わったか。
//
// 名前が勝手に変わって見えるのが一番困る ── 利用者は「自分が入れた覚えのない歌手名」を
// 見せられても、それが AI の正規化なのか DB 照合なのか判断できない。
// どちらの仕業かと、その根拠を添えて出せるようにしておく。
type FieldChange struct {
	Field  string  `json:"field"`            // name | artist
	By     string  `json:"by"`               // ai_normalize | db_match
	From   string  `json:"from"`             // 変わる前
	To     string  `json:"to"`               // 変わった後
	Reason string  `json:"reason,omitempty"` // db_match の根拠（exact / title_artist / ai …）
	Score  float64 `json:"score,omitempty"`  // db_match の確信度
}

// ArtistAliasProposal は「歌手名が DB と違うが、AI は同一人物だと言っている」ことの
// 申し送り。編集フォームでチェックボックスとして出し、**保存したときだけ**登録する。
//
// 作曲者と原曲歌手の取り違え（メルト / 初音ミク に対し DB は ryo (supercell)）は
// 「同じ曲だが別人」なので SameArtist=false になる。これを混同すると、
// その人の全楽曲に効く別名義が誤って作られる。
type ArtistAliasProposal struct {
	Canonical  string `json:"canonical"`   // DB 側の表記（別名義の本体になる）
	Alias      string `json:"alias"`       // コメント側の表記
	SameArtist bool   `json:"same_artist"` // AI の判定。true のときだけ既定でチェックが入る
}

// BatchFillStatus は一括セットリスト作成の進捗。
type BatchFillStatus struct {
	Running bool   `json:"running"`
	Mode    string `json:"mode,omitempty"`
	// SingerIDs は対象チャンネル（空なら全部）。IncludeCollabs が false なら
	// そのチャンネルが所有する配信だけが対象。
	SingerIDs      []string `json:"singer_ids,omitempty"`
	IncludeCollabs bool     `json:"include_collabs,omitempty"`
	RunID          string   `json:"run_id,omitempty"`
	Phase          string   `json:"phase,omitempty"` // scan / ai / write
	Total          int      `json:"total"`
	Done           int      `json:"done"`
	Current        string   `json:"current,omitempty"`
	Created        int      `json:"created"`
	Review         int      `json:"review"`
	// Gaps は「DB にあるが入力元に無い」歌唱の件数（force 実行のみ）。
	Gaps    int `json:"gaps"`
	AIAsked int `json:"ai_asked"`
	// Skipped は入力元を確定できず、この実行では扱わなかった配信の数。
	// Done に混ぜない ── 「扱った」と「扱えなかった」は運用上まったく違う意味を持つ。
	Skipped    int      `json:"skipped"`
	SkippedIDs []string `json:"skipped_ids,omitempty"`
	Message    string   `json:"message,omitempty"`
}

type AnalyzeCommentsResponse struct {
	Songs       []CommentSong `json:"songs"`
	RawComments []string      `json:"raw_comments"`
	// AI 正規化が失敗（全プロバイダー冷却等）し、抽出のみで返した場合に設定される。
	// バッチ分析はこれを見て冷却待ち後に force 再試行する。
	Warning string `json:"warning,omitempty"`
	// Deferred は「今回は結論を出さずに見送った」。live chat が取得できず、
	// **変換待ちか一時的な障害**と判断した回に立つ（chat_readiness.go）。
	// エラーではないので err は nil だが、**完了として数えてはいけない**
	// ── 数えると再試行されず、その配信の end は付かないまま残る。
	// Warning と違って**同じ実行での再試行は無意味**（変換は数時間かかる）。
	Deferred bool `json:"deferred,omitempty"`
	// 何が起きたかの内訳。応答だけで挙動を確かめられるようにするためのもので、
	// これが無いとサーバーのログを読まないと「どの経路を通ったか」が分からない。
	Stats *AnalyzeStats `json:"stats,omitempty"`
}

// AnalyzeStats は 1 回の解析で実際に何が起きたか。
type AnalyzeStats struct {
	// grouped（既定）/ two_stage / regex / cache / none のいずれか。
	// 経路によって後段の処理が変わるので、まずこれが分からないと何も判断できない。
	Path   string `json:"path"`
	DryRun bool   `json:"dry_run,omitempty"`
	Saved  bool   `json:"saved"` // comment_songs を書いたか

	Extracted      int `json:"extracted"`       // フィルタ・重複排除を通ったあとの曲数
	Matched        int `json:"matched"`         // 自動採用できた
	WithCandidates int `json:"with_candidates"` // 未照合だが候補あり（人が選べる）
	Unmatched      int `json:"unmatched"`       // 候補も無い

	// 別名義の AI 判定。asked は問い合わせた組、linked は同一人物と判定できた組。
	// 統合経路で判定が動いているかを応答から確かめられるようにしてある
	// （動いていない不具合を一度出しているため）。
	AliasPairsAsked int `json:"alias_pairs_asked"`
	AliasLinksAdded int `json:"alias_links_added"`
}

// BatchAnalyzeStatus 未処理配信の一括分析ジョブの進捗
type BatchAnalyzeStatus struct {
	Running  bool   `json:"running"`
	Mode     string `json:"mode,omitempty"`
	SingerID string `json:"singer_id,omitempty"` // 対象を絞ったチャンネル（空なら全チャンネル）
	Hidden   string `json:"hidden,omitempty"`    // 非表示配信の扱い（false/true/all）。画面に何が走っているか出すため
	Total    int    `json:"total"`
	Done     int    `json:"done"`
	Failed   int    `json:"failed"`
	// Deferred は live chat の取得待ちで見送った件数（失敗ではない）。
	// 次の実行で拾われるので、完了とも失敗とも分けて出す。
	Deferred  int      `json:"deferred"`
	Current   string   `json:"current,omitempty"`    // 処理中の配信タイトル
	FailedIDs []string `json:"failed_ids,omitempty"` // AI 失敗が解消しなかった配信
	Message   string   `json:"message,omitempty"`
}

// BatchAnalyzeRequest 一括分析の開始リクエスト
type BatchAnalyzeRequest struct {
	Mode     string `json:"mode"`      // unanalyzed / unprocessed / refresh / reanalyze
	SingerID string `json:"singer_id"` // 対象チャンネル（空なら全チャンネル）
	// Hidden は非表示配信の扱い。語彙は GET /api/singers/{id}/streams?hidden= と揃える。
	//
	//	""/"false" … 非表示を除く（既定。従来の挙動）
	//	"true"     … 非表示だけ
	//	"all"      … 両方
	//
	// 既定を「除く」に据え置くのは、通常の運用で非表示（雑談・ゲーム配信）を
	// 毎回 AI にかけたくないため。非表示を回すのは規則を変えた後の棚卸しという
	// 別の作業なので、明示的に選ばせる。
	Hidden string `json:"hidden,omitempty"`
}

// ========== 歌唱記録の直接作成 ==========

// Holodex またはコメントから読み込んだ楽曲候補。
type SongSuggestion struct {
	Name           string   `json:"name"`
	OriginalArtist string   `json:"original_artist"`
	StartSeconds   int      `json:"start_seconds"`
	EndSeconds     int      `json:"end_seconds"`
	Tags           []string `json:"tags"`       // 正規化で検出したバージョンタグ
	SingerIDs      []string `json:"singer_ids"` // 既定はチャンネル所有者
	ArtURL         *string  `json:"art_url,omitempty"`
	ItunesID       *int64   `json:"itunes_id,omitempty"` // Holodex が提供する iTunes ID

	// Chat の拍手検出結果（Holodex に明記された end との比較用。CommentSong と対称）
	ChatEnd int `json:"chat_end,omitempty"` // Chat が検出した終了秒数（ある場合）
	EndDiff int `json:"end_diff,omitempty"` // |end - chat_end|。両方に値がある場合のみ設定

	// ↓ 折り込んだ正規化結果（AnalyzeHolodexSongs 時に AI 正規化＋DB 照合し埋め込む。CommentSong と対称）
	NormalizedName           string  `json:"normalized_name,omitempty"`
	NormalizedNameReading    string  `json:"normalized_name_reading,omitempty"`
	NormalizedArtist         string  `json:"normalized_artist,omitempty"`
	NormalizedArtistReading  string  `json:"normalized_artist_reading,omitempty"`
	Confidence               float64 `json:"confidence,omitempty"`
	MatchedSongID            *string `json:"matched_song_id,omitempty"`
	MatchedSongName          *string `json:"matched_song_name,omitempty"`
	MatchedSongNameReading   *string `json:"matched_song_name_reading,omitempty"`
	MatchedSongArtist        *string `json:"matched_song_artist,omitempty"`
	MatchedSongArtistReading *string `json:"matched_song_artist_reading,omitempty"`
	MatchedSongArtURL        *string `json:"matched_song_art_url,omitempty"`
	MatchedSongItunesID      *int64  `json:"matched_song_itunes_id,omitempty"`
	// 自動採用に届かなかった照合候補（CommentSong と対称）
	MatchCandidates []SongMatchCandidate `json:"match_candidates,omitempty"`
	// AI が照合した場合の申し送り（歌手が DB と違うとき）
	ArtistAlias *ArtistAliasProposal `json:"artist_alias,omitempty"`
	// Changes は「抽出したままの値が、どの処理でどう変わったか」の履歴。
	// 保存しない（照合は読み取り時に計算する）。
	Changes []FieldChange `json:"changes,omitempty"`
}

// LoadHolodexSongsResponse は Holodex 楽曲の読み込みレスポンス。
type LoadHolodexSongsResponse struct {
	StreamID     string           `json:"stream_id"`
	StreamTitle  string           `json:"stream_title"`
	ChannelOwner SingerResponse   `json:"channel_owner"` // チャンネル所有者
	Participants []SingerResponse `json:"participants"`  // すべての参加者（チャンネル所有者を含む）
	Songs        []SongSuggestion `json:"songs"`
}

// CreatePerformancesRequest は歌唱記録の作成リクエスト。
type CreatePerformanceItem struct {
	Name                  string   `json:"name"`
	NameReading           string   `json:"name_reading,omitempty"`
	OriginalArtist        string   `json:"original_artist"`
	OriginalArtistReading string   `json:"original_artist_reading,omitempty"`
	StartSeconds          int      `json:"start_seconds"`
	EndSeconds            int      `json:"end_seconds"`
	Tags                  []string `json:"tags"`
	SingerIDs             []string `json:"singer_ids"`
	ArtURL                *string  `json:"art_url,omitempty"`
	ItunesID              *int64   `json:"itunes_id,omitempty"` // Holodex が提供する iTunes ID
	CustomTags            []string `json:"custom_tags,omitempty"`

	// 終了時間の由来と確認状態（詳細は docs/DATA_COMPLETION.md）。
	// フロントエンドは編集中ずっと endSource を追跡しているので、保存時にそれを送る。
	// 省略された場合は unknown 扱いになる。
	EndSource string `json:"end_source,omitempty"`
	// 編集画面から保存されたものは人が見たとみなせるので既定で true。
	// 一括自動作成など人手を介さない経路では明示的に false を送ること。
	EndConfirmed *bool `json:"end_confirmed,omitempty"`
}

type CreatePerformancesRequest struct {
	Performances []CreatePerformanceItem `json:"performances"`
}

type CreatePerformancesResponse struct {
	CreatedCount int `json:"created_count"`
}

// ========== AI 正規化 ==========

// AINormalizeInput は AI 正規化への入力項目。
type AINormalizationItem struct {
	Name           string  `json:"name"`
	OriginalArtist string  `json:"original_artist"`
	ArtURL         *string `json:"art_url,omitempty"`
	ItunesID       *int64  `json:"itunes_id,omitempty"`
}

// BatchAINormalizeRequest は AI 正規化の一括リクエスト。
type BatchAINormalizationRequest struct {
	Items []AINormalizationItem `json:"items"`
}

// AINormalizeResult は AI の提案結果。
type AISuggestionResult struct {
	Index                 int      `json:"index"` // 元のインデックスに対応
	NormalizedName        string   `json:"normalized_name"`
	NormalizedNameReading string   `json:"normalized_name_reading"` // 曲名の平仮名読み
	OriginalArtist        string   `json:"original_artist"`
	OriginalArtistReading string   `json:"original_artist_reading"` // アーティスト名の平仮名読み
	Tags                  []string `json:"tags"`
	Confidence            float64  `json:"confidence"`
	Reasoning             string   `json:"reasoning"`
	MatchedSongID         *string  `json:"matched_song_id,omitempty"` // 既存楽曲に一致した場合
	MatchReason           string   `json:"match_reason,omitempty"`    // 一致理由（title_primary, itunes_id …）
	MatchScore            float64  `json:"match_score,omitempty"`     // 確信度 0.0〜1.0
	// DB の楽曲情報（一致した場合に返す）
	MatchedSongName          *string `json:"matched_song_name,omitempty"`
	MatchedSongNameReading   *string `json:"matched_song_name_reading,omitempty"`
	MatchedSongArtist        *string `json:"matched_song_artist,omitempty"`
	MatchedSongArtistReading *string `json:"matched_song_artist_reading,omitempty"`
	MatchedSongArtURL        *string `json:"matched_song_art_url,omitempty"`
	MatchedSongItunesID      *int64  `json:"matched_song_itunes_id,omitempty"`
	// MatchCandidates は「似ているが自動採用の水準に届かなかった」既存楽曲。
	// 別名義（松任谷由実 / 荒井由実）や同名異曲がここに出る。UI で人に選ばせる。
	MatchCandidates []SongMatchCandidate `json:"match_candidates,omitempty"`
	// AI が照合した場合の申し送り（歌手が DB と違うとき）
	ArtistAlias *ArtistAliasProposal `json:"artist_alias,omitempty"`
}

// ========== 楽曲の統合候補 ==========

// MergeCandidateSong は統合候補に出す楽曲の要約。
// ItunesIDs は「編曲がどれだけ違うか」を人が判断するための手がかり
// （収録アルバム・再生時間・試聴）をフロントが引くために返す。
type MergeCandidateSong struct {
	ID               string  `json:"id"`
	Name             string  `json:"name"`
	OriginalArtist   string  `json:"original_artist"`
	ArtURL           string  `json:"art_url,omitempty"`
	PerformanceCount int     `json:"performance_count"`
	ItunesIDs        []int64 `json:"itunes_ids,omitempty"`
	Role             string  `json:"role,omitempty"` // AI が説明したこの曲の立ち位置
}

// MergeVerdictResponse は AI の見立て。統合するかどうかの決定ではない。
type MergeVerdictResponse struct {
	SameComposition *bool  `json:"same_composition,omitempty"`
	SameArrangement *bool  `json:"same_arrangement,omitempty"`
	Recommendation  string `json:"recommendation,omitempty"` // merge | keep_separate
	Note            string `json:"note,omitempty"`
	Source          string `json:"source,omitempty"`
	Judged          bool   `json:"judged"`
}

// MergeCandidateResponse は「新しく作られた曲が既存曲と似ている」1 件。
// 照合が外れて新曲として登録されたものを、人が統合して畳むための入口。
type MergeCandidateResponse struct {
	ID           string                `json:"id"`
	Score        float64               `json:"score"`
	Reason       string                `json:"reason"`
	Origin       string                `json:"origin"` // create | scan
	NewSong      MergeCandidateSong    `json:"new_song"`
	ExistingSong MergeCandidateSong    `json:"existing_song"`
	Verdict      *MergeVerdictResponse `json:"verdict,omitempty"`
}

// ========== 照合の学習層（別名義・別表記） ==========

// ArtistAliasMemberResponse は別名義グループの 1 名。
type ArtistAliasMemberResponse struct {
	NameKey     string `json:"name_key"`
	DisplayName string `json:"display_name"`
	Source      string `json:"source"` // manual | ai
	Note        string `json:"note,omitempty"`
}

// ArtistAliasGroupResponse は同一人物としてまとめられた名前の集まり。
type ArtistAliasGroupResponse struct {
	GroupID string                      `json:"group_id"`
	Members []ArtistAliasMemberResponse `json:"members"`
}

// SongAliasResponse は統合から学習した「この表記はこの曲」の 1 件。
type SongAliasResponse struct {
	NameKey    string `json:"name_key"`
	ArtistKey  string `json:"artist_key"`
	Source     string `json:"source"`
	SongID     string `json:"song_id"`
	SongName   string `json:"song_name"`
	SongArtist string `json:"song_artist"`
}

// SongMatchCandidate は既存楽曲との照合候補 1 件。
type SongMatchCandidate struct {
	SongID  string  `json:"song_id"`
	Name    string  `json:"name"`
	Artist  string  `json:"artist"`
	Score   float64 `json:"score"`
	Reason  string  `json:"reason"`
	ArtURL  string  `json:"art_url,omitempty"`
	IsMatch bool    `json:"is_match"` // 自動採用された候補か
}

// BatchAINormalizeResponse は AI 正規化の一括レスポンス。
type BatchAINormalizationResponse struct {
	Suggestions []AISuggestionResult `json:"suggestions"`
	Warning     string               `json:"warning,omitempty"`
}

// ========== 認証 ==========

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type LoginResponse struct {
	Token string `json:"token"`
	User  struct {
		ID          uuid.UUID `json:"id"`
		Username    string    `json:"username"`
		DisplayName string    `json:"display_name"`
		Role        string    `json:"role"`
	} `json:"user"`
}

// ========== アーティスト ==========

type ArtistResponse struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	NameReading *string   `json:"name_reading,omitempty"`
	SongCount   int       `json:"song_count"`
}

type ArtistListResponse struct {
	Artists    []ArtistResponse   `json:"artists"`
	Pagination PaginationResponse `json:"pagination"`
}

type ArtistDetailResponse struct {
	Artist     ArtistResponse     `json:"artist"`
	Songs      []SongResponse     `json:"songs"`
	Pagination PaginationResponse `json:"pagination"`
}

type UpdateArtistRequest struct {
	Name        *string `json:"name,omitempty"`         // 変更時のみ。所属楽曲の表示テキストも連動更新
	NameReading *string `json:"name_reading,omitempty"` // 変更時のみ
}

type MergeArtistRequest struct {
	TargetArtistID string `json:"target_artist_id"`
}

// BackfillReadingsResponse 読み仮名 AI 補完の結果
type BackfillReadingsResponse struct {
	ArtistsUpdated int    `json:"artists_updated"`
	SongsUpdated   int    `json:"songs_updated"`
	Warning        string `json:"warning,omitempty"`
}

// ========== 読みのエクスポート / インポート ==========

// ReadingItem はエクスポート/インポート1件（アーティスト or 楽曲の読み）。
type ReadingItem struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Reading string `json:"reading"`
}

// ReadingsExport は読みデータの一括エクスポート形式。外部 AI で reading を埋めて再取り込みできる。
type ReadingsExport struct {
	Artists []ReadingItem `json:"artists"`
	Songs   []ReadingItem `json:"songs"`
}

// ReadingsStats は読みの整備状況。管理画面で「あと何件残っているか」を出すために使う。
type ReadingsStats struct {
	ArtistsTotal    int `json:"artists_total"`
	ArtistsNeedsFix int `json:"artists_needs_fix"`
	SongsTotal      int `json:"songs_total"`
	SongsNeedsFix   int `json:"songs_needs_fix"`
}

// ImportReadingsResult は読み取り込みの結果。
type ImportReadingsResult struct {
	ArtistsUpdated int      `json:"artists_updated"`
	SongsUpdated   int      `json:"songs_updated"`
	Skipped        int      `json:"skipped"` // かな以外・不正な読みで採用しなかった件数
	Errors         []string `json:"errors,omitempty"`
}

// ========== 修正提案（閲覧モードからの提案 → 管理者レビュー） ==========

// MissingSongPayload 「この配信のこの時点に、登録されていない曲がある」という報告の中身。
// 既存レコードの修正ではないので before/after ではなくこの形で持つ。
//
// 前半（StreamID 〜 EndSeconds）は閲覧者からの報告でも埋まる。
// 後半は**一括セットリスト作成が審査へ回すとき**に付ける照合結果と監査情報で、
// 人手の報告では空のまま。
//
// SongID を持たせているのがこの構造体の要。持たせる前は承認が曲名から引き直していたので、
// 一括が決めた照合（`深昏睡` → `深昏睡 (Deep coma)`）が承認の瞬間に捨てられ、
// DB の表記と食い違う組では新曲が作られていた。
type MissingSongPayload struct {
	StreamID       string `json:"stream_id"` // YouTube 動画 ID
	SongName       string `json:"song_name"`
	OriginalArtist string `json:"original_artist"`
	StartSeconds   int    `json:"start_seconds"`
	EndSeconds     int    `json:"end_seconds"` // 0 = 未指定（動画の最後まで）

	// ---- 照合済みの内容（承認はこれをそのまま使う） ----

	// SongID は照合済みの楽曲。空でなければ承認は**文字列から引き直さない**。
	SongID string `json:"song_id,omitempty"`
	// SingerIDs は歌った人。空なら承認時に配信のオーナー（1人だけなら参加者）を使う。
	SingerIDs []string `json:"singer_ids,omitempty"`
	// EndSource は終了時間の確度（chat / holodex / comment / next_start / unknown）。
	EndSource string   `json:"end_source,omitempty"`
	Tags      []string `json:"tags,omitempty"`
	ItunesID  *int64   `json:"itunes_id,omitempty"`

	// ここから下は **CreatePerformanceItem が運ぶ欄と揃えるためのもの**。
	//
	// 承認は findOrCreateSong を通り、その中でジャケットと読みが使われる。運ばなければ
	// 空のまま渡り、審査から作った曲だけジャケットも読みも付かない ── 編集画面から
	// 作ったものとの差になる。**CreatePerformanceItem に欄を足したらここにも足すこと。**
	ArtURL                string   `json:"art_url,omitempty"`
	NameReading           string   `json:"name_reading,omitempty"`
	OriginalArtistReading string   `json:"original_artist_reading,omitempty"`
	CustomTags            []string `json:"custom_tags,omitempty"`

	// ---- 監査（どういう経緯でこの提案になったか） ----

	// ReviewReasons は審査へ回した理由（no_end / no_artist / unmatched / …）。
	ReviewReasons []string `json:"review_reasons,omitempty"`
	Source        string   `json:"source,omitempty"`       // holodex / comment
	Via           string   `json:"via,omitempty"`          // rule / ai
	Confidence    float64  `json:"confidence,omitempty"`   // AI が照合した場合の確信度
	AIReason      string   `json:"ai_reason,omitempty"`    // AI の判断理由
	BatchRunID    string   `json:"batch_run_id,omitempty"` // どの実行が積んだか

	// RawName / RawArtist は抽出したままの表記（正規化・照合で書き換わる前）。
	// 審査画面に「元は何と書かれていたか」を出すために持つ。
	RawName   string `json:"raw_name,omitempty"`
	RawArtist string `json:"raw_artist,omitempty"`

	// ConflictKind は既存の歌唱と食い違った理由（song / start / end / addition）。
	// 「食い違う」とだけ言われても人は何を見ればいいか分からないので、
	// **どこが違うのか**を持ち越す。addition は「既にセットリストがある配信への追加」で、
	// 対応する既存の歌唱が無い（＝突き合わせる相手が居ない）ことを表す。
	ConflictKind string `json:"conflict_kind,omitempty"`
	// Existing は突き合わせた既存の歌唱（addition のときは入らない）。
	Existing *ExistingPerformance `json:"existing,omitempty"`

	// Candidates は決めきれなかったときの候補。審査画面でそのまま選べる。
	Candidates []SongMatchCandidate `json:"candidates,omitempty"`
}

// ExistingPerformance は審査画面で「既存はこうなっている」と並べて見せるための最小限。
type ExistingPerformance struct {
	ID             string `json:"id"`
	SongName       string `json:"song_name"`
	OriginalArtist string `json:"original_artist"`
	StartSeconds   int    `json:"start_seconds"`
	EndSeconds     int    `json:"end_seconds"`
}

// SongSwapPayload 「この歌唱は別の曲だ」という指摘の中身。
//
// 曲の同一性は文字列の差分では表せない（曲名を直すのではなく、別の曲マスタへ繋ぎ替える）ため、
// フィールド差し替えではなくこの形で持つ。
// SongID があれば既存の曲へ、無ければ名前から曲を探す／作る（perf.missing と同じ経路）。
type SongSwapPayload struct {
	SongID         string `json:"song_id"`
	SongName       string `json:"song_name"`
	OriginalArtist string `json:"original_artist"`
	// CurrentSongName は提案時点の曲名。レビュー時に「何から何へ」を見せるためと、
	// 提案後に曲が差し替えられていないかの確認に使う。
	CurrentSongName string `json:"current_song_name"`

	// ここから下は **CreatePerformanceItem が運ぶ欄と揃えるためのもの**。
	// SongID が空（＝未登録の曲へ差し替える）ときだけ効く。
	//
	// MissingSongPayload と同じ理由で要る：承認は findOrCreateSong を通り、
	// その中でジャケットと読みが使われる。運ばなければ空のまま渡り、
	// **差し替えから作った曲だけ**ジャケットも読みも iTunes も付かない。
	// **CreatePerformanceItem に欄を足したらここにも足すこと。**
	ItunesID              *int64 `json:"itunes_id,omitempty"`
	ArtURL                string `json:"art_url,omitempty"`
	NameReading           string `json:"name_reading,omitempty"`
	OriginalArtistReading string `json:"original_artist_reading,omitempty"`
}

// CreateSuggestionRequest 修正提案の投稿（要ログイン）。
//
// kind = "field"（既定）… 既存レコードのフィールド差し替え。TargetType / TargetID / Fields を使う。
// kind = "perf.missing" … 未登録曲の追加報告。Payload を使う（TargetID は不要）。
// kind = "perf.meta"    … 歌唱の曲の差し替え。TargetID（歌唱）と SongSwap を使う。
type CreateSuggestionRequest struct {
	TargetType string              `json:"target_type"` // song / artist / performance
	TargetID   string              `json:"target_id"`
	Kind       string              `json:"kind,omitempty"`
	Fields     map[string]string   `json:"fields"` // 提案する編集値（キーは対象の編集可能フィールド）
	Payload    *MissingSongPayload `json:"payload,omitempty"`
	SongSwap   *SongSwapPayload    `json:"song_swap,omitempty"`
	Note       string              `json:"note"` // 提案者コメント（任意）
}

// ApproveSuggestionRequest 承認（任意ボディ）。
//
// Payload は perf.missing を承認するときだけ使う。審査担当が画面で直した内容
// （曲の差し替え・時間の微調整・歌手の選択）をそのまま反映するためのもので、
// 「提案の内容を書き換えてから承認する」操作を 1 往復で済ませる。
type ApproveSuggestionRequest struct {
	Payload *MissingSongPayload `json:"payload,omitempty"`
}

// RejectSuggestionRequest 却下（任意ボディ）。
type RejectSuggestionRequest struct {
	Note string `json:"note"`
	// NotThisSong は「この表記はこの曲ではない」という明確な否決。
	// song_identity_checks に残し、次の一括実行が同じ組を提案しないようにする。
	// 単に「今は要らない」という却下と区別するため、明示的に立ててもらう。
	NotThisSong bool `json:"not_this_song"`
}

// FieldConflict 承認時に検出した「提案時点の値」と「現在の値」のズレ。
type FieldConflict struct {
	Expected string `json:"expected"`
	Current  string `json:"current"`
}

// SuggestionResponse 提案1件（before/after を差分表示できるよう両方返す）。
type SuggestionResponse struct {
	ID          uuid.UUID         `json:"id"`
	TargetType  string            `json:"target_type"`
	TargetID    uuid.UUID         `json:"target_id"`
	TargetKey   string            `json:"target_key"` // 配信の YouTube 動画 ID（UUID 対象では空）
	TargetLabel string            `json:"target_label"`
	Kind        string            `json:"kind"`
	Before      map[string]string `json:"before"`
	After       map[string]string `json:"after"`
	// Payload は kind = perf.missing のときだけ入る（追加したい曲の内容）
	Payload *MissingSongPayload `json:"payload,omitempty"`
	// Overlaps は kind = perf.missing で、提案の時間帯に既存の歌唱があるときだけ入る
	Overlaps []OverlapInfo `json:"overlaps,omitempty"`
	// SongSwap は kind = perf.meta のときだけ入る（差し替え先の曲）
	SongSwap *SongSwapPayload `json:"song_swap,omitempty"`
	Note     string           `json:"note"`
	Status   string           `json:"status"`

	// Conflicts は未処理の提案について、対象が提案後に変更されたフィールドを示す。
	// 空でなければ、そのまま承認すると他人の編集を巻き戻すことになる。
	Conflicts map[string]FieldConflict `json:"conflicts,omitempty"`

	CreatedBy     *uuid.UUID `json:"created_by"`
	CreatedByName string     `json:"created_by_name"` // 匿名投稿では空
	ReviewNote    string     `json:"review_note"`
	CreatedAt     time.Time  `json:"created_at"`
	ReviewedAt    *time.Time `json:"reviewed_at"`
}

// UpdatePerformanceRequest 歌唱記録1件の部分更新。nil のフィールドは変更しない。
type UpdatePerformanceRequest struct {
	SongID       *string   `json:"song_id"`       // 別の曲へ差し替える場合
	StartSeconds *int      `json:"start_seconds"` // 開始秒
	EndSeconds   *int      `json:"end_seconds"`   // 終了秒（0 = 動画終了まで）
	CustomTags   *[]string `json:"custom_tags"`
	Tags         *[]string `json:"tags"`       // performance_tags の ID
	SingerIDs    *[]string `json:"singer_ids"` // 歌唱チャンネル
}

type SuggestionListResponse struct {
	Suggestions []SuggestionResponse `json:"suggestions"`
	Pagination  PaginationResponse   `json:"pagination"`
}

// OverlapInfo 未登録曲の追加提案と時間が重なる既存の歌唱。
// メドレーなど正当に重なる場合もあるので承認は止めないが、
// 「もう登録されている曲を報告していないか」をレビュー時に気づけるようにする。
type OverlapInfo struct {
	SongName     string `json:"song_name"`
	StartSeconds int    `json:"start_seconds"`
	EndSeconds   int    `json:"end_seconds"`
}

// SuggestionGroup 同一対象に集まった提案。同じ歌唱への通報を1枚で捌くための単位。
type SuggestionGroup struct {
	TargetType  string               `json:"target_type"`
	TargetID    uuid.UUID            `json:"target_id"`
	TargetKey   string               `json:"target_key"`
	TargetLabel string               `json:"target_label"`
	Current     map[string]string    `json:"current"` // 対象の現在値（提案と見比べるため）
	Suggestions []SuggestionResponse `json:"suggestions"`
}

// SuggestionGroupListResponse ページングの単位はグループ（対象）。
type SuggestionGroupListResponse struct {
	Groups     []SuggestionGroup  `json:"groups"`
	Pagination PaginationResponse `json:"pagination"`
}

// BatchReviewRequest 複数提案の一括承認/却下。
type BatchReviewRequest struct {
	IDs    []string `json:"ids"`
	Action string   `json:"action"`          // approve / reject
	Force  bool     `json:"force,omitempty"` // 承認時、衝突していても上書きする
	Note   string   `json:"note,omitempty"`  // 却下理由
}

type BatchReviewResult struct {
	ID       uuid.UUID `json:"id"`
	OK       bool      `json:"ok"`
	Error    string    `json:"error,omitempty"`
	Conflict bool      `json:"conflict,omitempty"` // 対象が変更済みで止まった
}

type BatchReviewResponse struct {
	Succeeded int                 `json:"succeeded"`
	Failed    int                 `json:"failed"`
	Results   []BatchReviewResult `json:"results"`
}

// MergeSuggestionsRequest 同一対象に集まった提案を、管理者が決めた値へ統合して反映する。
//
// 「どれか1つを丸ごと採用」では表せないケース（3人が 6708 / 6710 / 6716 と提案していて
// 中央値を採りたい、誰も出していない値にしたい、項目ごとに別の提案を採りたい）のための操作。
type MergeSuggestionsRequest struct {
	TargetType string            `json:"target_type"`
	TargetID   string            `json:"target_id"`
	Fields     map[string]string `json:"fields"` // 実際に反映する値
	IDs        []string          `json:"ids"`    // このグループの提案（すべて処理済みにする）
	Note       string            `json:"note"`   // レビューメモ（任意）
}

// MergeSuggestionsResponse 反映した値と、採用/不採用として記録した件数。
type MergeSuggestionsResponse struct {
	Applied  map[string]string `json:"applied"`
	Approved int               `json:"approved"` // 採用値と一致していた提案
	Rejected int               `json:"rejected"` // 別の値になった提案
}

// ========== グローバル検索 ==========

// SearchStreamItem 検索結果の配信（軽量版）
type SearchStreamItem struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	StreamDate   time.Time `json:"stream_date"`
	ThumbnailURL *string   `json:"thumbnail_url,omitempty"`
	// IsProcessed は content:edit のときだけ（StreamResponse と同じ理由）。
	IsProcessed *bool `json:"is_processed,omitempty"`
	IsHidden    bool  `json:"is_hidden"`
}

// SearchTagItem 検索結果のタグ（使用件数付き）
type SearchTagItem struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Color       string `json:"color"`
	Count       int    `json:"count"`
}

// GlobalSearchResponse 統一検索の結果。
// 入力が YouTube URL / video ID の場合は video_id を返し、テキスト検索はスキップする。
type GlobalSearchResponse struct {
	Query           string             `json:"query"`
	VideoID         string             `json:"video_id,omitempty"`
	VideoRegistered bool               `json:"video_registered"`
	Songs           []SongResponse     `json:"songs"`
	Streams         []SearchStreamItem `json:"streams"`
	Singers         []SingerResponse   `json:"singers"`
	Artists         []ArtistResponse   `json:"artists"`
	StreamTags      []SearchTagItem    `json:"stream_tags"`
	PerformanceTags []SearchTagItem    `json:"performance_tags"`
}

// TagPerformanceListResponse タグ検索（演出）のページング付き結果
type TagPerformanceListResponse struct {
	Performances []PerformanceResponse `json:"performances"`
	Pagination   PaginationResponse    `json:"pagination"`
}

// PerformanceListResponse ページングなしの歌唱一覧（ホームのおすすめ・ランダム再生用）
type PerformanceListResponse struct {
	Performances []PerformanceResponse `json:"performances"`
}

// ========== 共通レスポンス ==========

type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
}

type SuccessResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
}

// ========== 終了時刻の推定 ==========

type SongEndTimeEstimateRequest struct {
	Start     int    `json:"start"`
	End       int    `json:"end"`
	Name      string `json:"name"`
	Artist    string `json:"artist"`
	ItunesID  int64  `json:"itunes_id,omitempty"`
	NextStart int    `json:"next_start,omitempty"`
	StreamEnd int    `json:"stream_end,omitempty"`
}

type SongEndTimeEstimate struct {
	EstimatedEnd       int    `json:"estimated_end"`
	IsEndTimeEstimated bool   `json:"is_end_time_estimated"`
	Method             string `json:"method"` // "from_comment", "from_next_song", "from_itunes", "from_default"
	OriginalItunesDur  int    `json:"original_itunes_dur,omitempty"`
	Reason             string `json:"reason,omitempty"`
}

type EstimateEndTimesRequest struct {
	Songs       []SongEndTimeEstimateRequest `json:"songs"`
	StreamEnd   int                          `json:"stream_end"`
	StreamTitle string                       `json:"stream_title,omitempty"`
}

type EstimateEndTimesResponse struct {
	Estimates []SongEndTimeEstimate `json:"estimates"`
	Message   string                `json:"message,omitempty"`
}

// ========== iTunes 検索の拡張 ==========

// ItunesSearchResultWithSong は iTunes の検索結果（DB 登録済みの可能性あり）。
type ItunesSearchResultWithSong struct {
	ItunesID       int64      `json:"itunes_id"`
	CollectionName string     `json:"collection_name"`
	TrackName      string     `json:"track_name"`
	ArtistName     string     `json:"artist_name"`
	ArtworkURL     string     `json:"artwork_url"`
	Country        string     `json:"country"`
	ExistingSong   *SongBrief `json:"existing_song,omitempty"` // DB 登録済みなら楽曲の要約を返す
}

// ItunesQueryResultWithSong は iTunes ID 直引きの結果。検索と同じく
// 「その ID が既に DB の楽曲に紐づいているか」を併せて返す
// ── 分からないと、既にある曲を新規として作ってしまう。
type ItunesQueryResultWithSong struct {
	ItunesID        int64      `json:"itunes_id"`
	CollectionName  string     `json:"collection_name"`
	TrackName       string     `json:"track_name"`
	ArtistName      string     `json:"artist_name"`
	ArtworkURL      string     `json:"artwork_url"`
	TrackViewURL    string     `json:"track_view_url"`
	TrackTimeMillis int64      `json:"track_time_millis"`
	PreviewURL      string     `json:"preview_url"`
	Country         string     `json:"country"`
	ExistingSong    *SongBrief `json:"existing_song,omitempty"`
}

// SongBrief は iTunes 検索結果で使う楽曲の要約。
type SongBrief struct {
	ID                    uuid.UUID `json:"id"`
	Name                  string    `json:"name"`
	NameReading           *string   `json:"name_reading,omitempty"`
	OriginalArtist        string    `json:"original_artist"`
	OriginalArtistReading *string   `json:"original_artist_reading,omitempty"`
	Arts                  *string   `json:"arts,omitempty"`
	PerformanceCount      int       `json:"performance_count"`
}

type ItunesSearchResponseWithSongs struct {
	Results []ItunesSearchResultWithSong `json:"results"`
}

// ========== AI Provider ==========

// AIProviderResponse は AI プロバイダーの設定（API key を含まず、末尾 4 桁のヒントだけを返す）。
type AIProviderResponse struct {
	ID             int    `json:"id"`
	Name           string `json:"name"`
	BaseURL        string `json:"base_url"`
	Model          string `json:"model"`
	Enabled        bool   `json:"enabled"`
	Priority       int    `json:"priority"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	HasKey         bool   `json:"has_key"`
	KeyHint        string `json:"key_hint,omitempty"`
}

// ========== プレイリスト ==========

type CreatePlaylistRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Visibility  string `json:"visibility"` // private | unlisted | public（省略時 private）
}

type UpdatePlaylistRequest struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	Visibility  *string `json:"visibility,omitempty"`
}

// PlaylistResponse は一覧・詳細で共通に返すプレイリスト情報。
// share_slug は所有者にだけ返す（限定公開 URL を第三者へ漏らさないため）。
type PlaylistResponse struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Visibility  string    `json:"visibility"`
	ShareSlug   string    `json:"share_slug,omitempty"`
	ItemCount   int       `json:"item_count"`
	OwnerName   string    `json:"owner_name"`
	IsOwner     bool      `json:"is_owner"`
	CreatedAt   string    `json:"created_at"`
	UpdatedAt   string    `json:"updated_at"`
}

type PlaylistListResponse struct {
	Playlists []PlaylistResponse `json:"playlists"`
	Total     int                `json:"total"`
}

type AddPlaylistItemRequest struct {
	// PerformanceID は従来の1曲追加用。PerformanceIDs は一覧を順番どおり一括追加する。
	PerformanceID  string   `json:"performance_id"`
	PerformanceIDs []string `json:"performance_ids"`
}

type AddPlaylistItemsResponse struct {
	Added   int `json:"added"`
	Skipped int `json:"skipped"`
}

type ReorderPlaylistRequest struct {
	PerformanceIDs []string `json:"performance_ids"`
}

// PresetPlaylistResponse は運営が用意したプリセットプレイリスト。
// 所有者がいないので share_slug も is_owner も持たない（誰でも同じものが見える）。
type PresetPlaylistResponse struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	ItemCount   int    `json:"item_count"`
	IsFollowing bool   `json:"is_following"`
}

type PresetPlaylistListResponse struct {
	Presets []PresetPlaylistResponse `json:"presets"`
}

// AddPresetToPlaylistRequest はプリセットの中身をどこへ入れるか。
// PlaylistID があれば既存のプレイリストへ足し、無ければ新規作成する
// （Name は省略できる。その場合はプリセット名を使う）。
type AddPresetToPlaylistRequest struct {
	PlaylistID string `json:"playlist_id,omitempty"`
	Name       string `json:"name,omitempty"`
}

// AddPresetToPlaylistResponse は追加先と、実際に入った件数。
// Skipped は既に入っていて飛ばした件数（既存プレイリストへ足したときに出る）。
type AddPresetToPlaylistResponse struct {
	Playlist PlaylistResponse `json:"playlist"`
	Added    int              `json:"added"`
	Skipped  int              `json:"skipped"`
	Created  bool             `json:"created"` // 新しいプレイリストを作ったか
}

// ========== 外部サービス連携の設定（管理画面） ==========

// SecretFieldStatus は機密項目の状態。値そのものは返さない。
type SecretFieldStatus struct {
	Configured bool   `json:"configured"`
	Hint       string `json:"hint,omitempty"` // 末尾4文字
	FromEnv    bool   `json:"from_env"`       // true なら .env 由来（UI で保存すると DB 側が優先される）
}

type IntegrationSettingsResponse struct {
	// EncryptionEnabled が false だと機密を保存できない（SETTINGS_ENCRYPTION_KEY 未設定）
	EncryptionEnabled bool                         `json:"encryption_enabled"`
	Secrets           map[string]SecretFieldStatus `json:"secrets"`
	Plain             map[string]string            `json:"plain"`
	PlainFromEnv      map[string]bool              `json:"plain_from_env"`
}

type UpdateIntegrationSettingsRequest struct {
	// Secrets は項目名 -> 新しい値。空文字の項目は「変更なし」として無視される。
	Secrets map[string]string `json:"secrets,omitempty"`
	// Clear は明示的に消したい項目名。
	Clear                []string `json:"clear,omitempty"`
	GoogleDriveClientID  *string  `json:"google_drive_client_id,omitempty"`
	GoogleSigninClientID *string  `json:"google_signin_client_id,omitempty"`
}
