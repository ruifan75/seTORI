import { useQuery } from '@tanstack/react-query';
import { Link, useSearchParams } from 'react-router-dom';
import { streamApi } from '../api/client';
import Loading from '../components/ui/Loading';
import Pagination from '../components/ui/Pagination';
import Tag from '../components/ui/Tag';
import { SortControl, type SortDir, type SortState } from '../components/ui/Sort';

export default function StreamsPage() {
  const [searchParams, setSearchParams] = useSearchParams();
  const page = parseInt(searchParams.get('page') || '1');
  const sort = searchParams.get('sort') || 'date';
  const dir: SortDir = searchParams.get('dir')
    ? (searchParams.get('dir') === 'asc' ? 'asc' : 'desc')
    : (sort === 'title' ? 'asc' : 'desc');

  const { data, isLoading } = useQuery({
    queryKey: ['streams', page, sort, dir],
    queryFn: () => streamApi.list(page, 20, sort, dir),
  });

  const buildParams = (next: { page?: number; sort?: string; dir?: SortDir }) => {
    const params: Record<string, string> = {};
    const p = next.page ?? page;
    const so = next.sort ?? sort;
    const d = next.dir ?? dir;
    if (p > 1) params.page = String(p);
    if (so !== 'date') params.sort = so;
    const naturalDir = so === 'title' ? 'asc' : 'desc';
    if (d !== naturalDir) params.dir = d;
    return params;
  };

  const handlePageChange = (newPage: number) => {
    setSearchParams(buildParams({ page: newPage }));
  };

  const handleSort = (next: SortState) => {
    setSearchParams(buildParams({ sort: next.sort, dir: next.dir, page: 1 }));
  };

  return (
    <div className="space-y-6">
      <h1 className="text-3xl font-bold text-gray-900">歌枠一覧</h1>

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
              <div className="flex items-center justify-between gap-3">
                <div className="text-sm text-gray-500">
                  {data?.pagination.total}件の歌枠
                </div>
                <SortControl
                  options={[
                    { value: 'date', label: '配信日', firstDir: 'desc' },
                    { value: 'title', label: 'タイトル', firstDir: 'asc' },
                  ]}
                  sort={sort}
                  dir={dir}
                  onSort={handleSort}
                />
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
