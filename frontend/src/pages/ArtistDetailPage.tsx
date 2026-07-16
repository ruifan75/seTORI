import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom';
import { artistApi } from '../api/client';
import type { Artist } from '../api/types';
import Loading from '../components/ui/Loading';
import Pagination from '../components/ui/Pagination';
import { useToast } from '../components/ui/Toast';
import { useAuthStore, hasPermission, PERM } from '../store/auth';

// アーティスト詳細：名前・読みの修正、別アーティストへの統合、所属楽曲一覧。
export default function ArtistDetailPage() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const page = parseInt(searchParams.get('page') || '1');
  const { showToast } = useToast();
  const queryClient = useQueryClient();
  const canEdit = hasPermission(useAuthStore((s) => s.user), PERM.CONTENT_EDIT);

  const [isEditing, setIsEditing] = useState(false);
  const [editName, setEditName] = useState('');
  const [editReading, setEditReading] = useState('');
  const [mergeOpen, setMergeOpen] = useState(false);
  const [mergeQuery, setMergeQuery] = useState('');

  const { data, isLoading } = useQuery({
    queryKey: ['artist', id, page],
    queryFn: () => artistApi.get(id!, page, 20),
    enabled: !!id,
  });

  // 統合先の候補検索
  const { data: mergeCandidates } = useQuery({
    queryKey: ['artists', 'merge-search', mergeQuery],
    queryFn: () => artistApi.list(1, 8, mergeQuery),
    enabled: mergeOpen && mergeQuery.trim().length >= 1,
  });

  const invalidate = () => {
    queryClient.invalidateQueries({ queryKey: ['artist', id] });
    queryClient.invalidateQueries({ queryKey: ['artists'] });
    queryClient.invalidateQueries({ queryKey: ['songs'] });
  };

  const updateMutation = useMutation({
    mutationFn: (input: { name?: string; name_reading?: string }) => artistApi.update(id!, input),
    onSuccess: () => {
      showToast('アーティスト情報を更新しました', 'success');
      setIsEditing(false);
      invalidate();
    },
    onError: (err: Error) => showToast(`更新エラー: ${err.message}`, 'error'),
  });

  const mergeMutation = useMutation({
    mutationFn: (targetId: string) => artistApi.merge(id!, targetId),
    onSuccess: (target) => {
      showToast(`「${target.name}」に統合しました`, 'success');
      invalidate();
      navigate(`/artists/${target.id}`, { replace: true });
    },
    onError: (err: Error) => showToast(`統合エラー: ${err.message}`, 'error'),
  });

  const handleMerge = (target: Artist) => {
    if (
      !window.confirm(
        `「${data?.artist.name}」を「${target.name}」に統合しますか？\n` +
          `所属する全楽曲のアーティスト表記が「${target.name}」に変わります。` +
          `両者に同名の楽曲がある場合、その楽曲も統合されます。この操作は取り消せません。`
      )
    ) {
      return;
    }
    mergeMutation.mutate(target.id);
  };

  if (isLoading) return <Loading />;
  if (!data) {
    return <div className="text-center py-12 text-gray-500">アーティストが見つかりません</div>;
  }

  const { artist, songs, pagination } = data;

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="bg-white rounded-lg shadow-sm border p-6">
        {isEditing ? (
          <form
            onSubmit={(e) => {
              e.preventDefault();
              updateMutation.mutate({ name: editName.trim(), name_reading: editReading.trim() });
            }}
            className="space-y-3 max-w-lg"
          >
            <div>
              <label className="block text-sm font-medium text-gray-700 mb-1">アーティスト名</label>
              <input
                type="text"
                value={editName}
                onChange={(e) => setEditName(e.target.value)}
                className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
              />
              <p className="text-xs text-gray-400 mt-1">
                変更すると所属する全楽曲（{artist.song_count}曲）のアーティスト表記も更新されます
              </p>
            </div>
            <div>
              <label className="block text-sm font-medium text-gray-700 mb-1">読み（平仮名）</label>
              <input
                type="text"
                value={editReading}
                onChange={(e) => setEditReading(e.target.value)}
                placeholder="よみがな"
                className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
              />
            </div>
            <div className="flex gap-2">
              <button
                type="submit"
                disabled={updateMutation.isPending || !editName.trim()}
                className="px-4 py-2 text-sm bg-indigo-600 text-white font-medium rounded-lg hover:bg-indigo-700 disabled:opacity-50"
              >
                {updateMutation.isPending ? '保存中...' : '保存'}
              </button>
              <button
                type="button"
                onClick={() => setIsEditing(false)}
                className="px-4 py-2 text-sm bg-white border border-gray-300 text-gray-700 rounded-lg hover:bg-gray-50"
              >
                キャンセル
              </button>
            </div>
          </form>
        ) : (
          <div className="flex flex-wrap items-start justify-between gap-3">
            <div className="min-w-0">
              <h1 className="text-3xl font-bold text-gray-900 break-words">{artist.name}</h1>
              <p className="text-gray-500 mt-1">
                {artist.name_reading || <span className="text-gray-300">読み未設定</span>}
              </p>
            </div>
            <div className="flex items-center gap-2 shrink-0">
              <span className="inline-flex items-center px-3 py-1 rounded-full text-sm font-medium bg-indigo-100 text-indigo-800">
                {artist.song_count}曲
              </span>
              {canEdit && (
                <>
                  <button
                    onClick={() => {
                      setEditName(artist.name);
                      setEditReading(artist.name_reading || '');
                      setIsEditing(true);
                    }}
                    className="px-3 py-1.5 text-sm bg-white border border-gray-300 text-gray-700 rounded-lg hover:bg-gray-50"
                  >
                    編集
                  </button>
                  <button
                    onClick={() => setMergeOpen((v) => !v)}
                    className="px-3 py-1.5 text-sm bg-white border border-gray-300 text-gray-700 rounded-lg hover:bg-gray-50"
                    title="このアーティストを別のアーティストに統合します"
                  >
                    統合
                  </button>
                </>
              )}
            </div>
          </div>
        )}

        {/* Merge panel */}
        {mergeOpen && !isEditing && (
          <div className="mt-4 pt-4 border-t">
            <p className="text-sm text-gray-600 mb-2">
              統合先のアーティストを検索してください。「{artist.name}」の全楽曲が統合先の表記に変わり、このアーティストは削除されます。
            </p>
            <input
              type="text"
              value={mergeQuery}
              onChange={(e) => setMergeQuery(e.target.value)}
              placeholder="統合先アーティストを検索..."
              autoFocus
              className="w-full max-w-md px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
            />
            {mergeCandidates && (
              <div className="mt-2 max-w-md border border-gray-200 rounded-lg divide-y overflow-hidden">
                {mergeCandidates.artists.filter((a) => a.id !== artist.id).length === 0 ? (
                  <div className="px-3 py-2 text-sm text-gray-400">候補がありません</div>
                ) : (
                  mergeCandidates.artists
                    .filter((a) => a.id !== artist.id)
                    .map((a) => (
                      <button
                        key={a.id}
                        onClick={() => handleMerge(a)}
                        disabled={mergeMutation.isPending}
                        className="w-full text-left px-3 py-2 hover:bg-indigo-50 transition-colors disabled:opacity-50"
                      >
                        <span className="text-sm font-medium text-gray-900">{a.name}</span>
                        <span className="ml-2 text-xs text-gray-400">
                          {a.name_reading && `${a.name_reading} · `}
                          {a.song_count}曲
                        </span>
                      </button>
                    ))
                )}
              </div>
            )}
          </div>
        )}
      </div>

      {/* Songs */}
      <div>
        <h2 className="text-2xl font-bold text-gray-900 mb-4">楽曲</h2>
        {songs.length === 0 ? (
          <div className="text-center py-12 text-gray-500 bg-white rounded-lg border">楽曲がありません</div>
        ) : (
          <>
            <div className="bg-white rounded-lg shadow-sm border overflow-x-auto">
              <table className="w-full table-fixed divide-y divide-gray-200">
                <thead className="bg-gray-50">
                  <tr>
                    <th className="px-4 sm:px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider w-[75%]">
                      楽曲名
                    </th>
                    <th className="px-4 sm:px-6 py-3 text-right text-xs font-medium text-gray-500 uppercase tracking-wider w-[25%]">
                      歌唱回数
                    </th>
                  </tr>
                </thead>
                <tbody className="bg-white divide-y divide-gray-200">
                  {songs.map((song) => (
                    <tr key={song.id} className="hover:bg-gray-50">
                      <td className="px-4 sm:px-6 py-3 max-w-0">
                        <div className="flex items-center gap-3">
                          {song.arts ? (
                            <img src={song.arts} alt="" loading="lazy" className="w-10 h-10 object-cover rounded shrink-0" />
                          ) : (
                            <div className="w-10 h-10 bg-gray-100 rounded shrink-0 flex items-center justify-center">
                              <svg className="w-5 h-5 text-gray-300" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 19V6l12-3v13M9 19c0 1.105-1.343 2-3 2s-3-.895-3-2 1.343-2 3-2 3 .895 3 2zm12-3c0 1.105-1.343 2-3 2s-3-.895-3-2 1.343-2 3-2 3 .895 3 2z" />
                              </svg>
                            </div>
                          )}
                          <div className="min-w-0">
                            <Link
                              to={`/songs/${song.id}`}
                              className="text-indigo-600 hover:text-indigo-900 font-medium block truncate"
                            >
                              {song.name}
                            </Link>
                            {song.name_reading && (
                              <p className="text-xs text-gray-400 mt-0.5 truncate">{song.name_reading}</p>
                            )}
                          </div>
                        </div>
                      </td>
                      <td className="px-4 sm:px-6 py-4 text-right">
                        <span className="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium bg-indigo-100 text-indigo-800">
                          {song.performance_count}回
                        </span>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>

            {pagination.total_pages > 1 && (
              <div className="mt-4">
                <Pagination
                  page={page}
                  totalPages={pagination.total_pages}
                  onPageChange={(p) => setSearchParams(p <= 1 ? {} : { page: String(p) }, { replace: true })}
                />
              </div>
            )}
          </>
        )}
      </div>
    </div>
  );
}
