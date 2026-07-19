import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useInfiniteQuery, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link } from 'react-router-dom';
import { homeApi, songApi, tagApi } from '../api/client';
import type { Performance, Singer, Stream } from '../api/types';
import Loading from '../components/ui/Loading';
import Tag from '../components/ui/Tag';
import { useToast } from '../components/ui/ToastContext';
import QueueAddButton from '../components/QueueAddButton';
import ArtistLinks from '../components/ArtistLinks';
import { usePlayerStore, type PlayerTrack } from '../store/player';

const RECOMMENDATION_PAGE_SIZE = 20;
const RECOMMENDATION_PLAYBACK_MIN = 50;

function uniqueSongs(perfs: Performance[]): Performance[] {
  const songIds = new Set<string>();
  return perfs.filter((perf) => {
    if (songIds.has(perf.song_id)) return false;
    songIds.add(perf.song_id);
    return true;
  });
}

// 配信横断の歌唱一覧を再生キューのトラックへ変換する（TagPage と同形）
function toTracks(perfs: Performance[]): PlayerTrack[] {
  return perfs.map((p) => ({
    performanceId: p.id,
    streamId: p.stream_id,
    songId: p.song_id,
    songName: p.song_name,
    artist: p.original_artist,
    artists: p.artists ?? [],
    artUrl: p.arts,
    singers: p.singers.map((s) => ({ id: s.id, name: s.name })),
    streamTitle: p.stream_title,
    streamDate: p.stream_date,
    start: p.start_seconds,
    end: p.end_seconds,
  }));
}

function SingerImage({ singer }: { singer: Singer }) {
  const [imageFailed, setImageFailed] = useState(false);
  const imageUrl = singer.photo_url || `https://holodex.net/statics/channelImg/${singer.id}/50.png`;

  if (imageFailed) {
    return <span className="flex h-full w-full items-center justify-center">{singer.name.trim().charAt(0) || '?'}</span>;
  }

  return (
    <img
      src={imageUrl}
      alt=""
      loading="lazy"
      className="h-full w-full object-cover"
      onError={() => setImageFailed(true)}
    />
  );
}

function SingerAvatar({ singer }: { singer: Singer }) {
  return (
    <Link
      to={`/singers/${singer.id}`}
      aria-label={singer.name}
      title={singer.name}
      className="relative inline-flex h-7 w-7 shrink-0 items-center justify-center overflow-hidden rounded-full border-2 border-white bg-indigo-100 text-[10px] font-semibold text-indigo-700 shadow-sm transition-transform hover:z-10 hover:-translate-y-0.5 focus-visible:z-10 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-indigo-500"
    >
      <SingerImage singer={singer} />
    </Link>
  );
}

function SingerOverflowMenu({ singers }: { singers: Singer[] }) {
  const buttonRef = useRef<HTMLButtonElement>(null);
  const popoverRef = useRef<HTMLDivElement>(null);
  const [open, setOpen] = useState(false);

  const toggle = () => {
    const button = buttonRef.current;
    const popover = popoverRef.current;
    if (!button || !popover) return;
    if (popover.matches(':popover-open')) {
      popover.hidePopover();
      return;
    }

    const rect = button.getBoundingClientRect();
    const menuWidth = 224;
    popover.style.left = `${Math.min(Math.max(8, rect.right - menuWidth), window.innerWidth - menuWidth - 8)}px`;
    popover.style.top = `${rect.bottom + 6}px`;
    popover.showPopover();

    window.requestAnimationFrame(() => {
      if (rect.bottom + 6 + popover.offsetHeight > window.innerHeight - 8) {
        popover.style.top = `${Math.max(8, rect.top - popover.offsetHeight - 6)}px`;
      }
    });
  };

  return (
    <>
      <button
        ref={buttonRef}
        type="button"
        onClick={toggle}
        aria-label={`ほか${singers.length}チャンネルを表示`}
        aria-expanded={open}
        title={`ほか${singers.length}チャンネル`}
        className="relative inline-flex h-7 min-w-7 shrink-0 items-center justify-center rounded-full border-2 border-white bg-gray-700 px-1 text-[9px] font-semibold text-white shadow-sm transition-colors hover:z-10 hover:bg-gray-800 focus-visible:z-10 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-indigo-500"
      >
        +{singers.length}
      </button>
      <div
        ref={popoverRef}
        popover="auto"
        onToggle={(event) => setOpen(event.currentTarget.matches(':popover-open'))}
        className="fixed inset-auto z-[100] m-0 w-56 rounded-lg border border-gray-200 bg-white p-1 shadow-xl"
      >
        <div className="px-2 py-1.5 text-xs font-medium text-gray-400">その他のチャンネル</div>
        {singers.map((singer) => (
          <Link
            key={singer.id}
            to={`/singers/${singer.id}`}
            onClick={() => popoverRef.current?.hidePopover()}
            className="flex items-center gap-2 rounded-md px-2 py-2 text-sm text-gray-700 hover:bg-indigo-50 hover:text-indigo-700 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-indigo-500"
          >
            <span className="inline-flex h-8 w-8 shrink-0 overflow-hidden rounded-full bg-indigo-100 text-[10px] font-semibold text-indigo-700">
              <SingerImage singer={singer} />
            </span>
            <span className="min-w-0 truncate">{singer.name}</span>
          </Link>
        ))}
      </div>
    </>
  );
}

function SingerAvatars({ singers }: { singers: Singer[] }) {
  const uniqueSingers = singers.filter(
    (singer, index) => singers.findIndex((candidate) => candidate.id === singer.id) === index,
  );
  if (uniqueSingers.length === 0) return null;

  const visibleSingers = uniqueSingers.slice(0, 4);
  const remainingSingers = uniqueSingers.slice(4);

  return (
    <div className="flex shrink-0 -space-x-2 pl-2" aria-label="チャンネル">
      {visibleSingers.map((singer) => <SingerAvatar key={singer.id} singer={singer} />)}
      {remainingSingers.length > 0 && (
        <SingerOverflowMenu singers={remainingSingers} />
      )}
    </div>
  );
}

// セクション見出し（右側に「すべて見る」リンクや操作ボタンを置ける）
function SectionHeader({ title, linkTo, children }: { title: string; linkTo?: string; children?: React.ReactNode }) {
  return (
    <div className="flex flex-wrap items-center gap-3 mb-4">
      <h2 className="text-2xl font-bold text-gray-900">{title}</h2>
      {children}
      {linkTo && (
        <Link to={linkTo} className="ml-auto text-indigo-600 hover:text-indigo-700 text-sm font-medium">
          すべて見る →
        </Link>
      )}
    </div>
  );
}

// 横スクロールの配信カード列（オリジナル曲・歌ってみた用）
function StreamCardRow({ streams }: { streams: Stream[] }) {
  const rowRef = useRef<HTMLDivElement>(null);
  const [scrollState, setScrollState] = useState({ canGoLeft: false, canGoRight: true });

  const updateScrollState = useCallback(() => {
    const row = rowRef.current;
    if (!row) return;
    const next = {
      canGoLeft: row.scrollLeft > 2,
      canGoRight: row.scrollLeft + row.clientWidth < row.scrollWidth - 2,
    };
    setScrollState((current) =>
      current.canGoLeft === next.canGoLeft && current.canGoRight === next.canGoRight ? current : next,
    );
  }, []);

  useEffect(() => {
    const frame = window.requestAnimationFrame(updateScrollState);
    window.addEventListener('resize', updateScrollState);
    return () => {
      window.cancelAnimationFrame(frame);
      window.removeEventListener('resize', updateScrollState);
    };
  }, [streams.length, updateScrollState]);

  const scroll = (direction: -1 | 1) => {
    const row = rowRef.current;
    if (!row) return;
    row.scrollBy({
      left: direction * Math.max(240, row.clientWidth - 160),
      behavior: 'smooth',
    });
  };

  return (
    <div className="relative -mx-1">
      <div
        ref={rowRef}
        onScroll={updateScrollState}
        className="flex touch-pan-x snap-x snap-mandatory gap-4 overflow-x-auto scroll-smooth px-1 pb-2 overscroll-x-contain [scrollbar-width:none] [&::-webkit-scrollbar]:hidden"
      >
        {streams.map((stream) => (
          <article
            key={stream.id}
            className="w-56 shrink-0 snap-start bg-white rounded-lg shadow-sm border hover:shadow-md transition-shadow group"
          >
            <Link to={`/streams/${stream.id}`} className="block overflow-hidden rounded-t-lg">
              {stream.thumbnail_url ? (
                <img src={stream.thumbnail_url} alt={stream.title} loading="lazy" className="w-full h-32 object-cover transition-transform group-hover:scale-[1.02]" />
              ) : (
                <div className="w-full h-32 bg-gray-200 flex items-center justify-center text-gray-400 text-sm">
                  No Image
                </div>
              )}
            </Link>
            <div className="p-3">
              <Link
                to={`/streams/${stream.id}`}
                className="block h-10 text-sm font-medium leading-5 text-gray-900 line-clamp-2 group-hover:text-indigo-600 transition-colors"
              >
                {stream.title}
              </Link>
              <div className="mt-2 flex min-h-7 items-center justify-between gap-2">
                <time dateTime={stream.stream_date} className="min-w-0 text-xs text-gray-500">
                  {new Date(stream.stream_date).toLocaleDateString('ja-JP')}
                </time>
                <SingerAvatars singers={stream.participants ?? []} />
              </div>
            </div>
          </article>
        ))}
      </div>

      {scrollState.canGoLeft && (
        <button
          type="button"
          onClick={() => scroll(-1)}
          aria-label="前の配信を表示"
          title="前へ"
          className="absolute inset-y-0 left-0 z-10 hidden w-14 items-center justify-start bg-gradient-to-r from-gray-50 via-gray-50/80 to-transparent pl-2 opacity-0 transition-opacity md:flex md:hover:opacity-100 md:focus-visible:opacity-100"
        >
          <span className="flex h-10 w-10 items-center justify-center rounded-full bg-white text-gray-700 shadow-lg ring-1 ring-black/5">
            <svg className="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="m15 19-7-7 7-7" />
            </svg>
          </span>
        </button>
      )}

      {scrollState.canGoRight && (
        <button
          type="button"
          onClick={() => scroll(1)}
          aria-label="次の配信を表示"
          title="次へ"
          className="absolute inset-y-0 right-0 z-10 hidden w-14 items-center justify-end bg-gradient-to-l from-gray-50 via-gray-50/80 to-transparent pr-2 opacity-0 transition-opacity md:flex md:hover:opacity-100 md:focus-visible:opacity-100"
        >
          <span className="flex h-10 w-10 items-center justify-center rounded-full bg-white text-gray-700 shadow-lg ring-1 ring-black/5">
            <svg className="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="m9 5 7 7-7 7" />
            </svg>
          </span>
        </button>
      )}
    </div>
  );
}

export default function HomePage() {
  const { showToast } = useToast();
  const queryClient = useQueryClient();
  const recommendationRowRef = useRef<HTMLDivElement>(null);
  const pendingRecommendationReveal = useRef<{ previousCount: number; previousScrollWidth: number } | null>(null);
  const recommendationLoadInFlight = useRef(false);
  const [recommendationScroll, setRecommendationScroll] = useState({ canGoLeft: false, canGoRight: true });
  const [isPreparingRecommendations, setIsPreparingRecommendations] = useState(false);

  // おすすめは20曲ずつ追加する。pageParam に既出曲を渡し、API 側でも重複を除外する。
  const {
    data: recoData,
    isLoading: recoLoading,
    fetchNextPage: fetchMoreRecommendations,
    hasNextPage: hasMoreRecommendations,
    isFetchingNextPage: isFetchingMoreRecommendations,
  } = useInfiniteQuery({
    queryKey: ['random-performances', 'home', 'infinite'],
    initialPageParam: [] as string[],
    queryFn: ({ pageParam }) => homeApi.randomPerformances(RECOMMENDATION_PAGE_SIZE, pageParam),
    getNextPageParam: (lastPage, allPages) => {
      if (lastPage.performances.length < RECOMMENDATION_PAGE_SIZE) return undefined;
      return uniqueSongs(allPages.flatMap((page) => page.performances)).map((perf) => perf.song_id);
    },
    staleTime: Infinity,
  });

  const { data: singingData, isLoading: singingLoading } = useQuery({
    queryKey: ['tag-streams', 'singing', 'home'],
    queryFn: () => tagApi.getStreamsByTag('singing', 1, 6),
  });

  const { data: originalData } = useQuery({
    queryKey: ['tag-streams', 'original_song', 'home'],
    queryFn: () => tagApi.getStreamsByTag('original_song', 1, 10),
  });

  const { data: coverData } = useQuery({
    queryKey: ['tag-streams', 'music_cover', 'home'],
    queryFn: () => tagApi.getStreamsByTag('music_cover', 1, 10),
  });

  const { data: songsData, isLoading: songsLoading } = useQuery({
    queryKey: ['songs', 'popular'],
    queryFn: () => songApi.list(1, 10, undefined, 'performances', 'desc'),
  });

  const reco = useMemo(
    () => uniqueSongs(recoData?.pages.flatMap((page) => page.performances) ?? []),
    [recoData],
  );

  const updateRecommendationScroll = useCallback(() => {
    const row = recommendationRowRef.current;
    if (!row) return;
    const next = {
      canGoLeft: row.scrollLeft > 2,
      canGoRight: row.scrollLeft + row.clientWidth < row.scrollWidth - 2,
    };
    setRecommendationScroll((current) =>
      current.canGoLeft === next.canGoLeft && current.canGoRight === next.canGoRight ? current : next,
    );
  }, []);

  useEffect(() => {
    const frame = window.requestAnimationFrame(updateRecommendationScroll);
    window.addEventListener('resize', updateRecommendationScroll);
    return () => {
      window.cancelAnimationFrame(frame);
      window.removeEventListener('resize', updateRecommendationScroll);
    };
  }, [reco.length, updateRecommendationScroll]);

  useEffect(() => {
    const pending = pendingRecommendationReveal.current;
    if (!pending || reco.length <= pending.previousCount) return;
    pendingRecommendationReveal.current = null;
    const frame = window.requestAnimationFrame(() => {
      recommendationRowRef.current?.scrollTo({
        left: Math.max(0, pending.previousScrollWidth - 16),
        behavior: 'smooth',
      });
    });
    return () => window.cancelAnimationFrame(frame);
  }, [reco.length]);

  // おすすめを startIndex から連続再生
  const playRecoFrom = (startIndex: number) => {
    usePlayerStore.getState().playTracks(toTracks(reco), startIndex);
  };

  const scrollRecommendations = (direction: -1 | 1) => {
    const row = recommendationRowRef.current;
    if (!row) return;
    row.scrollBy({
      left: direction * Math.max(240, row.clientWidth - 160),
      behavior: 'smooth',
    });
  };

  const loadMoreRecommendations = async (revealNewRecommendations: boolean) => {
    if (recommendationLoadInFlight.current || isFetchingMoreRecommendations || !hasMoreRecommendations) return;
    recommendationLoadInFlight.current = true;
    const row = recommendationRowRef.current;
    pendingRecommendationReveal.current = revealNewRecommendations
      ? {
          previousCount: reco.length,
          previousScrollWidth: row?.scrollWidth ?? 0,
        }
      : null;
    try {
      const result = await fetchMoreRecommendations();
      if (result.isError) {
        pendingRecommendationReveal.current = null;
        showToast('おすすめを読み込めませんでした', 'error');
        return;
      }
      const nextCount = uniqueSongs(result.data?.pages.flatMap((page) => page.performances) ?? []).length;
      if (nextCount <= reco.length) pendingRecommendationReveal.current = null;
    } catch {
      pendingRecommendationReveal.current = null;
      showToast('おすすめを読み込めませんでした', 'error');
    } finally {
      recommendationLoadInFlight.current = false;
    }
  };

  const handleRecommendationScroll = () => {
    updateRecommendationScroll();
    const row = recommendationRowRef.current;
    if (!row || !window.matchMedia('(max-width: 767px)').matches) return;
    const reachedEnd = row.scrollLeft + row.clientWidth >= row.scrollWidth - 48;
    if (reachedEnd) void loadMoreRecommendations(false);
  };

  // 見えているおすすめをすべて再生し、50曲未満なら再生キューだけを重複なしで補う。
  const playAllRecommendations = async () => {
    if (reco.length === 0 || isPreparingRecommendations) return;
    setIsPreparingRecommendations(true);
    let playbackRecommendations = reco;
    try {
      if (playbackRecommendations.length < RECOMMENDATION_PLAYBACK_MIN) {
        const supplement = await homeApi.randomPerformances(
          RECOMMENDATION_PLAYBACK_MIN - playbackRecommendations.length,
          playbackRecommendations.map((perf) => perf.song_id),
        );
        playbackRecommendations = uniqueSongs([...playbackRecommendations, ...supplement.performances]);
      }
      usePlayerStore.getState().playTracks(toTracks(playbackRecommendations));
      if (playbackRecommendations.length < RECOMMENDATION_PLAYBACK_MIN) {
        showToast(`再生可能なおすすめ${playbackRecommendations.length}曲を読み込みました`, 'info');
      }
    } catch {
      usePlayerStore.getState().playTracks(toTracks(reco));
      showToast('追加のおすすめを取得できなかったため、現在のリストを再生します', 'error');
    } finally {
      setIsPreparingRecommendations(false);
    }
  };

  const enqueueCurrentRecommendations = () => {
    if (reco.length === 0) return;
    usePlayerStore.getState().enqueue(toTracks(reco));
    showToast(`現在のおすすめ${reco.length}曲をキューに追加しました`, 'success');
  };

  // おすすめを最初から選び直す（蓄積した全ページを破棄して 1 ページ目を取り直す）
  const reshuffleRecommendations = () => {
    pendingRecommendationReveal.current = null;
    recommendationRowRef.current?.scrollTo({ left: 0 });
    queryClient.resetQueries({ queryKey: ['random-performances', 'home', 'infinite'] });
  };

  return (
    <div className="space-y-8">
      {/* おすすめ（ランダムピックアップ） */}
      <section>
        <SectionHeader title="おすすめ">
          {reco.length > 0 && (
            <>
              <button
                onClick={playAllRecommendations}
                disabled={isPreparingRecommendations}
                className="inline-flex items-center justify-center w-8 h-8 bg-indigo-600 text-white rounded-full hover:bg-indigo-700 transition-colors disabled:opacity-60"
                title="おすすめをすべて再生（50曲未満なら補充）"
              >
                {isPreparingRecommendations ? (
                  <svg className="w-4 h-4 animate-spin" fill="none" viewBox="0 0 24 24">
                    <circle className="opacity-25" cx="12" cy="12" r="9" stroke="currentColor" strokeWidth="3" />
                    <path className="opacity-75" fill="currentColor" d="M12 3a9 9 0 0 1 9 9h-3a6 6 0 0 0-6-6V3Z" />
                  </svg>
                ) : (
                  <svg className="w-4 h-4 ml-0.5" fill="currentColor" viewBox="0 0 24 24"><path d="M8 5v14l11-7z" /></svg>
                )}
              </button>
              <button
                onClick={enqueueCurrentRecommendations}
                className="inline-flex items-center justify-center w-8 h-8 text-gray-500 border rounded-full hover:text-indigo-600 hover:bg-indigo-50 transition-colors"
                title={`現在のおすすめ${reco.length}曲をキューに追加`}
              >
                <svg className="w-5 h-5" fill="currentColor" viewBox="0 0 24 24">
                  <path d="M14 10H3v2h11v-2zm0-4H3v2h11V6zm4 8v-4h-2v4h-4v2h4v4h2v-4h4v-2h-4zM3 16h7v-2H3v2z" />
                </svg>
              </button>
              <button
                onClick={reshuffleRecommendations}
                className="inline-flex items-center justify-center w-8 h-8 text-gray-500 border rounded-full hover:text-indigo-600 hover:bg-indigo-50 transition-colors"
                title="おすすめを選び直す"
              >
                <svg className="w-4 h-4" fill="none" stroke="currentColor" strokeWidth={2} viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" d="M4 4v6h6M20 20v-6h-6M5.5 9a8 8 0 0 1 13.4-2.6L20 7.5M18.5 15a8 8 0 0 1-13.4 2.6L4 16.5" />
                </svg>
              </button>
            </>
          )}
        </SectionHeader>

        {recoLoading ? (
          <Loading />
        ) : (
          <div className="relative -mx-1">
            <div
              ref={recommendationRowRef}
              onScroll={handleRecommendationScroll}
              className="flex touch-pan-x snap-x snap-mandatory gap-4 overflow-x-auto scroll-smooth px-1 pb-2 overscroll-x-contain [scrollbar-width:none] [&::-webkit-scrollbar]:hidden"
            >
              {reco.map((perf, i) => (
                <div key={perf.id} className="w-56 shrink-0 snap-start bg-white rounded-lg shadow-sm border hover:shadow-md transition-shadow group">
                  {/* 配信サムネイル + 右下に CD 風の楽曲アート */}
                  <div className="relative">
                    <button
                      onClick={() => playRecoFrom(i)}
                      className="block w-full relative overflow-hidden rounded-t-lg"
                      title="この歌唱から連続再生"
                    >
                      {perf.thumbnail_url ? (
                        <img src={perf.thumbnail_url} alt="" loading="lazy" className="w-full h-32 object-cover" />
                      ) : (
                        <div className="w-full h-32 bg-gray-200" />
                      )}
                      <span className="absolute inset-0 bg-black bg-opacity-0 group-hover:bg-opacity-30 flex items-center justify-center transition-all">
                        <span className="w-10 h-10 rounded-full bg-indigo-600 flex items-center justify-center opacity-0 group-hover:opacity-100 transition-opacity">
                          <svg className="w-5 h-5 text-white ml-0.5" fill="currentColor" viewBox="0 0 24 24"><path d="M8 5v14l11-7z" /></svg>
                        </span>
                      </span>
                    </button>
                    {perf.arts && (
                      <span className="absolute -bottom-4 right-2 w-14 h-14 pointer-events-none">
                        <img src={perf.arts} alt="" loading="lazy" className="w-14 h-14 rounded-full object-cover border-2 border-white shadow-md" />
                        {/* CD の中心穴 */}
                        <span className="absolute inset-0 m-auto w-3 h-3 rounded-full bg-white border border-gray-300" />
                      </span>
                    )}
                    <QueueAddButton
                      track={toTracks([perf])[0]}
                      className="absolute top-1.5 right-1.5 bg-white/90 shadow opacity-0 group-hover:opacity-100"
                    />
                  </div>
                  <div className="p-3 pt-4">
                    <Link
                      to={`/songs/${perf.song_id}`}
                      className="block h-10 text-sm font-medium leading-5 text-gray-900 hover:text-indigo-600 line-clamp-2 pr-14"
                      title={perf.song_name}
                    >
                      {perf.song_name}
                    </Link>
                    <ArtistLinks
                      artists={perf.artists}
                      fallback={perf.original_artist}
                      className="block text-xs text-gray-500 truncate pr-14"
                      linkClassName="hover:text-indigo-600"
                    />
                    <div className="mt-1 flex min-h-7 items-center justify-between gap-2">
                      {perf.stream_date ? (
                        <time dateTime={perf.stream_date} className="min-w-0 text-xs text-gray-400">
                          {new Date(perf.stream_date).toLocaleDateString('ja-JP')}
                        </time>
                      ) : <span />}
                      <SingerAvatars singers={perf.singers} />
                    </div>
                  </div>
                </div>
              ))}
            </div>

            {recommendationScroll.canGoLeft && (
              <button
                type="button"
                onClick={() => scrollRecommendations(-1)}
                aria-label="前のおすすめを表示"
                title="前へ"
                className="absolute inset-y-0 left-0 z-10 hidden w-14 items-center justify-start bg-gradient-to-r from-gray-50 via-gray-50/80 to-transparent pl-2 opacity-0 transition-opacity md:flex md:hover:opacity-100 md:focus-visible:opacity-100"
              >
                <span className="flex h-10 w-10 items-center justify-center rounded-full bg-white text-gray-700 shadow-lg ring-1 ring-black/5">
                  <svg className="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="m15 19-7-7 7-7" />
                  </svg>
                </span>
              </button>
            )}

            {reco.length > 0 && (recommendationScroll.canGoRight || hasMoreRecommendations) && (
              <button
                type="button"
                onClick={() => recommendationScroll.canGoRight ? scrollRecommendations(1) : loadMoreRecommendations(true)}
                disabled={!recommendationScroll.canGoRight && isFetchingMoreRecommendations}
                aria-label={recommendationScroll.canGoRight ? '次のおすすめを表示' : 'おすすめを20曲追加'}
                title={recommendationScroll.canGoRight ? '次へ' : 'さらに20曲読み込む'}
                className="absolute inset-y-0 right-0 z-10 hidden w-14 items-center justify-end bg-gradient-to-l from-gray-50 via-gray-50/80 to-transparent pr-2 opacity-0 transition-opacity disabled:cursor-wait md:flex md:hover:opacity-100 md:focus-visible:opacity-100 md:disabled:opacity-100"
              >
                <span className="flex h-10 w-10 items-center justify-center rounded-full bg-white text-gray-700 shadow-lg ring-1 ring-black/5">
                  {!recommendationScroll.canGoRight && isFetchingMoreRecommendations ? (
                    <svg className="h-4 w-4 animate-spin" fill="none" viewBox="0 0 24 24">
                      <circle className="opacity-25" cx="12" cy="12" r="9" stroke="currentColor" strokeWidth="3" />
                      <path className="opacity-75" fill="currentColor" d="M12 3a9 9 0 0 1 9 9h-3a6 6 0 0 0-6-6V3Z" />
                    </svg>
                  ) : (
                    <svg className="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="m9 5 7 7-7 7" />
                    </svg>
                  )}
                </span>
              </button>
            )}
          </div>
        )}
      </section>

      {/* 最近の歌枠（singing タグ付きのみ） */}
      <section>
        <SectionHeader title="最近の歌枠" linkTo="/tags/stream/singing" />
        {singingLoading ? (
          <Loading />
        ) : (
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
            {singingData?.streams.map((stream) => (
              <article
                key={stream.id}
                className="bg-white rounded-lg shadow-sm border hover:shadow-md transition-shadow group"
              >
                <Link to={`/streams/${stream.id}`} className="block overflow-hidden rounded-t-lg">
                  {stream.thumbnail_url ? (
                    <img src={stream.thumbnail_url} alt={stream.title} loading="lazy" className="w-full h-40 object-cover transition-transform group-hover:scale-[1.01]" />
                  ) : (
                    <div className="w-full h-40 bg-gray-200" />
                  )}
                </Link>
                <div className="p-4">
                  <Link
                    to={`/streams/${stream.id}`}
                    className="block h-12 font-medium leading-6 text-gray-900 line-clamp-2 group-hover:text-indigo-600 transition-colors"
                  >
                    {stream.title}
                  </Link>
                  <div className="mt-2 flex min-h-7 items-center justify-between gap-2">
                    <time dateTime={stream.stream_date} className="min-w-0 text-sm text-gray-500">
                      {new Date(stream.stream_date).toLocaleDateString('ja-JP')}
                    </time>
                    <SingerAvatars singers={stream.participants ?? []} />
                  </div>
                  {stream.tags.length > 0 && (
                    <div className="flex flex-wrap gap-1.5 mt-2">
                      {stream.tags.map((tag) => (
                        <Tag key={tag.id} label={tag.display_name} color={tag.color} />
                      ))}
                    </div>
                  )}
                </div>
              </article>
            ))}
          </div>
        )}
      </section>

      {/* 新着オリジナル曲 */}
      {(originalData?.streams.length ?? 0) > 0 && (
        <section>
          <SectionHeader title="新着オリジナル曲" linkTo="/tags/stream/original_song" />
          <StreamCardRow streams={originalData!.streams} />
        </section>
      )}

      {/* 新着歌ってみた */}
      {(coverData?.streams.length ?? 0) > 0 && (
        <section>
          <SectionHeader title="新着歌ってみた" linkTo="/tags/stream/music_cover" />
          <StreamCardRow streams={coverData!.streams} />
        </section>
      )}

      {/* 人気の楽曲 */}
      <section>
        <SectionHeader title="人気の楽曲" linkTo="/songs" />
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
                      <ArtistLinks
                        artists={song.artists}
                        fallback={song.original_artist}
                        linkClassName="hover:text-indigo-600"
                      />
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
