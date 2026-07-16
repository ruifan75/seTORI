import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Link, useSearchParams } from 'react-router-dom';
import { streamApi, batchAnalyzeApi } from '../api/client';
import Loading from '../components/ui/Loading';
import Pagination from '../components/ui/Pagination';
import Tag from '../components/ui/Tag';
import { useToast } from '../components/ui/Toast';
import { useAuthStore, hasPermission, PERM } from '../store/auth';

export default function StreamsPage() {
  const [searchParams, setSearchParams] = useSearchParams();
  const page = parseInt(searchParams.get('page') || '1');
  const { showToast } = useToast();
  const queryClient = useQueryClient();
  const canEdit = hasPermission(useAuthStore((s) => s.user), PERM.CONTENT_EDIT);

  const { data, isLoading } = useQuery({
    queryKey: ['streams', page],
    queryFn: () => streamApi.list(page, 20),
  });

  // 一括プレ分析：実行中は 3 秒ごとに進捗をポーリング
  const { data: batchStatus } = useQuery({
    queryKey: ['batch-analyze-status'],
    queryFn: batchAnalyzeApi.status,
    enabled: canEdit,
    refetchInterval: (query) => (query.state.data?.running ? 3000 : false),
  });

  const startBatchMutation = useMutation({
    mutationFn: batchAnalyzeApi.start,
    onSuccess: () => {
      showToast('一括分析を開始しました（バックグラウンドで実行されます）', 'success');
      queryClient.invalidateQueries({ queryKey: ['batch-analyze-status'] });
    },
    onError: (err: Error) => showToast(err.message, 'error'),
  });

  const cancelBatchMutation = useMutation({
    mutationFn: batchAnalyzeApi.cancel,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['batch-analyze-status'] }),
  });

  const handlePageChange = (newPage: number) => {
    setSearchParams({ page: String(newPage) });
  };

  return (
    <div className="space-y-6">
      <div className="flex flex-col sm:flex-row sm:justify-between sm:items-center gap-3">
        <h1 className="text-3xl font-bold text-gray-900">歌枠一覧</h1>
        {canEdit && !batchStatus?.running && (
          <button
            onClick={() => startBatchMutation.mutate()}
            disabled={startBatchMutation.isPending}
            title="未処理の配信をまとめてプレ分析します（抽出→AI正規化→拍手end をキャッシュ。setlist の作成は行いません）"
            className="px-3 py-2 text-sm bg-indigo-50 text-indigo-700 border border-indigo-200 font-medium rounded-lg hover:bg-indigo-100 transition-colors disabled:opacity-50 shrink-0"
          >
            未処理を一括分析
          </button>
        )}
      </div>

      {/* 一括分析の進捗 */}
      {canEdit && batchStatus && (batchStatus.running || batchStatus.total > 0) && (
        <div className={`rounded-lg border p-3 text-sm flex flex-wrap items-center gap-3 ${
          batchStatus.running ? 'bg-indigo-50 border-indigo-200' : 'bg-white border-gray-200'
        }`}>
          {batchStatus.running ? (
            <>
              <svg className="animate-spin h-4 w-4 text-indigo-500 shrink-0" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
                <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
                <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
              </svg>
              <span className="font-medium text-indigo-700">
                一括分析中 {batchStatus.done + batchStatus.failed}/{batchStatus.total}
              </span>
              {batchStatus.current && (
                <span className="text-gray-500 truncate max-w-md">{batchStatus.current}</span>
              )}
              <button
                onClick={() => cancelBatchMutation.mutate()}
                className="ml-auto text-xs text-gray-500 hover:text-gray-800 underline shrink-0"
              >
                キャンセル
              </button>
            </>
          ) : (
            <>
              <span className="text-gray-600">
                一括分析 {batchStatus.message}：成功 {batchStatus.done} 件
                {batchStatus.failed > 0 && `・失敗 ${batchStatus.failed} 件`}
              </span>
              {batchStatus.failed > 0 && batchStatus.failed_ids && (
                <span className="text-xs text-gray-400 truncate max-w-md" title={batchStatus.failed_ids.join(', ')}>
                  （{batchStatus.failed_ids.slice(0, 3).join(', ')}{batchStatus.failed_ids.length > 3 ? ' …' : ''}）
                </span>
              )}
            </>
          )}
        </div>
      )}

      {isLoading ? (
        <Loading />
      ) : (
        <>
          {data?.pagination.total === 0 ? (
            <div className="text-center py-12 text-gray-500">
              歌枠がありません
            </div>
          ) : (
            <>
              <div className="text-sm text-gray-500">
                {data?.pagination.total}件の歌枠
              </div>

              <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
                {data?.streams.map((stream) => (
                  <Link
                    key={stream.id}
                    to={`/streams/${stream.id}`}
                    className="bg-white rounded-lg shadow-sm border overflow-hidden hover:shadow-md transition-shadow group"
                  >
                    {/* Thumbnail */}
                    <div className="relative">
                      {stream.thumbnail_url ? (
                        <img
                          src={stream.thumbnail_url}
                          alt={stream.title}
                          className="w-full h-48 object-cover"
                        />
                      ) : (
                        <div className="w-full h-48 bg-gray-200 flex items-center justify-center">
                          <span className="text-gray-400">No Image</span>
                        </div>
                      )}
                      {/* Duration badge */}
                      {stream.duration_seconds && (
                        <div className="absolute bottom-2 right-2 bg-black bg-opacity-80 text-white text-xs px-1.5 py-0.5 rounded">
                          {Math.floor(stream.duration_seconds / 3600)}:
                          {Math.floor((stream.duration_seconds % 3600) / 60)
                            .toString()
                            .padStart(2, '0')}:
                          {(stream.duration_seconds % 60).toString().padStart(2, '0')}
                        </div>
                      )}
                    </div>

                    {/* Content */}
                    <div className="p-4">
                      <h3 className="font-medium text-gray-900 line-clamp-2 group-hover:text-indigo-600 transition-colors">
                        {stream.title}
                      </h3>
                      <p className="text-sm text-gray-500 mt-1">
                        {new Date(stream.stream_date).toLocaleString('ja-JP', {
                          year: 'numeric',
                          month: '2-digit',
                          day: '2-digit',
                          hour: '2-digit',
                          minute: '2-digit',
                          second: '2-digit',
                          hour12: false
                        })}
                      </p>

                      {/* Tags */}
                      {stream.tags.length > 0 && (
                        <div className="flex flex-wrap gap-1.5 mt-3">
                          {stream.tags.map((tag) => (
                            <Tag key={tag.id} label={tag.display_name} color={tag.color} />
                          ))}
                        </div>
                      )}
                    </div>
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
    </div>
  );
}
