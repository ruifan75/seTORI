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

export interface Song {
  id: string;
  name: string;
  name_reading?: string;
  original_artist: string;
  original_artist_reading?: string;
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

export interface Performance {
  id: string;
  stream_id: string;
  song_id: string;
  song_name: string;
  original_artist: string;
  arts?: string;
  start_seconds: number;
  end_seconds: number;
  order_index: number;
  tags: PerformanceTag[];
  custom_tags: string[];
  singers: Singer[];
  youtube_url: string;
  created_at: string;
  // タグ検索など配信横断の一覧でのみ設定される
  stream_title?: string;
  stream_date?: string;
  thumbnail_url?: string;
}

export interface StreamDetailResponse extends Stream {
  performances: Performance[];
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
