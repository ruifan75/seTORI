import axios from 'axios';
import type {
  AutoFillSettings,
  NonSingingCandidate,
  AutoFillRunResult,
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
  SingerGroupListResponse,
  Organization,
  OrganizationListResponse,
  CreateOrganizationRequest,
  UpdateOrganizationRequest,
  SingerDetailResponse,
  SingerPerformanceListResponse,
  CreateSingerResponse,
  UpdateSingerRequest,
  SyncHolodexRequest,
  SyncHolodexResponse,
  SongSuggestion,
  MergeCandidate,
  SongIdentityCheck,
  TagGap,
  TagGapDismissal,
  AnalyzeCommentsResponse,
  Chapter,
  BatchAnalyzeStatus,
  BatchFillStatus,
  BatchFillRun,
  BatchFillGap,
  MissingSongPayload,
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
  ReadingsStats,
  CreateSuggestionRequest,
  SuggestionListResponse,
  SuggestionGroupListResponse,
  BatchReviewResponse,
  MergeSuggestionsRequest,
  MergeSuggestionsResponse,
  AutoApplySettings,
  SuggestionStatus,
  SuggestionKind,
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
  PresetPlaylist,
  PresetPlaylistListResponse,
  AddPresetToPlaylistResult,
  AddPlaylistItemsResult,
  BackupStatusResponse,
  BackupSettings,
  BackupResult,
  DriveDeviceAuth,
  DriveStatus,
  DriveFile,
  BuildVersion,
  ActivityListResponse,
  ActivityStatsResponse,
  UserActivitySummaryResponse,
  AvailabilityBackfillStatus,
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

// 認証：ログイン中のセッショントークンを保持。無い場合は環境変数 VITE_API_TOKEN
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

// エラーインターセプター：バックエンドのエラーメッセージを取り出す
api.interceptors.response.use(
  (response) => response,
  (error) => {
    const url: string = error.config?.url ?? '';
    // ログイン以外で 401 の場合はセッション失効とみなしてログアウト処理を促す
    if (error.response?.status === 401 && !url.includes('/api/auth/login')) {
      onUnauthorized?.();
    }
    // バックエンドから返されたエラーメッセージを取り出す
    if (error.response?.data?.error) {
      error.message = error.response.data.error;
    } else if (!error.response) {
      error.message = 'サーバーに接続できません';
    }
    return Promise.reject(error);
  }
);

// ========== 楽曲 API ==========

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

  // 「この表記はこの曲ではない」という否決の一覧。
  // 否決は候補からその曲を外し続けるので、誤判定を見直せるようにしてある。
  identityChecks: async (limit = 200): Promise<{ checks: SongIdentityCheck[] }> => {
    const { data } = await api.get(`/api/songs/identity-checks?limit=${limit}`);
    return data;
  },

  // 否決を 1 件取り消す。次からまた候補に出て、AI にも聞き直す。
  deleteIdentityCheck: async (pairKey: string): Promise<{ message: string }> => {
    const { data } = await api.post('/api/songs/identity-checks/delete', { pair_key: pairKey });
    return data;
  },
};

// ========== 照合の学習層 API ==========
//
// アーティストの別名義（松任谷由実 = 荒井由実）と、統合から学習した楽曲の別表記。
// どちらも照合の結果を左右するので content:edit が要る。


// ========== 歌枠 API ==========

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

// ========== 歌手 API ==========

export const singerApi = {
  // includeHidden は content:edit を持つ場合のみ有効（無ければサーバー側で無視される）
  list: async (
    page = 1,
    limit = 20,
    sort?: string,
    dir?: string,
    includeHidden = false
  ): Promise<SingerListResponse> => {
    const params = new URLSearchParams({ page: String(page), limit: String(limit) });
    if (sort) params.set('sort', sort);
    if (dir) params.set('dir', dir);
    if (includeHidden) params.set('include_hidden', 'true');
    const { data } = await api.get(`/api/singers?${params}`);
    return data;
  },

  // 事務所別（ページングなし）
  listGrouped: async (includeHidden = false): Promise<SingerGroupListResponse> => {
    const params = new URLSearchParams({ group: 'organization' });
    if (includeHidden) params.set('include_hidden', 'true');
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

  // singer を追加する（Holodex を優先し、見つからなければ YouTube Data API へフォールバック）
  create: async (channelInput: string): Promise<CreateSingerResponse> => {
    const { data } = await api.post('/api/singers', { id: channelInput });
    return data;
  },

  update: async (id: string, req: UpdateSingerRequest): Promise<Singer> => {
    const { data } = await api.put(`/api/singers/${id}`, req);
    return data;
  },

  // Holodex の事務所分類の手動上書き（空文字で解除して Holodex の値に戻す）。
  // Holodex 管理チャンネルでも設定できる（seTORI 側の判断であってメタデータではないため）
  setOrganization: async (id: string, organization: string): Promise<{ id: string; organization: string }> => {
    const { data } = await api.put(`/api/singers/${id}/organization`, { organization });
    return data;
  },

  // チャンネル一覧での表示/非表示（Holodex 管理チャンネルでも切り替えられる）
  setHidden: async (id: string, isHidden: boolean): Promise<{ id: string; is_hidden: boolean }> => {
    const { data } = await api.put(`/api/singers/${id}/visibility`, { is_hidden: isHidden });
    return data;
  },

  // 自動処理（定期同期＋コメント解析＋歌単作成）の対象かを切り替える。
  // 立てても最後の確認（is_processed）は自動では付かない。
  setAutoFill: async (id: string, enabled: boolean): Promise<{ id: string; auto_fill_enabled: boolean }> => {
    const { data } = await api.put(`/api/singers/${id}/auto-fill`, { auto_fill_enabled: enabled });
    return data;
  },

  // 自動処理が有効なチャンネルの一覧（content:edit）。
  listAutoFill: async (): Promise<{ singers: Singer[] }> => {
    const { data } = await api.get('/api/singers/auto-fill');
    return data;
  },

  // 会限セットリストの公開可否（チャンネル単位）。空文字で「未確認」へ戻す。
  //
  // **配信単位ではない。** 配信主に訊けば答えは「全部いい」か「全部だめ」なので、
  // 会限が 85 本あるチャンネルで 85 回操作させないため。個別の例外は配信側の
  // 「セットリストを伏せる」で扱う。
  setMembersPolicy: async (
    id: string,
    policy: 'allow' | 'deny' | '',
  ): Promise<{ id: string; members_only_policy: string }> => {
    // 項目名は members_only_policy。バックエンドは項目が無いと 400 を返す
    // （`{}` を「未確認へ戻す」と読まないため）ので、必ず値を入れて送る。
    const { data } = await api.put(`/api/singers/${id}/members-policy`, {
      members_only_policy: policy,
    });
    return data;
  },
};

// ========== 見直しが要る配信 API ==========
// 非表示だが現行規則で曲が出た配信。**自動で非表示は解除しない**
// （誤判定は両方向にある）ので、判断は人が行う。
export const nonSingingApi = {
  list: async (limit = 100): Promise<{ candidates: NonSingingCandidate[]; total: number }> => {
    const { data } = await api.get('/api/non-singing-candidates', { params: { limit } });
    return data;
  },

  // 「見たが歌回ではない」を記録して候補から外す。取り消せる（下）。
  dismiss: async (id: string, note = ''): Promise<void> => {
    await api.post(`/api/non-singing-candidates/${id}/dismiss`, { note });
  },

  restore: async (id: string): Promise<void> => {
    await api.delete(`/api/non-singing-candidates/${id}/dismiss`);
  },
};

// ========== 自動処理（定期実行）API ==========
// 登録チャンネルを定期的に 同期 → コメント取り直し → 歌単作成 する。
// 審査と処理完了は自動化しない（設計上わざと人に残した関門）。
export const autoFillApi = {
  getSettings: async (): Promise<AutoFillSettings> => {
    const { data } = await api.get('/api/auto-fill/settings');
    return data;
  },

  updateSettings: async (
    enabled: boolean,
    intervalHours: number,
    refreshDays: number,
  ): Promise<AutoFillSettings> => {
    // 3 項目とも送る。バックエンドは欠けていると 400 を返す
    // （`{}` が黙って自動処理を止めるのを防ぐため）。
    const { data } = await api.put('/api/auto-fill/settings', {
      enabled,
      interval_hours: intervalHours,
      refresh_days: refreshDays,
    });
    return data;
  },

  // 今すぐ 1 回だけ走らせる。**設定が無効でも走る** ── 有効にする前に
  // 何が起きるか確かめられないと、いきなり自動で回すことになる。
  run: async (): Promise<AutoFillRunResult> => {
    const { data } = await api.post('/api/auto-fill/run');
    return data;
  },
};

// ========== 事務所 API ==========
// key は取り込み時の値なので変更できない。編集できるのは表示名と並び順のみ。

export const organizationApi = {
  list: async (): Promise<OrganizationListResponse> => {
    const { data } = await api.get('/api/organizations');
    return data;
  },

  create: async (req: CreateOrganizationRequest): Promise<Organization> => {
    const { data } = await api.post('/api/organizations', req);
    return data;
  },

  update: async (key: string, req: UpdateOrganizationRequest): Promise<Organization> => {
    const { data } = await api.put(`/api/organizations/${encodeURIComponent(key)}`, req);
    return data;
  },

  remove: async (key: string): Promise<{ key: string }> => {
    const { data } = await api.delete(`/api/organizations/${encodeURIComponent(key)}`);
    return data;
  },
};

// ========== Holodex 同期 API ==========

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

  // 自動採用に届かなかった候補を人が確定させる（AI は呼ばない）。
  // 確定は別表記として学習されるので、同じ表記は次から迷わない。
  // name は画面に出ていた曲名で、保存する行がズレていないかの確認に使う。
};

// ========== チャプター API ==========

// 配信者が付けた目次を 3 つ目の入力元にする。Holodex にも曲が無く、コメントも
// 取れない配信の受け皿。取得は yt-dlp なので、初回は数秒かかる。
export const chapterApi = {
  getChapters: async (videoId: string): Promise<{ video_id: string; chapters: Chapter[] }> => {
    const { data } = await api.get(`/api/streams/${videoId}/chapters`);
    return data;
  },

  // 配信者が後から目次を足した場合に取り直す
  sync: async (videoId: string): Promise<{ video_id: string; chapter_count: number; chapters: Chapter[] }> => {
    const { data } = await api.post(`/api/streams/${videoId}/chapters/sync`);
    return data;
  },

  // 返す形はコメント分析と同じ（下流の変換・照合を共有しているため）
  analyze: async (videoId: string, force = false): Promise<AnalyzeCommentsResponse> => {
    const { data } = await api.post(`/api/streams/${videoId}/chapters/analyze${force ? '?force=true' : ''}`);
    return data;
  },
};

// ========== 一括プレ分析 API ==========

// 再生可否（会限・削除済みの判定材料）の一括取得。
// **log では足りない** ── 直近 1000 件しか残らず、20 件ごとの進捗行が失敗行を押し流すので、
// 進捗と最後のエラーは status から読む。
export const availabilityApi = {
  // recheck を立てると、`public` で確定済みの弱い判定も対象に戻す。
  backfill: async (
    concurrency: number,
    recheck = false
  ): Promise<{ targets: number; concurrency: number; recheck: boolean; message: string }> => {
    const params = new URLSearchParams({ concurrency: String(concurrency) });
    if (recheck) params.set('recheck', '1');
    const { data } = await api.post(`/api/availability/backfill?${params}`);
    return data;
  },
  status: async (): Promise<AvailabilityBackfillStatus> => {
    const { data } = await api.get('/api/availability/backfill/status');
    return data;
  },
  cancel: async (): Promise<{ message: string; progress: AvailabilityBackfillStatus }> => {
    const { data } = await api.post('/api/availability/backfill/cancel');
    return data;
  },
};

// 一括セットリスト作成。歌唱（performances）を直接作るので、実行記録と撤回がある。
// singerIds は対象チャンネル（空なら全部）。既定はそのチャンネルが**所有する**配信で、
// includeCollabs を立てるとゲスト参加した配信も含む。
export const batchFillApi = {
  start: async (
    mode: string,
    singerIds: string[] = [],
    includeCollabs = false
  ): Promise<{ run_id: string; message: string }> => {
    const { data } = await api.post('/api/streams/batch-fill', {
      mode,
      singer_ids: singerIds,
      include_collabs: includeCollabs,
    });
    return data;
  },
  cancel: async (): Promise<{ message: string }> => {
    const { data } = await api.post('/api/streams/batch-fill/cancel');
    return data;
  },
  status: async (): Promise<BatchFillStatus> => {
    const { data } = await api.get('/api/streams/batch-fill/status');
    return data;
  },
  listRuns: async (limit = 20): Promise<{ runs: BatchFillRun[] }> => {
    const { data } = await api.get(`/api/streams/batch-fill/runs?limit=${limit}`);
    return data;
  },
  revert: async (runId: string): Promise<{ deleted: number; message: string }> => {
    const { data } = await api.post(`/api/streams/batch-fill/runs/${runId}/revert`);
    return data;
  },
  // その実行で「DB にあるが入力元に無い」と分かった既存の歌唱。
  // 提案としては積まないので、実行履歴からここへ辿るのが唯一の入口。
  gaps: async (runId: string): Promise<{ gaps: BatchFillGap[] }> => {
    const { data } = await api.get(`/api/streams/batch-fill/runs/${runId}/gaps`);
    return data;
  },
};

export const batchAnalyzeApi = {
  // 一括プレ分析を開始（背景ジョブ・singleton）
  // mode: unanalyzed（未分析のみ）/ unprocessed（未処理すべて）/ refresh（コメント再取得）/ reanalyze（すべて再分析）
  // singerId: 対象チャンネル（空なら全チャンネル）
  // hidden: ''/'false'（非表示を除く・既定）/ 'true'（非表示だけ）/ 'all'（両方）
  start: async (
    mode: string,
    singerId?: string,
    hidden?: 'all' | 'true' | 'false'
  ): Promise<{ message: string }> => {
    const { data } = await api.post('/api/streams/batch-analyze', {
      mode,
      singer_id: singerId ?? '',
      hidden: hidden ?? '',
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

// ========== 歌唱 API ==========

export const performanceApi = {
  // Holodex から楽曲を読み込む（正規化キューには追加しない）
  loadHolodexSongs: async (streamId: string): Promise<LoadHolodexSongsResponse> => {
    const { data } = await api.get(`/api/streams/${streamId}/holodex-songs`);
    return data;
  },

  // 歌唱記録を直接作成する
  create: async (streamId: string, req: CreatePerformancesRequest): Promise<CreatePerformancesResponse> => {
    const { data } = await api.post(`/api/streams/${streamId}/performances`, req);
    return data;
  },

  // 指定した配信の歌唱記録をすべて削除する
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
  // AI 正規化を一括実行する
  normalize: async (req: BatchAINormalizationRequest): Promise<BatchAINormalizationResponse> => {
    const { data } = await api.post('/api/ai/normalize', req);
    return data;
  },
};

// ========== iTunes API ==========

export const itunesApi = {
  // iTunes を検索する
  search: async (songName: string): Promise<ITunesSearchResponse> => {
    const params = new URLSearchParams({ term: songName });
    const { data } = await api.get(`/api/itunes/search?${params}`);
    return data;
  },

  // iTunes ID で詳細情報を取得する
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

// ========== タグ漏れ API ==========
//
// 「解析キャッシュにタグがあるのに歌唱に無い」組のレビュー（要 content:edit）。
// 付けるのは既存の performanceApi.update を使う（タグは総入れ替えなので、
// 今のタグに足したものを送る）。

export const tagGapApi = {
  list: async (limit = 300): Promise<{ gaps: TagGap[]; dismissed: TagGapDismissal[] }> => {
    const { data } = await api.get(`/api/tag-gaps?limit=${limit}`);
    return data;
  },

  // このタグは付けない、と記録する（次回から一覧に出ない）
  dismiss: async (performanceId: string, tagId: string): Promise<{ message: string }> => {
    const { data } = await api.post('/api/tag-gaps/dismiss', { performance_id: performanceId, tag_id: tagId });
    return data;
  },

  // 無視を取り消す（次回からまた出る）
  undismiss: async (performanceId: string, tagId: string): Promise<{ message: string }> => {
    const { data } = await api.post('/api/tag-gaps/undismiss', { performance_id: performanceId, tag_id: tagId });
    return data;
  },
};

// ========== ホーム（おすすめ）API ==========

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

  // プロバイダーに保存済みの API key で利用可能なモデル一覧を取得する
  listModels: async (id: number): Promise<AIModelInfo[]> => {
    const { data } = await api.get(`/api/ai-providers/${id}/models`);
    return data.models ?? [];
  },

  // 未保存の base_url と api_key で利用可能なモデルを取得する（新規追加フォーム用）
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
  // 「この 2 つは同じ人」を登録する。権限があれば即時反映、無ければ提案として積まれる。
  proposeAlias: async (canonical: string, alias: string): Promise<{ applied: boolean }> => {
    const { data } = await api.post('/api/artists/aliases', { canonical, alias });
    return data;
  },

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
  // 読みの整備状況（未整備の残件数）
  stats: async (): Promise<ReadingsStats> => {
    const { data } = await api.get('/api/readings/stats');
    return data;
  },

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

  // 提案一覧（要 content:edit）。status / kind で絞り込み
  list: async (
    status: SuggestionStatus | '' = 'pending',
    page = 1,
    limit = 20,
    kind?: SuggestionKind
  ): Promise<SuggestionListResponse> => {
    const params = new URLSearchParams({ page: String(page), limit: String(limit) });
    if (status) params.set('status', status);
    if (kind) params.set('kind', kind);
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
  // kind を渡すとその種別だけを返す（レビュー画面の種別の絞り込み用）。
  listGrouped: async (
    status: SuggestionStatus | '' = 'pending',
    page = 1,
    limit = 20,
    kind?: SuggestionKind
  ): Promise<SuggestionGroupListResponse> => {
    const params = new URLSearchParams({ page: String(page), limit: String(limit), group: 'target' });
    if (status) params.set('status', status);
    if (kind) params.set('kind', kind);
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
  //
  // payload は perf.missing を承認するときだけ使う。審査の画面で直した内容
  // （曲の差し替え・時間・歌手）を添えて、承認と修正を 1 往復で済ませる。
  approve: async (
    id: string,
    force = false,
    payload?: MissingSongPayload
  ): Promise<{ message: string }> => {
    const { data } = await api.post(
      `/api/suggestions/${id}/approve${force ? '?force=1' : ''}`,
      payload ? { payload } : undefined
    );
    return data;
  },

  // notThisSong を立てると「この表記はこの曲ではない」を学習し、
  // 次の一括実行が同じ組を審査へ積まないようにする。
  reject: async (id: string, note = '', notThisSong = false): Promise<{ message: string }> => {
    const { data } = await api.post(`/api/suggestions/${id}/reject`, {
      note,
      not_this_song: notThisSong,
    });
    return data;
  },

  // 自分が出した未処理の提案を取り下げる（content:edit なら他人の分も可）。
  // 処理済みのものは 409、他人のものは 404。
  withdraw: async (id: string): Promise<{ message: string }> => {
    const { data } = await api.delete(`/api/suggestions/${id}`);
    return data;
  },

  // 却下を取り消す（要 content:edit・perf.missing のみ）。
  // 却下した行は status に関係なく重複判定に引っかかるので、そのままだと
  // 次の一括作成で二度と出てこない。「この曲ではない」の否決記録も一緒に消える。
  undoRejection: async (id: string): Promise<{ message: string }> => {
    const { data } = await api.post(`/api/suggestions/${id}/undo-rejection`);
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

// ========== 認証 API ==========

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

  revokeSessions: async (id: string): Promise<void> => {
    await api.post(`/api/users/${id}/revoke-sessions`);
  },
};

// ========== 訪客／利用者活動 API ==========

export const activityApi = {
  // 公開ページの表示を記録。失敗は呼び出し側で握り、閲覧機能を妨げない。
  recordVisit: async (path: string): Promise<void> => {
    await api.post('/api/activity/visit', { path });
  },

  policy: async (): Promise<{ retention_days: number }> => {
    const { data } = await api.get('/api/activity/policy');
    return data;
  },

  list: async (opts: {
    days?: number;
    page?: number;
    limit?: number;
    kind?: 'all' | 'anonymous' | 'authenticated';
    q?: string;
  } = {}): Promise<ActivityListResponse> => {
    const params = new URLSearchParams({
      days: String(opts.days ?? 7),
      page: String(opts.page ?? 1),
      limit: String(opts.limit ?? 50),
      kind: opts.kind ?? 'all',
    });
    if (opts.q) params.set('q', opts.q);
    const { data } = await api.get(`/api/activity?${params}`);
    return data;
  },

  stats: async (days = 7): Promise<ActivityStatsResponse> => {
    const { data } = await api.get(`/api/activity/stats?days=${days}`);
    return data;
  },

  userSummaries: async (days = 30): Promise<UserActivitySummaryResponse> => {
    const { data } = await api.get(`/api/activity/users?days=${days}`);
    return data;
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

  // 既存のローカルバックアップを Drive へ送る。作成時のアップロードが失敗したものや、
  // 連携前・自動アップロード無効時に作ったものを後から救うための口。
  uploadToDrive: async (name: string): Promise<{ message: string }> => {
    const { data } = await api.post(`/api/backups/${encodeURIComponent(name)}/upload-drive`);
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

  /** 複数の歌唱を指定順で末尾へ追加する（既存の項目は飛ばす） */
  addItems: async (id: string, performanceIds: string[]): Promise<AddPlaylistItemsResult> => {
    const { data } = await api.post(`/api/playlists/${id}/items`, { performance_ids: performanceIds });
    return data;
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

// ========== プリセットプレイリスト API ==========
// 一覧・中身の閲覧は未ログインでも可。フォローとコピーは要ログイン（権限は不要）。

export const presetPlaylistApi = {
  list: async (): Promise<PresetPlaylistListResponse> => {
    const { data } = await api.get('/api/presets');
    return data;
  },

  /** フォロー中のみ（要ログイン） */
  listFollowed: async (): Promise<PresetPlaylistListResponse> => {
    const { data } = await api.get('/api/presets/followed');
    return data;
  },

  get: async (key: string): Promise<PresetPlaylist> => {
    const { data } = await api.get(`/api/presets/${key}`);
    return data;
  },

  items: async (key: string, limit?: number): Promise<PerformanceListResponse> => {
    const { data } = await api.get(`/api/presets/${key}/items`, { params: limit ? { limit } : undefined });
    return data;
  },

  follow: async (key: string): Promise<void> => {
    await api.post(`/api/presets/${key}/follow`);
  },

  unfollow: async (key: string): Promise<void> => {
    await api.delete(`/api/presets/${key}/follow`);
  },

  /**
   * 現在の中身を自分のプレイリストへ入れる（以後プリセットとは無関係になる）。
   * playlist_id を渡せば既存へ追加、name を渡せば新規作成。どちらも無ければプリセット名で新規作成。
   */
  addToPlaylist: async (key: string, target: { playlistId?: string; name?: string } = {}): Promise<AddPresetToPlaylistResult> => {
    const { data } = await api.post(`/api/presets/${key}/add`, {
      playlist_id: target.playlistId,
      name: target.name,
    });
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
