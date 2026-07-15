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
  AnalyzeCommentsResponse,
  SuccessResponse,
  LoadHolodexSongsResponse,
  CreatePerformancesRequest,
  CreatePerformancesResponse,
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
} from './types';

const DEFAULT_API_BASE_URL =
  typeof window !== 'undefined'
    ? `${window.location.protocol}//${window.location.hostname}:8080`
    : 'http://localhost:8080';
const API_BASE_URL = import.meta.env.VITE_API_URL || DEFAULT_API_BASE_URL;

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
  list: async (page = 1, limit = 20, search?: string): Promise<SongListResponse> => {
    const params = new URLSearchParams({ page: String(page), limit: String(limit) });
    if (search) params.set('search', search);
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
};

// ========== 歌回 API ==========

export const streamApi = {
  list: async (page = 1, limit = 20): Promise<StreamListResponse> => {
    const params = new URLSearchParams({ page: String(page), limit: String(limit) });
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
  list: async (page = 1, limit = 20): Promise<SingerListResponse> => {
    const params = new URLSearchParams({ page: String(page), limit: String(limit) });
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

  getPerformances: async (id: string, page = 1, limit = 20): Promise<SingerPerformanceListResponse> => {
    const params = new URLSearchParams({ page: String(page), limit: String(limit) });
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

  analyze: async (videoId: string, force = false): Promise<AnalyzeCommentsResponse> => {
    const { data } = await api.post(`/api/streams/${videoId}/comments/analyze${force ? '?force=true' : ''}`);
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

// ========== グローバル検索 API ==========

export const searchApi = {
  global: async (q: string, limit = 5): Promise<GlobalSearchResponse> => {
    const params = new URLSearchParams({ q, limit: String(limit) });
    const { data } = await api.get(`/api/search?${params}`);
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
};

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

export default api;
