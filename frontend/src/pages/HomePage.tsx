import { useQuery } from '@tanstack/react-query';
import { Link } from 'react-router-dom';
import { songApi, streamApi } from '../api/client';
import Loading from '../components/ui/Loading';

export default function HomePage() {
  const { data: songsData, isLoading: songsLoading } = useQuery({
    queryKey: ['songs', 'recent'],
    queryFn: () => songApi.list(1, 10),
  });

  const { data: streamsData, isLoading: streamsLoading } = useQuery({
    queryKey: ['streams', 'recent'],
    queryFn: () => streamApi.list(1, 5),
  });

  return (
    <div className="space-y-6">
      {/* Recent Streams */}
      <section>
        <div className="flex justify-between items-center mb-4">
          <h2 className="text-2xl font-bold text-gray-900">最近の歌枠</h2>
          <Link to="/streams" className="text-indigo-600 hover:text-indigo-700 text-sm font-medium">
            すべて見る →
          </Link>
        </div>

        {streamsLoading ? (
          <Loading />
        ) : (
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
            {streamsData?.streams.map((stream) => (
              <Link
                key={stream.id}
                to={`/streams/${stream.id}`}
                className="bg-white rounded-lg shadow-sm border overflow-hidden hover:shadow-md transition-shadow"
              >
                {stream.thumbnail_url && (
                  <img
                    src={stream.thumbnail_url}
                    alt={stream.title}
                    className="w-full h-40 object-cover"
                  />
                )}
                <div className="p-4">
                  <h3 className="font-medium text-gray-900 line-clamp-2">{stream.title}</h3>
                  <p className="text-sm text-gray-500 mt-1">{stream.stream_date}</p>
                </div>
              </Link>
            ))}
          </div>
        )}
      </section>

      {/* Popular Songs */}
      <section>
        <div className="flex justify-between items-center mb-4">
          <h2 className="text-2xl font-bold text-gray-900">人気の楽曲</h2>
          <Link to="/songs" className="text-indigo-600 hover:text-indigo-700 text-sm font-medium">
            すべて見る →
          </Link>
        </div>

        {songsLoading ? (
          <Loading />
        ) : (
          <div className="bg-white rounded-lg shadow-sm border overflow-x-auto">
            <table className="min-w-full divide-y divide-gray-200">
              <thead className="bg-gray-50">
                <tr>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
                    楽曲名
                  </th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
                    アーティスト
                  </th>
                  <th className="px-6 py-3 text-right text-xs font-medium text-gray-500 uppercase tracking-wider">
                    歌唱回数
                  </th>
                </tr>
              </thead>
              <tbody className="bg-white divide-y divide-gray-200">
                {songsData?.songs.map((song) => (
                  <tr key={song.id} className="hover:bg-gray-50">
                    <td className="px-6 py-3">
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
                        <Link
                          to={`/songs/${song.id}`}
                          className="text-indigo-600 hover:text-indigo-900 font-medium truncate"
                        >
                          {song.name}
                        </Link>
                      </div>
                    </td>
                    <td className="px-6 py-4 whitespace-nowrap text-gray-500">
                      {song.original_artist}
                    </td>
                    <td className="px-6 py-4 whitespace-nowrap text-right text-gray-500">
                      {song.performance_count}回
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>
    </div>
  );
}
