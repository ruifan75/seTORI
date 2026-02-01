// ========== 分頁 ==========

export interface PaginationResponse {
  page: number;
  limit: number;
  total: number;
  total_pages: number;
}

// ========== 歌曲 ==========

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
  existing_song?: SongBrief; // 如果已在 DB 中，包含歌曲資訊
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

// ========== 演唱者 ==========

export interface Singer {
  id: string;
  name: string;
  english_name?: string;
  photo_url?: string;
  organization?: string;
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
  id: string; // YouTube Channel ID
  name: string;
  english_name?: string;
  photo_url?: string;
  organization?: string;
}

// ========== 歌回 ==========

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
  participants: Singer[];  // 參與者
  channel_owner?: Singer;  // 頻道擁有者
  is_processed: boolean;   // 處理完成
  is_hidden: boolean;      // 隱藏
  holodex_timeline_songs?: SongSuggestion[];  // Holodex timeline 資料
  comment_timeline_songs?: CommentSong[];     // Comment timeline 資料
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

// ========== 演出 ==========

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
  singers: Singer[];
  youtube_url: string;
  created_at: string;
}

export interface StreamDetailResponse extends Stream {
  performances: Performance[];
}

// 用於歌曲詳情頁的反向查詢（也用於演唱者詳情頁）
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
  singers: Singer[];
  youtube_url: string;
  created_at: string;
}

export interface SongPerformanceListResponse {
  song: Song;
  performances: SongPerformance[];
  pagination: PaginationResponse;
}

// ========== Holodex 同步 ==========

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

// ========== Comment 分析 ==========

export interface CommentSong {
  start: number;
  end: number;
  name: string;
  original_artist: string;
  original_comment: string;
  is_end_time_estimated: boolean;
}

export interface AnalyzeCommentsResponse {
  songs: CommentSong[];
  raw_comments: string[];
}

// ========== 直接建立演出 ==========

export interface SongSuggestion {
  name: string;
  original_artist: string;
  start_seconds: number;
  end_seconds: number;
  tags: string[];
  singer_ids: string[];
  art_url?: string;
  itunes_id?: number; // Holodex 提供的 iTunes ID
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
}

export interface BatchAINormalizationResponse {
  suggestions: AISuggestionResult[];
}

// ========== 通用回應 ==========

export interface ErrorResponse {
  error: string;
  message?: string;
}

export interface SuccessResponse {
  success: boolean;
  message?: string;
}
// ========== 推算結束時間 ==========

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