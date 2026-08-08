import axios from 'axios';
import type {
  SongListResponse,
  Song,
  SongPerformanceListResponse,
  CreateSongRequest,
  UpdateSongRequest,
  StreamListResponse,
  StreamDetailResponse,
  UpdateStreamRequest,
  Singer,
  SingerListResponse,
  SingerDetailResponse,
  SingerPerformanceListResponse,
  CreateSingerResponse,
  UpdateSingerRequest,
  SyncHolodexRequest,
  SyncHolodexResponse,
  SongSuggestion,
  MergeCandidate,
  ArtistAliasGroup,
  SongAlias,
  AnalyzeCommentsResponse,
  BatchAnalyzeStatus,
  SuccessResponse,
  LoadHolodexSongsResponse,
  CreatePerformancesRequest,
  CreatePerformancesResponse,
  Performance,
  UpdatePerformanceRequest,
  BatchAINormalizationRequest,
  BatchAINormalizationResponse,
  ITunesSearchResponse,
  ITunesQueryResult,
  EstimateEndTimesRequest,
  EstimateEndTimesResponse,
  FilterKeyword,
  StreamTag,
  PerformanceTag,
  TagKeywordRule,
  GlobalSearchResponse,
  TagPerformanceListResponse,
  PerformanceListResponse,
  Artist,
  ArtistListResponse,
  ArtistDetailResponse,
  BackfillReadingsResponse,
  ReadingsExport,
  ImportReadingsResult,
  CreateSuggestionRequest,
  SuggestionListResponse,
  SuggestionGroupListResponse,
  BatchReviewResponse,
  MergeSuggestionsRequest,
  MergeSuggestionsResponse,
  AutoApplySettings,
  SuggestionStatus,
  AIProvider,
  AIProviderInput,
  AIModelInfo,
  LogsResponse,
  AuthUser,
  LoginResponse,
  Role,
  PermissionInfo,
  CreateUserRequest,
  UpdateUserRequest,
  IntegrationSettings,
  UpdateIntegrationSettingsRequest,
  OAuthIdentity,
  Playlist,
  PlaylistListResponse,
  CreatePlaylistRequest,
  UpdatePlaylistRequest,
  BackupStatusResponse,
  BackupSettings,
  BackupResult,
  DriveDeviceAuth,
  DriveStatus,
  DriveFile,
  BuildVersion,
} from './types';

const DEFAULT_API_BASE_URL =
  typeof window !== 'undefined'
    ? `${window.location.protocol}//${window.location.hostname}:8080`
    : 'http://localhost:8080';
// 本番は Caddy が同一オリジンで静的ファイルと /api の両方を配信するため、
// ビルド時に VITE_API_URL="" を渡して相対パスにする。
// 空文字は「同一オリジン」という指定なので ?? で受ける（|| だと既定値に落ちる）。
const API_BASE_URL = import.meta.env.VITE_API_URL ?? DEFAULT_API_BASE_URL;

const api = axios.create({
  baseURL: API_BASE_URL,
  headers: {
    'Content-Type': 'application/json',
  },
});

// 認證：ログイン中のセッショントークンを保持。無い場合は環境変数 VITE_API_TOKEN
// （旧来の静的トークン）にフォールバックする。auth store が setAuthToken で更新する。
const ENV_API_TOKEN = import.meta.env.VITE_API_TOKEN as string | undefined;
let authToken: string | null = null;

export function setAuthToken(token: string | null) {
  authToken = token;
}

api.interceptors.request.use((config) => {
  const token = authToken || ENV_API_TOKEN;
  if (token) {
    config.headers.Authorization = `Bearer ${token}`;
  }
  return config;
});

// セッション失効（401）時に呼ばれるハンドラ。auth store が登録する。
let onUnauthorized: (() => void) | null = null;
export function setUnauthorizedHandler(fn: (() => void) | null) {
  onUnauthorized = fn;
}

// 錯誤攔截器 - 提取後端錯誤訊息
api.interceptors.response.use(
  (response) => response,
  (error) => {
    const url: string = error.config?.url ?? '';
    // ログイン以外で 401 の場合はセッション失効とみなしてログアウト処理を促す
    if (error.response?.status === 401 && !url.includes('/api/auth/login')) {
      onUnauthorized?.();
    }
    // 提取後端回傳的錯誤訊息
    if (error.response?.data?.error) {
      error.message = error.response.data.error;
    } else if (!error.response) {
      error.message = 'サーバーに接続できません';
    }
    return Promise.reject(error);
  }
);

// ========== 歌曲 API ==========

export const songApi = {
  list: async (page = 1, limit = 20, search?: string, sort?: string, dir?: string): Promise<SongListResponse> => {
    const params = new URLSearchParams({ page: String(page), limit: String(limit) });
    if (search) params.set('search', search);
    if (sort) params.set('sort', sort);
    if (dir) params.set('dir', dir);
    const { data } = await api.get(`/api/songs?${params}`);
    return data;
  },

  get: async (id: string): Promise<Song> => {
    const { data } = await api.get(`/api/songs/${id}`);
    return data;
  },

  getPerformances: async (id: string, page = 1, limit = 20): Promise<SongPerformanceListResponse> => {
    const params = new URLSearchParams({ page: String(page), limit: String(limit) });
    const { data } = await api.get(`/api/songs/${id}/performances?${params}`);
    return data;
  },

  create: async (song: CreateSongRequest): Promise<Song> => {
    const { data } = await api.post('/api/songs', song);
    return data;
  },

  update: async (id: string, song: UpdateSongRequest): Promise<Song> => {
    const { data } = await api.put(`/api/songs/${id}`, song);
    return data;
  },

  delete: async (id: string): Promise<{ message: string; id: string }> => {
    const { data } = await api.delete(`/api/songs/${id}`);
    return data;
  },

  merge: async (sourceSongId: string, targetSongId: string): Promise<{ message: string; source_id: string; target_id: string; target_song: Song }> => {
    const { data } = await api.post(`/api/songs/${sourceSongId}/merge`, { target_song_id: targetSongId });
    return data;
  },

  // 照合が外れて新曲として登録された疑いのある組。統合して畳むためのレビュー用。
  mergeCandidates: async (limit = 50): Promise<{ candidates: MergeCandidate[]; total: number }> => {
    const { data } = await api.get(`/api/songs/merge-candidates?limit=${limit}`);
    return data;
  },

  mergeCandidatesForSong: async (songId: string): Promise<{ candidates: MergeCandidate[] }> => {
    const { data } = await api.get(`/api/songs/${songId}/merge-candidates`);
    return data;
  },

  dismissMergeCandidate: async (id: string): Promise<{ message: string }> => {
    const { data } = await api.post(`/api/songs/merge-candidates/${id}/dismiss`);
    return data;
  },

  // 既存データを走査して同名の組を候補に積む（取り込み前からある重複を拾う）
  scanDuplicates: async (): Promise<{ added: number; message: string }> => {
    const { data } = await api.post('/api/songs/merge-candidates/scan');
    return data;
  },

  // 未判定の候補について AI の見立てを取る。統合は実行しない
  adjudicateDuplicates: async (): Promise<{ judged: number; message: string }> => {
    const { data } = await api.post('/api/songs/merge-candidates/adjudicate');
    return data;
  },
};

// ========== 照合の学習層 API ==========
//
// アーティストの別名義（松任谷由実 = 荒井由実）と、統合から学習した楽曲の別表記。
// どちらも照合の結果を左右するので content:edit が要る。

export const aliasApi = {
  listArtists: async (): Promise<{ groups: ArtistAliasGroup[] }> => {
    const { data } = await api.get('/api/aliases/artists');
    return data;
  },

  linkArtists: async (nameA: string, nameB: string, note?: string): Promise<{ message: string }> => {
    const { data } = await api.post('/api/aliases/artists', { name_a: nameA, name_b: nameB, note });
    return data;
  },

  unlinkArtist: async (nameKey: string): Promise<{ message: string }> => {
    const { data } = await api.delete(`/api/aliases/artists/${encodeURIComponent(nameKey)}`);
    return data;
  },

  listSongs: async (limit = 100): Promise<{ aliases: SongAlias[] }> => {
    const { data } = await api.get(`/api/aliases/songs?limit=${limit}`);
    return data;
  },

  deleteSong: async (nameKey: string, artistKey: string): Promise<{ message: string }> => {
    const params = new URLSearchParams({ name_key: nameKey, artist_key: artistKey });
    const { data } = await api.delete(`/api/aliases/songs?${params}`);
    return data;
  },
};

// ========== 歌回 API ==========

export const streamApi = {
  list: async (page = 1, limit = 20, sort?: string, dir?: string): Promise<StreamListResponse> => {
    const params = new URLSearchParams({ page: String(page), limit: String(limit) });
    if (sort) params.set('sort', sort);
    if (dir) params.set('dir', dir);
    const { data } = await api.get(`/api/streams?${params}`);
    return data;
  },

  get: async (id: string): Promise<StreamDetailResponse> => {
    const { data } = await api.get(`/api/streams/${id}`);
    return data;
  },

  update: async (id: string, req: UpdateStreamRequest): Promise<StreamDetailResponse> => {
    const { data } = await api.put(`/api/streams/${id}`, req);
    return data;
  },

  estimateEndTimes: async (id: string, req: EstimateEndTimesRequest): Promise<EstimateEndTimesResponse> => {
    const { data } = await api.post(`/api/streams/${id}/estimate-end-times`, req);
    return data;
  },
};

// ========== 演唱者 API ==========

export const singerApi = {
  list: async (page = 1, limit = 20, sort?: string, dir?: string): Promise<SingerListResponse> => {
    const params = new URLSearchParams({ page: String(page), limit: String(limit) });
    if (sort) params.set('sort', sort);
    if (dir) params.set('dir', dir);
    const { data } = await api.get(`/api/singers?${params}`);
    return data;
  },

  get: async (id: string): Promise<SingerDetailResponse> => {
    const { data } = await api.get(`/api/singers/${id}`);
    return data;
  },

  search: async (query: string, limit = 10): Promise<Singer[]> => {
    const params = new URLSearchParams({ q: query, limit: String(limit) });
    const { data } = await api.get(`/api/singers/search?${params}`);
    return data;
  },

  getStreams: async (
    id: string,
    page = 1,
    limit = 20,
    processed?: 'all' | 'true' | 'false',
    hidden?: 'all' | 'true' | 'false'
  ): Promise<StreamListResponse> => {
    const params = new URLSearchParams({ page: String(page), limit: String(limit) });
    if (processed) params.set('processed', processed);
    if (hidden) params.set('hidden', hidden);
    const { data } = await api.get(`/api/singers/${id}/streams?${params}`);
    return data;
  },

  getPerformances: async (id: string, page = 1, limit = 20, sort?: string, dir?: string): Promise<SingerPerformanceListResponse> => {
    const params = new URLSearchParams({ page: String(page), limit: String(limit) });
    if (sort) params.set('sort', sort);
    if (dir) params.set('dir', dir);
    const { data } = await api.get(`/api/singers/${id}/performances?${params}`);
    return data;
  },

  // 新增 singer（優先 Holodex，找不到時 fallback 到 YouTube Data API）
  create: async (channelInput: string): Promise<CreateSingerResponse> => {
    const { data } = await api.post('/api/singers', { id: channelInput });
    return data;
  },

  update: async (id: string, req: UpdateSingerRequest): Promise<Singer> => {
    const { data } = await api.put(`/api/singers/${id}`, req);
    return data;
  },
};

// ========== Holodex 同步 API ==========

export const holodexApi = {
  syncChannel: async (req: SyncHolodexRequest): Promise<SyncHolodexResponse> => {
    const { data } = await api.post('/api/sync/holodex', req);
    return data;
  },

  syncVideo: async (videoId: string): Promise<SyncHolodexResponse> => {
    const { data } = await api.post(`/api/sync/holodex/video/${videoId}`);
    return data;
  },

  syncSetoriToHolodex: async (streamId: string): Promise<SyncHolodexResponse> => {
    const { data } = await api.post(`/api/sync/holodex/to-holodex/${streamId}`);
    return data;
  },

  // stored holodex_data を正規化＋DB照合＋拍手end補完（holodex_hash キャッシュ）。force=true で再分析
  analyzeSongs: async (videoId: string, force = false): Promise<SongSuggestion[]> => {
    const { data } = await api.post(`/api/streams/${videoId}/holodex-songs/analyze${force ? '?force=true' : ''}`);
    return data.songs ?? [];
  },
};

// ========== Comment 分析 API ==========

export const commentApi = {
  getComments: async (videoId: string): Promise<{ video_id: string; comments: string[] }> => {
    const { data } = await api.get(`/api/streams/${videoId}/comments`);
    return data;
  },

  syncYouTube: async (videoId: string): Promise<{ video_id: string; comment_count: number }> => {
    const { data } = await api.post(`/api/streams/${videoId}/comments/sync-youtube`);
    return data;
  },

  analyze: async (videoId: string, force = false): Promise<AnalyzeCommentsResponse> => {
    const { data } = await api.post(`/api/streams/${videoId}/comments/analyze${force ? '?force=true' : ''}`);
    return data;
  },

  // 指定した開始秒数の拍手 end 推定（生コメントからの単曲追加用）。キーは start 秒の文字列
  estimateChatEnds: async (videoId: string, starts: number[]): Promise<{ ends: Record<string, number> }> => {
    const { data } = await api.post(`/api/streams/${videoId}/chat-end-estimate`, { starts });
    return data;
  },

  // 分析済みの曲に対して live chat の拍手から end だけを取り直す（AI は呼ばない）。
  // 一括プレ分析はキャッシュ命中だと拍手 end を飛ばすので、その取りこぼしを埋める用。
  // live chat のダウンロードを伴うため、初回は数十秒〜数分かかることがある。
  analyzeChatEnds: async (
    videoId: string,
  ): Promise<{ id: string; total: number; filled: number; changed: number }> => {
    const { data } = await api.post(`/api/streams/${videoId}/analyze-chat-ends`);
    return data;
  },
};

// ========== 一括プレ分析 API ==========

export const batchAnalyzeApi = {
  // 一括プレ分析を開始（背景ジョブ・singleton）
  // mode: unanalyzed（未分析のみ）/ unprocessed（未処理すべて）/ refresh（コメント再取得）/ reanalyze（全部再分析）
  // singerId: 対象チャンネル（空なら全チャンネル）
  start: async (mode: string, singerId?: string): Promise<{ message: string }> => {
    const { data } = await api.post('/api/streams/batch-analyze', {
      mode,
      singer_id: singerId ?? '',
    });
    return data;
  },

  cancel: async (): Promise<{ message: string }> => {
    const { data } = await api.post('/api/streams/batch-analyze/cancel');
    return data;
  },

  status: async (): Promise<BatchAnalyzeStatus> => {
    const { data } = await api.get('/api/streams/batch-analyze/status');
    return data;
  },
};

// ========== 演出 API ==========

export const performanceApi = {
  // 從 Holodex 載入歌曲（不加入正規化佇列）
  loadHolodexSongs: async (streamId: string): Promise<LoadHolodexSongsResponse> => {
    const { data } = await api.get(`/api/streams/${streamId}/holodex-songs`);
    return data;
  },

  // 直接建立演出記錄
  create: async (streamId: string, req: CreatePerformancesRequest): Promise<CreatePerformancesResponse> => {
    const { data } = await api.post(`/api/streams/${streamId}/performances`, req);
    return data;
  },

  // 刪除指定 stream 的所有演出記錄
  deleteAll: async (streamId: string): Promise<SuccessResponse> => {
    const { data } = await api.delete(`/api/streams/${streamId}/performances`);
    return data;
  },

  // 歌唱1件を取得（配信・楽曲情報付き）
  get: async (id: string): Promise<Performance> => {
    const { data } = await api.get(`/api/performances/${id}`);
    return data;
  },

  // 歌唱1件だけを更新（要 content:edit）。
  // セットリスト全体を送り直す create と違い、他の曲を巻き込まない。
  update: async (id: string, req: UpdatePerformanceRequest): Promise<Performance> => {
    const { data } = await api.put(`/api/performances/${id}`, req);
    return data;
  },
};

// ========== AI 正規化 API ==========

export const aiApi = {
  // 批量 AI 正規化
  normalize: async (req: BatchAINormalizationRequest): Promise<BatchAINormalizationResponse> => {
    const { data } = await api.post('/api/ai/normalize', req);
    return data;
  },
};

// ========== iTunes API ==========

export const itunesApi = {
  // 搜尋 iTunes
  search: async (songName: string): Promise<ITunesSearchResponse> => {
    const params = new URLSearchParams({ term: songName });
    const { data } = await api.get(`/api/itunes/search?${params}`);
    return data;
  },

  // 查詢 iTunes ID 取得詳細資訊
  queryById: async (itunesId: number): Promise<ITunesQueryResult> => {
    const { data } = await api.get(`/api/itunes/${itunesId}`);
    return data;
  },
};

// ========== フィルターキーワード API ==========

export const filterKeywordApi = {
  list: async (): Promise<FilterKeyword[]> => {
    const { data } = await api.get('/api/filter-keywords');
    return data;
  },

  create: async (keyword: string, type: 'filter' | 'keep'): Promise<FilterKeyword> => {
    const { data } = await api.post('/api/filter-keywords', { keyword, type });
    return data;
  },

  delete: async (id: number): Promise<void> => {
    await api.delete(`/api/filter-keywords/${id}`);
  },
};

// ========== タグ管理 API ==========

export const tagApi = {
  // Stream tags
  listStreamTags: async (): Promise<StreamTag[]> => {
    const { data } = await api.get('/api/stream-tags');
    return data;
  },

  createStreamTag: async (id: string, displayName: string, color: string): Promise<StreamTag> => {
    const { data } = await api.post('/api/stream-tags', { id, display_name: displayName, color });
    return data;
  },

  deleteStreamTag: async (id: string): Promise<void> => {
    await api.delete(`/api/stream-tags/${id}`);
  },

  // Performance tags
  listPerformanceTags: async (): Promise<PerformanceTag[]> => {
    const { data } = await api.get('/api/performance-tags');
    return data;
  },

  createPerformanceTag: async (id: string, displayName: string, color: string): Promise<PerformanceTag> => {
    const { data } = await api.post('/api/performance-tags', { id, display_name: displayName, color });
    return data;
  },

  deletePerformanceTag: async (id: string): Promise<void> => {
    await api.delete(`/api/performance-tags/${id}`);
  },

  // タグが付いた配信一覧（タグ検索ページ）
  getStreamsByTag: async (tagId: string, page = 1, limit = 20): Promise<StreamListResponse> => {
    const params = new URLSearchParams({ page: String(page), limit: String(limit) });
    const { data } = await api.get(`/api/stream-tags/${encodeURIComponent(tagId)}/streams?${params}`);
    return data;
  },

  // タグが付いた演出一覧（タグ検索ページ）
  getPerformancesByTag: async (tagId: string, page = 1, limit = 20): Promise<TagPerformanceListResponse> => {
    const params = new URLSearchParams({ page: String(page), limit: String(limit) });
    const { data } = await api.get(`/api/performance-tags/${encodeURIComponent(tagId)}/performances?${params}`);
    return data;
  },
};

// ========== 首頁（おすすめ） API ==========

export const homeApi = {
  // 曲単位で重複しないランダムな歌唱。追加読み込み時は既出の曲 ID を除外する。
  randomPerformances: async (limit = 50, excludeSongIds: string[] = []): Promise<PerformanceListResponse> => {
    const params = new URLSearchParams({ limit: String(limit) });
    if (excludeSongIds.length > 0) params.set('exclude_song_ids', excludeSongIds.join(','));
    const { data } = await api.get(`/api/performances/random?${params}`);
    return data;
  },
};

// ========== タイトル自動タグ付けルール API ==========

export const tagRuleApi = {
  list: async (): Promise<TagKeywordRule[]> => {
    const { data } = await api.get('/api/tag-keyword-rules');
    return data;
  },

  create: async (tagId: string, keyword: string): Promise<TagKeywordRule> => {
    const { data } = await api.post('/api/tag-keyword-rules', { tag_id: tagId, keyword });
    return data;
  },

  delete: async (id: number): Promise<void> => {
    await api.delete(`/api/tag-keyword-rules/${id}`);
  },

  // 全配信にルールを適用（既存配信へのバックフィル）。追加されたタグ数を返す。
  backfill: async (): Promise<{ message: string; added: number }> => {
    const { data } = await api.post('/api/tag-rules/backfill');
    return data;
  },
};

// ========== AI Provider API ==========

export const aiProviderApi = {
  list: async (): Promise<AIProvider[]> => {
    const { data } = await api.get('/api/ai-providers');
    return data;
  },

  create: async (input: AIProviderInput): Promise<AIProvider> => {
    const { data } = await api.post('/api/ai-providers', input);
    return data;
  },

  update: async (id: number, input: Partial<AIProviderInput>): Promise<AIProvider> => {
    const { data } = await api.put(`/api/ai-providers/${id}`, input);
    return data;
  },

  delete: async (id: number): Promise<void> => {
    await api.delete(`/api/ai-providers/${id}`);
  },

  // 透過 provider 已儲存的 API key 查詢可用 model 清單
  listModels: async (id: number): Promise<AIModelInfo[]> => {
    const { data } = await api.get(`/api/ai-providers/${id}/models`);
    return data.models ?? [];
  },

  // 用尚未儲存的 base_url + api_key 查詢可用 model（新增表單用）
  previewModels: async (input: { base_url: string; api_key: string }): Promise<AIModelInfo[]> => {
    const { data } = await api.post('/api/ai-providers/models/preview', input);
    return data.models ?? [];
  },
};

// ========== Logs API ==========

export const logsApi = {
  list: async (limit = 100): Promise<LogsResponse> => {
    const { data } = await api.get(`/api/logs?limit=${limit}`);
    return data;
  },

  setLevel: async (level: string): Promise<{ level: string }> => {
    const { data } = await api.put('/api/logs/level', { level });
    return data;
  },
};

// ========== アーティスト API ==========

export const artistApi = {
  list: async (
    page = 1,
    limit = 50,
    search?: string,
    sort?: string,
    dir?: string
  ): Promise<ArtistListResponse> => {
    const params = new URLSearchParams({ page: String(page), limit: String(limit) });
    if (search) params.set('search', search);
    if (sort) params.set('sort', sort);
    if (dir) params.set('dir', dir);
    const { data } = await api.get(`/api/artists?${params}`);
    return data;
  },

  get: async (id: string, page = 1, limit = 20, sort?: string, dir?: string): Promise<ArtistDetailResponse> => {
    const params = new URLSearchParams({ page: String(page), limit: String(limit) });
    if (sort) params.set('sort', sort);
    if (dir) params.set('dir', dir);
    const { data } = await api.get(`/api/artists/${id}?${params}`);
    return data;
  },

  // 名前・読み仮名の修正（名前変更時は所属楽曲の表示テキストも連動更新される）
  update: async (id: string, input: { name?: string; name_reading?: string }): Promise<Artist> => {
    const { data } = await api.put(`/api/artists/${id}`, input);
    return data;
  },

  // source（このアーティスト）を target に統合
  merge: async (sourceId: string, targetArtistId: string): Promise<Artist> => {
    const { data } = await api.post(`/api/artists/${sourceId}/merge`, { target_artist_id: targetArtistId });
    return data;
  },

  // 読み仮名の AI 補完（1回で各対象最大30件、連打で続き）
  backfillReadings: async (): Promise<BackfillReadingsResponse> => {
    const { data } = await api.post('/api/ai/backfill-readings');
    return data;
  },
};

// ========== 読みのエクスポート / インポート API ==========

export const readingApi = {
  // 読みデータを Blob で取得（filter=needs_fix で未整備のみ、format=csv で CSV）
  exportBlob: async (
    filter: 'all' | 'needs_fix',
    format: 'json' | 'csv'
  ): Promise<Blob> => {
    const params = new URLSearchParams();
    if (filter === 'needs_fix') params.set('filter', 'needs_fix');
    if (format === 'csv') params.set('format', 'csv');
    const { data } = await api.get(`/api/readings/export?${params}`, { responseType: 'blob' });
    return data as Blob;
  },

  // JSON で読みデータを取り込む
  importJSON: async (payload: ReadingsExport): Promise<ImportReadingsResult> => {
    const { data } = await api.post('/api/readings/import', payload);
    return data;
  },

  // CSV 文字列で読みデータを取り込む
  importCSV: async (csv: string): Promise<ImportReadingsResult> => {
    const { data } = await api.post('/api/readings/import', csv, {
      headers: { 'Content-Type': 'text/csv' },
    });
    return data;
  },
};

// ========== 修正提案 API ==========

export const suggestionApi = {
  // 修正提案を投稿（閲覧モードでも可・匿名可）
  create: async (req: CreateSuggestionRequest): Promise<{ message: string; id: string }> => {
    const { data } = await api.post('/api/suggestions', req);
    return data;
  },

  // 提案一覧（要 content:edit）。status で絞り込み
  list: async (
    status: SuggestionStatus | '' = 'pending',
    page = 1,
    limit = 20
  ): Promise<SuggestionListResponse> => {
    const params = new URLSearchParams({ page: String(page), limit: String(limit) });
    if (status) params.set('status', status);
    const { data } = await api.get(`/api/suggestions?${params}`);
    return data;
  },

  // 自分が出した提案（要ログイン・権限不要）。取り下げと結果の確認用。
  listMine: async (
    status: SuggestionStatus | '' = '',
    page = 1,
    limit = 20
  ): Promise<SuggestionListResponse> => {
    const params = new URLSearchParams({ page: String(page), limit: String(limit) });
    if (status) params.set('status', status);
    const { data } = await api.get(`/api/suggestions/mine?${params}`);
    return data;
  },

  // 対象ごとにまとめた提案一覧（要 content:edit）。同じ歌唱への通報を1枚で捌くため。
  // ページングの単位はグループ（対象）。
  listGrouped: async (
    status: SuggestionStatus | '' = 'pending',
    page = 1,
    limit = 20
  ): Promise<SuggestionGroupListResponse> => {
    const params = new URLSearchParams({ page: String(page), limit: String(limit), group: 'target' });
    if (status) params.set('status', status);
    const { data } = await api.get(`/api/suggestions?${params}`);
    return data;
  },

  // 複数の提案をまとめて承認/却下。一部が失敗しても残りは処理され、結果は個別に返る。
  batchReview: async (
    ids: string[],
    action: 'approve' | 'reject',
    opts: { force?: boolean; note?: string } = {}
  ): Promise<BatchReviewResponse> => {
    const { data } = await api.post('/api/suggestions/batch', { ids, action, ...opts });
    return data;
  },

  // 同一対象の提案を、管理者が決めた値へ統合して反映する。
  // 「どれか1つを丸ごと採用」では表せない決着（中央値・誰も出していない値）のための操作。
  merge: async (req: MergeSuggestionsRequest): Promise<MergeSuggestionsResponse> => {
    const { data } = await api.post('/api/suggestions/merge', req);
    return data;
  },

  // timing 提案の自動適用条件（要 content:edit）
  getSettings: async (): Promise<AutoApplySettings> => {
    const { data } = await api.get('/api/suggestions/settings');
    return data;
  },

  // 値はサーバー側で安全な範囲に丸められる（丸めた結果が返る）
  updateSettings: async (settings: AutoApplySettings): Promise<AutoApplySettings> => {
    const { data } = await api.put('/api/suggestions/settings', settings);
    return data;
  },

  // 未処理提案数（バッジ用）
  count: async (): Promise<number> => {
    const { data } = await api.get('/api/suggestions/count');
    return data.pending ?? 0;
  },

  // 承認して対象へ反映。提案後に対象が変更されていると 409（conflicts 付き）で止まる。
  // force = true は差分を確認した上で現在値を上書きする場合。
  approve: async (id: string, force = false): Promise<{ message: string }> => {
    const { data } = await api.post(`/api/suggestions/${id}/approve${force ? '?force=1' : ''}`);
    return data;
  },

  reject: async (id: string, note = ''): Promise<{ message: string }> => {
    const { data } = await api.post(`/api/suggestions/${id}/reject`, { note });
    return data;
  },

  // 自分が出した未処理の提案を取り下げる（content:edit なら他人の分も可）。
  // 処理済みのものは 409、他人のものは 404。
  withdraw: async (id: string): Promise<{ message: string }> => {
    const { data } = await api.delete(`/api/suggestions/${id}`);
    return data;
  },
};

// ========== グローバル検索 API ==========

export const versionApi = {
  // 稼働中のバックエンドのビルド。フロント側は自分の値を埋め込みで持つので、
  // 突き合わせてデプロイの取りこぼし（片方だけ入れ替わった状態）を検出する。
  get: async (): Promise<BuildVersion> => {
    const { data } = await api.get('/api/version');
    return data;
  },
};

export const searchApi = {
  global: async (q: string, limit = 5): Promise<GlobalSearchResponse> => {
    const params = new URLSearchParams({ q, limit: String(limit) });
    const { data } = await api.get(`/api/search?${params}`);
    return data;
  },

  // 複合条件の動画検索。非表示を含み、指定した条件はすべて AND で評価される。
  searchStreams: async (opts: {
    q?: string;
    ownerId?: string;
    participantIds?: string[];
    vocalistIds?: string[];
    // 旧クライアント互換の単値指定。
    participantId?: string;
    vocalistId?: string;
    streamTags?: string[];
    performanceTags?: string[];
    // 旧クライアント互換: singerId は participantId として送信する。
    singerId?: string;
    page?: number;
    limit?: number;
  }): Promise<StreamListResponse> => {
    const params = new URLSearchParams({
      page: String(opts.page ?? 1),
      limit: String(opts.limit ?? 20),
    });
    if (opts.q) params.set('q', opts.q);
    if (opts.ownerId) params.set('owner_id', opts.ownerId);
    if (opts.participantIds && opts.participantIds.length > 0) {
      params.set('participant_ids', opts.participantIds.join(','));
    } else if (opts.participantId || opts.singerId) {
      params.set('participant_id', opts.participantId || opts.singerId!);
    }
    if (opts.vocalistIds && opts.vocalistIds.length > 0) {
      params.set('vocalist_ids', opts.vocalistIds.join(','));
    } else if (opts.vocalistId) {
      params.set('vocalist_id', opts.vocalistId);
    }
    if (opts.streamTags && opts.streamTags.length > 0) params.set('tags', opts.streamTags.join(','));
    if (opts.performanceTags && opts.performanceTags.length > 0) {
      params.set('performance_tags', opts.performanceTags.join(','));
    }
    const { data } = await api.get(`/api/streams/search?${params}`);
    return data;
  },
};

// ========== 認證 API ==========

export const authApi = {
  login: async (username: string, password: string): Promise<LoginResponse> => {
    const { data } = await api.post('/api/auth/login', { username, password });
    return data;
  },

  logout: async (): Promise<void> => {
    await api.post('/api/auth/logout');
  },

  me: async (): Promise<AuthUser> => {
    const { data } = await api.get('/api/auth/me');
    return data;
  },

  /** 設定済みの外部連携先（ログイン画面のボタン出し分け） */
  oauthProviders: async (): Promise<string[]> => {
    const { data } = await api.get('/api/auth/oauth/providers');
    return data.providers ?? [];
  },

  /** OAuth コールバックの引き換えコードをセッショントークンに替える */
  exchangeOAuthCode: async (code: string): Promise<LoginResponse> => {
    const { data } = await api.post('/api/auth/oauth/exchange', { code });
    return data;
  },

  /** 自分に紐付いた外部アカウント一覧 */
  oauthIdentities: async (): Promise<OAuthIdentity[]> => {
    const { data } = await api.get('/api/auth/oauth/identities');
    return data.identities ?? [];
  },

  unlinkOAuth: async (provider: string): Promise<void> => {
    await api.delete(`/api/auth/oauth/${provider}`);
  },
};

/**
 * 認可画面へ入る。ログイン中に呼べば「既存アカウントへの連携追加」になる。
 *
 * 必ず axios 経由で URL を取ってから遷移すること。トークンは localStorage にあり
 * axios のインターセプタで付くので、ここでブラウザの全ページ遷移をしてしまうと
 * Authorization ヘッダーが飛ばず、サーバー側が常に「新規ログイン」と解釈する。
 */
export async function startOAuth(provider: string): Promise<void> {
  const { data } = await api.post(`/api/auth/oauth/${provider}/start`);
  window.location.href = data.auth_url;
}

// ========== ユーザー管理 API（要 users:manage） ==========

export const userApi = {
  list: async (): Promise<AuthUser[]> => {
    const { data } = await api.get('/api/users');
    return data;
  },

  create: async (req: CreateUserRequest): Promise<AuthUser> => {
    const { data } = await api.post('/api/users', req);
    return data;
  },

  update: async (id: string, req: UpdateUserRequest): Promise<AuthUser> => {
    const { data } = await api.put(`/api/users/${id}`, req);
    return data;
  },

  changePassword: async (id: string, password: string): Promise<void> => {
    await api.put(`/api/users/${id}/password`, { password });
  },

  delete: async (id: string): Promise<void> => {
    await api.delete(`/api/users/${id}`);
  },
};

// ========== ロール管理 API（要 users:manage） ==========

export const roleApi = {
  list: async (): Promise<Role[]> => {
    const { data } = await api.get('/api/roles');
    return data;
  },

  create: async (name: string, description: string, permissions: string[]): Promise<Role> => {
    const { data } = await api.post('/api/roles', { name, description, permissions });
    return data;
  },

  update: async (id: string, description: string, permissions: string[]): Promise<Role> => {
    const { data } = await api.put(`/api/roles/${id}`, { description, permissions });
    return data;
  },

  delete: async (id: string): Promise<void> => {
    await api.delete(`/api/roles/${id}`);
  },

  listPermissions: async (): Promise<PermissionInfo[]> => {
    const { data } = await api.get('/api/permissions');
    return data;
  },
};

// ========== DB バックアップ API（要 backup:manage） ==========

export const backupApi = {
  status: async (): Promise<BackupStatusResponse> => {
    const { data } = await api.get('/api/backups');
    return data;
  },

  create: async (): Promise<BackupResult> => {
    const { data } = await api.post('/api/backups');
    return data;
  },

  updateSettings: async (settings: {
    auto_enabled: boolean;
    interval_hours: number;
    retention_local: number;
    retention_drive: number;
    drive_upload: boolean;
  }): Promise<BackupSettings> => {
    const { data } = await api.put('/api/backups/settings', settings);
    return data;
  },

  downloadBlob: async (name: string): Promise<Blob> => {
    const { data } = await api.get(`/api/backups/${encodeURIComponent(name)}/download`, {
      responseType: 'blob',
    });
    return data as Blob;
  },

  delete: async (name: string): Promise<void> => {
    await api.delete(`/api/backups/${encodeURIComponent(name)}`);
  },

  restore: async (name: string): Promise<{ message: string }> => {
    const { data } = await api.post(`/api/backups/${encodeURIComponent(name)}/restore`);
    return data;
  },

  restoreUpload: async (file: File): Promise<{ message: string }> => {
    const form = new FormData();
    form.append('file', file);
    const { data } = await api.post('/api/backups/restore-upload', form, {
      headers: { 'Content-Type': 'multipart/form-data' },
    });
    return data;
  },

  // Google Drive 連携（デバイスフロー）
  gdriveAuthStart: async (): Promise<DriveDeviceAuth> => {
    const { data } = await api.post('/api/backups/gdrive/auth/start');
    return data;
  },

  gdriveAuthPoll: async (deviceCode: string): Promise<{ connected: boolean; gdrive: DriveStatus }> => {
    const { data } = await api.post('/api/backups/gdrive/auth/poll', { device_code: deviceCode });
    return data;
  },

  gdriveDisconnect: async (): Promise<void> => {
    await api.delete('/api/backups/gdrive');
  },

  gdriveFiles: async (): Promise<DriveFile[]> => {
    const { data } = await api.get('/api/backups/gdrive/files');
    return data.files ?? [];
  },

  gdriveDeleteFile: async (id: string): Promise<void> => {
    await api.delete(`/api/backups/gdrive/files/${encodeURIComponent(id)}`);
  },

  gdriveRestoreFile: async (id: string): Promise<{ message: string }> => {
    const { data } = await api.post(`/api/backups/gdrive/files/${encodeURIComponent(id)}/restore`);
    return data;
  },
};

export default api;

// ========== プレイリスト API ==========
// 公開・限定公開の閲覧は未ログインでも可。作成・編集はログインと所有者であることが必要。

export const playlistApi = {
  /** 自分のプレイリスト一覧（private 含む・要ログイン） */
  listMine: async (): Promise<PlaylistListResponse> => {
    const { data } = await api.get('/api/playlists');
    return data;
  },

  /** 公開プレイリスト一覧（unlisted は含まれない） */
  listPublic: async (page = 1, limit = 50): Promise<PlaylistListResponse> => {
    const { data } = await api.get('/api/playlists/public', { params: { page, limit } });
    return data;
  },

  get: async (id: string): Promise<Playlist> => {
    const { data } = await api.get(`/api/playlists/${id}`);
    return data;
  },

  items: async (id: string): Promise<PerformanceListResponse> => {
    const { data } = await api.get(`/api/playlists/${id}/items`);
    return data;
  },

  create: async (body: CreatePlaylistRequest): Promise<Playlist> => {
    const { data } = await api.post('/api/playlists', body);
    return data;
  },

  update: async (id: string, body: UpdatePlaylistRequest): Promise<Playlist> => {
    const { data } = await api.put(`/api/playlists/${id}`, body);
    return data;
  },

  remove: async (id: string): Promise<void> => {
    await api.delete(`/api/playlists/${id}`);
  },

  addItem: async (id: string, performanceId: string): Promise<void> => {
    await api.post(`/api/playlists/${id}/items`, { performance_id: performanceId });
  },

  removeItem: async (id: string, performanceId: string): Promise<void> => {
    await api.delete(`/api/playlists/${id}/items/${performanceId}`);
  },

  reorder: async (id: string, performanceIds: string[]): Promise<void> => {
    await api.put(`/api/playlists/${id}/order`, { performance_ids: performanceIds });
  },

  /** 共有リンク（限定公開）から取得。ログイン不要 */
  getShared: async (slug: string): Promise<Playlist> => {
    const { data } = await api.get(`/api/shared/playlists/${slug}`);
    return data;
  },

  sharedItems: async (slug: string): Promise<PerformanceListResponse> => {
    const { data } = await api.get(`/api/shared/playlists/${slug}/items`);
    return data;
  },
};

// ========== 外部サービス連携の設定 API（要 users:manage） ==========
// キーの値は返らない。設定済みか・末尾4桁・.env 由来かだけが返る。

export const integrationSettingsApi = {
  get: async (): Promise<IntegrationSettings> => {
    const { data } = await api.get('/api/settings/integrations');
    return data;
  },

  update: async (body: UpdateIntegrationSettingsRequest): Promise<IntegrationSettings> => {
    const { data } = await api.put('/api/settings/integrations', body);
    return data;
  },
};
