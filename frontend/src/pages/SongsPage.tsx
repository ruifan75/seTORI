import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Link, useSearchParams } from 'react-router-dom';
import { songApi } from '../api/client';
import Loading from '../components/ui/Loading';
import Pagination from '../components/ui/Pagination';
import { SortableTh, type SortDir, type SortState } from '../components/ui/Sort';

export default function SongsPage() {
  const [searchParams, setSearchParams] = useSearchParams();
  const page = parseInt(searchParams.get('page') || '1');
  const search = searchParams.get('search') || '';
  const sort = searchParams.get('sort') || 'name';
  const dir: SortDir = searchParams.get('dir') === 'desc' ? 'desc' : 'asc';
  const [searchInput, setSearchInput] = useState(search);

  const { data, isLoading } = useQuery({
    queryKey: ['songs', page, search, sort, dir],
    queryFn: () => songApi.list(page, 20, search || undefined, sort, dir),
  });

  // 検索・ソート・ページを URL クエリにまとめる（既定値は省略）
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
        <h1 className="text-3xl font-bold text-gray-900">楽曲一覧</h1>

        {/* Search */}
        <form onSubmit={handleSearch} className="flex gap-2">
          <input
            type="text"
            value={searchInput}
            onChange={(e) => setSearchInput(e.target.value)}
            placeholder="楽曲名・アーティスト名で検索"
            className="px-4 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent flex-1 sm:flex-none sm:w-64 min-w-0"
          />
          <button
            type="submit"
            className="px-4 py-2 bg-indigo-600 text-white rounded-lg hover:bg-indigo-700 transition-colors shrink-0"
          >
            検索
          </button>
        </form>
      </div>

      {isLoading ? (
        <Loading />
      ) : (
        <>
          {data?.pagination.total === 0 ? (
            <div className="text-center py-12 text-gray-500">
              {search ? `「${search}」に一致する楽曲が見つかりませんでした` : '楽曲がありません'}
            </div>
          ) : (
            <>
              <div className="text-sm text-gray-500">
                {data?.pagination.total}件の楽曲
              </div>

              <div className="bg-white rounded-lg shadow-sm border overflow-hidden">
                <table className="w-full table-fixed divide-y divide-gray-200">
                  <thead className="bg-gray-50">
                    <tr>
                      <SortableTh
                        label="楽曲名"
                        sortKey="name"
                        sort={sort}
                        dir={dir}
                        onSort={handleSort}
                        className="w-[72%] sm:w-[45%]"
                      />
                      {/* モバイルではアーティスト列を隠し、曲名の下に表示する */}
                      <SortableTh
                        label="アーティスト"
                        sortKey="artist"
                        sort={sort}
                        dir={dir}
                        onSort={handleSort}
                        className="hidden sm:table-cell sm:w-[40%]"
                      />
                      <SortableTh
                        label="歌唱回数"
                        sortKey="performances"
                        sort={sort}
                        dir={dir}
                        onSort={handleSort}
                        align="right"
                        firstDir="desc"
                        className="w-[28%] sm:w-[15%]"
                      />
                    </tr>
                  </thead>
                  <tbody className="bg-white divide-y divide-gray-200">
                    {data?.songs.map((song) => (
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
                              <p className="text-xs text-gray-500 mt-0.5 truncate sm:hidden">{song.original_artist}</p>
                            </div>
                          </div>
                        </td>
                        <td className="hidden sm:table-cell px-6 py-4 text-gray-500 max-w-0">
                          <span className="block truncate">{song.original_artist}</span>
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
