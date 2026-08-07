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
  country: string;
}

// ========== 歌手 ==========

export interface Singer {
  id: string;
  name: string;
  english_name?: string;
  photo_url?: string;
  organization?: string;
  metadata_source: string;
  can_edit_metadata: boolean;
  created_at: string;
  updated_at: string;
}

export interface SingerListResponse {
  singers: Singer[];
  pagination: PaginationResponse;
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

export interface UpdateSingerRequest {
  name: string;
  english_name?: string;
  photo_url?: string;
  organization?: string;
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
  is_processed: boolean;   // 処理済み
  is_hidden: boolean;      // 非表示
  holodex_timeline_songs?: SongSuggestion[];  // Holodex タイムライン データ
  comment_timeline_songs?: CommentSong[];     // コメント解析済みタイムライン（分析キャッシュ）
  has_comment_raw?: boolean;                  // 分析可能な生コメントがあるか
  created_at: string;
  updated_at: string;
}

export interface StreamListResponse {
  streams: Stream[];
  pagination: PaginationResponse;
}

export interface UpdateStreamRequest {
  title?: string;
  stream_date?: string;
  tag_ids?: string[];
  participant_ids?: string[];
  is_processed?: boolean;
  is_hidden?: boolean;
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

// ========== コメント解析 ==========

export interface CommentSong {
  start: number;
  end: number;
  name: string;
  original_artist: string;
  original_comment: string;
  is_end_time_estimated: boolean;

  // Chat 拍手偵測結果（用來跟 comment explicit end 比較）
  chat_end?: number;
  end_diff?: number; // |end - chat_end|，只有兩邊都有值時才會有

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
}

export interface AnalyzeCommentsResponse {
  songs: CommentSong[];
  raw_comments: string[];
  warning?: string; // AI 正規化が失敗し抽出のみで返した場合
}

// 未処理配信の一括プレ分析ジョブの進捗
export interface BatchAnalyzeStatus {
  running: boolean;
  mode?: string;
  singer_id?: string;
  total: number;
  done: number;
  failed: number;
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
  itunes_id?: number; // Holodex 提供的 iTunes ID

  // Chat 拍手偵測結果（Holodex 明示 end との比較用。CommentSong と対称）
  chat_end?: number;
  end_diff?: number; // |end - chat_end|，只有兩邊都有值時才會有

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
}

export interface LoadHolodexSongsResponse {
  stream_id: string;
  stream_title: string;
  channel_owner: Singer;
  participants: Singer[];  // 所有參與者（包含頻道擁有者）
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
  itunes_id?: number; // Holodex 提供的 iTunes ID
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
  matched_song_name?: string;
  matched_song_name_reading?: string;
  matched_song_artist?: string;
  matched_song_artist_reading?: string;
  matched_song_art_url?: string;
  matched_song_itunes_id?: number;
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
}

// 「この配信のこの時点に、登録されていない曲がある」という報告の中身
export interface MissingSongPayload {
  stream_id: string;
  song_name: string;
  original_artist: string;
  start_seconds: number;
  end_seconds: number; // 0 = 未指定（動画の最後まで）
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
  is_processed: boolean;
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

// 首頁のランダム再生用（ページングなし）
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
