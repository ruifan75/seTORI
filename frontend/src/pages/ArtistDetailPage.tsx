import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Link, useParams, useSearchParams } from 'react-router-dom';
import { artistApi } from '../api/client';
import Loading from '../components/ui/Loading';
import Pagination from '../components/ui/Pagination';
import { useToast } from '../components/ui/Toast';
import { useAuthStore, hasPermission, PERM } from '../store/auth';

// アーティスト詳細：読み仮名の確認・修正と、このアーティストの楽曲一覧。
export default function ArtistDetailPage() {
  const { id } = useParams<{ id: string }>();
  const [searchParams, setSearchParams] = useSearchParams();
  const page = parseInt(searchParams.get('page') || '1');
  const { showToast } = useToast();
  const queryClient = useQueryClient();
  const canEdit = hasPermission(useAuthStore((s) => s.user), PERM.CONTENT_EDIT);

  const [editingReading, setEditingReading] = useState<string | null>(null);

  const { data, isLoading } = useQuery({
    queryKey: ['artist', id, page],
    queryFn: () => artistApi.get(id!, page, 20),
    enabled: !!id,
  });

  const updateMutation = useMutation({
    mutationFn: (reading: string) => artistApi.updateReading(id!, reading),
    onSuccess: () => {
      showToast('読み仮名を更新しました', 'success');
      setEditingReading(null);
      queryClient.invalidateQueries({ queryKey: ['artist', id] });
      queryClient.invalidateQueries({ queryKey: ['artists'] });
    },
    onError: (err: Error) => showToast(`更新エラー: ${err.message}`, 'error'),
  });

  if (isLoading) return <Loading />;
  if (!data) {
    return <div className="text-center py-12 text-gray-500">アーティストが見つかりません</div>;
  }

  const { artist, songs, pagination } = data;

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="bg-white rounded-lg shadow-sm border p-6">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="min-w-0">
            <h1 className="text-3xl font-bold text-gray-900 break-words">{artist.name}</h1>
            {editingReading !== null ? (
              <form
                onSubmit={(e) => {
                  e.preventDefault();
                  updateMutation.mutate(editingReading.trim());
                }}
                className="mt-2 flex items-center gap-2"
              >
                <input
                  type="text"
                  value={editingReading}
                  onChange={(e) => setEditingReading(e.target.value)}
                  placeholder="読み（平仮名）"
                  autoFocus
                  className="px-3 py-1.5 text-sm border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent w-64"
                />
                <button
                  type="submit"
                  disabled={updateMutation.isPending}
                  className="px-3 py-1.5 text-sm bg-indigo-600 text-white rounded-lg hover:bg-indigo-700 disabled:opacity-50"
                >
                  保存
                </button>
                <button
                  type="button"
                  onClick={() => setEditingReading(null)}
                  className="px-3 py-1.5 text-sm text-gray-600 hover:text-gray-800"
                >
                  取消
                </button>
              </form>
            ) : (
              <p className="text-gray-500 mt-1 flex items-center gap-2">
                {artist.name_reading || <span className="text-gray-300">読み未設定</span>}
                {canEdit && (
                  <button
                    onClick={() => setEditingReading(artist.name_reading || '')}
                    className="text-xs text-indigo-600 hover:text-indigo-800 underline"
                    title="読み仮名を修正（一箇所直せば全楽曲に反映されます）"
                  >
                    編集
                  </button>
                )}
              </p>
            )}
          </div>
          <span className="inline-flex items-center px-3 py-1 rounded-full text-sm font-medium bg-indigo-100 text-indigo-800 shrink-0">
            {artist.song_count}曲
          </span>
        </div>
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
                      <td className="px-4 sm:px-6 py-4 max-w-0">
                        <Link
                          to={`/songs/${song.id}`}
                          className="text-indigo-600 hover:text-indigo-900 font-medium block truncate"
                        >
                          {song.name}
                        </Link>
                        {song.name_reading && (
                          <p className="text-xs text-gray-400 mt-0.5 truncate">{song.name_reading}</p>
                        )}
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
