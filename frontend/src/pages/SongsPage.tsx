import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Link, useSearchParams } from 'react-router-dom';
import { songApi } from '../api/client';
import Loading from '../components/ui/Loading';
import Pagination from '../components/ui/Pagination';

export default function SongsPage() {
  const [searchParams, setSearchParams] = useSearchParams();
  const page = parseInt(searchParams.get('page') || '1');
  const search = searchParams.get('search') || '';
  const [searchInput, setSearchInput] = useState(search);

  const { data, isLoading } = useQuery({
    queryKey: ['songs', page, search],
    queryFn: () => songApi.list(page, 20, search || undefined),
  });

  const handleSearch = (e: React.FormEvent) => {
    e.preventDefault();
    setSearchParams({ search: searchInput, page: '1' });
  };

  const handlePageChange = (newPage: number) => {
    setSearchParams({ search, page: String(newPage) });
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
                      <th className="px-4 sm:px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider w-[72%] sm:w-[45%]">
                        楽曲名
                      </th>
                      {/* モバイルではアーティスト列を隠し、曲名の下に表示する */}
                      <th className="hidden sm:table-cell px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider sm:w-[40%]">
                        アーティスト
                      </th>
                      <th className="px-4 sm:px-6 py-3 text-right text-xs font-medium text-gray-500 uppercase tracking-wider w-[28%] sm:w-[15%]">
                        歌唱回数
                      </th>
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
                              {song.name_reading && (
                                <p className="text-xs text-gray-400 mt-0.5 truncate hidden sm:block">{song.name_reading}</p>
                              )}
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
