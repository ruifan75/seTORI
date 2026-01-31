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
            <div className="mt-4 flex flex-wrap items-center gap-2">
              <span className="inline-flex items-center px-3 py-1 rounded-full text-sm font-medium bg-indigo-100 text-indigo-800">
                歌唱回数: {song.performance_count}回
              </span>
              {/* iTunes/Apple Music Links */}
              {song.itunes_ids && song.itunes_ids.length > 0 && (
                <>
                  {song.itunes_ids.map((itunes) => (
                    <a
                      key={itunes.itunes_id}
                      href={`https://music.apple.com/song/${itunes.itunes_id}`}
                      target="_blank"
                      rel="noopener noreferrer"
                      className={`inline-flex items-center gap-1.5 px-3 py-1 rounded-full text-sm font-medium transition-colors ${
                        itunes.is_primary
                          ? 'bg-pink-500 text-white hover:bg-pink-600'
                          : 'bg-pink-100 text-pink-700 hover:bg-pink-200'
                      }`}
                      title={itunes.collection_name ? `${itunes.collection_name}${itunes.country ? ` (${itunes.country})` : ''}` : 'Apple Musicで開く'}
                    >
                      <svg className="w-4 h-4" viewBox="0 0 24 24" fill="currentColor">
                        <path d="M12 2C6.477 2 2 6.477 2 12s4.477 10 10 10 10-4.477 10-10S17.523 2 12 2zm4.586 14.424c-.161.27-.47.405-.747.405a.93.93 0 01-.464-.124c-1.278-.77-2.89-1.187-4.549-1.187-1.66 0-3.271.416-4.549 1.187a.93.93 0 01-.464.124c-.277 0-.586-.135-.747-.405-.27-.452-.111-1.039.346-1.301 1.499-.9 3.371-1.39 5.414-1.39s3.915.49 5.414 1.39c.457.262.616.849.346 1.301zm1.21-2.778c-.187.312-.524.493-.863.493a.996.996 0 01-.512-.143c-1.575-.94-3.492-1.44-5.421-1.44-1.93 0-3.847.5-5.421 1.44a.996.996 0 01-.512.143c-.34 0-.676-.181-.863-.493-.312-.52-.13-1.203.395-1.513 1.789-1.068 3.988-1.635 6.401-1.635s4.612.567 6.401 1.635c.525.31.707.993.395 1.513zm1.39-3.178c-.214.357-.595.565-.985.565a1.13 1.13 0 01-.584-.164C15.64 9.573 13.87 8.999 12 8.999s-3.64.574-5.617 1.87a1.13 1.13 0 01-.584.164c-.39 0-.771-.208-.985-.565-.357-.595-.148-1.37.452-1.72C7.488 7.308 9.691 6.55 12 6.55s4.512.758 6.734 2.198c.6.35.809 1.125.452 1.72z"/>
                      </svg>
                      {itunes.is_primary ? 'Apple Music' : `iTunes ${itunes.country || ''}`}
                    </a>
                  ))}
                </>
              )}
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
