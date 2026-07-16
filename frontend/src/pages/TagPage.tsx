import { useQuery } from '@tanstack/react-query';
import { Link, useParams, useSearchParams } from 'react-router-dom';
import { tagApi } from '../api/client';
import Loading from '../components/ui/Loading';
import Pagination from '../components/ui/Pagination';
import Tag from '../components/ui/Tag';
import { usePlayerStore, type PlayerTrack } from '../store/player';

// タグ検索結果ページ。/tags/stream/:id は配信一覧、/tags/performance/:id は演出一覧。
export default function TagPage() {
  const { kind, id } = useParams<{ kind: 'stream' | 'performance'; id: string }>();
  const [searchParams, setSearchParams] = useSearchParams();
  const page = Math.max(1, parseInt(searchParams.get('page') || '1') || 1);

  const isStream = kind === 'stream';
  const tagId = id ?? '';

  // タグの表示名・色（カタログから引く）
  const { data: catalog = [] } = useQuery({
    queryKey: [isStream ? 'stream-tags' : 'performance-tags'],
    queryFn: isStream ? tagApi.listStreamTags : tagApi.listPerformanceTags,
  });
  const tagInfo = catalog.find((t) => t.id === tagId);

  const { data: streamData, isLoading: streamsLoading } = useQuery({
    queryKey: ['tag-streams', tagId, page],
    queryFn: () => tagApi.getStreamsByTag(tagId, page, 20),
    enabled: isStream && !!tagId,
  });

  const { data: perfData, isLoading: perfsLoading } = useQuery({
    queryKey: ['tag-performances', tagId, page],
    queryFn: () => tagApi.getPerformancesByTag(tagId, page, 20),
    enabled: !isStream && !!tagId,
  });

  const isLoading = isStream ? streamsLoading : perfsLoading;
  const total = isStream ? streamData?.pagination.total : perfData?.pagination.total;
  const totalPages = (isStream ? streamData?.pagination.total_pages : perfData?.pagination.total_pages) ?? 1;

  const handlePageChange = (newPage: number) => {
    setSearchParams(newPage <= 1 ? {} : { page: String(newPage) }, { replace: true });
  };

  // このページの演唱一覧をキューに載せて startIndex から連続再生
  const playFrom = (startIndex: number) => {
    const tracks: PlayerTrack[] = (perfData?.performances ?? []).map((p) => ({
      performanceId: p.id,
      streamId: p.stream_id,
      songId: p.song_id,
      songName: p.song_name,
      artist: p.original_artist,
      artUrl: p.arts,
      singers: p.singers.map((s) => ({ id: s.id, name: s.name })),
      streamTitle: p.stream_title,
      streamDate: p.stream_date,
      start: p.start_seconds,
      end: p.end_seconds,
    }));
    usePlayerStore.getState().playTracks(tracks, startIndex);
  };

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center gap-3">
        <h1 className="text-3xl font-bold text-gray-900">タグ検索</h1>
        {tagInfo ? (
          <span
            className="inline-flex items-center px-3 py-1 text-base font-medium border rounded-full"
            style={{
              backgroundColor: tagInfo.color + '20',
              color: tagInfo.color,
              borderColor: tagInfo.color + '40',
            }}
          >
            {tagInfo.display_name}
          </span>
        ) : (
          <span className="inline-flex items-center px-3 py-1 text-base font-medium border rounded-full bg-gray-100 text-gray-600">
            {tagId}
          </span>
        )}
        <span className="text-sm text-gray-500">
          {isStream ? '配信タグ' : '演出タグ'}
          {total !== undefined && ` · ${total}件`}
        </span>
        {!isStream && (perfData?.performances.length ?? 0) > 0 && (
          <button
            onClick={() => playFrom(0)}
            className="inline-flex items-center gap-1.5 px-3 py-1.5 text-sm bg-indigo-600 text-white font-medium rounded-full hover:bg-indigo-700 transition-colors"
            title="このページの歌唱を連続再生"
          >
            <svg className="w-4 h-4" fill="currentColor" viewBox="0 0 24 24"><path d="M8 5v14l11-7z"/></svg>
            すべて再生
          </button>
        )}
      </div>

      {isLoading ? (
        <Loading />
      ) : total === 0 ? (
        <div className="text-center py-12 text-gray-500">
          このタグが付いた{isStream ? '配信' : '演出'}はありません
        </div>
      ) : isStream ? (
        <>
          {/* 配信カードグリッド（歌枠一覧と同じ形式） */}
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
            {streamData?.streams.map((stream) => (
              <Link
                key={stream.id}
                to={`/streams/${stream.id}`}
                className="bg-white rounded-lg shadow-sm border overflow-hidden hover:shadow-md transition-shadow group"
              >
                <div className="relative">
                  {stream.thumbnail_url ? (
                    <img src={stream.thumbnail_url} alt={stream.title} className="w-full h-48 object-cover" />
                  ) : (
                    <div className="w-full h-48 bg-gray-200 flex items-center justify-center">
                      <span className="text-gray-400">No Image</span>
                    </div>
                  )}
                </div>
                <div className="p-4">
                  <h3 className="font-medium text-gray-900 line-clamp-2 group-hover:text-indigo-600 transition-colors">
                    {stream.title}
                  </h3>
                  <p className="text-sm text-gray-500 mt-1">
                    {new Date(stream.stream_date).toLocaleDateString('ja-JP')}
                  </p>
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
        </>
      ) : (
        <>
          {/* 演出リスト */}
          <div className="space-y-3">
            {perfData?.performances.map((perf, perfIndex) => (
              <div
                key={perf.id}
                className="bg-white rounded-lg shadow-sm border overflow-hidden hover:shadow-md transition-shadow flex"
              >
                <button
                  onClick={() => playFrom(perfIndex)}
                  className="relative w-28 sm:w-40 flex-shrink-0 group"
                  title="この歌唱から連続再生"
                >
                  {perf.thumbnail_url ? (
                    <img src={perf.thumbnail_url} alt="" className="w-full h-full object-cover" />
                  ) : (
                    <div className="w-full h-full bg-gray-200" />
                  )}
                  <span className="absolute inset-0 bg-black bg-opacity-0 group-hover:bg-opacity-30 flex items-center justify-center transition-all">
                    <span className="w-9 h-9 rounded-full bg-indigo-600 flex items-center justify-center opacity-0 group-hover:opacity-100 transition-opacity">
                      <svg className="w-5 h-5 text-white ml-0.5" fill="currentColor" viewBox="0 0 24 24"><path d="M8 5v14l11-7z"/></svg>
                    </span>
                  </span>
                </button>
                <div className="p-3 sm:p-4 flex-1 min-w-0">
                  <div className="flex flex-wrap items-center gap-2">
                    <Link
                      to={`/songs/${perf.song_id}`}
                      className="font-medium text-indigo-600 hover:text-indigo-800 truncate"
                    >
                      {perf.song_name}
                    </Link>
                    <span className="text-sm text-gray-500 truncate">{perf.original_artist}</span>
                    {perf.tags.map((tag) => (
                      <Tag key={tag.id} label={tag.display_name} color={tag.color} />
                    ))}
                  </div>
                  <Link
                    to={`/streams/${perf.stream_id}`}
                    className="block text-sm text-gray-600 hover:text-gray-900 truncate mt-1"
                  >
                    {perf.stream_title}
                  </Link>
                  <p className="text-xs text-gray-400 mt-1">
                    {perf.stream_date && new Date(perf.stream_date).toLocaleDateString('ja-JP')}
                    {perf.singers.length > 0 && ` · ${perf.singers.map((s) => s.name).join('、')}`}
                  </p>
                </div>
              </div>
            ))}
          </div>
        </>
      )}

      {totalPages > 1 && (
        <Pagination page={page} totalPages={totalPages} onPageChange={handlePageChange} />
      )}
    </div>
  );
}
