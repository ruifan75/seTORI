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
      <div className="flex justify-between items-center">
        <h1 className="text-3xl font-bold text-gray-900">楽曲一覧</h1>

        {/* Search */}
        <form onSubmit={handleSearch} className="flex gap-2">
          <input
            type="text"
            value={searchInput}
            onChange={(e) => setSearchInput(e.target.value)}
            placeholder="楽曲名・アーティスト名で検索"
            className="px-4 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent w-64"
          />
          <button
            type="submit"
            className="px-4 py-2 bg-indigo-600 text-white rounded-lg hover:bg-indigo-700 transition-colors"
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
                      <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider w-[45%]">
                        楽曲名
                      </th>
                      <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider w-[40%]">
                        アーティスト
                      </th>
                      <th className="px-6 py-3 text-right text-xs font-medium text-gray-500 uppercase tracking-wider w-[15%]">
                        歌唱回数
                      </th>
                    </tr>
                  </thead>
                  <tbody className="bg-white divide-y divide-gray-200">
                    {data?.songs.map((song) => (
                      <tr key={song.id} className="hover:bg-gray-50">
                        <td className="px-6 py-4 max-w-0">
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
                        <td className="px-6 py-4 text-gray-500 max-w-0">
                          <span className="block truncate">{song.original_artist}</span>
                        </td>
                        <td className="px-6 py-4 text-right">
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
