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
  SyncHolodexRequest,
  SyncHolodexResponse,
  AnalyzeCommentsResponse,
  SuccessResponse,
  LoadHolodexSongsResponse,
  CreatePerformancesRequest,
  CreatePerformancesResponse,
  BatchAINormalizationRequest,
  BatchAINormalizationResponse,
  ITunesSearchResponse,
  ITunesQueryResult,
} from './types';

const API_BASE_URL = import.meta.env.VITE_API_URL || 'http://localhost:8080';

const api = axios.create({
  baseURL: API_BASE_URL,
  headers: {
    'Content-Type': 'application/json',
  },
});

// 錯誤攔截器 - 提取後端錯誤訊息
api.interceptors.response.use(
  (response) => response,
  (error) => {
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

  // 新增 singer（透過 Holodex 同步）
  create: async (channelId: string): Promise<SyncHolodexResponse> => {
    const { data } = await api.post('/api/singers', { id: channelId, name: 'Syncing...' });
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
};

// ========== Comment 分析 API ==========

export const commentApi = {
  getComments: async (videoId: string): Promise<{ video_id: string; comments: string[] }> => {
    const { data } = await api.get(`/api/streams/${videoId}/comments`);
    return data;
  },

  analyze: async (videoId: string): Promise<AnalyzeCommentsResponse> => {
    const { data } = await api.post(`/api/streams/${videoId}/comments/analyze`);
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

export default api;
