import { useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Link, useSearchParams } from 'react-router-dom';
import { searchApi, singerApi, tagApi } from '../api/client';
import type { PerformanceTag, Singer, StreamTag } from '../api/types';
import Loading from '../components/ui/Loading';
import Pagination from '../components/ui/Pagination';
import Tag from '../components/ui/Tag';

interface SingerConditionProps {
  label: string;
  singerIds: string[];
  onSelect: (singer: Singer) => void;
  onRemove: (singerId: string) => void;
  maxSelections?: number;
}

function SelectedSingerToken({ singerId, onRemove }: { singerId: string; onRemove: () => void }) {
  const { data: singer } = useQuery({
    queryKey: ['singer', singerId],
    queryFn: () => singerApi.get(singerId),
  });

  return (
    <button
      type="button"
      onClick={onRemove}
      className="inline-flex max-w-full items-center gap-1.5 border border-indigo-200 bg-indigo-50 px-2 py-1 text-xs text-indigo-800 rounded-full"
      title="解除"
    >
      {singer?.photo_url && <img src={singer.photo_url} alt="" className="h-4 w-4 shrink-0 rounded-full object-cover" />}
      <span className="truncate">{singer?.name || singerId}</span>
      <span aria-hidden="true">×</span>
    </button>
  );
}

function SingerCondition({ label, singerIds, onSelect, onRemove, maxSelections }: SingerConditionProps) {
  const [query, setQuery] = useState('');
  const [open, setOpen] = useState(false);

  const { data: candidates = [] } = useQuery({
    queryKey: ['singer-search', query],
    queryFn: () => singerApi.search(query, 8),
    enabled: open && query.trim().length >= 1,
  });
  const availableCandidates = candidates.filter((singer) => !singerIds.includes(singer.id));
  const canAdd = maxSelections === undefined || singerIds.length < maxSelections;

  const select = (singer: Singer) => {
    onSelect(singer);
    setQuery('');
    setOpen(false);
  };

  return (
    <div className="min-w-0">
      <label className="mb-1.5 block text-xs font-medium text-gray-500">{label}</label>
      <div className="relative">
        <div className="flex min-h-10 flex-wrap items-center gap-1.5 border border-gray-300 bg-white px-2 py-1 rounded-lg focus-within:border-transparent focus-within:ring-2 focus-within:ring-indigo-500">
          {singerIds.map((singerId) => (
            <SelectedSingerToken key={singerId} singerId={singerId} onRemove={() => onRemove(singerId)} />
          ))}
          {canAdd && (
          <input
            type="text"
            value={query}
            onChange={(event) => {
              setQuery(event.target.value);
              setOpen(true);
            }}
            onFocus={() => setOpen(true)}
            onBlur={() => setTimeout(() => setOpen(false), 150)}
            placeholder={`${label}を検索`}
              className="h-7 min-w-28 flex-1 border-0 bg-transparent px-1 text-sm outline-none focus:ring-0"
          />
          )}
        </div>
        {canAdd && open && query.trim() && (
          <div className="absolute left-0 right-0 top-full z-30 mt-1 max-h-64 overflow-y-auto border border-gray-200 bg-white shadow-lg rounded-lg">
              {availableCandidates.map((singer) => (
                <button
                  key={singer.id}
                  type="button"
                  onMouseDown={(event) => event.preventDefault()}
                  onClick={() => select(singer)}
                  className="flex w-full items-center gap-2 px-3 py-2 text-left hover:bg-indigo-50"
                >
                  {singer.photo_url && <img src={singer.photo_url} alt="" className="h-7 w-7 shrink-0 rounded-full object-cover" />}
                  <span className="truncate text-sm text-gray-900">{singer.name}</span>
                </button>
              ))}
            {availableCandidates.length === 0 && <div className="px-3 py-3 text-sm text-gray-400">該当なし</div>}
          </div>
        )}
      </div>
    </div>
  );
}

function parseIDs(value: string): string[] {
  return [...new Set(value.split(',').map((id) => id.trim()).filter(Boolean))];
}

interface TagConditionProps {
  label: string;
  tags: Array<StreamTag | PerformanceTag>;
  selected: string[];
  onToggle: (id: string) => void;
}

function TagCondition({ label, tags, selected, onToggle }: TagConditionProps) {
  return (
    <div className="flex flex-wrap items-center gap-2">
      <span className="w-24 shrink-0 text-sm font-medium text-gray-700">{label}</span>
      {tags.map((tag) => {
        const active = selected.includes(tag.id);
        return (
          <button
            key={tag.id}
            type="button"
            onClick={() => onToggle(tag.id)}
            className={`border px-3 py-1 text-sm font-medium rounded-full transition-colors ${
              active ? 'text-white' : 'border-gray-300 bg-white text-gray-600 hover:border-gray-500'
            }`}
            style={active ? { backgroundColor: tag.color, borderColor: tag.color } : undefined}
          >
            {tag.display_name}
          </button>
        );
      })}
    </div>
  );
}

export default function SearchPage() {
  const [searchParams, setSearchParams] = useSearchParams();
  const q = searchParams.get('q') || '';
  const ownerId = searchParams.get('channel') || '';
  const participantsParam = searchParams.get('participants') || searchParams.get('participant') || searchParams.get('singer') || '';
  const vocalistsParam = searchParams.get('vocalists') || searchParams.get('vocalist') || '';
  const participantIds = parseIDs(participantsParam);
  const vocalistIds = parseIDs(vocalistsParam);
  const streamTagsParam = searchParams.get('tags') || '';
  const performanceTagsParam = searchParams.get('performance_tags') || '';
  const selectedStreamTags = streamTagsParam ? streamTagsParam.split(',').filter(Boolean) : [];
  const selectedPerformanceTags = performanceTagsParam ? performanceTagsParam.split(',').filter(Boolean) : [];
  const page = Math.max(1, parseInt(searchParams.get('page') || '1') || 1);

  const keywordInputRef = useRef<HTMLInputElement>(null);
  const isComposingRef = useRef(false);
  const compositionEndedAtRef = useRef(0);

  const updateParams = (updates: Record<string, string | null>) => {
    const next = new URLSearchParams(searchParams);
    next.delete('page');
    const legacyParticipant = next.get('participant') || next.get('singer');
    if (legacyParticipant && !next.has('participants')) next.set('participants', legacyParticipant);
    const legacyVocalist = next.get('vocalist');
    if (legacyVocalist && !next.has('vocalists')) next.set('vocalists', legacyVocalist);
    next.delete('singer');
    next.delete('participant');
    next.delete('vocalist');
    for (const [key, value] of Object.entries(updates)) {
      if (!value) next.delete(key);
      else next.set(key, value);
    }
    setSearchParams(next, { replace: true });
  };

  const { data: streamTags = [] } = useQuery({ queryKey: ['stream-tags'], queryFn: tagApi.listStreamTags });
  const { data: performanceTags = [] } = useQuery({ queryKey: ['performance-tags'], queryFn: tagApi.listPerformanceTags });

  const { data: globalResults } = useQuery({
    queryKey: ['global-search', q],
    queryFn: () => searchApi.global(q),
    enabled: q.length >= 1,
    staleTime: 1000 * 30,
  });

  const hasCondition = !!(
    q || ownerId || participantIds.length || vocalistIds.length || selectedStreamTags.length || selectedPerformanceTags.length
  );
  const { data: streamResults, isLoading: streamsLoading } = useQuery({
    queryKey: [
      'stream-search', q, ownerId, participantsParam, vocalistsParam, streamTagsParam, performanceTagsParam, page,
    ],
    queryFn: () => searchApi.searchStreams({
      q,
      ownerId,
      participantIds,
      vocalistIds,
      streamTags: selectedStreamTags,
      performanceTags: selectedPerformanceTags,
      page,
      limit: 20,
    }),
    enabled: hasCondition,
  });

  const toggleListValue = (param: string, values: string[], id: string) => {
    const next = values.includes(id) ? values.filter((value) => value !== id) : [...values, id];
    updateParams({ [param]: next.join(',') });
  };

  return (
    <div className="space-y-6">
      <div className="flex items-end justify-between gap-4">
        <h1 className="text-3xl font-bold text-gray-900">検索</h1>
        {hasCondition && (
          <button type="button" onClick={() => setSearchParams({}, { replace: true })} className="text-sm text-gray-500 hover:text-gray-900">
            条件をクリア
          </button>
        )}
      </div>

      <section className="border-y border-gray-200 bg-white py-5">
        <form
          onSubmit={(event) => {
            event.preventDefault();
            // IME 候補の確定直後に発生する submit は検索として扱わない。
            if (isComposingRef.current || Date.now() - compositionEndedAtRef.current < 100) return;
            updateParams({ q: keywordInputRef.current?.value.trim() || '' });
          }}
          className="flex gap-2 px-4 sm:px-5"
        >
          <input
            type="text"
            key={q}
            ref={keywordInputRef}
            defaultValue={q}
            onCompositionStart={() => {
              isComposingRef.current = true;
            }}
            onCompositionEnd={() => {
              isComposingRef.current = false;
              compositionEndedAtRef.current = Date.now();
            }}
            placeholder="配信タイトル"
            className="h-10 min-w-0 flex-1 border border-gray-300 px-4 rounded-lg focus:border-transparent focus:ring-2 focus:ring-indigo-500"
          />
          <button type="submit" className="h-10 shrink-0 bg-indigo-600 px-4 text-sm font-medium text-white rounded-lg hover:bg-indigo-700">
            検索
          </button>
        </form>

        <div className="mt-4 grid gap-3 border-t border-gray-100 px-4 pt-4 sm:px-5 md:grid-cols-3">
          <SingerCondition
            label="配信元チャンネル"
            singerIds={ownerId ? [ownerId] : []}
            maxSelections={1}
            onSelect={(singer) => updateParams({ channel: singer.id })}
            onRemove={() => updateParams({ channel: null })}
          />
          <SingerCondition
            label="参加チャンネル"
            singerIds={participantIds}
            onSelect={(singer) => updateParams({ participants: [...participantIds, singer.id].join(',') })}
            onRemove={(singerId) => updateParams({ participants: participantIds.filter((id) => id !== singerId).join(',') })}
          />
          <SingerCondition
            label="ボーカル"
            singerIds={vocalistIds}
            onSelect={(singer) => updateParams({ vocalists: [...vocalistIds, singer.id].join(',') })}
            onRemove={(singerId) => updateParams({ vocalists: vocalistIds.filter((id) => id !== singerId).join(',') })}
          />
        </div>

        <div className="mt-4 space-y-3 border-t border-gray-100 px-4 pt-4 sm:px-5">
          <TagCondition
            label="配信タグ"
            tags={streamTags}
            selected={selectedStreamTags}
            onToggle={(id) => toggleListValue('tags', selectedStreamTags, id)}
          />
          <TagCondition
            label="楽曲タグ"
            tags={performanceTags}
            selected={selectedPerformanceTags}
            onToggle={(id) => toggleListValue('performance_tags', selectedPerformanceTags, id)}
          />
        </div>
      </section>

      {q && globalResults && (
        <div className="grid gap-4 md:grid-cols-3">
          {globalResults.songs.length > 0 && (
            <section className="border-l-2 border-indigo-200 px-4 py-2">
              <h2 className="mb-2 text-sm font-semibold text-gray-500">楽曲</h2>
              <div className="space-y-1">
                {globalResults.songs.map((song) => (
                  <Link key={song.id} to={`/songs/${song.id}`} className="block truncate text-sm text-indigo-600 hover:text-indigo-800">
                    {song.name}<span className="ml-1 text-xs text-gray-400">{song.original_artist}</span>
                  </Link>
                ))}
              </div>
            </section>
          )}
          {globalResults.artists.length > 0 && (
            <section className="border-l-2 border-emerald-200 px-4 py-2">
              <h2 className="mb-2 text-sm font-semibold text-gray-500">アーティスト</h2>
              <div className="space-y-1">
                {globalResults.artists.map((artist) => (
                  <Link key={artist.id} to={`/artists/${artist.id}`} className="block truncate text-sm text-indigo-600 hover:text-indigo-800">
                    {artist.name}<span className="ml-1 text-xs text-gray-400">{artist.song_count}曲</span>
                  </Link>
                ))}
              </div>
            </section>
          )}
          {globalResults.singers.length > 0 && (
            <section className="border-l-2 border-amber-200 px-4 py-2">
              <h2 className="mb-2 text-sm font-semibold text-gray-500">チャンネル</h2>
              <div className="space-y-1">
                {globalResults.singers.map((singer) => (
                  <Link key={singer.id} to={`/singers/${singer.id}`} className="block truncate text-sm text-indigo-600 hover:text-indigo-800">
                    {singer.name}
                  </Link>
                ))}
              </div>
            </section>
          )}
        </div>
      )}

      {!hasCondition ? (
        <div className="py-12 text-center text-gray-500">検索条件を指定してください</div>
      ) : streamsLoading ? (
        <Loading />
      ) : (
        <>
          <div className="text-sm text-gray-500">歌枠 {streamResults?.pagination.total ?? 0}件</div>
          {streamResults && streamResults.streams.length === 0 && (
            <div className="py-12 text-center text-gray-500">条件に一致する歌枠がありません</div>
          )}
          {streamResults && streamResults.streams.length > 0 && (
            <div className="grid grid-cols-1 gap-5 md:grid-cols-2 lg:grid-cols-3">
              {streamResults.streams.map((stream) => (
                <Link
                  key={stream.id}
                  to={`/streams/${stream.id}`}
                  className="overflow-hidden border border-gray-200 bg-white rounded-lg transition-shadow hover:shadow-md"
                >
                  {stream.thumbnail_url ? (
                    <img src={stream.thumbnail_url} alt="" loading="lazy" className="aspect-video w-full object-cover" />
                  ) : (
                    <div className="aspect-video w-full bg-gray-200" />
                  )}
                  <div className="p-4">
                    <h2 className="line-clamp-2 font-medium text-gray-900">{stream.title}</h2>
                    <p className="mt-1 truncate text-sm text-gray-500">
                      {stream.channel_owner?.name || 'チャンネル不明'} · {new Date(stream.stream_date).toLocaleDateString('ja-JP')}
                    </p>
                    {stream.tags.length > 0 && (
                      <div className="mt-3 flex flex-wrap gap-1.5">
                        {stream.tags.map((tag) => <Tag key={tag.id} label={tag.display_name} color={tag.color} />)}
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
              onPageChange={(nextPage) => {
                const next = new URLSearchParams(searchParams);
                if (nextPage <= 1) next.delete('page');
                else next.set('page', String(nextPage));
                setSearchParams(next, { replace: true });
              }}
            />
          )}
        </>
      )}
    </div>
  );
}
