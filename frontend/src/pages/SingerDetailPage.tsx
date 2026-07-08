import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { useParams, Link } from 'react-router-dom';
import { singerApi, holodexApi } from '../api/client';
import Loading from '../components/ui/Loading';
import Pagination from '../components/ui/Pagination';
import Tag from '../components/ui/Tag';

type TabType = 'streams' | 'performances';
type ProcessedFilter = 'all' | 'true' | 'false';
type HiddenFilter = 'all' | 'true' | 'false';

export default function SingerDetailPage() {
  const { id } = useParams<{ id: string }>();
  const queryClient = useQueryClient();
  const [activeTab, setActiveTab] = useState<TabType>('streams');
  const [streamPage, setStreamPage] = useState(1);
  const [perfPage, setPerfPage] = useState(1);
  const [syncMode, setSyncMode] = useState<'new' | 'all'>('new');
  const [showEditModal, setShowEditModal] = useState(false);
  const [editForm, setEditForm] = useState({
    name: '',
    english_name: '',
    photo_url: '',
    organization: '',
  });
  // Filter states - デフォルトでは非表示を除外
  const [processedFilter, setProcessedFilter] = useState<ProcessedFilter>('all');
  const [hiddenFilter, setHiddenFilter] = useState<HiddenFilter>('false');

  // Singer detail
  const { data: singer, isLoading: singerLoading } = useQuery({
    queryKey: ['singer', id],
    queryFn: () => singerApi.get(id!),
    enabled: !!id,
  });

  // Streams
  const { data: streams, isLoading: streamsLoading } = useQuery({
    queryKey: ['singerStreams', id, streamPage, processedFilter, hiddenFilter],
    queryFn: () => singerApi.getStreams(id!, streamPage, 20, processedFilter, hiddenFilter),
    enabled: !!id && activeTab === 'streams',
  });

  // Performances
  const { data: performances, isLoading: perfsLoading } = useQuery({
    queryKey: ['singerPerformances', id, perfPage],
    queryFn: () => singerApi.getPerformances(id!, perfPage, 20),
    enabled: !!id && activeTab === 'performances',
  });

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

  const updateMutation = useMutation({
    mutationFn: () => singerApi.update(id!, {
      name: editForm.name,
      english_name: editForm.english_name,
      photo_url: editForm.photo_url,
      organization: editForm.organization,
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
      organization: singer.organization || '',
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
        <div className="flex items-start gap-6">
          {singer.photo_url ? (
            <img
              src={singer.photo_url}
              alt={singer.name}
              className="w-24 h-24 rounded-full object-cover"
              onError={(e) => {
                e.currentTarget.onerror = null;
                e.currentTarget.src = `https://holodex.net/statics/channelImg/${singer.id}/50.png`;
              }}
            />
          ) : (
            <div className="w-24 h-24 rounded-full bg-gray-200 flex items-center justify-center">
              <svg className="w-12 h-12 text-gray-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M16 7a4 4 0 11-8 0 4 4 0 018 0zM12 14a7 7 0 00-7 7h14a7 7 0 00-7-7z" />
              </svg>
            </div>
          )}
          <div className="flex-1">
            <h1 className="text-2xl font-bold text-gray-900">{singer.name}</h1>
            {singer.english_name && (
              <p className="text-gray-500 mt-1">{singer.english_name}</p>
            )}
            {singer.organization && (
              <span className="inline-block mt-2 px-3 py-1 bg-purple-100 text-purple-700 text-sm rounded-full">
                {singer.organization}
              </span>
            )}
            <div className="flex gap-4 mt-4 text-sm text-gray-600">
              <div>
                <span className="font-medium text-gray-900">{singer.stream_count}</span> 歌枠
              </div>
              <div>
                <span className="font-medium text-gray-900">{singer.performance_count}</span> 曲
              </div>
            </div>
          </div>
          <div className="flex gap-3">
            {singer.can_edit_metadata && (
              <button
                onClick={openEditModal}
                className="px-4 py-2 bg-gray-900 text-white rounded-lg hover:bg-gray-800 transition-colors flex items-center gap-2"
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
              <div>
                <label className="block text-sm font-medium text-gray-700 mb-1">
                  所属
                </label>
                <input
                  type="text"
                  value={editForm.organization}
                  onChange={(e) => setEditForm((prev) => ({ ...prev, organization: e.target.value }))}
                  placeholder="Independents"
                  className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
                />
              </div>
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
                    setProcessedFilter(e.target.value as ProcessedFilter);
                    setStreamPage(1);
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
                    setHiddenFilter(e.target.value as HiddenFilter);
                    setStreamPage(1);
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
                    <div className="relative w-48 flex-shrink-0">
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
                        {stream.is_processed && (
                          <span className="px-2 py-0.5 bg-green-500 text-white text-xs font-medium rounded">
                            処理済み
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
              <div className="bg-white rounded-lg shadow-sm border overflow-hidden">
                <table className="min-w-full divide-y divide-gray-200">
                  <thead className="bg-gray-50">
                    <tr>
                      <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
                        楽曲
                      </th>
                      <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
                        歌枠
                      </th>
                      <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
                        日付
                      </th>
                      <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
                        時間
                      </th>
                      <th className="px-4 py-3 text-right text-xs font-medium text-gray-500 uppercase tracking-wider w-20">
                        再生
                      </th>
                    </tr>
                  </thead>
                  <tbody className="bg-white divide-y divide-gray-200">
                    {performances?.performances.map((perf) => (
                      <tr key={perf.id} className="hover:bg-gray-50">
                        <td className="px-4 py-4">
                          {perf.song_id ? (
                            <Link
                              to={`/songs/${perf.song_id}`}
                              className="text-indigo-600 hover:text-indigo-900 font-medium"
                            >
                              {perf.song_name}
                            </Link>
                          ) : (
                            <span className="text-gray-900">{perf.song_name}</span>
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
                        <td className="px-4 py-4 text-right">
                          <a
                            href={perf.youtube_url}
                            target="_blank"
                            rel="noopener noreferrer"
                            className="inline-flex items-center justify-center w-8 h-8 rounded-full bg-red-100 text-red-600 hover:bg-red-200 transition-colors"
                          >
                            <svg className="w-4 h-4" fill="currentColor" viewBox="0 0 24 24">
                              <path d="M8 5v14l11-7z" />
                            </svg>
                          </a>
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
