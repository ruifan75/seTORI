import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { useParams, useSearchParams, Link } from 'react-router-dom';
import { songApi, itunesApi } from '../api/client';
import type { UpdateSongRequest, ITunesSearchResult } from '../api/types';
import Loading from '../components/ui/Loading';
import Pagination from '../components/ui/Pagination';
import Tag from '../components/ui/Tag';
import { useToast } from '../components/ui/Toast';

function formatTime(seconds: number): string {
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  const s = seconds % 60;

  if (h > 0) {
    return `${h}:${m.toString().padStart(2, '0')}:${s.toString().padStart(2, '0')}`;
  }
  return `${m}:${s.toString().padStart(2, '0')}`;
}

export default function SongDetailPage() {
  const { id } = useParams<{ id: string }>();
  const [searchParams, setSearchParams] = useSearchParams();
  const page = parseInt(searchParams.get('page') || '1');
  const { showToast } = useToast();
  const queryClient = useQueryClient();
  const [isEditing, setIsEditing] = useState(false);
  const [editedSong, setEditedSong] = useState<UpdateSongRequest>({
    name: '',
    name_reading: '',
    original_artist: '',
    original_artist_reading: '',
    arts: '',
    itunes_ids: [],
  });

  // iTunes 搜尋相關狀態
  const [itunesSearchQuery, setItunesSearchQuery] = useState('');
  const [itunesSearchResults, setItunesSearchResults] = useState<ITunesSearchResult[]>([]);
  const [showItunesSearch, setShowItunesSearch] = useState(false);
  const [directItunesId, setDirectItunesId] = useState('');
  const [isSearching, setIsSearching] = useState(false);

  const { data, isLoading } = useQuery({
    queryKey: ['song', id, 'performances', page],
    queryFn: () => songApi.getPerformances(id!, page, 20),
    enabled: !!id,
  });

  const updateMutation = useMutation({
    mutationFn: (data: UpdateSongRequest) => songApi.update(id!, data),
    onSuccess: () => {
      showToast('楽曲情報を更新しました', 'success');
      setIsEditing(false);
      queryClient.invalidateQueries({ queryKey: ['song', id, 'performances', page] });
    },
    onError: (error: any) => {
      showToast(error.response?.data?.message || '更新に失敗しました', 'error');
    },
  });

  const toggleEditing = () => {
    if (!isEditing && data?.song) {
      setEditedSong({
        name: data.song.name,
        name_reading: data.song.name_reading || '',
        original_artist: data.song.original_artist,
        original_artist_reading: data.song.original_artist_reading || '',
        arts: data.song.arts || '',
        itunes_ids: (data.song.itunes_ids || []).map(i => ({
          itunes_id: i.itunes_id,
          is_primary: i.is_primary,
        })),
      });
      setItunesSearchResults([]);
      setItunesSearchQuery('');
      setDirectItunesId('');
    }
    setIsEditing(!isEditing);
  };

  const handleSave = () => {
    if (!editedSong.name.trim() || !editedSong.original_artist.trim()) {
      showToast('楽曲名と原曲アーティストは必須です', 'error');
      return;
    }
    updateMutation.mutate(editedSong);
  };

  const handleCancel = () => {
    setIsEditing(false);
  };

  const handleSearchItunes = async () => {
    if (!itunesSearchQuery.trim()) {
      showToast('検索キーワードを入力してください', 'error');
      return;
    }
    
    setIsSearching(true);
    try {
      const results = await itunesApi.search(itunesSearchQuery);
      setItunesSearchResults(results.results);
      setShowItunesSearch(true);
    } catch (error) {
      showToast('iTunes検索に失敗しました', 'error');
    } finally {
      setIsSearching(false);
    }
  };

  const handleAddItunesFromSearch = async (result: ITunesSearchResult) => {
    const existingIds = (editedSong.itunes_ids || []).map(i => i.itunes_id);
    if (existingIds.includes(result.itunes_id)) {
      showToast('既に追加されています', 'error');
      return;
    }

    const newItunes = {
      itunes_id: result.itunes_id,
      is_primary: (editedSong.itunes_ids || []).length === 0,
    };

    setEditedSong({
      ...editedSong,
      itunes_ids: [...(editedSong.itunes_ids || []), newItunes],
      arts: !editedSong.arts ? result.artwork_url : editedSong.arts,
    });

    setItunesSearchResults([]);
    setItunesSearchQuery('');
    setShowItunesSearch(false);
    showToast('iTunesを追加しました', 'success');
  };

  const handleAddItunesDirectId = async () => {
    if (!directItunesId.trim()) {
      showToast('iTunes IDを入力してください', 'error');
      return;
    }

    const itunesIdNum = parseInt(directItunesId, 10);
    if (isNaN(itunesIdNum)) {
      showToast('有効なiTunes IDを入力してください', 'error');
      return;
    }

    const existingIds = (editedSong.itunes_ids || []).map(i => i.itunes_id);
    if (existingIds.includes(itunesIdNum)) {
      showToast('既に追加されています', 'error');
      return;
    }

    setIsSearching(true);
    try {
      const result = await itunesApi.queryById(itunesIdNum);
      const newItunes = {
        itunes_id: itunesIdNum,
        is_primary: (editedSong.itunes_ids || []).length === 0,
      };

      setEditedSong({
        ...editedSong,
        itunes_ids: [...(editedSong.itunes_ids || []), newItunes],
        arts: !editedSong.arts ? result.artwork_url : editedSong.arts,
      });

      setDirectItunesId('');
      showToast('iTunesを追加しました', 'success');
    } catch (error) {
      showToast('iTunes IDが見つかりません', 'error');
    } finally {
      setIsSearching(false);
    }
  };

  const handleRemoveItunes = (itunesId: number) => {
    const updated = (editedSong.itunes_ids || []).filter(i => i.itunes_id !== itunesId);
    
    // 削除したのがプライマリの場合、最初のものをプライマリに設定
    if (updated.length > 0 && !updated.some(i => i.is_primary)) {
      updated[0].is_primary = true;
    }

    setEditedSong({
      ...editedSong,
      itunes_ids: updated,
    });
  };

  const handleSetPrimary = (itunesId: number) => {
    const updated = (editedSong.itunes_ids || []).map(i => ({
      ...i,
      is_primary: i.itunes_id === itunesId,
    }));

    setEditedSong({
      ...editedSong,
      itunes_ids: updated,
    });
  };

  const handleSyncArtFromItunes = async (itunesId: number) => {
    try {
      setIsSearching(true);
      const result = await itunesApi.queryById(itunesId);
      setEditedSong({
        ...editedSong,
        arts: result.artwork_url,
      });
      showToast('アートワークを同期しました', 'success');
    } catch (error) {
      showToast('アートワークの取得に失敗しました', 'error');
    } finally {
      setIsSearching(false);
    }
  };

  const handlePageChange = (newPage: number) => {
    setSearchParams({ page: String(newPage) });
  };

  if (isLoading) {
    return <Loading />;
  }

  if (!data) {
    return (
      <div className="text-center py-12 text-gray-500">
        楽曲が見つかりませんでした
      </div>
    );
  }

  const { song, performances, pagination } = data;

  return (
    <div className="space-y-6">
      {/* Song Header */}
      <div className="bg-white rounded-lg shadow-sm border p-6">
        <div className="flex items-start gap-6">
          {!isEditing && song.arts && (
            <img
              src={song.arts}
              alt={song.name}
              className="w-32 h-32 rounded-lg object-cover shadow-md"
            />
          )}
          <div className="flex-1">
            {isEditing ? (
              <div className="space-y-4">
                <div>
                  <label className="block text-sm font-medium text-gray-700 mb-1">
                    楽曲名 *
                  </label>
                  <input
                    type="text"
                    value={editedSong.name}
                    onChange={(e) => setEditedSong({ ...editedSong, name: e.target.value })}
                    className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-indigo-500"
                    placeholder="楽曲名を入力"
                  />
                </div>
                <div>
                  <label className="block text-sm font-medium text-gray-700 mb-1">
                    読み方
                  </label>
                  <input
                    type="text"
                    value={editedSong.name_reading}
                    onChange={(e) => setEditedSong({ ...editedSong, name_reading: e.target.value })}
                    className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-indigo-500"
                    placeholder="読み方を入力"
                  />
                </div>
                <div>
                  <label className="block text-sm font-medium text-gray-700 mb-1">
                    原曲アーティスト *
                  </label>
                  <input
                    type="text"
                    value={editedSong.original_artist}
                    onChange={(e) => setEditedSong({ ...editedSong, original_artist: e.target.value })}
                    className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-indigo-500"
                    placeholder="原曲アーティストを入力"
                  />
                </div>
                <div>
                  <label className="block text-sm font-medium text-gray-700 mb-1">
                    アーティスト読み方
                  </label>
                  <input
                    type="text"
                    value={editedSong.original_artist_reading}
                    onChange={(e) => setEditedSong({ ...editedSong, original_artist_reading: e.target.value })}
                    className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-indigo-500"
                    placeholder="アーティスト読み方を入力"
                  />
                </div>
                <div>
                  <label className="block text-sm font-medium text-gray-700 mb-1">
                    アートワーク URL
                  </label>
                  <input
                    type="text"
                    value={editedSong.arts}
                    onChange={(e) => setEditedSong({ ...editedSong, arts: e.target.value })}
                    className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-indigo-500"
                    placeholder="画像URLを入力"
                  />
                  {editedSong.arts && (
                    <img
                      src={editedSong.arts}
                      alt="Preview"
                      className="mt-2 w-32 h-32 rounded-lg object-cover shadow-md"
                      onError={(e) => {
                        e.currentTarget.style.display = 'none';
                      }}
                    />
                  )}
                </div>

                {/* iTunes 編輯 */}
                <div className="border-t pt-4">
                  <h3 className="text-sm font-semibold text-gray-900 mb-3">iTunes ID 管理</h3>

                  {/* 既有的 iTunes */}
                  {(editedSong.itunes_ids || []).length > 0 && (
                    <div className="mb-4 space-y-2">
                      {editedSong.itunes_ids.map((itunes) => (
                        <div key={itunes.itunes_id} className="flex items-center gap-2 p-2 bg-gray-50 rounded-lg">
                          <div className="flex-1">
                            <a
                              href={`https://music.apple.com/song/${itunes.itunes_id}`}
                              target="_blank"
                              rel="noopener noreferrer"
                              className="text-sm font-medium text-indigo-600 hover:text-indigo-700 hover:underline"
                            >
                              ID: {itunes.itunes_id}
                            </a>
                          </div>
                          <button
                            onClick={() => handleSyncArtFromItunes(itunes.itunes_id)}
                            disabled={isSearching}
                            className="px-2 py-1 text-xs bg-blue-100 text-blue-700 rounded hover:bg-blue-200 disabled:opacity-50 disabled:cursor-not-allowed"
                            title="このiTunes IDのアートワークを設定"
                          >
                            アート設定
                          </button>
                          <button
                            onClick={() => handleSetPrimary(itunes.itunes_id)}
                            className={`px-2 py-1 text-xs rounded ${
                              itunes.is_primary
                                ? 'bg-pink-500 text-white'
                                : 'bg-gray-200 text-gray-700 hover:bg-gray-300'
                            }`}
                          >
                            {itunes.is_primary ? 'Primary' : 'Set Primary'}
                          </button>
                          <button
                            onClick={() => handleRemoveItunes(itunes.itunes_id)}
                            className="px-2 py-1 text-xs bg-red-100 text-red-700 rounded hover:bg-red-200"
                          >
                            削除
                          </button>
                        </div>
                      ))}
                    </div>
                  )}

                  {/* iTunes 検索 */}
                  <div className="space-y-2 mb-3">
                    <label className="block text-sm font-medium text-gray-700">
                      iTunes 検索
                    </label>
                    <div className="flex gap-2">
                      <input
                        type="text"
                        value={itunesSearchQuery}
                        onChange={(e) => setItunesSearchQuery(e.target.value)}
                        placeholder="曲名で検索"
                        className="flex-1 px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-indigo-500"
                        disabled={isSearching}
                      />
                      <button
                        onClick={handleSearchItunes}
                        disabled={isSearching}
                        className="px-3 py-2 bg-blue-600 text-white rounded-lg hover:bg-blue-700 disabled:opacity-50 disabled:cursor-not-allowed"
                      >
                        {isSearching ? '検索中...' : '検索'}
                      </button>
                    </div>
                  </div>

                  {/* 検索結果 */}
                  {showItunesSearch && itunesSearchResults.length > 0 && (
                    <div className="mb-3 max-h-60 overflow-y-auto border rounded-lg">
                      {itunesSearchResults.map((result) => (
                        <div
                          key={result.itunes_id}
                          className="p-2 border-b last:border-b-0 hover:bg-gray-50 flex items-center justify-between"
                        >
                          <div className="flex-1 min-w-0">
                            <p className="text-sm font-medium text-gray-900 truncate">{result.track_name}</p>
                            <p className="text-xs text-gray-600 truncate">{result.artist_name}</p>
                            {result.collection_name && (
                              <p className="text-xs text-gray-500 truncate">{result.collection_name}</p>
                            )}
                          </div>
                          <button
                            onClick={() => handleAddItunesFromSearch(result)}
                            className="ml-2 px-2 py-1 text-xs bg-green-100 text-green-700 rounded hover:bg-green-200 flex-shrink-0"
                          >
                            追加
                          </button>
                        </div>
                      ))}
                    </div>
                  )}

                  {/* 直接 iTunes ID 入力 */}
                  <div className="space-y-2">
                    <label className="block text-sm font-medium text-gray-700">
                      または iTunes ID を直接入力
                    </label>
                    <div className="flex gap-2">
                      <input
                        type="text"
                        value={directItunesId}
                        onChange={(e) => setDirectItunesId(e.target.value)}
                        placeholder="iTunes ID (数字のみ)"
                        className="flex-1 px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-indigo-500"
                        disabled={isSearching}
                      />
                      <button
                        onClick={handleAddItunesDirectId}
                        disabled={isSearching}
                        className="px-3 py-2 bg-green-600 text-white rounded-lg hover:bg-green-700 disabled:opacity-50 disabled:cursor-not-allowed"
                      >
                        {isSearching ? '追加中...' : '追加'}
                      </button>
                    </div>
                  </div>
                </div>

                <div className="flex gap-2 pt-4 border-t">
                  <button
                    onClick={handleSave}
                    disabled={updateMutation.isPending}
                    className="px-4 py-2 bg-indigo-600 text-white rounded-lg hover:bg-indigo-700 disabled:opacity-50 disabled:cursor-not-allowed"
                  >
                    {updateMutation.isPending ? '保存中...' : '保存'}
                  </button>
                  <button
                    onClick={handleCancel}
                    disabled={updateMutation.isPending}
                    className="px-4 py-2 bg-gray-200 text-gray-700 rounded-lg hover:bg-gray-300 disabled:opacity-50 disabled:cursor-not-allowed"
                  >
                    キャンセル
                  </button>
                </div>
              </div>
            ) : (
              <>
                <div className="flex items-start justify-between">
                  <div>
                    <h1 className="text-3xl font-bold text-gray-900">{song.name}</h1>
                    {song.name_reading && (
                      <p className="text-gray-500 mt-1">{song.name_reading}</p>
                    )}
                    <p className="text-xl text-gray-600 mt-2">{song.original_artist}</p>
                    {song.original_artist_reading && (
                      <p className="text-gray-500 text-sm mt-1">{song.original_artist_reading}</p>
                    )}
                  </div>
                  <button
                    onClick={toggleEditing}
                    className="px-4 py-2 bg-white border border-gray-300 text-gray-700 rounded-lg hover:bg-gray-50"
                  >
                    編集
                  </button>
                </div>
              </>
            )}
            {!isEditing && (
              <div className="mt-4 flex flex-wrap items-center gap-2">
              <span className="inline-flex items-center px-3 py-1 rounded-full text-sm font-medium bg-indigo-100 text-indigo-800">
                歌唱回数: {song.performance_count}回
              </span>
              {/* iTunes/Apple Music Links */}
              {song.itunes_ids && song.itunes_ids.some(i => i.is_primary) && (
                <>
                  {song.itunes_ids.filter(i => i.is_primary).map((itunes) => (
                    <a
                      key={itunes.itunes_id}
                      href={`https://music.apple.com/song/${itunes.itunes_id}`}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="inline-flex items-center gap-1.5 px-3 py-1 rounded-full text-sm font-medium transition-colors bg-pink-500 text-white hover:bg-pink-600"
                      title={itunes.collection_name ? `${itunes.collection_name}${itunes.country ? ` (${itunes.country})` : ''}` : 'Apple Musicで開く'}
                    >
                      <svg className="w-4 h-4" viewBox="0 0 24 24" fill="currentColor">
                        <path d="M12 2C6.477 2 2 6.477 2 12s4.477 10 10 10 10-4.477 10-10S17.523 2 12 2zm4.586 14.424c-.161.27-.47.405-.747.405a.93.93 0 01-.464-.124c-1.278-.77-2.89-1.187-4.549-1.187-1.66 0-3.271.416-4.549 1.187a.93.93 0 01-.464.124c-.277 0-.586-.135-.747-.405-.27-.452-.111-1.039.346-1.301 1.499-.9 3.371-1.39 5.414-1.39s3.915.49 5.414 1.39c.457.262.616.849.346 1.301zm1.21-2.778c-.187.312-.524.493-.863.493a.996.996 0 01-.512-.143c-1.575-.94-3.492-1.44-5.421-1.44-1.93 0-3.847.5-5.421 1.44a.996.996 0 01-.512.143c-.34 0-.676-.181-.863-.493-.312-.52-.13-1.203.395-1.513 1.789-1.068 3.988-1.635 6.401-1.635s4.612.567 6.401 1.635c.525.31.707.993.395 1.513zm1.39-3.178c-.214.357-.595.565-.985.565a1.13 1.13 0 01-.584-.164C15.64 9.573 13.87 8.999 12 8.999s-3.64.574-5.617 1.87a1.13 1.13 0 01-.584.164c-.39 0-.771-.208-.985-.565-.357-.595-.148-1.37.452-1.72C7.488 7.308 9.691 6.55 12 6.55s4.512.758 6.734 2.198c.6.35.809 1.125.452 1.72z"/>
                      </svg>
                      Apple Music
                    </a>
                  ))}
                </>
              )}
              </div>
            )}
          </div>
        </div>
      </div>

      {/* Performances List */}
      <div>
        <h2 className="text-2xl font-bold text-gray-900 mb-4">歌唱履歴</h2>

        {performances.length === 0 ? (
          <div className="text-center py-12 text-gray-500 bg-white rounded-lg border">
            歌唱履歴がありません
          </div>
        ) : (
          <>
            <div className="space-y-4">
              {performances.map((perf) => (
                <div
                  key={perf.id}
                  className="bg-white rounded-lg shadow-sm border overflow-hidden hover:shadow-md transition-shadow"
                >
                  <div className="flex">
                    {/* Thumbnail */}
                    <a
                      href={perf.youtube_url}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="flex-shrink-0 relative group"
                    >
                      {perf.thumbnail_url ? (
                        <img
                          src={perf.thumbnail_url}
                          alt={perf.stream_title}
                          className="w-48 h-28 object-cover"
                        />
                      ) : (
                        <div className="w-48 h-28 bg-gray-200 flex items-center justify-center">
                          <span className="text-gray-400">No Image</span>
                        </div>
                      )}
                      {/* Play overlay */}
                      <div className="absolute inset-0 bg-black bg-opacity-0 group-hover:bg-opacity-30 flex items-center justify-center transition-all">
                        <div className="w-12 h-12 rounded-full bg-red-600 flex items-center justify-center opacity-0 group-hover:opacity-100 transition-opacity">
                          <svg className="w-6 h-6 text-white ml-1" fill="currentColor" viewBox="0 0 24 24">
                            <path d="M8 5v14l11-7z" />
                          </svg>
                        </div>
                      </div>
                      {/* Timestamp badge */}
                      <div className="absolute bottom-1 right-1 bg-black bg-opacity-80 text-white text-xs px-1.5 py-0.5 rounded">
                        {formatTime(perf.start_seconds)}
                      </div>
                    </a>

                    {/* Content */}
                    <div className="flex-1 p-4">
                      <div className="flex items-start justify-between">
                        <div>
                          <Link
                            to={`/streams/${perf.stream_id}`}
                            className="font-medium text-gray-900 hover:text-indigo-600 line-clamp-1"
                          >
                            {perf.stream_title}
                          </Link>
                          <p className="text-sm text-gray-500 mt-1">
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
                          </p>
                        </div>
                        <a
                          href={perf.youtube_url}
                          target="_blank"
                          rel="noopener noreferrer"
                          className="flex-shrink-0 ml-4 px-3 py-1.5 bg-red-600 text-white text-sm font-medium rounded-lg hover:bg-red-700 transition-colors"
                        >
                          再生
                        </a>
                      </div>

                      {/* Tags */}
                      {perf.tags.length > 0 && (
                        <div className="flex flex-wrap gap-1.5 mt-3">
                          {perf.tags.map((tag) => (
                            <Tag key={tag.id} label={tag.display_name} color={tag.color} />
                          ))}
                        </div>
                      )}

                      {/* Singers */}
                      {perf.singers.length > 0 && (
                        <div className="flex items-center gap-2 mt-3">
                          <span className="text-xs text-gray-500">歌唱:</span>
                          <div className="flex flex-wrap gap-1">
                            {perf.singers.map((singer) => (
                              <span
                                key={singer.id}
                                className="text-xs text-gray-700 bg-gray-100 px-2 py-0.5 rounded"
                              >
                                {singer.name}
                              </span>
                            ))}
                          </div>
                        </div>
                      )}
                    </div>
                  </div>
                </div>
              ))}
            </div>

            <div className="mt-6">
              <Pagination
                page={page}
                totalPages={pagination.total_pages}
                onPageChange={handlePageChange}
              />
            </div>
          </>
        )}
      </div>
    </div>
  );
}
