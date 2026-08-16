import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useInfiniteQuery, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link } from 'react-router-dom';
import { homeApi, presetPlaylistApi, songApi, tagApi } from '../api/client';
import type { Performance, PresetPlaylist } from '../api/types';
import Loading from '../components/ui/Loading';
import Tag from '../components/ui/Tag';
import { useToast } from '../components/ui/ToastContext';
import SingerAvatars from '../components/SingerAvatars';
import PerformanceCard from '../components/PerformanceCard';
import PerformanceCardRow from '../components/PerformanceCardRow';
import PresetActions from '../components/PresetActions';
import QueueAddButton from '../components/QueueAddButton';
import { usePlayerStore, performancesToTracks as toTracks } from '../store/player';

const RECOMMENDATION_PAGE_SIZE = 20;
const RECOMMENDATION_PLAYBACK_MIN = 50;
// プリセットの列に出す件数。再生ボタンは足りなければ残りを取りに行く。
const PRESET_ROW_SIZE = 20;

function uniqueSongs(perfs: Performance[]): Performance[] {
  const songIds = new Set<string>();
  return perfs.filter((perf) => {
    if (songIds.has(perf.song_id)) return false;
    songIds.add(perf.song_id);
    return true;
  });
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

// プリセットプレイリスト 1 本ぶんの列。中身はプリセットごとに取りに行く。
function PresetSection({ preset }: { preset: PresetPlaylist }) {
  const { showToast } = useToast();
  const [isPreparingPlayback, setIsPreparingPlayback] = useState(false);

  // キーの先頭を 'presets' にしない：フォローの更新で invalidate する範囲に入り、
  // 押すたびに全プリセットの中身を取り直すことになる。
  const { data, isLoading } = useQuery({
    queryKey: ['preset-items', preset.key, PRESET_ROW_SIZE],
    queryFn: () => presetPlaylistApi.items(preset.key, PRESET_ROW_SIZE),
    staleTime: 5 * 60 * 1000,
  });

  const performances = useMemo(() => data?.performances ?? [], [data]);

  const playFrom = (startIndex: number) => {
    usePlayerStore.getState().playTracks(toTracks(performances), startIndex);
  };

  // 列に出しているのは先頭の数曲なので、再生ボタンは残りを取りに行ってから流す。
  const playAll = async () => {
    if (performances.length === 0 || isPreparingPlayback) return;
    if (preset.item_count <= performances.length) {
      playFrom(0);
      return;
    }
    setIsPreparingPlayback(true);
    try {
      const full = await presetPlaylistApi.items(preset.key);
      usePlayerStore.getState().playTracks(toTracks(full.performances));
    } catch {
      usePlayerStore.getState().playTracks(toTracks(performances));
      showToast('全曲を取得できなかったため、表示中の曲を再生します', 'error');
    } finally {
      setIsPreparingPlayback(false);
    }
  };

  if (!isLoading && performances.length === 0) return null;

  return (
    <section>
      <SectionHeader title={preset.name} linkTo={`/playlists/preset/${preset.key}`}>
        <span className="text-sm text-gray-500">{preset.item_count}曲</span>
        <PresetActions preset={preset} onPlayAll={playAll} playDisabled={isPreparingPlayback} />
      </SectionHeader>
      {isLoading ? <Loading /> : <PerformanceCardRow performances={performances} onPlayFrom={playFrom} />}
    </section>
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

  const { data: songsData, isLoading: songsLoading } = useQuery({
    queryKey: ['songs', 'popular'],
    queryFn: () => songApi.list(1, 10, undefined, 'performances', 'desc'),
  });

  const { data: presetData } = useQuery({
    queryKey: ['presets'],
    queryFn: () => presetPlaylistApi.list(),
  });

  const reco = useMemo(
    () => uniqueSongs(recoData?.pages.flatMap((page) => page.performances) ?? []),
    [recoData],
  );
  const recommendationTracks = useMemo(() => toTracks(reco), [reco]);

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
              <QueueAddButton
                tracks={recommendationTracks}
                description={`現在のおすすめ${reco.length}曲`}
                playlistHeading={`${reco.length}曲を追加`}
                defaultPlaylistName="おすすめ"
                className="border"
              />
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
            {/* touch-action は指定しない。pan-x にすると縦の手繰りが祖先ごと禁止され、
                この列に指が乗った状態でページを下へ送れなくなる（PerformanceCardRow と同じ） */}
            <div
              ref={recommendationRowRef}
              onScroll={handleRecommendationScroll}
              className="flex snap-x snap-mandatory gap-4 overflow-x-auto scroll-smooth px-1 pb-2 overscroll-x-contain [scrollbar-width:none] [&::-webkit-scrollbar]:hidden"
            >
              {reco.map((perf, i) => (
                <PerformanceCard key={perf.id} performance={perf} onPlay={() => playRecoFrom(i)} />
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

      {/* プリセットプレイリスト（運営が用意した歌単） */}
      {presetData?.presets.map((preset) => (
        <PresetSection key={preset.key} preset={preset} />
      ))}

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

      {/* 人気の楽曲（歌唱回数の多い順） */}
      <section>
        <SectionHeader title="人気の楽曲" linkTo="/songs" />
        {songsLoading ? (
          <Loading />
        ) : (
          <ul className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5">
            {songsData?.songs.map((song, index) => (
              <li key={song.id}>
                <Link
                  to={`/songs/${song.id}`}
                  className="group flex h-full flex-col items-center rounded-lg border bg-white p-4 text-center shadow-sm transition-shadow hover:shadow-md"
                >
                  {/* ジャケットを CD に見立てる（おすすめのカードと同じ絵柄） */}
                  <span className="relative mb-3 block h-24 w-24">
                    {song.arts ? (
                      <img
                        src={song.arts}
                        alt=""
                        loading="lazy"
                        className="h-24 w-24 rounded-full object-cover shadow-md ring-1 ring-black/5 transition-transform group-hover:scale-105"
                      />
                    ) : (
                      <span className="flex h-24 w-24 items-center justify-center rounded-full bg-gray-100 shadow-md ring-1 ring-black/5 transition-transform group-hover:scale-105">
                        <svg className="h-8 w-8 text-gray-300" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 19V6l12-3v13M9 19c0 1.105-1.343 2-3 2s-3-.895-3-2 1.343-2 3-2 3 .895 3 2zm12-3c0 1.105-1.343 2-3 2s-3-.895-3-2 1.343-2 3-2 3 .895 3 2z" />
                        </svg>
                      </span>
                    )}
                    {/* CD の中心穴 */}
                    <span className="absolute inset-0 m-auto h-5 w-5 rounded-full border border-gray-300 bg-white" />
                    {/* 順位。上位3曲だけ色を付ける */}
                    <span
                      className={`absolute -left-1 -top-1 flex h-6 w-6 items-center justify-center rounded-full text-xs font-bold shadow ring-1 ring-black/5 ${
                        index < 3 ? 'bg-indigo-600 text-white' : 'bg-white text-gray-500'
                      }`}
                    >
                      {index + 1}
                    </span>
                  </span>

                  <span className="line-clamp-2 text-sm font-medium leading-5 text-gray-900 group-hover:text-indigo-600" title={song.name}>
                    {song.name}
                  </span>
                  <span className="mt-0.5 w-full truncate text-xs text-gray-500">
                    {song.artists?.length ? song.artists.map((a) => a.name).join('、') : song.original_artist}
                  </span>
                  <span className="mt-2 text-xs font-medium text-gray-400">{song.performance_count}回</span>
                </Link>
              </li>
            ))}
          </ul>
        )}
      </section>
    </div>
  );
}
