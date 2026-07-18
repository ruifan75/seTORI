import { useEffect, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useMutation, useQuery } from '@tanstack/react-query';
import { holodexApi, searchApi } from '../api/client';
import type { CSSProperties } from 'react';
import type { SearchTagItem, Singer } from '../api/types';
import { useAuthStore, hasPermission, PERM } from '../store/auth';
import { useToast } from './ui/Toast';

interface ActiveSearchToken {
  key: string;
  label: string;
  title: string;
  className: string;
  style?: CSSProperties;
  remove: () => void;
}

interface GlobalSearchProps {
  autoFocus?: boolean;
  expandable?: boolean;
  onExpandedChange?: (expanded: boolean) => void;
}

export default function GlobalSearch({
  autoFocus = false,
  expandable = false,
  onExpandedChange,
}: GlobalSearchProps) {
  const navigate = useNavigate();
  const { showToast } = useToast();
  const canSync = hasPermission(useAuthStore((state) => state.user), PERM.SYNC_RUN);

  const [query, setQuery] = useState('');
  const [debounced, setDebounced] = useState('');
  const [open, setOpen] = useState(false);
  const [expanded, setExpanded] = useState(autoFocus);
  const [titleQuery, setTitleQuery] = useState('');
  const [owner, setOwner] = useState<Singer | null>(null);
  const [participants, setParticipants] = useState<Singer[]>([]);
  const [vocalists, setVocalists] = useState<Singer[]>([]);
  const [streamTags, setStreamTags] = useState<SearchTagItem[]>([]);
  const [performanceTags, setPerformanceTags] = useState<SearchTagItem[]>([]);
  const [focusedTokenKey, setFocusedTokenKey] = useState<string | null>(null);
  const rootRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const tokenRefs = useRef<Array<HTMLButtonElement | null>>([]);

  useEffect(() => {
    const timer = setTimeout(() => setDebounced(query.trim()), 250);
    return () => clearTimeout(timer);
  }, [query]);

  useEffect(() => {
    if (expandable) onExpandedChange?.(expanded);
  }, [expandable, expanded, onExpandedChange]);

  const { data, isFetching } = useQuery({
    queryKey: ['global-search', debounced],
    queryFn: () => searchApi.global(debounced),
    enabled: debounced.length >= 1,
    staleTime: 1000 * 30,
  });

  const hasFilters = !!(
    titleQuery || owner || participants.length || vocalists.length || streamTags.length || performanceTags.length
  );
  const { data: filteredResults, isFetching: filtersFetching } = useQuery({
    queryKey: [
      'global-filter-preview', titleQuery, owner?.id,
      participants.map((singer) => singer.id).join(','), vocalists.map((singer) => singer.id).join(','),
      streamTags.map((tag) => tag.id).join(','), performanceTags.map((tag) => tag.id).join(','),
    ],
    queryFn: () => searchApi.searchStreams({
      q: titleQuery,
      ownerId: owner?.id,
      participantIds: participants.map((singer) => singer.id),
      vocalistIds: vocalists.map((singer) => singer.id),
      streamTags: streamTags.map((tag) => tag.id),
      performanceTags: performanceTags.map((tag) => tag.id),
      page: 1,
      limit: 4,
    }),
    enabled: hasFilters,
  });

  useEffect(() => {
    const onMouseDown = (event: MouseEvent) => {
      if (rootRef.current && !rootRef.current.contains(event.target as Node)) {
        setOpen(false);
        setExpanded(false);
      }
    };
    document.addEventListener('mousedown', onMouseDown);
    return () => document.removeEventListener('mousedown', onMouseDown);
  }, []);

  const reset = () => {
    setQuery('');
    setDebounced('');
    setTitleQuery('');
    setOwner(null);
    setParticipants([]);
    setVocalists([]);
    setStreamTags([]);
    setPerformanceTags([]);
    setFocusedTokenKey(null);
  };

  const go = (path: string) => {
    setOpen(false);
    setExpanded(false);
    reset();
    navigate(path);
  };

  const expandAndFocus = () => {
    setExpanded(true);
    setOpen(true);
    window.setTimeout(() => inputRef.current?.focus(), 0);
  };

  const buildSearchPath = (fallbackTitle = '') => {
    const params = new URLSearchParams();
    const effectiveTitle = titleQuery || fallbackTitle.trim();
    if (effectiveTitle) params.set('q', effectiveTitle);
    if (owner) params.set('channel', owner.id);
    if (participants.length) params.set('participants', participants.map((singer) => singer.id).join(','));
    if (vocalists.length) params.set('vocalists', vocalists.map((singer) => singer.id).join(','));
    if (streamTags.length) params.set('tags', streamTags.map((tag) => tag.id).join(','));
    if (performanceTags.length) params.set('performance_tags', performanceTags.map((tag) => tag.id).join(','));
    return `/search?${params.toString()}`;
  };

  const focusForNextToken = () => {
    setQuery('');
    setDebounced('');
    setFocusedTokenKey(null);
    setOpen(true);
    window.setTimeout(() => inputRef.current?.focus(), 0);
  };

  const addSingerToken = (singer: Singer, role: 'owner' | 'participant' | 'vocalist') => {
    if (role === 'owner') setOwner(singer);
    if (role === 'participant') {
      setParticipants((current) => current.some((item) => item.id === singer.id) ? current : [...current, singer]);
    }
    if (role === 'vocalist') {
      setVocalists((current) => current.some((item) => item.id === singer.id) ? current : [...current, singer]);
    }
    focusForNextToken();
  };

  const addTagToken = (tag: SearchTagItem, kind: 'stream' | 'performance') => {
    if (kind === 'stream') {
      setStreamTags((current) => current.some((item) => item.id === tag.id) ? current : [...current, tag]);
    } else {
      setPerformanceTags((current) => current.some((item) => item.id === tag.id) ? current : [...current, tag]);
    }
    focusForNextToken();
  };

  const addTitleToken = (value: string) => {
    const normalized = value.trim();
    if (!normalized) return;
    setTitleQuery(normalized);
    focusForNextToken();
  };

  const syncMutation = useMutation({
    mutationFn: (videoId: string) => holodexApi.syncVideo(videoId),
    onSuccess: (_response, videoId) => {
      showToast('動画を同期しました', 'success');
      go(`/streams/${videoId}`);
    },
    onError: (error: Error) => showToast(`同期エラー: ${error.message}`, 'error'),
  });

  const executeSearch = () => {
    const rawQuery = query.trim();
    if (!hasFilters && data?.video_id && data.video_registered) {
      go(`/streams/${data.video_id}`);
      return;
    }
    if (!hasFilters && !rawQuery) return;
    go(buildSearchPath(rawQuery));
  };

  const activeTokens: ActiveSearchToken[] = [];
  if (titleQuery) {
    activeTokens.push({
      key: 'title',
      label: `タイトル: ${titleQuery}`,
      title: `タイトルに「${titleQuery}」を含む`,
      className: 'border-gray-300 bg-white text-gray-700',
      remove: () => setTitleQuery(''),
    });
  }
  if (owner) {
    activeTokens.push({
      key: `owner-${owner.id}`,
      label: `配信元: ${owner.name}`,
      title: `配信元チャンネル: ${owner.name}`,
      className: 'border-indigo-200 bg-indigo-50 text-indigo-800',
      remove: () => setOwner(null),
    });
  }
  participants.forEach((singer) => activeTokens.push({
    key: `participant-${singer.id}`,
    label: `参加: ${singer.name}`,
    title: `参加チャンネル: ${singer.name}`,
    className: 'border-sky-200 bg-sky-50 text-sky-800',
    remove: () => setParticipants((items) => items.filter((item) => item.id !== singer.id)),
  }));
  vocalists.forEach((singer) => activeTokens.push({
    key: `vocalist-${singer.id}`,
    label: `ボーカル: ${singer.name}`,
    title: `ボーカル: ${singer.name}`,
    className: 'border-emerald-200 bg-emerald-50 text-emerald-800',
    remove: () => setVocalists((items) => items.filter((item) => item.id !== singer.id)),
  }));
  streamTags.forEach((tag) => activeTokens.push({
    key: `stream-${tag.id}`,
    label: tag.display_name,
    title: `配信タグ: ${tag.display_name}`,
    className: 'border',
    style: { color: tag.color, borderColor: `${tag.color}66`, backgroundColor: `${tag.color}12` },
    remove: () => setStreamTags((items) => items.filter((item) => item.id !== tag.id)),
  }));
  performanceTags.forEach((tag) => activeTokens.push({
    key: `performance-${tag.id}`,
    label: `楽曲: ${tag.display_name}`,
    title: `楽曲タグ: ${tag.display_name}`,
    className: 'border-fuchsia-200 bg-fuchsia-50 text-fuchsia-800',
    remove: () => setPerformanceTags((items) => items.filter((item) => item.id !== tag.id)),
  }));

  const focusedToken = activeTokens.find((token) => token.key === focusedTokenKey);

  const handleTokenKeyDown = (event: React.KeyboardEvent<HTMLButtonElement>, index: number) => {
    if (event.key === 'ArrowLeft') {
      event.preventDefault();
      tokenRefs.current[Math.max(0, index - 1)]?.focus();
      return;
    }
    if (event.key === 'ArrowRight') {
      event.preventDefault();
      if (index + 1 < activeTokens.length) tokenRefs.current[index + 1]?.focus();
      else inputRef.current?.focus();
      return;
    }
    if (event.key === 'Delete' || event.key === 'Backspace') {
      event.preventDefault();
      activeTokens[index].remove();
      setFocusedTokenKey(null);
      window.setTimeout(() => {
        if (index > 0) tokenRefs.current[index - 1]?.focus();
        else inputRef.current?.focus();
      }, 0);
      return;
    }
    if (event.key === 'Escape') {
      event.preventDefault();
      setFocusedTokenKey(null);
      inputRef.current?.focus();
    }
  };

  const handleKeyDown = (event: React.KeyboardEvent<HTMLInputElement>) => {
    // IME の候補確定 Enter は検索として扱わない。Safari は keyCode=229 で通知する。
    if (event.nativeEvent.isComposing || event.keyCode === 229) return;

    if (event.key === 'Escape') {
      setOpen(false);
      setExpanded(false);
      event.currentTarget.blur();
      return;
    }
    if (event.key === 'ArrowLeft' && query === '' && activeTokens.length > 0) {
      event.preventDefault();
      tokenRefs.current[activeTokens.length - 1]?.focus();
      return;
    }
    if (event.key === 'Backspace' && query === '') {
      if (activeTokens.length > 0) {
        event.preventDefault();
        tokenRefs.current[activeTokens.length - 1]?.focus();
      }
      return;
    }
    if (event.key !== 'Enter') return;

    event.preventDefault();
    if (data?.video_id && !hasFilters) {
      if (data.video_registered) go(`/streams/${data.video_id}`);
      return;
    }
    if (query.trim()) addTitleToken(query);
    else if (hasFilters) executeSearch();
  };

  const showPanel = open && (debounced.length >= 1 || hasFilters);
  const hasCandidates = !!data && (
    data.singers.length > 0 || data.stream_tags.length > 0 || data.performance_tags.length > 0 ||
    data.songs.length > 0 || data.streams.length > 0 || data.artists.length > 0
  );

  const handleSearchButtonClick = () => {
    if (expandable && !expanded) {
      expandAndFocus();
      return;
    }
    executeSearch();
  };

  return (
    <div
      ref={rootRef}
      className={`relative w-full min-w-0 ${open ? 'z-50' : ''} ${
        expandable
          ? expanded
            ? 'lg:mx-auto lg:flex-[0_1_36rem] lg:-translate-x-14 lg:transition-[flex-basis,transform] lg:duration-300 lg:ease-out xl:flex-[0_1_42rem] motion-reduce:transition-none'
            : 'lg:ml-auto lg:flex-[0_0_10rem] lg:transition-[flex-basis,transform] lg:duration-300 lg:ease-out motion-reduce:transition-none'
          : 'lg:w-72 xl:w-96'
      }`}
    >
      <div className="flex h-9 items-center border border-gray-300 bg-gray-50 rounded-lg focus-within:border-transparent focus-within:bg-white focus-within:ring-2 focus-within:ring-indigo-500">
        <div className="flex min-w-0 flex-1 items-center gap-1 overflow-x-auto px-2 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden">
          {activeTokens.map((token, index) => (
            <button
              key={token.key}
              ref={(element) => {
                tokenRefs.current[index] = element;
              }}
              type="button"
              onClick={() => {
                token.remove();
                setFocusedTokenKey(null);
                window.setTimeout(() => inputRef.current?.focus(), 0);
              }}
              onFocus={(event) => {
                setFocusedTokenKey(token.key);
                setOpen(true);
                setExpanded(true);
                event.currentTarget.scrollIntoView({ block: 'nearest', inline: 'center' });
              }}
              onKeyDown={(event) => handleTokenKeyDown(event, index)}
              className={`max-w-36 shrink-0 truncate border px-2 py-0.5 text-xs rounded-full outline-none focus:ring-2 focus:ring-indigo-500 focus:ring-offset-1 ${token.className}`}
              style={token.style}
              title={`${token.title}（クリックで解除）`}
            >
              {token.label} ×
            </button>
          ))}
          <input
            ref={inputRef}
            type="text"
            value={query}
            autoFocus={autoFocus}
            onChange={(event) => {
              setQuery(event.target.value);
              setOpen(true);
            }}
            onFocus={() => {
              setFocusedTokenKey(null);
              setOpen(true);
              setExpanded(true);
            }}
            onKeyDown={handleKeyDown}
            placeholder={expandable && !expanded ? '検索' : hasFilters ? '条件を追加' : '検索 / URL・動画ID'}
            className="h-full min-w-24 flex-1 border-0 bg-transparent px-1 text-sm outline-none placeholder:text-gray-400 focus:ring-0"
          />
        </div>
        <button
          type="button"
          onClick={handleSearchButtonClick}
          aria-label="検索"
          title="検索"
          className="flex h-full w-9 shrink-0 items-center justify-center border-l border-gray-200 text-indigo-700 transition-colors hover:bg-indigo-50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-indigo-500 rounded-r-lg"
        >
          <svg aria-hidden="true" className="h-[18px] w-[18px]" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="m21 21-4.35-4.35m2.35-5.4A7.75 7.75 0 1 1 3.5 11.25a7.75 7.75 0 0 1 15.5 0Z" />
          </svg>
        </button>
      </div>

      {showPanel && (
        <div className="absolute left-0 top-full z-50 mt-1 max-h-[75vh] w-[34rem] max-w-[calc(100vw-2rem)] overflow-y-auto border border-gray-200 bg-white shadow-xl rounded-lg">
          {focusedToken && (
            <div className="flex items-center gap-3 border-b border-gray-200 bg-gray-50 px-3 py-2">
              <span className="shrink-0 text-xs font-medium text-gray-400">選択中</span>
              <span className="min-w-0 break-words text-sm text-gray-800">{focusedToken.title}</span>
            </div>
          )}
          {data?.video_id && !hasFilters && (
            <div className="border-b p-2">
              {data.video_registered ? (
                <button type="button" onClick={() => go(`/streams/${data.video_id}`)} className="w-full px-3 py-2 text-left rounded hover:bg-indigo-50">
                  <span className="block text-sm font-medium text-indigo-700">歌枠を開く</span>
                  <span className="font-mono text-xs text-gray-500">{data.video_id}</span>
                </button>
              ) : canSync ? (
                <button type="button" onClick={() => syncMutation.mutate(data.video_id!)} disabled={syncMutation.isPending} className="w-full px-3 py-2 text-left rounded hover:bg-indigo-50 disabled:opacity-60">
                  <span className="block text-sm font-medium text-indigo-700">{syncMutation.isPending ? '同期中...' : 'Holodexから同期して開く'}</span>
                  <span className="font-mono text-xs text-gray-500">{data.video_id}</span>
                </button>
              ) : (
                <div className="px-3 py-2 text-sm text-gray-500">未登録の動画IDです</div>
              )}
            </div>
          )}

          {debounced && !data?.video_id && (
            <div className="border-b py-1">
              <div className="px-3 pb-1 pt-2 text-xs font-medium text-gray-400">条件として追加</div>
              {data?.singers.map((singer) => (
                <div key={singer.id}>
                  {owner?.id !== singer.id && (
                    <button type="button" onClick={() => addSingerToken(singer, 'owner')} className="flex w-full items-center gap-2 px-3 py-2 text-left hover:bg-indigo-50">
                      {singer.photo_url && <img src={singer.photo_url} alt="" className="h-7 w-7 shrink-0 rounded-full object-cover" />}
                      <span className="w-16 shrink-0 text-xs font-medium text-indigo-600">配信元</span>
                      <span className="truncate text-sm text-gray-900">{singer.name}</span>
                    </button>
                  )}
                  {!participants.some((item) => item.id === singer.id) && (
                    <button type="button" onClick={() => addSingerToken(singer, 'participant')} className="flex w-full items-center gap-2 px-3 py-2 text-left hover:bg-sky-50">
                      <span className="h-7 w-7 shrink-0" />
                      <span className="w-16 shrink-0 text-xs font-medium text-sky-600">参加</span>
                      <span className="truncate text-sm text-gray-900">{singer.name}</span>
                    </button>
                  )}
                  {!vocalists.some((item) => item.id === singer.id) && (
                    <button type="button" onClick={() => addSingerToken(singer, 'vocalist')} className="flex w-full items-center gap-2 px-3 py-2 text-left hover:bg-emerald-50">
                      <span className="h-7 w-7 shrink-0" />
                      <span className="w-16 shrink-0 text-xs font-medium text-emerald-600">ボーカル</span>
                      <span className="truncate text-sm text-gray-900">{singer.name}</span>
                    </button>
                  )}
                </div>
              ))}
              {data?.stream_tags.map((tag) => (
                <button key={`stream-tag-${tag.id}`} type="button" onClick={() => addTagToken(tag, 'stream')} className="flex w-full items-center gap-2 px-3 py-2 text-left hover:bg-amber-50">
                  <span className="h-3 w-3 shrink-0 rounded-full" style={{ backgroundColor: tag.color }} />
                  <span className="w-16 shrink-0 text-xs font-medium text-amber-700">配信タグ</span>
                  <span className="truncate text-sm text-gray-900">{tag.display_name}</span>
                </button>
              ))}
              {data?.performance_tags.map((tag) => (
                <button key={`performance-tag-${tag.id}`} type="button" onClick={() => addTagToken(tag, 'performance')} className="flex w-full items-center gap-2 px-3 py-2 text-left hover:bg-fuchsia-50">
                  <span className="h-3 w-3 shrink-0 rounded-full" style={{ backgroundColor: tag.color }} />
                  <span className="w-16 shrink-0 text-xs font-medium text-fuchsia-700">楽曲タグ</span>
                  <span className="truncate text-sm text-gray-900">{tag.display_name}</span>
                </button>
              ))}
              <button type="button" onClick={() => addTitleToken(debounced)} className="flex w-full items-center gap-2 px-3 py-2 text-left hover:bg-gray-50">
                <span className="h-3 w-3 shrink-0 border border-gray-400" />
                <span className="w-16 shrink-0 text-xs font-medium text-gray-600">タイトル</span>
                <span className="truncate text-sm text-gray-900">「{debounced}」を含む</span>
              </button>
            </div>
          )}

          {data && !data.video_id && !hasFilters && (
            <div className="border-b py-1">
              <div className="px-3 pb-1 pt-2 text-xs font-medium text-gray-400">直接開く</div>
              {data.songs.map((song) => (
                <button key={song.id} type="button" onClick={() => go(`/songs/${song.id}`)} className="w-full px-3 py-2 text-left hover:bg-gray-50">
                  <span className="block truncate text-sm text-gray-900">{song.name}</span>
                  <span className="block truncate text-xs text-gray-500">{song.original_artist}</span>
                </button>
              ))}
              {data.streams.map((stream) => (
                <button key={stream.id} type="button" onClick={() => go(`/streams/${stream.id}`)} className="flex w-full items-center gap-2 px-3 py-2 text-left hover:bg-gray-50">
                  {stream.thumbnail_url && <img src={stream.thumbnail_url} alt="" className="h-7 w-12 shrink-0 object-cover rounded" />}
                  <span className="truncate text-sm text-gray-900">{stream.title}</span>
                </button>
              ))}
              {data.artists.map((artist) => (
                <button key={artist.id} type="button" onClick={() => go(`/artists/${artist.id}`)} className="w-full px-3 py-2 text-left text-sm text-gray-900 hover:bg-gray-50">
                  {artist.name}<span className="ml-2 text-xs text-gray-400">{artist.song_count}曲</span>
                </button>
              ))}
            </div>
          )}

          {hasFilters && (
            <div className="border-b py-1">
              <div className="flex items-center justify-between px-3 pb-1 pt-2">
                <span className="text-xs font-medium text-gray-400">現在の条件</span>
                <span className="text-xs text-gray-500">{filtersFetching ? '検索中...' : `${filteredResults?.pagination.total ?? 0}件`}</span>
              </div>
              {filteredResults?.streams.map((stream) => (
                <button key={stream.id} type="button" onClick={() => go(`/streams/${stream.id}`)} className="flex w-full items-center gap-2 px-3 py-2 text-left hover:bg-gray-50">
                  {stream.thumbnail_url && <img src={stream.thumbnail_url} alt="" className="h-7 w-12 shrink-0 object-cover rounded" />}
                  <span className="min-w-0">
                    <span className="block truncate text-sm text-gray-900">{stream.title}</span>
                    <span className="text-xs text-gray-500">{stream.channel_owner?.name || new Date(stream.stream_date).toLocaleDateString('ja-JP')}</span>
                  </span>
                </button>
              ))}
              <button type="button" onClick={executeSearch} className="w-full px-3 py-2.5 text-center text-sm font-medium text-indigo-700 hover:bg-indigo-50">
                この条件で検索
              </button>
            </div>
          )}

          {debounced && !hasCandidates && !isFetching && (
            <div className="px-3 py-4 text-center text-sm text-gray-400">タイトル条件として追加できます</div>
          )}
          {isFetching && !data && <div className="px-3 py-4 text-center text-sm text-gray-400">検索中...</div>}
        </div>
      )}
    </div>
  );
}
