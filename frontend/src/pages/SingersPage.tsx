import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Link, useSearchParams } from 'react-router-dom';
import { singerApi } from '../api/client';
import Loading from '../components/ui/Loading';
import Pagination from '../components/ui/Pagination';

export default function SingersPage() {
  const queryClient = useQueryClient();
  const [searchParams, setSearchParams] = useSearchParams();
  const page = parseInt(searchParams.get('page') || '1');
  const [showAddModal, setShowAddModal] = useState(false);
  const [channelInput, setChannelInput] = useState('');

  const { data, isLoading } = useQuery({
    queryKey: ['singers', page],
    queryFn: () => singerApi.list(page, 20),
  });

  const syncMutation = useMutation({
    mutationFn: (channelId: string) => singerApi.create(channelId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['singers'] });
      setShowAddModal(false);
      setChannelInput('');
    },
  });

  const handlePageChange = (newPage: number) => {
    setSearchParams({ page: String(newPage) });
  };

  const handleAddSinger = (e: React.FormEvent) => {
    e.preventDefault();
    if (!channelInput.trim()) return;

    // 入力から Channel ID または @handle を抽出
    let channelValue = channelInput.trim();

    // YouTube URL の場合は Channel ID を抽出
    const channelUrlMatch = channelValue.match(/youtube\.com\/channel\/([^/?#]+)/);
    if (channelUrlMatch) {
      channelValue = channelUrlMatch[1];
    }

    const handleUrlMatch = channelValue.match(/youtube\.com\/@([^/?#]+)/);
    if (handleUrlMatch) {
      channelValue = `@${handleUrlMatch[1]}`;
    }

    syncMutation.mutate(channelValue);
  };

  return (
    <div className="space-y-6">
      <div className="flex justify-between items-center">
        <h1 className="text-3xl font-bold text-gray-900">チャンネル一覧</h1>

        <button
          onClick={() => setShowAddModal(true)}
          className="px-4 py-2 bg-indigo-600 text-white rounded-lg hover:bg-indigo-700 transition-colors flex items-center gap-2"
        >
          <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 4v16m8-8H4" />
          </svg>
          チャンネルを追加
        </button>
      </div>

      {isLoading ? (
        <Loading />
      ) : (
        <>
          {data?.pagination.total === 0 ? (
            <div className="text-center py-12 text-gray-500">
              チャンネルがありません。右上のボタンから追加してください。
            </div>
          ) : (
            <>
              <div className="text-sm text-gray-500">
                {data?.pagination.total}件のチャンネル
              </div>

              <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
                {data?.singers.map((singer) => (
                  <Link
                    key={singer.id}
                    to={`/singers/${singer.id}`}
                    className="bg-white rounded-lg shadow-sm border p-4 hover:shadow-md transition-shadow flex items-center gap-4"
                  >
                    {singer.photo_url ? (
                      <img
                        src={singer.photo_url}
                        alt={singer.name}
                        className="w-16 h-16 rounded-full object-cover"
                        onError={(e) => {
                          e.currentTarget.onerror = null;
                          e.currentTarget.src = `https://holodex.net/statics/channelImg/${singer.id}/50.png`;
                        }}
                      />
                    ) : (
                      <div className="w-16 h-16 rounded-full bg-gray-200 flex items-center justify-center">
                        <svg className="w-8 h-8 text-gray-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M16 7a4 4 0 11-8 0 4 4 0 018 0zM12 14a7 7 0 00-7 7h14a7 7 0 00-7-7z" />
                        </svg>
                      </div>
                    )}
                    <div className="flex-1 min-w-0">
                      <h3 className="font-medium text-gray-900 truncate">{singer.name}</h3>
                      {singer.english_name && (
                        <p className="text-sm text-gray-500 truncate">{singer.english_name}</p>
                      )}
                      {singer.organization && (
                        <span className="inline-block mt-1 px-2 py-0.5 bg-purple-100 text-purple-700 text-xs rounded-full">
                          {singer.organization}
                        </span>
                      )}
                    </div>
                    <svg className="w-5 h-5 text-gray-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5l7 7-7 7" />
                    </svg>
                  </Link>
                ))}
              </div>

              {data && (
                <Pagination
                  page={page}
                  totalPages={data.pagination.total_pages}
                  onPageChange={handlePageChange}
                />
              )}
            </>
          )}
        </>
      )}

      {/* Add Singer Modal */}
      {showAddModal && (
        <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
          <div className="bg-white rounded-lg shadow-xl p-6 w-full max-w-md mx-4">
            <h2 className="text-xl font-bold text-gray-900 mb-4">チャンネルを追加</h2>
            <form onSubmit={handleAddSinger}>
              <div className="mb-4">
                <label className="block text-sm font-medium text-gray-700 mb-2">
                  YouTube Channel ID / Handle
                </label>
                <input
                  type="text"
                  value={channelInput}
                  onChange={(e) => setChannelInput(e.target.value)}
                  placeholder="UCxxxxxxxxxxxxxxxxxxxxxx または @handle"
                  className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
                />
                <p className="mt-2 text-sm text-gray-500">
                  YouTube Channel URL、@handle、またはChannel ID を入力してください
                </p>
              </div>

              {syncMutation.isError && (
                <div className="mb-4 p-3 bg-red-50 border border-red-200 rounded-lg text-red-700 text-sm">
                  {(syncMutation.error as Error).message}
                </div>
              )}

              <div className="flex justify-end gap-3">
                <button
                  type="button"
                  onClick={() => {
                    setShowAddModal(false);
                    setChannelInput('');
                  }}
                  className="px-4 py-2 text-gray-700 hover:text-gray-900"
                >
                  キャンセル
                </button>
                <button
                  type="submit"
                  disabled={syncMutation.isPending || !channelInput.trim()}
                  className="px-4 py-2 bg-indigo-600 text-white rounded-lg hover:bg-indigo-700 transition-colors disabled:opacity-50"
                >
                  {syncMutation.isPending ? '追加中...' : 'チャンネルを追加'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  );
}
