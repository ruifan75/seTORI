import { useQuery } from '@tanstack/react-query';
import { Link, useParams, useSearchParams } from 'react-router-dom';
import { tagApi } from '../api/client';
import Loading from '../components/ui/Loading';
import PerformanceList from '../components/PerformanceList';
import PerformanceListToolbar from '../components/PerformanceListToolbar';
import { toPerformanceListItem } from '../utils/performanceList';
import Pagination from '../components/ui/Pagination';
import Tag from '../components/ui/Tag';

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

  const { data: perfData, isLoading: perfsLoading, isError: perfsError } = useQuery({
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

  const listItems = (perfData?.performances ?? []).map((perf) => toPerformanceListItem(perf));

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
        {/* 配信一覧でも同じタグで絞れる（並び替え・タグの組み合わせができる。issue #63） */}
        {isStream && tagId && (
          <Link
            to={`/streams?tag=${encodeURIComponent(tagId)}`}
            className="text-sm text-indigo-600 hover:underline"
            title="配信一覧で、このタグで絞り込む（並び替えや他のタグとの組み合わせができます）"
          >
            配信一覧で絞り込む →
          </Link>
        )}

      </div>

      {!isStream && perfsError ? (
        <p role="alert" className="py-12 text-center text-red-700">歌唱リストを取得できませんでした</p>
      ) : isLoading ? (
        <Loading />
      ) : total === 0 ? (
        <div className="text-center py-12 text-gray-500">
          このタグが付いた{isStream ? '配信' : '演出'}はありません
        </div>
      ) : isStream ? (
        <>
          {/* 配信カードグリッド（配信一覧と同じ形式） */}
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
          <div className="space-y-4">
            <PerformanceListToolbar items={listItems} scope="page" total={total} />
            <PerformanceList items={listItems} />
          </div>
        </>
      )}

      {totalPages > 1 && (
        <Pagination page={page} totalPages={totalPages} onPageChange={handlePageChange} />
      )}
    </div>
  );
}
