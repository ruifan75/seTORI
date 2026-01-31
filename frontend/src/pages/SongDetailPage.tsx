import { useQuery } from '@tanstack/react-query';
import { useParams, useSearchParams, Link } from 'react-router-dom';
import { songApi } from '../api/client';
import Loading from '../components/ui/Loading';
import Pagination from '../components/ui/Pagination';
import Tag from '../components/ui/Tag';

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

  const { data, isLoading } = useQuery({
    queryKey: ['song', id, 'performances', page],
    queryFn: () => songApi.getPerformances(id!, page, 20),
    enabled: !!id,
  });

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
          {song.arts && (
            <img
              src={song.arts}
              alt={song.name}
              className="w-32 h-32 rounded-lg object-cover shadow-md"
            />
          )}
          <div className="flex-1">
            <h1 className="text-3xl font-bold text-gray-900">{song.name}</h1>
            {song.name_reading && (
              <p className="text-gray-500 mt-1">{song.name_reading}</p>
            )}
            <p className="text-xl text-gray-600 mt-2">{song.original_artist}</p>
            <div className="mt-4">
              <span className="inline-flex items-center px-3 py-1 rounded-full text-sm font-medium bg-indigo-100 text-indigo-800">
                歌唱回数: {song.performance_count}回
              </span>
            </div>
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
                          <p className="text-sm text-gray-500 mt-1">{perf.stream_date}</p>
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
