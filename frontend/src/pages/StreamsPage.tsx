import { useQuery } from '@tanstack/react-query';
import { Link, useSearchParams } from 'react-router-dom';
import { streamApi, tagApi } from '../api/client';
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

  // 選んだ配信タグ（URL の tag を複数）。**全部を持つ配信**に絞る（AND。issue #63）。
  // 既定は何も選ばない＝全部。「歌枠一覧」だった頃の見え方に寄せて singing を既定に
  // すると、全部を見る場所が無くなる。
  const selectedTags = searchParams.getAll('tag');

  const { data, isLoading } = useQuery({
    queryKey: ['streams', page, sort, dir, selectedTags],
    queryFn: () => streamApi.list(page, 20, sort, dir, selectedTags),
  });

  // チップの語彙と件数。件数は「今の絞り込みに足すと何件になるか」（一覧と同じ母集合）
  const { data: allTags } = useQuery({
    queryKey: ['stream-tags'],
    queryFn: tagApi.listStreamTags,
    staleTime: 1000 * 60 * 30,
  });
  const { data: tagCounts } = useQuery({
    queryKey: ['streams', 'tag-counts', selectedTags],
    queryFn: () => streamApi.tagCounts(selectedTags),
  });

  const buildParams = (next: { page?: number; sort?: string; dir?: SortDir; tags?: string[] }) => {
    const params = new URLSearchParams();
    const p = next.page ?? page;
    const so = next.sort ?? sort;
    const d = next.dir ?? dir;
    const tags = next.tags ?? selectedTags;
    for (const t of tags) params.append('tag', t);
    if (p > 1) params.set('page', String(p));
    if (so !== 'date') params.set('sort', so);
    const naturalDir = so === 'title' ? 'asc' : 'desc';
    if (d !== naturalDir) params.set('dir', d);
    return params;
  };

  const toggleTag = (id: string) => {
    const tags = selectedTags.includes(id) ? selectedTags.filter((t) => t !== id) : [...selectedTags, id];
    // 絞り込みを変えたら 1 ページ目へ（前の page のままだと範囲外になりうる）
    setSearchParams(buildParams({ tags, page: 1 }));
  };

  // 件数のあるタグだけ、多い順に並べる。選んでいるものは件数が 0 でも残す（外せるように）。
  const chips = (allTags ?? [])
    .map((t) => ({ ...t, count: tagCounts?.[t.id] ?? 0 }))
    .filter((t) => t.count > 0 || selectedTags.includes(t.id))
    .sort((a, b) => b.count - a.count);

  const handlePageChange = (newPage: number) => {
    setSearchParams(buildParams({ page: newPage }));
  };

  const handleSort = (next: SortState) => {
    setSearchParams(buildParams({ sort: next.sort, dir: next.dir, page: 1 }));
  };

  return (
    <div className="space-y-6">
      <h1 className="text-3xl font-bold text-gray-900">配信一覧</h1>

      {/* 配信タグで絞る。複数選ぶと**全部を持つ**配信だけ（AND） */}
      {(chips.length > 0 || selectedTags.length > 0) && (
        <div className="flex flex-wrap items-center gap-2">
          {chips.map((t) => {
            const on = selectedTags.includes(t.id);
            return (
              <button
                key={t.id}
                onClick={() => toggleTag(t.id)}
                aria-pressed={on}
                title={on ? 'この絞り込みを外す' : `絞り込みに足す（${t.count} 件）`}
                className={`inline-flex items-center gap-1 rounded-full border px-3 py-1 text-sm transition-colors ${
                  on ? 'text-white' : 'bg-white hover:bg-gray-50'
                }`}
                style={
                  on
                    ? { backgroundColor: t.color || '#6366f1', borderColor: t.color || '#6366f1' }
                    : { color: t.color || '#4b5563', borderColor: `${t.color || '#6b7280'}66` }
                }
              >
                {t.display_name}
                <span className={on ? 'text-white/80' : 'text-gray-400'}>{t.count}</span>
              </button>
            );
          })}
          {selectedTags.length > 0 && (
            <button
              onClick={() => setSearchParams(buildParams({ tags: [], page: 1 }))}
              className="text-sm text-gray-500 hover:text-indigo-600 underline"
            >
              絞り込みを解除
            </button>
          )}
        </div>
      )}

      {isLoading ? (
        <Loading />
      ) : (
        <>
          {data?.pagination.total === 0 ? (
            <div className="text-center py-12 text-gray-500">
              {selectedTags.length > 0 ? '条件に合う配信がありません' : '配信がありません'}
            </div>
          ) : (
            <>
              <div className="flex items-center justify-between gap-3">
                <div className="text-sm text-gray-500">
                  {data?.pagination.total}件の配信
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
