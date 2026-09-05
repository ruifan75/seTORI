// ========== ページング ==========

export interface PaginationResponse {
  page: number;
  limit: number;
  total: number;
  total_pages: number;
}

// ========== 楽曲 ==========

export interface SongItunes {
  itunes_id: number;
  collection_name?: string;
  country?: string;
  is_primary: boolean;
}

export interface ArtistReference {
  id: string;
  name: string;
}

export interface Song {
  id: string;
  name: string;
  name_reading?: string;
  original_artist: string;
  original_artist_reading?: string;
  artists: ArtistReference[];
  arts?: string;
  performance_count: number;
  itunes_ids?: SongItunes[];
  created_at: string;
  updated_at: string;
}

export interface SongListResponse {
  songs: Song[];
  pagination: PaginationResponse;
}

export interface CreateSongRequest {
  name: string;
  name_reading?: string;
  original_artist: string;
  original_artist_reading?: string;
  arts?: string;
}

export interface UpdateSongRequest {
  name: string;
  name_reading?: string;
  original_artist: string;
  original_artist_reading?: string;
  arts?: string;
  itunes_ids?: Array<{ itunes_id: number; is_primary: boolean }>;
}

// ========== iTunes API ==========

export interface SongBrief {
  id: string;
  name: string;
  name_reading?: string;
  original_artist: string;
  original_artist_reading?: string;
  arts?: string;
  performance_count: number;
}

export interface ITunesSearchResult {
  itunes_id: number;
  collection_name: string;
  track_name: string;
  artist_name: string;
  artwork_url: string;
  country: string;
  existing_song?: SongBrief; // DB に既に存在する場合、楽曲情報を含む
}

export interface ITunesSearchResponse {
  results: ITunesSearchResult[];
}

export interface ITunesQueryResult {
  itunes_id: number;
  collection_name: string;
  track_name: string;
  artist_name: string;
  artwork_url: string;
  track_view_url: string;
  track_time_millis: number;
  preview_url?: string;
  country: string;
  /** その ID が既に DB の楽曲に紐づいていれば、その楽曲（検索結果と同じ意味） */
  existing_song?: SongBrief;
}

// ========== 歌手 ==========

export interface Singer {
  id: string;
  name: string;
  english_name?: string;
  photo_url?: string;
  /** 実効値の organizations.key（override があればそれ、無ければ Holodex の値） */
  organization?: string;
  /** 実効値の表示名。「所属なし」を意味する分類のときは空（バッジを出さない） */
  organization_name?: string;
  /** 手動指定。あれば Holodex の分類を上書きしている */
  organization_override?: string;
  /** Holodex が返している分類（同期のたびに更新される） */
  organization_holodex?: string;
  metadata_source: string;
  can_edit_metadata: boolean;
  is_hidden: boolean; // チャンネル一覧から外す（詳細ページは非表示でも閲覧可）
  /**
   * 会限セットリストの公開可否（チャンネル単位）。**content:edit のときだけ返る。**
   * 省略＝未確認（まだ配信主に訊いていない）。'allow' 以外はすべて伏せる側。
   */
  members_only_policy?: 'allow' | 'deny';
  /**
   * 自動処理の対象か（定期同期＋コメント解析＋歌単作成）。
   * **content:edit のときだけ返る**。既定は false のオプトイン。
   * 立てても最後の確認（is_processed）は自動では付かない。
   */
  auto_fill_enabled?: boolean;
  /**
   * 所有する会限配信の本数。**content:edit のときだけ返る**（0 なら省略）。
   * 0 本のチャンネルに公開可否を訊いても意味が無いので、方針の導線はこれで出し分ける。
   */
  members_only_stream_count?: number;
  created_at: string;
  updated_at: string;
}

// ========== 事務所 ==========

// key は取り込み時の生の値（Holodex の org）、display_name が画面に出る名前。
export interface Organization {
  key: string;
  display_name: string;
  sort_order: number;
  /** 「所属なし」を意味する分類（Holodex の Independents など） */
  is_unaffiliated: boolean;
  singer_count: number;
  created_at: string;
  updated_at: string;
}

export interface OrganizationListResponse {
  organizations: Organization[];
}

export interface CreateOrganizationRequest {
  key?: string;
  display_name: string;
  sort_order?: number;
  is_unaffiliated?: boolean;
}

export interface UpdateOrganizationRequest {
  display_name: string;
  sort_order: number;
  is_unaffiliated: boolean;
}

export interface SingerListResponse {
  singers: Singer[];
  pagination: PaginationResponse;
}

// 事務所別のチャンネル一覧。organization が空文字の組は「所属なし」。
export interface SingerGroup {
  organization: string;
  display_name: string;
  singers: Singer[];
}

export interface SingerGroupListResponse {
  groups: SingerGroup[];
  total: number;
}

export interface SingerDetailResponse extends Singer {
  stream_count: number;
  performance_count: number;
}

export interface SingerPerformanceListResponse {
  singer: Singer;
  performances: SongPerformance[];
  pagination: PaginationResponse;
}

export interface CreateSingerRequest {
  id: string; // YouTube Channel ID / @handle / URL
  name?: string;
  english_name?: string;
  photo_url?: string;
  organization?: string;
}

export interface CreateSingerResponse {
  message: string;
  id: string;
  name: string;
}

// 事務所は含まない（singerApi.setOrganization が唯一の窓口）
export interface UpdateSingerRequest {
  name: string;
  english_name?: string;
  photo_url?: string;
}

// ========== 歌枠 ==========

export interface StreamTag {
  id: string;
  display_name: string;
  color: string;
}

export interface Stream {
  id: string;
  title: string;
  stream_date: string;
  duration_seconds?: number;
  thumbnail_url?: string;
  tags: StreamTag[];
  participants: Singer[];  // 参加者
  channel_owner?: Singer;  // チャンネルオーナー
  /**
   * 処理済み（セットリストを作り終えたか）。**content:edit のときだけ返る**。
   * 閲覧者には意味が無く、編集者が「まだ手を付けていない配信」を見分けるための印。
   * 未処理の配信そのものは誰でも見られる（隠すのは印だけ）。
   */
  is_processed?: boolean;
  is_hidden: boolean;      // 非表示（初回登録後は手動編集のみ）
  // 中身（セットリスト・解析結果）を公開してよいか未確認。**is_hidden とは別の軸**。
  // 立っている間、歌唱は編集者にしか返らない（曲/歌手/ランダム/プレイリストからも消える）
  is_restricted: boolean;
  // Holodex への**送信を試みた**時刻（PUT の前に記録するので「送信済み」ではない）。
  // content:edit のときだけ返る。秘匿にしても向こうのコピーは残りうる
  holodex_uploaded_at?: string;
  // 台帳の追跡開始より前から存在する配信。**台帳が空でも「送っていない」とは言えない**
  holodex_upload_unknown?: boolean;
  holodex_timeline_songs?: SongSuggestion[];  // Holodex タイムライン データ
  comment_timeline_songs?: CommentSong[];     // コメント解析済みタイムライン（分析キャッシュ）
  // 解析を最後に走らせた時刻。updated_at は Holodex 同期でも動くので代用できない
  comment_songs_analyzed_at?: string;
  has_comment_raw?: boolean;                  // 分析可能な生コメントがあるか
  chapter_timeline_songs?: CommentSong[];     // チャプター解析済みタイムライン（分析キャッシュ）
  // 配信者が付けた章節の数。-1 は「まだ調べていない」で、0（＝調べたが無い）とは別
  chapter_count?: number;
  // 埋め込みプレイヤーで再生できるか。**詳細でしか返らない**（一覧・検索では undefined）。
  // undefined＝この応答は判定していない。従来どおり描いてよい
  playability?: Playability;
  // 生の判定材料は content:edit のときだけ返る
  availability?: string;
  playable_in_embed?: boolean;
  availability_checked_at?: string;
  created_at: string;
  updated_at: string;
}

// Playability は「この配信を埋め込みプレイヤーで再生できるか」。
//
// 会限の動画は YouTube が埋め込みを塞いでいて、メンバー資格があっても再生できない。
// 必ず失敗するプレイヤーを描いてから onError: 150 で気付くより、最初から描かない。
// unknown は「まだ調べていない」で、再生不可とは違う（描いてよい）。
export type Playability =
  | 'unknown'
  | 'playable'
  | 'members_only'
  | 'embed_disabled'
  | 'unavailable';

// Chapter は配信者が付けた YouTube の目次。end は次の章節の開始なので、
// 「その曲が終わった時刻」ではない（曲のあとの MC を含む）
export interface Chapter {
  start: number;
  end: number;
  title: string;
}

export interface StreamListResponse {
  streams: Stream[];
  pagination: PaginationResponse;
}

// 再生可否の一括取得の進捗。**saved と failed の意味を取り違えないこと** ──
// saved は「DB に記録できた」（「動画が無い」も含む。再試行不要）、
// failed は「記録できなかった」＝再試行が要るもの。error の有無ではない。
export interface AvailabilityBackfillStatus {
  running: boolean;
  total: number;
  done: number;
  saved: number;
  failed: number;
  cancelled: boolean;
  last_error?: string;
}

export interface UpdateStreamRequest {
  title?: string;
  stream_date?: string;
  tag_ids?: string[];
  participant_ids?: string[];
  is_processed?: boolean;
  is_hidden?: boolean;
  // false にするのが「中身を公開してよい」という人の判断。自動では下がらない
  is_restricted?: boolean;
}

// ========== パフォーマンス ==========

export interface PerformanceTag {
  id: string;
  display_name: string;
  color: string;
}

/** 終了時間の由来（docs/DATA_COMPLETION.md）。unknown は記録開始前のデータ */
export type EndSource =
  | 'manual' | 'holodex' | 'comment' | 'chat'
  | 'itunes' | 'next_start' | 'default' | 'unknown';

export interface Performance {
  id: string;
  stream_id: string;
  song_id: string;
  song_name: string;
  original_artist: string;
  artists: ArtistReference[];
  arts?: string;
  start_seconds: number;
  end_seconds: number;
  order_index: number;
  tags: PerformanceTag[];
  custom_tags: string[];
  singers: Singer[];
  youtube_url: string;
  created_at: string;
  /** 終了時間の由来 */
  end_source: EndSource;
  /** 人が値を見て認めたか。由来とは独立 */
  end_confirmed: boolean;
  // この曲に紐付いている primary な iTunes ID（配信詳細でのみ返る）
  itunes_id?: number;
  // タグ検索など配信横断の一覧でのみ設定される
  stream_title?: string;
  stream_date?: string;
  thumbnail_url?: string;
}

export interface StreamDetailResponse extends Stream {
  performances: Performance[];
}

// 歌唱記録1件の部分更新。省いたフィールドは変更されない。
// end_seconds = 0 は「動画の最後まで」を意味する。
export interface UpdatePerformanceRequest {
  song_id?: string;
  start_seconds?: number;
  end_seconds?: number;
  custom_tags?: string[];
  tags?: string[];
  singer_ids?: string[];
}

// 楽曲詳細ページからの逆引きクエリ用（歌手詳細ページでも使用）
export interface SongPerformance {
  id: string;
  stream_id: string;
  stream_title: string;
  stream_date: string;
  thumbnail_url?: string;
  song_id?: string;
  song_name?: string;
  original_artist?: string;
  artists: ArtistReference[];
  start_seconds: number;
  end_seconds: number;
  tags: PerformanceTag[];
  custom_tags: string[];
  singers: Singer[];
  youtube_url: string;
  created_at: string;
}

export interface SongPerformanceListResponse {
  song: Song;
  performances: SongPerformance[];
  pagination: PaginationResponse;
}

// ========== Holodex 同期 ==========

export interface SyncHolodexRequest {
  channel_id: string;
  limit?: number;
  force_update?: boolean;
}

export interface SyncHolodexResponse {
  synced_count: number;
  total_streams: number;
  processed: number;
  new_streams: string[];
  updated: string[];
  skipped: string[];
  in_progress: boolean;
  message?: string;
}

// ========== タグ漏れ（解析キャッシュ vs 歌唱） ==========
//
// 解析キャッシュ（コメント / Holodex）が付けた演奏バージョンのタグのうち、
// 対応する歌唱に付いていないもの。差分は毎回計算される派生値で、保存されるのは
// 「付けない」と判断した否定（dismissed）だけ。

export interface TagGap {
  performance_id: string;
  stream_id: string;
  stream_title: string;
  start_seconds: number;
  song_id: string;
  song_name: string;
  song_artist: string;
  current_tags: string[];
  missing_tags: string[];
  sources: string[];   // comment / holodex
  cached_name: string; // 解析側の曲名（原文のバージョン表記が残っている）
  /** 解析側の曲名と歌唱の曲名が同じものを指していそうか。false は「同じ時刻に別の曲」の合図 */
  name_matches: boolean;
}

export interface TagGapDismissal {
  performance_id: string;
  tag_id: string;
  stream_id: string;
  stream_title: string;
  start_seconds: number;
  song_name: string;
  checked_by: string;
  checked_at: string;
}

// ========== コメント解析 ==========

export interface CommentSong {
  start: number;
  end: number;
  name: string;
  original_artist: string;
  original_comment: string;
  is_end_time_estimated: boolean;

  // Chat の拍手検出結果（コメントに明記された終了時刻との比較用）
  chat_end?: number;
  end_diff?: number; // |end - chat_end|。両方に値がある場合のみ設定される

  // 分析時に折り込んだ正規化結果（あれば）
  normalized_name?: string;
  normalized_name_reading?: string;
  normalized_artist?: string;
  normalized_artist_reading?: string;
  tags?: string[];
  confidence?: number;
  matched_song_id?: string;
  matched_song_name?: string;
  matched_song_name_reading?: string;
  matched_song_artist?: string;
  matched_song_artist_reading?: string;
  matched_song_art_url?: string;
  matched_song_itunes_id?: number;
  // 自動採用に届かなかった照合候補（別名義・同名異曲など）。UI で人に選ばせる
  match_candidates?: SongMatchCandidate[];
  artist_alias?: ArtistAliasProposal;
  changes?: FieldChange[];
}

// 「抽出したままの値が、どの処理でどう変わったか」。
// by は ai_normalize（AI 正規化）か db_match（DB 照合）。
// 画面に理由なく名前が変わって見えるのを防ぐためのもので、保存はされない。
// AI が照合したときの申し送り。歌手が DB と違う場合だけ付く。
// 登録は編集フォームで人がチェックを入れて保存したときだけ（docs/SETLIST_FLOW.md）。
export interface ArtistAliasProposal {
  canonical: string;   // DB 側の表記（別名義の本体）
  alias: string;       // コメント側の表記
  same_artist: boolean; // AI の判定。true のときだけ既定でチェックが入る
}

export interface FieldChange {
  field: 'name' | 'artist';
  by: 'ai_normalize' | 'db_match' | 'ai_match';
  from: string;
  to: string;
  reason?: string;  // db_match の根拠（exact / title_artist / ai …）
  score?: number;
}

export interface AnalyzeCommentsResponse {
  songs: CommentSong[];
  raw_comments: string[];
  warning?: string; // AI 正規化が失敗し抽出のみで返した場合
}

// 未処理配信の一括プレ分析ジョブの進捗
/** 自動処理（定期実行）の設定。**content:edit のみ**。 */
export interface AutoFillSettings {
  enabled: boolean;
  /** 実行間隔。短いほど「まだ変換中」の配信を何度も触ることになる */
  interval_hours: number;
  /** コメントを取り直す対象の上限日数（歌単は配信後に貼られることが多い） */
  refresh_days: number;
  last_run_at?: string;
  last_run_note?: string;
  last_run_error?: string;
}

/** 自動処理を 1 回走らせた結果。 */
export interface AutoFillRunResult {
  channels: number;
  synced: number;
  refreshed: number;
  fill_run_id?: string;
  note?: string;
}

export interface BatchAnalyzeStatus {
  running: boolean;
  mode?: string;
  singer_id?: string;
  hidden?: 'all' | 'true' | 'false'; // 非表示配信の扱い（何が走っているかを画面に出すため）
  total: number;
  done: number;
  failed: number;
  /**
   * live chat の取得待ちで見送った件数。**失敗ではない**（次の実行で拾われる）。
   * 完了に混ぜると、その配信の end が付かないまま「終わった」ことになる。
   */
  deferred: number;
  current?: string;
  failed_ids?: string[];
  message?: string;
}

// ========== 直接パフォーマンス作成 ==========

export interface SongSuggestion {
  name: string;
  original_artist: string;
  start_seconds: number;
  end_seconds: number;
  tags: string[];
  singer_ids: string[];
  art_url?: string;
  itunes_id?: number; // Holodex が提供する iTunes ID

  // Chat の拍手検出結果（Holodex に明記された end との比較用。CommentSong と対称）
  chat_end?: number;
  end_diff?: number; // |end - chat_end|。両方に値がある場合のみ設定される

  // analyzeSongs 時に折り込んだ正規化結果（あれば）
  normalized_name?: string;
  normalized_name_reading?: string;
  normalized_artist?: string;
  normalized_artist_reading?: string;
  confidence?: number;
  matched_song_id?: string;
  matched_song_name?: string;
  matched_song_name_reading?: string;
  matched_song_artist?: string;
  matched_song_artist_reading?: string;
  matched_song_art_url?: string;
  matched_song_itunes_id?: number;
  // 自動採用に届かなかった照合候補（別名義・同名異曲など）。UI で人に選ばせる
  match_candidates?: SongMatchCandidate[];
  artist_alias?: ArtistAliasProposal;
  changes?: FieldChange[];
}

export interface LoadHolodexSongsResponse {
  stream_id: string;
  stream_title: string;
  channel_owner: Singer;
  participants: Singer[];  // すべての参加者（チャンネル所有者を含む）
  songs: SongSuggestion[];
}

export interface CreatePerformanceItem {
  name: string;
  name_reading?: string;
  original_artist: string;
  original_artist_reading?: string;
  start_seconds: number;
  end_seconds: number;
  tags: string[];
  singer_ids: string[];
  art_url?: string;
  itunes_id?: number; // Holodex が提供する iTunes ID
  custom_tags?: string[];
  /** 終了時間の由来。省略すると unknown 扱い（docs/DATA_COMPLETION.md） */
  end_source?: EndSource;
  /** 人が値を見て認めたか。編集画面からの保存は既定で true。自動生成は false を明示する */
  end_confirmed?: boolean;
}

export interface CreatePerformancesRequest {
  performances: CreatePerformanceItem[];
}

export interface CreatePerformancesResponse {
  created_count: number;
}

// ========== AI 正規化 ==========

export interface AINormalizationItem {
  name: string;
  original_artist: string;
  art_url?: string;
  itunes_id?: number;
}

export interface BatchAINormalizationRequest {
  items: AINormalizationItem[];
}

export interface AISuggestionResult {
  index: number;
  normalized_name: string;
  normalized_name_reading: string;
  original_artist: string;
  original_artist_reading: string;
  tags: string[];
  confidence: number;
  reasoning: string;
  matched_song_id?: string;
  match_reason?: string;
  match_score?: number;
  matched_song_name?: string;
  matched_song_name_reading?: string;
  matched_song_artist?: string;
  matched_song_artist_reading?: string;
  matched_song_art_url?: string;
  matched_song_itunes_id?: number;
  // 自動採用に届かなかった照合候補（別名義・同名異曲など）。UI で人に選ばせる
  match_candidates?: SongMatchCandidate[];
  artist_alias?: ArtistAliasProposal;
  changes?: FieldChange[];
}

export interface BatchAINormalizationResponse {
  suggestions: AISuggestionResult[];
  warning?: string;
}

// ========== フィルターキーワード ==========

export interface FilterKeyword {
  id: number;
  keyword: string;
  type: 'filter' | 'keep';
  created_at: string;
}

// ========== アーティスト ==========

export interface Artist {
  id: string;
  name: string;
  name_reading?: string;
  song_count: number;
}

export interface ArtistListResponse {
  artists: Artist[];
  pagination: PaginationResponse;
}

export interface ArtistDetailResponse {
  artist: Artist;
  songs: Song[];
  pagination: PaginationResponse;
}

export interface BackfillReadingsResponse {
  artists_updated: number;
  songs_updated: number;
  warning?: string;
}

// 読みのエクスポート / インポート
export interface ReadingItem {
  id: string;
  name: string;
  reading: string;
}

export interface ReadingsExport {
  artists: ReadingItem[];
  songs: ReadingItem[];
}

export interface ImportReadingsResult {
  artists_updated: number;
  songs_updated: number;
  skipped: number;
  errors?: string[];
}

// 読みの整備状況（管理画面の残件表示用）。needs_fix は「名前に漢字を含むのに読みが空 or 読みに漢字が残る」件数
export interface ReadingsStats {
  artists_total: number;
  artists_needs_fix: number;
  songs_total: number;
  songs_needs_fix: number;
}

// 修正提案（閲覧モードからの提案 → 管理者レビュー）
export type SuggestionTargetType = 'song' | 'artist' | 'performance' | 'stream';
// conflict … 提案後に対象が変更された状態。承認すると他人の編集を巻き戻すため保留される
export type SuggestionStatus = 'pending' | 'approved' | 'rejected' | 'conflict';

// 提案の種別
// field        … 既存レコードのフィールド差し替え
// perf.missing … 未登録曲の追加報告
// perf.meta    … 歌唱の曲の差し替え（「この曲ではない」）
export type SuggestionKind = 'field' | 'perf.missing' | 'perf.meta';

// 「この歌唱は別の曲だ」という指摘の中身。
// song_id があれば既存の曲へ、無ければ曲名から探す／作る。
export interface SongSwapPayload {
  song_id: string;
  song_name: string;
  original_artist: string;
  current_song_name: string; // 提案時点の曲名（レビュー時の表示・衝突判定用）
  // 未登録の曲へ差し替えるとき（song_id が空）だけ効く。承認時の曲作成に渡る。
  // 落とすと差し替えから作った曲だけジャケットも読みも iTunes も付かない
  itunes_id?: number;
  art_url?: string;
  name_reading?: string;
  original_artist_reading?: string;
}

// 「この配信のこの時点に、登録されていない曲がある」という報告の中身
// 「この配信のこの時点に曲がある」という報告の中身。
// 前半は閲覧者の投稿でも埋まる。後半は一括セットリスト作成が審査へ回すときに付ける
// 照合結果と監査情報で、人手の投稿では空になる。
export interface MissingSongPayload {
  stream_id: string;
  song_name: string;
  original_artist: string;
  start_seconds: number;
  end_seconds: number; // 0 = 未指定（動画の最後まで）

  // 照合済みの内容。song_id があれば承認は曲名から引き直さない
  song_id?: string;
  singer_ids?: string[];
  end_source?: string;
  tags?: string[];
  itunes_id?: number;
  // 編集画面が運ぶ欄と揃える。運ばないと承認から作った曲だけジャケット画像も読みも付かない
  art_url?: string;
  name_reading?: string;
  original_artist_reading?: string;
  custom_tags?: string[];

  // 監査（どういう経緯でこの提案になったか）
  review_reasons?: string[]; // no_end / no_artist / unmatched / …
  source?: string; // holodex / comment
  via?: string; // rule / ai
  confidence?: number;
  ai_reason?: string;
  batch_run_id?: string;

  // 抽出したままの表記（正規化・照合で書き換わる前）
  raw_name?: string;
  raw_artist?: string;

  // 決めきれなかったときの候補（審査画面でそのまま選べる）
  candidates?: SongMatchCandidate[];

  // 既存の歌唱と食い違った理由。「食い違う」だけでは何を見ればいいか分からないので、
  // どこが違うのか（song / start / end）と相手を持ち越す。
  // addition は「既にセットリストがある配信への追加」で、対応する既存の歌唱が無い。
  conflict_kind?: string;
  existing?: ExistingPerformance;
}

// 審査画面で「既存はこうなっている」と並べて見せるための最小限。
export interface ExistingPerformance {
  id: string;
  song_name: string;
  original_artist: string;
  start_seconds: number;
  end_seconds: number;
}

export interface CreateSuggestionRequest {
  target_type?: SuggestionTargetType;
  target_id?: string;
  kind?: SuggestionKind; // 省略時は field
  fields?: Record<string, string>;
  payload?: MissingSongPayload; // kind = perf.missing のとき
  song_swap?: Omit<SongSwapPayload, 'current_song_name'>; // kind = perf.meta のとき
  note?: string;
}

// 提案時点の値（expected）と現在の値（current）のズレ
export interface FieldConflict {
  expected: string;
  current: string;
}

// 未登録曲の追加提案と時間が重なる既存の歌唱（メドレー等で正当に重なることもある）
export interface OverlapInfo {
  song_name: string;
  start_seconds: number;
  end_seconds: number;
}

// timing 提案の自動適用条件（管理画面から調整できる）
export interface AutoApplySettings {
  enabled: boolean;
  min_votes: number;
  max_spread_seconds: number;
  max_delta_seconds: number;
}

export interface Suggestion {
  id: string;
  target_type: SuggestionTargetType;
  target_id: string;
  target_key: string; // 配信の YouTube 動画 ID（UUID 対象では空）
  target_label: string;
  kind: SuggestionKind;
  before: Record<string, string>;
  after: Record<string, string>;
  payload?: MissingSongPayload; // kind = perf.missing のときだけ
  // 提案の時間帯に既存の歌唱があるとき（perf.missing のみ）。承認は止まらないが確認が要る
  overlaps?: OverlapInfo[];
  song_swap?: SongSwapPayload; // kind = perf.meta のときだけ
  note: string;
  status: SuggestionStatus;
  // 未処理の提案で、対象が提案後に変更されたフィールド。空でなければ承認前に確認が要る
  conflicts?: Record<string, FieldConflict>;
  created_by?: string | null;
  created_by_name: string; // 匿名投稿では空
  review_note: string;
  created_at: string;
  reviewed_at?: string | null;
}

export interface SuggestionListResponse {
  suggestions: Suggestion[];
  pagination: PaginationResponse;
}

// 同一対象に集まった提案。同じ歌唱への通報を1枚のカードで捌くための単位。
export interface SuggestionGroup {
  target_type: SuggestionTargetType;
  target_id: string;
  target_key: string;
  target_label: string;
  current: Record<string, string>; // 対象の現在値（提案と見比べるため）
  suggestions: Suggestion[];
}

// ページングの単位はグループ（対象）
export interface SuggestionGroupListResponse {
  groups: SuggestionGroup[];
  pagination: PaginationResponse;
}

export interface BatchReviewResult {
  id: string;
  ok: boolean;
  error?: string;
  conflict?: boolean; // 対象が変更済みで止まった
}

export interface BatchReviewResponse {
  succeeded: number;
  failed: number;
  results: BatchReviewResult[];
}

// 同一対象に集まった提案を、管理者が決めた値へ統合して反映する。
// 「どれか1つを丸ごと採用」では表せない決着（中央値・誰も出していない値）のための操作。
export interface MergeSuggestionsRequest {
  target_type: SuggestionTargetType;
  target_id: string;
  fields: Record<string, string>; // 実際に反映する値
  ids: string[]; // このグループの提案（すべて処理済みにする）
  note?: string;
}

export interface MergeSuggestionsResponse {
  applied: Record<string, string>;
  approved: number; // 採用値と一致していた提案
  rejected: number; // 別の値になった提案
}

// ========== グローバル検索 ==========

export interface SearchStreamItem {
  id: string;
  title: string;
  stream_date: string;
  thumbnail_url?: string;
  is_processed?: boolean;
  is_hidden: boolean;
}

export interface SearchTagItem {
  id: string;
  display_name: string;
  color: string;
  count: number;
}

export interface GlobalSearchResponse {
  query: string;
  video_id?: string;      // 入力が YouTube URL / video ID のとき
  video_registered: boolean;
  songs: Song[];
  streams: SearchStreamItem[];
  singers: Singer[];
  artists: Artist[];
  stream_tags: SearchTagItem[];
  performance_tags: SearchTagItem[];
}

export interface TagPerformanceListResponse {
  performances: Performance[];
  pagination: PaginationResponse;
}

// ホームのランダム再生用（ページングなし）
export interface PerformanceListResponse {
  performances: Performance[];
}

// タイトル自動タグ付けルール（キーワードを含めば stream tag を付与）
export interface TagKeywordRule {
  id: number;
  tag_id: string;
  keyword: string;
  created_at: string;
}

// ========== AI Provider ==========

export interface AIProvider {
  id: number;
  name: string;
  base_url: string;
  model: string;
  enabled: boolean;
  priority: number;
  timeout_seconds: number;
  has_key: boolean;
  key_hint?: string;
}

export interface AIProviderInput {
  name: string;
  base_url: string;
  model: string;
  api_key?: string;
  enabled?: boolean;
  priority?: number;
  timeout_seconds?: number;
}

// プロバイダーから取得した chat 利用可能なモデル情報
export interface AIModelInfo {
  id: string;
  display_name?: string;
  context_window?: number;
  description?: string;
}

// ========== 汎用レスポンス ==========

export interface ErrorResponse {
  error: string;
  message?: string;
}

export interface SuccessResponse {
  success: boolean;
  message?: string;
}

export interface LogEntry {
  time: string;
  level: string;
  message: string;
}

export interface LogsResponse {
  logs: LogEntry[];
  level: string;
}

// ========== 終了時間推定 ==========

export interface SongEndTimeEstimateRequest {
  start: number;
  end: number;
  name: string;
  artist: string;
  itunes_id?: number;
  next_start?: number;
  stream_end?: number;
}

export interface SongEndTimeEstimate {
  estimated_end: number;
  is_end_time_estimated: boolean;
  method: string; // "from_comment", "from_next_song", "from_itunes", "from_default"
  original_itunes_dur?: number;
  reason?: string;
}

export interface EstimateEndTimesRequest {
  songs: SongEndTimeEstimateRequest[];
  stream_end: number;
  stream_title?: string;
}

export interface EstimateEndTimesResponse {
  estimates: SongEndTimeEstimate[];
  message?: string;
}

// ========== 認証・ACL ==========

export interface AuthUser {
  id: string;
  username: string;
  display_name: string;
  role: string;          // roles.name
  role_id: string;
  permissions: string[]; // role の権限（'*' は全権限）
  is_active: boolean;
  last_login?: string | null;
  created_at?: string;
  updated_at?: string;
}

export interface LoginResponse {
  token: string;
  user: AuthUser;
}

export interface Role {
  id: string;
  name: string;
  description: string;
  permissions: string[];
  is_system: boolean;
  created_at?: string;
  updated_at?: string;
}

export interface PermissionInfo {
  key: string;
  description: string;
}

export interface CreateUserRequest {
  username: string;
  display_name: string;
  password: string;
  role_id: string;
  is_active: boolean;
}

export interface UpdateUserRequest {
  display_name: string;
  role_id: string;
  is_active: boolean;
}

// ========== 訪客／利用者活動（要 users:manage） ==========

export interface VisitorActivity {
  id: number;
  visit_date: string;
  ip_address: string;
  user_id?: string | null;
  username: string;
  display_name: string;
  first_seen: string;
  last_seen: string;
  page_views: number;
  last_path: string;
  user_agent: string;
}

export interface ActivityListResponse {
  activity: VisitorActivity[];
  total: number;
  page: number;
  limit: number;
  retention_days: number;
}

export interface ActivityStats {
  unique_ips: number;
  authenticated_users: number;
  page_views: number;
  anonymous_ips: number;
}

export interface ActivityStatsResponse {
  stats: ActivityStats;
  retention_days: number;
}

export interface UserActivitySummary {
  user_id: string;
  last_ip_address: string;
  last_seen?: string | null;
  page_views: number;
  distinct_ips: number;
  active_sessions: number;
}

export interface UserActivitySummaryResponse {
  users: UserActivitySummary[];
  retention_days: number;
}

// ========== DB バックアップ ==========

export interface BackupSettings {
  auto_enabled: boolean;
  interval_hours: number;
  retention_local: number;
  retention_drive: number;
  drive_upload: boolean;
  drive_folder_id?: string;
  last_backup_at?: string | null;
  last_backup_status?: string;
}

export interface BackupFileInfo {
  name: string;
  size: number;
  modified_at: string;
}

export interface DriveStatus {
  configured: boolean;
  connected: boolean;
  email?: string;
  folder_name?: string;
}

export interface BackupStatusResponse {
  settings: BackupSettings;
  backups: BackupFileInfo[];
  gdrive: DriveStatus;
}

export interface BackupResult {
  name: string;
  size: number;
  drive_uploaded: boolean;
  drive_error?: string;
}

export interface DriveDeviceAuth {
  device_code: string;
  user_code: string;
  verification_url: string;
  expires_in: number;
  interval: number;
}

export interface DriveFile {
  id: string;
  name: string;
  size?: number;
  createdTime?: string;
  mimeType?: string;
}

// ========== プレイリスト ==========

export type PlaylistVisibility = 'private' | 'unlisted' | 'public';

export interface Playlist {
  id: string;
  name: string;
  description: string;
  visibility: PlaylistVisibility;
  /** 所有者にだけ返る限定公開キー */
  share_slug?: string;
  item_count: number;
  owner_name: string;
  is_owner: boolean;
  created_at: string;
  updated_at: string;
}

export interface PlaylistListResponse {
  playlists: Playlist[];
  total: number;
}

export interface CreatePlaylistRequest {
  name: string;
  description?: string;
  visibility?: PlaylistVisibility;
}

export interface UpdatePlaylistRequest {
  name?: string;
  description?: string;
  visibility?: PlaylistVisibility;
}

/** プレイリストへ一括追加した結果。skipped は既に入っていて飛ばした件数 */
export interface AddPlaylistItemsResult {
  added: number;
  skipped: number;
}

/**
 * 運営が用意したプリセットプレイリスト（コラボ・睡眠導入など）。
 * 中身は毎回サーバー側で計算されるので、フォローしていれば新しい歌唱が自動で入る。
 * 中身を固定したい場合は copy で自分のプレイリストへ複製する。
 */
export interface PresetPlaylist {
  key: string;
  name: string;
  description: string;
  item_count: number;
  is_following: boolean;
}

export interface PresetPlaylistListResponse {
  presets: PresetPlaylist[];
}

/** プリセットをプレイリストへ入れた結果。skipped は既に入っていて飛ばした件数 */
export interface AddPresetToPlaylistResult {
  playlist: Playlist;
  added: number;
  skipped: number;
  created: boolean;
}

// ========== 外部アカウント連携 ==========

export interface OAuthIdentity {
  id: string;
  user_id: string;
  provider: string;
  provider_user_id: string;
  email?: string | null;
  display_name?: string | null;
  avatar_url?: string | null;
  created_at: string;
  updated_at: string;
}

// ========== 外部サービス連携の設定 ==========

export interface SecretFieldStatus {
  configured: boolean;
  /** 末尾4文字のヒント。値そのものは API から返らない */
  hint?: string;
  /** true なら .env 由来。UI で保存すると DB 側が優先される */
  from_env: boolean;
}

export interface IntegrationSettings {
  /** false なら SETTINGS_ENCRYPTION_KEY 未設定で、機密を保存できない */
  encryption_enabled: boolean;
  secrets: Record<string, SecretFieldStatus>;
  plain: Record<string, string>;
  plain_from_env: Record<string, boolean>;
}

export interface UpdateIntegrationSettingsRequest {
  /** 項目名 -> 新しい値。空文字は「変更なし」として無視される */
  secrets?: Record<string, string>;
  /** 明示的に消す項目名 */
  clear?: string[];
  google_drive_client_id?: string;
  google_signin_client_id?: string;
}

// ========== 楽曲の照合・統合候補 ==========

// 既存楽曲との照合候補。score >= 0.85 が自動採用（is_match）、
// それ未満は「似ているが決められない」ので人に選ばせる。
export interface SongMatchCandidate {
  song_id: string;
  name: string;
  artist: string;
  score: number;
  reason: string;
  art_url?: string;
  is_match: boolean;
}

// 「この表記はこの曲ではない」という否決 1 件。
//
// 否決は照合の候補からその曲を外し続け、AI にも聞き直さない。効き続けるものなので
// 見直せる必要がある（source: ai = AI の判定、review = 人が審査画面で否決）。
export interface SongIdentityCheck {
  pair_key: string;
  name_key: string;   // 抽出された表記の曲名キー（DB の曲名ではない）
  artist_key: string;
  song_id: string;
  song_name: string;
  song_artist: string;
  source: string;
  note: string;
  checked_at: string;
}

export interface MergeCandidateSong {
  id: string;
  name: string;
  original_artist: string;
  art_url?: string;
  performance_count: number;
  itunes_ids?: number[];
  role?: string; // AI が説明したこの曲の立ち位置
}

// AI の見立て。「統合すべきか」の決定ではなく、判断材料。
export interface MergeVerdict {
  same_composition?: boolean;
  same_arrangement?: boolean;
  recommendation?: 'merge' | 'keep_separate' | string;
  note?: string;
  source?: string;
  judged: boolean;
}

// 照合が外れて新曲として登録された疑いのある組。統合して畳むためのレビュー対象。
export interface MergeCandidate {
  id: string;
  score: number;
  reason: string;
  origin: 'create' | 'scan' | string;
  new_song: MergeCandidateSong;
  existing_song: MergeCandidateSong;
  verdict?: MergeVerdict;
}

// ========== 照合の学習層 ==========

// 同一人物としてまとめられたアーティスト名義。source='ai' は AI が判定したもので、
// 誤っていれば解除できる（解除は「別人」という判定として残る）。
export interface BuildVersion {
  commit: string;
  built_at: string;
}

// ========== 一括セットリスト作成 ==========

// 一括プレ分析（BatchAnalyzeStatus）とは別物。あちらは抽出だけで主データを触らないが、
// こちらは performances に直接書く（docs/SETLIST_FLOW.md）。
export interface BatchFillStatus {
  running: boolean;
  mode?: string;
  // 対象チャンネル（空なら全部）。include_collabs が false なら
  // そのチャンネルが所有する配信だけが対象
  singer_ids?: string[];
  include_collabs?: boolean;
  run_id?: string;
  phase?: 'ai' | 'write';  // 未設定なら配信を読んでいる最中
  total: number;
  done: number;
  current?: string;
  created: number;
  review: number;
  /** 「DB にあるが入力元に無い」歌唱の件数（force 実行のみ） */
  gaps: number;
  ai_asked: number;
  // 入力元を確定できず今回は扱わなかった配信（done とは別に数える）
  skipped?: number;
  skipped_ids?: string[];
  message?: string;
}

// force 実行が見つけた「DB にあるが入力元に無い」歌唱 1 件。
// 提案としては積まないので、実行履歴からここへ辿るのが唯一の入口。
export interface BatchFillGap {
  stream_id: string;
  stream_title: string;
  performance_id: string;
  song_name: string;
  start_seconds: number;
}

export interface BatchFillRun {
  id: string;
  mode: string;
  singer_id?: string;
  status: 'running' | 'done' | 'cancelled' | 'failed' | 'reverted';
  streams_total: number;
  streams_done: number;
  songs_created: number;
  songs_review: number;
  /** 「DB にあるが入力元に無い」と分かった既存の歌唱の件数（force 実行のみ） */
  songs_gap: number;
  ai_asked: number;
  message: string;
  started_at: string;
  finished_at?: string;
  started_by_name?: string;
}
