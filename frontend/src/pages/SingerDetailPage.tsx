import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { useParams, Link, useSearchParams } from 'react-router-dom';
import { singerApi, holodexApi, organizationApi } from '../api/client';
import type { Organization } from '../api/types';
import { useAuthStore, hasPermission, PERM } from '../store/auth';
import { usePlayerStore, performanceToTrack } from '../store/player';
import Loading from '../components/ui/Loading';
import Pagination from '../components/ui/Pagination';
import Tag from '../components/ui/Tag';
import ArtistLinks from '../components/ArtistLinks';
import QueueAddButton from '../components/QueueAddButton';
import { SortableTh, type SortDir, type SortState } from '../components/ui/Sort';
import { useToast } from '../components/ui/ToastContext';
import VisibilityIcon from '../components/ui/VisibilityIcon';

type TabType = 'streams' | 'performances';
type ProcessedFilter = 'all' | 'true' | 'false';
type HiddenFilter = 'all' | 'true' | 'false';

export default function SingerDetailPage() {
  const { id } = useParams<{ id: string }>();
  const queryClient = useQueryClient();
  const { showToast } = useToast();
  const authUser = useAuthStore((s) => s.user);
  const canEdit = hasPermission(authUser, PERM.CONTENT_EDIT);
  const canSync = hasPermission(authUser, PERM.SYNC_RUN);
  // タブ・ページ・フィルタは URL クエリに保持する。
  // 詳細ページへ遷移して「戻る」した際に state だとリセットされるため（URL なら復元される）。
  const [searchParams, setSearchParams] = useSearchParams();
  const activeTab: TabType = searchParams.get('tab') === 'performances' ? 'performances' : 'streams';
  const streamPage = Math.max(1, parseInt(searchParams.get('page') || '1') || 1);
  const perfPage = Math.max(1, parseInt(searchParams.get('ppage') || '1') || 1);
  // 歌唱曲一覧テーブルの並び替え（既定は配信日の新しい順）
  const perfSort = searchParams.get('psort') || 'date';
  const perfDir: SortDir = searchParams.get('pdir')
    ? (searchParams.get('pdir') === 'asc' ? 'asc' : 'desc')
    : (perfSort === 'date' ? 'desc' : 'asc');

  // 複数キーを一括更新（null は削除＝デフォルト値）。replace で履歴を汚さない。
  const updateParams = (updates: Record<string, string | null>) => {
    const next = new URLSearchParams(searchParams);
    for (const [k, v] of Object.entries(updates)) {
      if (v === null) next.delete(k);
      else next.set(k, v);
    }
    setSearchParams(next, { replace: true });
  };

  const setActiveTab = (t: TabType) => updateParams({ tab: t === 'streams' ? null : t });
  const setStreamPage = (p: number) => updateParams({ page: p <= 1 ? null : String(p) });
  const setPerfPage = (p: number) => updateParams({ ppage: p <= 1 ? null : String(p) });
  // 並び替え変更時は 1 ページ目に戻す。既定値（date / 自然方向）は URL から省く。
  const handlePerfSort = (next: SortState) => {
    const naturalDir = next.sort === 'date' ? 'desc' : 'asc';
    updateParams({
      psort: next.sort === 'date' ? null : next.sort,
      pdir: next.dir === naturalDir ? null : next.dir,
      ppage: null,
    });
  };
  const [syncMode, setSyncMode] = useState<'new' | 'all'>('new');
  const [showEditModal, setShowEditModal] = useState(false);
  const [editForm, setEditForm] = useState({
    name: '',
    english_name: '',
    photo_url: '',
  });
  // Filter states - デフォルトでは非表示を除外
  // フィルタも URL に保持（processed のデフォルトは all、hidden のデフォルトは false=非表示を除く）
  const processedParam = searchParams.get('processed');
  const processedFilter: ProcessedFilter =
    processedParam === 'true' || processedParam === 'false' ? processedParam : 'all';
  const hiddenParam = searchParams.get('hidden');
  const hiddenFilter: HiddenFilter =
    hiddenParam === 'all' || hiddenParam === 'true' ? hiddenParam : 'false';

  // Singer detail
  const { data: singer, isLoading: singerLoading } = useQuery({
    queryKey: ['singer', id],
    queryFn: () => singerApi.get(id!),
    enabled: !!id,
  });

  // 編集フォームの所属プルダウン用。編集できる人だけ引く。
  const { data: orgList } = useQuery({
    queryKey: ['organizations'],
    queryFn: organizationApi.list,
    enabled: canEdit,
    staleTime: 5 * 60 * 1000,
  });
  const organizations = orgList?.organizations ?? [];

  // Streams
  const { data: streams, isLoading: streamsLoading } = useQuery({
    queryKey: ['singerStreams', id, streamPage, processedFilter, hiddenFilter],
    queryFn: () => singerApi.getStreams(id!, streamPage, 20, processedFilter, hiddenFilter),
    enabled: !!id && activeTab === 'streams',
  });

  // Performances
  const { data: performances, isLoading: perfsLoading } = useQuery({
    queryKey: ['singerPerformances', id, perfPage, perfSort, perfDir],
    queryFn: () => singerApi.getPerformances(id!, perfPage, 20, perfSort, perfDir),
    enabled: !!id && activeTab === 'performances',
  });

  // 歌唱記録1件を再生キューのトラックへ変換
  const toTrack = performanceToTrack;

  // 歌唱曲一覧（現在のページ）をキューに載せて startIndex から連続再生
  const playPerformancesFrom = (startIndex: number) => {
    const tracks = (performances?.performances ?? []).map(toTrack);
    usePlayerStore.getState().playTracks(tracks, startIndex);
  };

  // 現在のページの歌唱曲をシャッフルして先頭から連続再生（Fisher–Yates）
  const shufflePlay = () => {
    const tracks = (performances?.performances ?? []).map(toTrack);
    for (let i = tracks.length - 1; i > 0; i--) {
      const j = Math.floor(Math.random() * (i + 1));
      [tracks[i], tracks[j]] = [tracks[j], tracks[i]];
    }
    usePlayerStore.getState().playTracks(tracks, 0);
  };

  // Sync mutation
  const syncMutation = useMutation({
    mutationFn: () => holodexApi.syncChannel({ 
      channel_id: id!,
      force_update: syncMode === 'all'
    }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['singer', id] });
      queryClient.invalidateQueries({ queryKey: ['singerStreams', id] });
    },
  });

  // チャンネル一覧での表示/非表示。非表示にしてもこのページ自体は誰でも開ける。
  const visibilityMutation = useMutation({
    mutationFn: (isHidden: boolean) => singerApi.setHidden(id!, isHidden),
    onSuccess: (_, isHidden) => {
      queryClient.invalidateQueries({ queryKey: ['singer', id] });
      queryClient.invalidateQueries({ queryKey: ['singers'] });
      showToast(
        isHidden ? 'チャンネル一覧から非表示にしました' : 'チャンネル一覧に表示しました',
        'success'
      );
    },
    onError: (err: Error) => showToast(err.message, 'error'),
  });

  const updateMutation = useMutation({
    mutationFn: () => singerApi.update(id!, {
      name: editForm.name,
      english_name: editForm.english_name,
      photo_url: editForm.photo_url,
    }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['singer', id] });
      queryClient.invalidateQueries({ queryKey: ['singers'] });
      setShowEditModal(false);
    },
  });

  const openEditModal = () => {
    if (!singer) return;
    updateMutation.reset();
    setEditForm({
      name: singer.name,
      english_name: singer.english_name || '',
      photo_url: singer.photo_url || '',
    });
    setShowEditModal(true);
  };

  const handleUpdateSinger = (e: React.FormEvent) => {
    e.preventDefault();
    if (!editForm.name.trim()) return;
    updateMutation.mutate();
  };

  const formatTime = (seconds: number) => {
    const h = Math.floor(seconds / 3600);
    const m = Math.floor((seconds % 3600) / 60);
    const s = seconds % 60;
    if (h > 0) {
      return `${h}:${m.toString().padStart(2, '0')}:${s.toString().padStart(2, '0')}`;
    }
    return `${m}:${s.toString().padStart(2, '0')}`;
  };

  if (singerLoading) {
    return <Loading />;
  }

  if (!singer) {
    return (
      <div className="text-center py-12 text-gray-500">
        チャンネルが見つかりません
      </div>
    );
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="bg-white rounded-lg shadow-sm border p-6">
        <div className="flex flex-wrap items-start gap-4 sm:gap-6">
          {singer.photo_url ? (
            <img
              src={singer.photo_url}
              alt={singer.name}
              className="w-16 h-16 sm:w-24 sm:h-24 rounded-full object-cover"
              onError={(e) => {
                e.currentTarget.onerror = null;
                e.currentTarget.src = `https://holodex.net/statics/channelImg/${singer.id}/50.png`;
              }}
            />
          ) : (
            <div className="w-16 h-16 sm:w-24 sm:h-24 rounded-full bg-gray-200 flex items-center justify-center">
              <svg className="w-12 h-12 text-gray-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M16 7a4 4 0 11-8 0 4 4 0 018 0zM12 14a7 7 0 00-7 7h14a7 7 0 00-7-7z" />
              </svg>
            </div>
          )}
          <div className="flex-1 min-w-0">
            <h1 className="text-2xl font-bold text-gray-900">{singer.name}</h1>
            {singer.english_name && (
              <p className="text-gray-500 mt-1">{singer.english_name}</p>
            )}
            <div className="flex flex-wrap items-center gap-2 mt-2">
              {/* 「所属なし」を意味する分類（Independents など）は organization_name が空になり、
                  バッジも出ない。見出しが「所属なし」なのにバッジは別名、という矛盾を避けるため */}
              {singer.organization_name && (
                <span className="inline-block px-3 py-1 bg-purple-100 text-purple-700 text-sm rounded-full">
                  {singer.organization_name}
                </span>
              )}
              {canEdit && <OrganizationPicker singer={singer} organizations={organizations} />}
              {/* 非表示でもこのページは誰でも開けるので、閲覧者にも状態を見せる */}
              {singer.is_hidden && (
                <span
                  className="inline-flex items-center gap-1 px-3 py-1 bg-gray-200 text-gray-600 text-sm rounded-full"
                  title="チャンネル一覧には表示されません（このページは閲覧できます）"
                >
                  <VisibilityIcon hidden className="w-4 h-4" />
                  一覧で非表示
                </span>
              )}
            </div>
            <div className="flex gap-4 mt-4 text-sm text-gray-600">
              <div>
                <span className="font-medium text-gray-900">{singer.stream_count}</span> 歌枠
              </div>
              <div>
                <span className="font-medium text-gray-900">{singer.performance_count}</span> 曲
              </div>
            </div>
          </div>
          <div className="flex flex-wrap gap-3 w-full sm:w-auto">
            {/* Holodex 管理チャンネルでも切り替えられる（メタデータではなく seTORI 側の都合なので） */}
            {canEdit && (
              <button
                onClick={() => visibilityMutation.mutate(!singer.is_hidden)}
                disabled={visibilityMutation.isPending}
                title={singer.is_hidden ? 'チャンネル一覧に表示する' : 'チャンネル一覧から非表示にする'}
                aria-label={singer.is_hidden ? 'チャンネル一覧に表示する' : 'チャンネル一覧から非表示にする'}
                className="px-3 py-2 bg-white border border-gray-300 text-gray-600 rounded-lg hover:bg-gray-50 hover:text-gray-900 transition-colors disabled:opacity-50"
              >
                <VisibilityIcon hidden={singer.is_hidden} className="w-5 h-5" />
              </button>
            )}
            {singer.can_edit_metadata && canEdit && (
              <button
                onClick={openEditModal}
                className="px-4 py-2 bg-white border border-gray-300 text-gray-700 rounded-lg hover:bg-gray-50 transition-colors flex items-center gap-2"
              >
                <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M16.862 4.487l1.687-1.688a1.875 1.875 0 112.652 2.652L10.582 16.07a4.5 4.5 0 01-1.897 1.13L6 18l.8-2.685a4.5 4.5 0 011.13-1.897l8.932-8.931z" />
                </svg>
                編集
              </button>
            )}
            <a
              href={`https://www.youtube.com/channel/${singer.id}`}
              target="_blank"
              rel="noopener noreferrer"
              className="px-4 py-2 bg-red-600 text-white rounded-lg hover:bg-red-700 transition-colors flex items-center gap-2"
            >
              <svg className="w-5 h-5" fill="currentColor" viewBox="0 0 24 24">
                <path d="M23.498 6.186a3.016 3.016 0 0 0-2.122-2.136C19.505 3.545 12 3.545 12 3.545s-7.505 0-9.377.505A3.017 3.017 0 0 0 .502 6.186C0 8.07 0 12 0 12s0 3.93.502 5.814a3.016 3.016 0 0 0 2.122 2.136c1.871.505 9.376.505 9.376.505s7.505 0 9.377-.505a3.015 3.015 0 0 0 2.122-2.136C24 15.93 24 12 24 12s0-3.93-.502-5.814zM9.545 15.568V8.432L15.818 12l-6.273 3.568z" />
              </svg>
              YouTube
            </a>
            {canSync && (
            <div className="flex gap-2">
              <select
                value={syncMode}
                onChange={(e) => setSyncMode(e.target.value as 'new' | 'all')}
                className="px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent text-sm"
              >
                <option value="new">新しい配信のみ</option>
                <option value="all">すべての配信</option>
              </select>
              <button
                onClick={() => syncMutation.mutate()}
                disabled={syncMutation.isPending}
                className="px-4 py-2 bg-indigo-600 text-white rounded-lg hover:bg-indigo-700 transition-colors disabled:opacity-50 flex items-center gap-2"
              >
                <svg className={`w-5 h-5 ${syncMutation.isPending ? 'animate-spin' : ''}`} fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
                </svg>
                {syncMutation.isPending ? '同期中...' : '同期'}
              </button>
            </div>
            )}
          </div>
        </div>

        {syncMutation.isSuccess && (
          <div className="mt-4 p-3 bg-green-50 border border-green-200 rounded-lg text-green-700 text-sm">
            同期完了: {syncMutation.data.synced_count} 件の歌枠を同期しました
          </div>
        )}
      </div>

      {showEditModal && (
        <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
          <div className="bg-white rounded-lg shadow-xl p-6 w-full max-w-lg mx-4">
            <h2 className="text-xl font-bold text-gray-900 mb-4">チャンネル情報を編集</h2>
            <form onSubmit={handleUpdateSinger} className="space-y-4">
              <div>
                <label className="block text-sm font-medium text-gray-700 mb-1">
                  チャンネル名
                </label>
                <input
                  type="text"
                  value={editForm.name}
                  onChange={(e) => setEditForm((prev) => ({ ...prev, name: e.target.value }))}
                  className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
                />
              </div>
              <div>
                <label className="block text-sm font-medium text-gray-700 mb-1">
                  English Name
                </label>
                <input
                  type="text"
                  value={editForm.english_name}
                  onChange={(e) => setEditForm((prev) => ({ ...prev, english_name: e.target.value }))}
                  className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
                />
              </div>
              {/* 所属はこのモーダルには無い。Holodex 管理チャンネルでも変えられる必要があるので
                  ヘッダー側の独立した操作（上書き）にしてある。 */}
              <div>
                <label className="block text-sm font-medium text-gray-700 mb-1">
                  Photo URL
                </label>
                <input
                  type="url"
                  value={editForm.photo_url}
                  onChange={(e) => setEditForm((prev) => ({ ...prev, photo_url: e.target.value }))}
                  className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
                />
              </div>

              {updateMutation.isError && (
                <div className="p-3 bg-red-50 border border-red-200 rounded-lg text-red-700 text-sm">
                  {(updateMutation.error as Error).message}
                </div>
              )}

              <div className="flex justify-end gap-3 pt-2">
                <button
                  type="button"
                  onClick={() => {
                    updateMutation.reset();
                    setShowEditModal(false);
                  }}
                  className="px-4 py-2 text-gray-700 hover:text-gray-900"
                >
                  キャンセル
                </button>
                <button
                  type="submit"
                  disabled={updateMutation.isPending || !editForm.name.trim()}
                  className="px-4 py-2 bg-indigo-600 text-white rounded-lg hover:bg-indigo-700 transition-colors disabled:opacity-50"
                >
                  {updateMutation.isPending ? '保存中...' : '保存'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}

      {/* Tabs */}
      <div className="border-b border-gray-200">
        <nav className="-mb-px flex gap-6">
          <button
            onClick={() => setActiveTab('streams')}
            className={`py-3 px-1 border-b-2 font-medium text-sm ${
              activeTab === 'streams'
                ? 'border-indigo-500 text-indigo-600'
                : 'border-transparent text-gray-500 hover:text-gray-700 hover:border-gray-300'
            }`}
          >
            歌枠一覧
          </button>
          <button
            onClick={() => setActiveTab('performances')}
            className={`py-3 px-1 border-b-2 font-medium text-sm ${
              activeTab === 'performances'
                ? 'border-indigo-500 text-indigo-600'
                : 'border-transparent text-gray-500 hover:text-gray-700 hover:border-gray-300'
            }`}
          >
            歌唱曲一覧
          </button>
        </nav>
      </div>

      {/* Tab Content */}
      {activeTab === 'streams' && (
        <div className="space-y-4">
          {/* Filter Controls */}
          <div className="bg-white rounded-lg shadow-sm border p-4">
            <div className="flex flex-wrap gap-4 items-center">
              {/* Processed Filter */}
              <div className="flex items-center gap-2">
                <span className="text-sm font-medium text-gray-700">処理状態:</span>
                <select
                  value={processedFilter}
                  onChange={(e) => {
                    const v = e.target.value;
                    // フィルタ変更時は1ページ目へ戻す（page キーを削除）
                    updateParams({ processed: v === 'all' ? null : v, page: null });
                  }}
                  className="text-sm border-gray-300 rounded-md focus:ring-indigo-500 focus:border-indigo-500"
                >
                  <option value="all">すべて</option>
                  <option value="false">未処理</option>
                  <option value="true">処理済み</option>
                </select>
              </div>
              {/* Hidden Filter */}
              <div className="flex items-center gap-2">
                <span className="text-sm font-medium text-gray-700">表示:</span>
                <select
                  value={hiddenFilter}
                  onChange={(e) => {
                    const v = e.target.value;
                    updateParams({ hidden: v === 'false' ? null : v, page: null });
                  }}
                  className="text-sm border-gray-300 rounded-md focus:ring-indigo-500 focus:border-indigo-500"
                >
                  <option value="false">非表示を除く</option>
                  <option value="all">すべて表示</option>
                  <option value="true">非表示のみ</option>
                </select>
              </div>
            </div>
          </div>

          {streamsLoading ? (
            <Loading />
          ) : streams?.streams.length === 0 ? (
            <div className="text-center py-12 text-gray-500">
              まだ歌枠がありません。上のボタンで同期してください。
            </div>
          ) : (
            <>
              <div className="grid gap-4">
                {streams?.streams.map((stream) => (
                  <Link
                    key={stream.id}
                    to={`/streams/${stream.id}`}
                    className={`bg-white rounded-lg shadow-sm border overflow-hidden hover:shadow-md transition-shadow flex ${
                      stream.is_hidden ? 'opacity-60' : ''
                    }`}
                  >
                    {/* Thumbnail */}
                    <div className="relative w-28 sm:w-48 flex-shrink-0">
                      {stream.thumbnail_url ? (
                        <img
                          src={stream.thumbnail_url}
                          alt={stream.title}
                          className="w-full h-full object-cover"
                        />
                      ) : (
                        <div className="w-full h-full bg-gray-200 flex items-center justify-center">
                          <svg className="w-12 h-12 text-gray-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M14.752 11.168l-3.197-2.132A1 1 0 0010 9.87v4.263a1 1 0 001.555.832l3.197-2.132a1 1 0 000-1.664z" />
                            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
                          </svg>
                        </div>
                      )}
                      {/* Status badges */}
                      <div className="absolute top-2 left-2 flex flex-col gap-1">
                        {!stream.is_processed && (
                          <span className="px-2 py-0.5 bg-amber-500 text-white text-xs font-medium rounded">
                            未処理
                          </span>
                        )}
                        {stream.is_hidden && (
                          <span className="px-2 py-0.5 bg-gray-500 text-white text-xs font-medium rounded">
                            非表示
                          </span>
                        )}
                      </div>
                    </div>
                    {/* Content */}
                    <div className="p-4 flex-1">
                      <h3 className="font-medium text-gray-900 line-clamp-2">{stream.title}</h3>
                      <p className="text-sm text-gray-500 mt-1">
                        {new Date(stream.stream_date).toLocaleString('ja-JP', {
                          year: 'numeric',
                          month: '2-digit',
                          day: '2-digit',
                          hour: '2-digit',
                          minute: '2-digit',
                          second: '2-digit',
                          hour12: false,
                        })}
                      </p>
                      {stream.tags.length > 0 && (
                        <div className="flex flex-wrap gap-1 mt-2">
                          {stream.tags.map((tag) => (
                            <Tag key={tag.id} label={tag.display_name} color={tag.color} />
                          ))}
                        </div>
                      )}
                    </div>
                  </Link>
                ))}
              </div>

              {streams && (
                <Pagination
                  page={streamPage}
                  totalPages={streams.pagination.total_pages}
                  onPageChange={setStreamPage}
                />
              )}
            </>
          )}
        </div>
      )}

      {activeTab === 'performances' && (
        <div className="space-y-4">
          {perfsLoading ? (
            <Loading />
          ) : performances?.performances.length === 0 ? (
            <div className="text-center py-12 text-gray-500">
              まだ歌唱記録がありません
            </div>
          ) : (
            <>
              {/* 一覧全体の再生操作（このページの歌唱曲が対象） */}
              <div className="flex items-center gap-2">
                <button
                  onClick={() => playPerformancesFrom(0)}
                  className="inline-flex items-center gap-1.5 px-4 py-2 text-sm bg-indigo-600 text-white font-medium rounded-full hover:bg-indigo-700 transition-colors"
                  title="このページの歌唱曲を上から連続再生"
                >
                  <svg className="w-4 h-4" fill="currentColor" viewBox="0 0 24 24"><path d="M8 5v14l11-7z" /></svg>
                  再生
                </button>
                <button
                  onClick={shufflePlay}
                  className="inline-flex items-center gap-1.5 px-4 py-2 text-sm bg-white border border-gray-300 text-gray-700 font-medium rounded-full hover:bg-gray-50 transition-colors"
                  title="このページの歌唱曲をシャッフル再生"
                >
                  <svg className="w-4 h-4" fill="currentColor" viewBox="0 0 24 24"><path d="M10.59 9.17L5.41 4 4 5.41l5.17 5.17 1.42-1.41zM14.5 4l2.04 2.04L4 18.59 5.41 20 17.96 7.46 20 9.5V4h-5.5zm.33 9.41l-1.41 1.41 3.13 3.13L14.5 20H20v-5.5l-2.04 2.04-3.13-3.13z" /></svg>
                  ランダム再生
                </button>
              </div>
              <div className="bg-white rounded-lg shadow-sm border overflow-x-auto">
                <table className="min-w-full divide-y divide-gray-200">
                  <thead className="bg-gray-50">
                    <tr>
                      <SortableTh label="楽曲" sortKey="song" sort={perfSort} dir={perfDir} onSort={handlePerfSort} />
                      <SortableTh label="歌枠" sortKey="stream" sort={perfSort} dir={perfDir} onSort={handlePerfSort} />
                      <SortableTh label="日付" sortKey="date" sort={perfSort} dir={perfDir} onSort={handlePerfSort} firstDir="desc" />
                      <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
                        時間
                      </th>
                      <th className="px-4 py-3 text-right text-xs font-medium text-gray-500 uppercase tracking-wider w-32">
                        操作
                      </th>
                    </tr>
                  </thead>
                  <tbody className="bg-white divide-y divide-gray-200">
                    {performances?.performances.map((perf, perfIndex) => (
                      <tr key={perf.id} className="hover:bg-gray-50">
                        <td className="px-4 py-4">
                          {perf.song_id ? (
                            <div>
                              <Link
                                to={`/songs/${perf.song_id}`}
                                className="text-indigo-600 hover:text-indigo-900 font-medium"
                              >
                                {perf.song_name}
                              </Link>
                              <ArtistLinks
                                artists={perf.artists}
                                fallback={perf.original_artist}
                                className="block text-xs text-gray-500"
                                linkClassName="hover:text-indigo-600"
                              />
                            </div>
                          ) : (
                            <div>
                              <span className="text-gray-900">{perf.song_name}</span>
                              <ArtistLinks
                                artists={perf.artists}
                                fallback={perf.original_artist}
                                className="block text-xs text-gray-500"
                                linkClassName="hover:text-indigo-600"
                              />
                            </div>
                          )}
                        </td>
                        <td className="px-4 py-4">
                          <Link
                            to={`/streams/${perf.stream_id}`}
                            className="text-gray-600 hover:text-gray-900 text-sm line-clamp-1"
                          >
                            {perf.stream_title}
                          </Link>
                        </td>
                        <td className="px-4 py-4 text-sm text-gray-500">
                          {(() => {
                            const streamDate = new Date(perf.stream_date);
                            const singTime = new Date(streamDate.getTime() + perf.start_seconds * 1000);
                            return singTime.toLocaleString('ja-JP', {
                              year: 'numeric',
                              month: '2-digit',
                              day: '2-digit',
                              hour: '2-digit',
                              minute: '2-digit',
                              second: '2-digit',
                              hour12: false,
                            });
                          })()}
                        </td>
                        <td className="px-4 py-4 text-sm text-gray-500 font-mono">
                          {formatTime(perf.start_seconds)}
                        </td>
                        <td className="px-4 py-4">
                          <div className="flex items-center justify-end gap-1.5">
                            <button
                              onClick={() => playPerformancesFrom(perfIndex)}
                              className="inline-flex items-center justify-center w-8 h-8 rounded-full bg-indigo-600 text-white hover:bg-indigo-700 transition-colors"
                              title="この歌唱から連続再生"
                            >
                              <svg className="w-4 h-4 ml-0.5" fill="currentColor" viewBox="0 0 24 24">
                                <path d="M8 5v14l11-7z" />
                              </svg>
                            </button>
                            <QueueAddButton track={toTrack(perf)} />
                            <a
                              href={perf.youtube_url}
                              target="_blank"
                              rel="noopener noreferrer"
                              className="inline-flex items-center justify-center w-8 h-8 rounded-full text-gray-400 hover:text-red-600 hover:bg-red-50 transition-colors"
                              title="YouTubeで開く"
                            >
                              <svg className="w-4 h-4" fill="currentColor" viewBox="0 0 24 24">
                                <path d="M23.498 6.186a3.016 3.016 0 0 0-2.122-2.136C19.505 3.545 12 3.545 12 3.545s-7.505 0-9.377.505A3.017 3.017 0 0 0 .502 6.186C0 8.07 0 12 0 12s0 3.93.502 5.814a3.016 3.016 0 0 0 2.122 2.136c1.871.505 9.376.505 9.376.505s7.505 0 9.377-.505a3.015 3.015 0 0 0 2.122-2.136C24 15.93 24 12 24 12s0-3.93-.502-5.814zM9.545 15.568V8.432L15.818 12l-6.273 3.568z" />
                              </svg>
                            </a>
                          </div>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>

              {performances && (
                <Pagination
                  page={perfPage}
                  totalPages={performances.pagination.total_pages}
                  onPageChange={setPerfPage}
                />
              )}
            </>
          )}
        </div>
      )}
    </div>
  );
}

// OrganizationPicker は事務所の手動上書き。
//
// Holodex 同期は singers.organization を毎回上書きするので、分類を直しても残らなかった。
// ここで書くのは organization_override のほうで、Holodex の値は触らない
// （＝「Holodex に戻す」を選べば最新の同期結果に戻る）。
// メタデータ編集モーダルの外に置いてあるのは、あちらが Holodex 管理チャンネルでは
// 開けない一方、この上書きはどのチャンネルでも要るため。
function OrganizationPicker({
  singer,
  organizations,
}: {
  singer: { id: string; organization?: string; organization_override?: string; organization_holodex?: string };
  organizations: Organization[];
}) {
  const queryClient = useQueryClient();
  const { showToast } = useToast();
  const [open, setOpen] = useState(false);

  const mutation = useMutation({
    mutationFn: (organization: string) => singerApi.setOrganization(singer.id, organization),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['singer', singer.id] });
      queryClient.invalidateQueries({ queryKey: ['singers'] });
      setOpen(false);
      showToast('所属を更新しました', 'success');
    },
    onError: (err: Error) => showToast(err.message, 'error'),
  });

  const holodexName = singer.organization_holodex
    ? organizations.find((o) => o.key === singer.organization_holodex)?.display_name ??
      singer.organization_holodex
    : '所属なし';

  if (!open) {
    return (
      <button
        onClick={() => setOpen(true)}
        title="所属を変更する（Holodex の分類を上書き）"
        aria-label="所属を変更する"
        className="inline-flex items-center gap-1 px-2 py-1 text-xs text-gray-500 border border-dashed border-gray-300 rounded-full hover:text-indigo-600 hover:border-indigo-300"
      >
        <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M16.862 4.487l1.687-1.688a1.875 1.875 0 112.652 2.652L10.582 16.07a4.5 4.5 0 01-1.897 1.13L6 18l.8-2.685a4.5 4.5 0 011.13-1.897l8.932-8.931z" />
        </svg>
        所属
        {/* 上書き中であることは常に見えるようにする。Holodex と違う値が出ている理由が
            分からないと、同期がおかしいのか人が変えたのか判別できない */}
        {singer.organization_override && <span className="text-amber-600">（手動）</span>}
      </button>
    );
  }

  return (
    <span className="inline-flex items-center gap-2">
      <select
        autoFocus
        defaultValue={singer.organization_override ?? ''}
        onChange={(e) => mutation.mutate(e.target.value)}
        disabled={mutation.isPending}
        className="px-2 py-1 text-sm border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500"
      >
        <option value="">Holodex に従う（{holodexName}）</option>
        {organizations.map((org) => (
          <option key={org.key} value={org.key}>
            {org.display_name}
            {org.is_unaffiliated ? '（所属なし扱い）' : ''}
          </option>
        ))}
      </select>
      <button
        onClick={() => setOpen(false)}
        title="キャンセル"
        aria-label="キャンセル"
        className="p-1 text-gray-400 hover:text-gray-700"
      >
        <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
        </svg>
      </button>
    </span>
  );
}
