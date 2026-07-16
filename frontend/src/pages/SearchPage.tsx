import { useEffect, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Link, useSearchParams } from 'react-router-dom';
import { searchApi, singerApi, tagApi } from '../api/client';
import type { Singer } from '../api/types';
import Loading from '../components/ui/Loading';
import Pagination from '../components/ui/Pagination';
import Tag from '../components/ui/Tag';

// 詳細検索ページ。キーワード × チャンネル × タグ（AND）の複合条件で配信を絞り込める
// （Holodex の org/topic/channel 検索に相当）。キーワードには楽曲・アーティスト等の横断結果も表示。
export default function SearchPage() {
  const [searchParams, setSearchParams] = useSearchParams();
  const q = searchParams.get('q') || '';
  const singerId = searchParams.get('singer') || '';
  const tagsParam = searchParams.get('tags') || '';
  const selectedTags = tagsParam ? tagsParam.split(',') : [];
  const page = Math.max(1, parseInt(searchParams.get('page') || '1') || 1);

  const [keywordInput, setKeywordInput] = useState(q);
  useEffect(() => setKeywordInput(q), [q]);

  // URL パラメータ更新（page はリセット）
  const updateParams = (updates: Record<string, string | null>) => {
    const next = new URLSearchParams(searchParams);
    next.delete('page');
    for (const [k, v] of Object.entries(updates)) {
      if (v === null || v === '') next.delete(k);
      else next.set(k, v);
    }
    setSearchParams(next, { replace: true });
  };

  // タグカタログ
  const { data: streamTags = [] } = useQuery({ queryKey: ['stream-tags'], queryFn: tagApi.listStreamTags });

  // 選択中チャンネルの表示情報
  const { data: selectedSinger } = useQuery({
    queryKey: ['singer', singerId],
    queryFn: () => singerApi.get(singerId),
    enabled: !!singerId,
  });

  // チャンネル検索（オートコンプリート）
  const [singerQuery, setSingerQuery] = useState('');
  const [singerOpen, setSingerOpen] = useState(false);
  const { data: singerCandidates = [] } = useQuery({
    queryKey: ['singer-search', singerQuery],
    queryFn: () => singerApi.search(singerQuery, 8),
    enabled: singerOpen && singerQuery.trim().length >= 1,
  });

  // 横断検索（キーワードがあるとき）
  const { data: globalResults } = useQuery({
    queryKey: ['global-search', q],
    queryFn: () => searchApi.global(q),
    enabled: q.length >= 1,
    staleTime: 1000 * 30,
  });

  // 配信の複合検索
  const hasCondition = !!(q || singerId || selectedTags.length > 0);
  const { data: streamResults, isLoading: streamsLoading } = useQuery({
    queryKey: ['stream-search', q, singerId, tagsParam, page],
    queryFn: () => searchApi.searchStreams({ q, singerId, tags: selectedTags, page, limit: 20 }),
    enabled: hasCondition,
  });

  const toggleTag = (tagId: string) => {
    const next = selectedTags.includes(tagId)
      ? selectedTags.filter((t) => t !== tagId)
      : [...selectedTags, tagId];
    updateParams({ tags: next.join(',') });
  };

  const selectSinger = (singer: Singer) => {
    updateParams({ singer: singer.id });
    setSingerOpen(false);
    setSingerQuery('');
  };

  return (
    <div className="space-y-6">
      <h1 className="text-3xl font-bold text-gray-900">検索</h1>

      {/* Filter bar */}
      <div className="bg-white rounded-lg shadow-sm border p-4 space-y-4">
        {/* キーワード */}
        <form
          onSubmit={(e) => {
            e.preventDefault();
            updateParams({ q: keywordInput.trim() });
          }}
          className="flex gap-2"
        >
          <input
            type="text"
            value={keywordInput}
            onChange={(e) => setKeywordInput(e.target.value)}
            placeholder="キーワード（配信タイトル・楽曲・アーティスト・チャンネル）"
            className="flex-1 min-w-0 px-4 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
          />
          <button
            type="submit"
            className="px-4 py-2 bg-indigo-600 text-white rounded-lg hover:bg-indigo-700 transition-colors shrink-0"
          >
            検索
          </button>
        </form>

        {/* チャンネル絞り込み */}
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-sm font-medium text-gray-700 shrink-0">チャンネル:</span>
          {singerId && selectedSinger ? (
            <span className="inline-flex items-center gap-2 px-3 py-1 bg-indigo-100 text-indigo-700 rounded-full text-sm">
              {selectedSinger.photo_url && (
                <img src={selectedSinger.photo_url} alt="" className="w-5 h-5 rounded-full object-cover" />
              )}
              {selectedSinger.name}
              <button onClick={() => updateParams({ singer: null })} className="hover:text-indigo-900" title="解除">
                ✕
              </button>
            </span>
          ) : (
            <div className="relative">
              <input
                type="text"
                value={singerQuery}
                onChange={(e) => {
                  setSingerQuery(e.target.value);
                  setSingerOpen(true);
                }}
                onFocus={() => setSingerOpen(true)}
                onBlur={() => setTimeout(() => setSingerOpen(false), 150)}
                placeholder="チャンネルを検索..."
                className="w-56 px-3 py-1.5 text-sm border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
              />
              {singerOpen && singerCandidates.length > 0 && (
                <div className="absolute left-0 top-full mt-1 z-20 w-72 bg-white border border-gray-200 rounded-lg shadow-lg overflow-hidden">
                  {singerCandidates.map((s) => (
                    <button
                      key={s.id}
                      onMouseDown={(e) => e.preventDefault()}
                      onClick={() => selectSinger(s)}
                      className="w-full text-left px-3 py-2 hover:bg-indigo-50 transition-colors flex items-center gap-2"
                    >
                      {s.photo_url && <img src={s.photo_url} alt="" className="w-6 h-6 rounded-full object-cover shrink-0" />}
                      <span className="text-sm text-gray-900 truncate">{s.name}</span>
                    </button>
                  ))}
                </div>
              )}
            </div>
          )}
        </div>

        {/* タグ絞り込み（複数選択 = AND） */}
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-sm font-medium text-gray-700 shrink-0">タグ:</span>
          {streamTags.map((tag) => {
            const active = selectedTags.includes(tag.id);
            return (
              <button
                key={tag.id}
                onClick={() => toggleTag(tag.id)}
                className={`px-3 py-1 rounded-full text-sm font-medium border transition-colors ${
                  active ? 'text-white' : 'bg-white text-gray-600 border-gray-300 hover:border-gray-400'
                }`}
                style={active ? { backgroundColor: tag.color, borderColor: tag.color } : {}}
              >
                {tag.display_name}
              </button>
            );
          })}
          {selectedTags.length > 1 && (
            <span className="text-xs text-gray-400">※ 選択したタグをすべて持つ配信を表示</span>
          )}
        </div>
      </div>

      {/* 横断検索結果（キーワードあり時のみ） */}
      {q && globalResults && (
        <div className="grid gap-4 md:grid-cols-3">
          {globalResults.songs.length > 0 && (
            <div className="bg-white rounded-lg shadow-sm border p-4">
              <h3 className="text-sm font-semibold text-gray-500 mb-2">楽曲</h3>
              <div className="space-y-1">
                {globalResults.songs.map((song) => (
                  <Link key={song.id} to={`/songs/${song.id}`} className="block text-sm text-indigo-600 hover:text-indigo-800 truncate">
                    {song.name}
                    <span className="text-xs text-gray-400 ml-1">{song.original_artist}</span>
                  </Link>
                ))}
              </div>
            </div>
          )}
          {globalResults.artists.length > 0 && (
            <div className="bg-white rounded-lg shadow-sm border p-4">
              <h3 className="text-sm font-semibold text-gray-500 mb-2">アーティスト</h3>
              <div className="space-y-1">
                {globalResults.artists.map((a) => (
                  <Link key={a.id} to={`/artists/${a.id}`} className="block text-sm text-indigo-600 hover:text-indigo-800 truncate">
                    {a.name}
                    <span className="text-xs text-gray-400 ml-1">{a.song_count}曲</span>
                  </Link>
                ))}
              </div>
            </div>
          )}
          {globalResults.singers.length > 0 && (
            <div className="bg-white rounded-lg shadow-sm border p-4">
              <h3 className="text-sm font-semibold text-gray-500 mb-2">チャンネル</h3>
              <div className="space-y-1">
                {globalResults.singers.map((s) => (
                  <Link key={s.id} to={`/singers/${s.id}`} className="block text-sm text-indigo-600 hover:text-indigo-800 truncate">
                    {s.name}
                  </Link>
                ))}
              </div>
            </div>
          )}
        </div>
      )}

      {/* 配信結果 */}
      {!hasCondition ? (
        <div className="text-center py-12 text-gray-500">
          キーワード・チャンネル・タグのいずれかを指定して検索してください
        </div>
      ) : streamsLoading ? (
        <Loading />
      ) : (
        <>
          <div className="text-sm text-gray-500">
            歌枠 {streamResults?.pagination.total ?? 0}件
          </div>
          {streamResults && streamResults.streams.length > 0 && (
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
              {streamResults.streams.map((stream) => (
                <Link
                  key={stream.id}
                  to={`/streams/${stream.id}`}
                  className="bg-white rounded-lg shadow-sm border overflow-hidden hover:shadow-md transition-shadow group"
                >
                  <div className="relative">
                    {stream.thumbnail_url ? (
                      <img src={stream.thumbnail_url} alt={stream.title} loading="lazy" className="w-full h-48 object-cover" />
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
          )}
          {streamResults && streamResults.pagination.total_pages > 1 && (
            <Pagination
              page={page}
              totalPages={streamResults.pagination.total_pages}
              onPageChange={(p) => {
                const next = new URLSearchParams(searchParams);
                if (p <= 1) next.delete('page');
                else next.set('page', String(p));
                setSearchParams(next, { replace: true });
              }}
            />
          )}
        </>
      )}
    </div>
  );
}
