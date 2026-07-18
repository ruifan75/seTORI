import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Link, useSearchParams } from 'react-router-dom';
import { artistApi } from '../api/client';
import Loading from '../components/ui/Loading';
import Pagination from '../components/ui/Pagination';
import ReadingsIO from '../components/ReadingsIO';
import { SortableTh, type SortDir, type SortState } from '../components/ui/Sort';
import { useToast } from '../components/ui/Toast';
import { useAuthStore, hasPermission, PERM } from '../store/auth';

// 原曲アーティスト一覧。表頭クリックで名前順／楽曲数順を昇降切替、名前・読みで検索できる。
export default function ArtistsPage() {
  const [searchParams, setSearchParams] = useSearchParams();
  const page = parseInt(searchParams.get('page') || '1');
  const search = searchParams.get('search') || '';
  const sort = searchParams.get('sort') || 'name';
  const dir: SortDir = searchParams.get('dir') === 'desc' ? 'desc' : 'asc';
  const [searchInput, setSearchInput] = useState(search);
  const { showToast } = useToast();
  const queryClient = useQueryClient();
  const canEdit = hasPermission(useAuthStore((s) => s.user), PERM.CONTENT_EDIT);

  const { data, isLoading } = useQuery({
    queryKey: ['artists', page, search, sort, dir],
    queryFn: () => artistApi.list(page, 50, search || undefined, sort, dir),
  });

  // AI 読み仮名補完（1回で最大30件ずつ、複数回押して続きを処理）
  const backfillMutation = useMutation({
    mutationFn: () => artistApi.backfillReadings(),
    onSuccess: (r) => {
      const msg = `読み補完: アーティスト${r.artists_updated}件・曲名${r.songs_updated}件`;
      showToast(r.warning ? `${msg}（${r.warning}）` : msg, r.warning ? 'error' : 'success');
      queryClient.invalidateQueries({ queryKey: ['artists'] });
      queryClient.invalidateQueries({ queryKey: ['songs'] });
    },
    onError: (err: Error) => showToast(`補完エラー: ${err.message}`, 'error'),
  });

  // 検索・ソート・ページを URL クエリにまとめる（既定値は URL から省略する）
  const buildParams = (next: { search?: string; sort?: string; dir?: SortDir; page?: number }) => {
    const params: Record<string, string> = {};
    const s = next.search ?? search;
    const so = next.sort ?? sort;
    const d = next.dir ?? dir;
    const p = next.page ?? 1;
    if (s) params.search = s;
    if (so !== 'name') params.sort = so;
    if (d !== 'asc') params.dir = d;
    if (p > 1) params.page = String(p);
    return params;
  };

  const handleSearch = (e: React.FormEvent) => {
    e.preventDefault();
    setSearchParams(buildParams({ search: searchInput, page: 1 }));
  };

  const handleSort = (next: SortState) => {
    setSearchParams(buildParams({ sort: next.sort, dir: next.dir, page: 1 }));
  };

  const handlePageChange = (newPage: number) => {
    setSearchParams(buildParams({ page: newPage }));
  };

  return (
    <div className="space-y-6">
      <div className="flex flex-col sm:flex-row sm:justify-between sm:items-center gap-3">
        <h1 className="text-3xl font-bold text-gray-900">アーティスト一覧</h1>

        <div className="flex gap-2 items-center flex-wrap justify-end">
          {canEdit && (
            <>
              <button
                onClick={() => backfillMutation.mutate()}
                disabled={backfillMutation.isPending}
                title="読みが未整備のアーティスト・曲名の読み仮名を AI で補完します（1回で最大30件ずつ）"
                className="px-3 py-2 text-sm bg-indigo-50 text-indigo-700 border border-indigo-200 font-medium rounded-lg hover:bg-indigo-100 transition-colors disabled:opacity-50 shrink-0"
              >
                {backfillMutation.isPending ? 'AI補完中...' : '読みをAIで補完'}
              </button>
              <ReadingsIO />
            </>
          )}
          <form onSubmit={handleSearch} className="flex gap-2">
            <input
              type="text"
              value={searchInput}
              onChange={(e) => setSearchInput(e.target.value)}
              placeholder="アーティスト名・読みで検索"
              className="px-4 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent flex-1 sm:flex-none sm:w-56 min-w-0"
            />
            <button
              type="submit"
              className="px-4 py-2 bg-indigo-600 text-white rounded-lg hover:bg-indigo-700 transition-colors shrink-0"
            >
              検索
            </button>
          </form>
        </div>
      </div>

      {isLoading ? (
        <Loading />
      ) : data?.pagination.total === 0 ? (
        <div className="text-center py-12 text-gray-500">
          {search ? `「${search}」に一致するアーティストが見つかりませんでした` : 'アーティストがありません'}
        </div>
      ) : (
        <>
          <div className="text-sm text-gray-500">{data?.pagination.total}名のアーティスト</div>

          <div className="bg-white rounded-lg shadow-sm border overflow-x-auto">
            <table className="w-full table-fixed divide-y divide-gray-200">
              <thead className="bg-gray-50">
                <tr>
                  <SortableTh
                    label="アーティスト"
                    sortKey="name"
                    sort={sort}
                    dir={dir}
                    onSort={handleSort}
                    className="w-[75%]"
                  />
                  <SortableTh
                    label="楽曲数"
                    sortKey="songs"
                    sort={sort}
                    dir={dir}
                    onSort={handleSort}
                    align="right"
                    firstDir="desc"
                    className="w-[25%]"
                  />
                </tr>
              </thead>
              <tbody className="bg-white divide-y divide-gray-200">
                {data?.artists.map((artist) => (
                  <tr key={artist.id} className="hover:bg-gray-50">
                    <td className="px-4 sm:px-6 py-4 max-w-0">
                      <Link
                        to={`/artists/${artist.id}`}
                        className="text-indigo-600 hover:text-indigo-900 font-medium block truncate"
                      >
                        {artist.name}
                      </Link>
                    </td>
                    <td className="px-4 sm:px-6 py-4 text-right">
                      <span className="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium bg-indigo-100 text-indigo-800">
                        {artist.song_count}曲
                      </span>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          {data && data.pagination.total_pages > 1 && (
            <Pagination
              page={page}
              totalPages={data.pagination.total_pages}
              onPageChange={handlePageChange}
            />
          )}
        </>
      )}
    </div>
  );
}
