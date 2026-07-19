import { useEffect, useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import { usePlayerStore } from '../store/player';
import ArtistLinks from './ArtistLinks';
import type { YouTubePlayerInstance, YouTubePlayerStateChangeEvent } from '../types/youtube';

// 秒 → M:SS
function fmt(sec: number): string {
  const s = Math.max(0, Math.floor(sec));
  const m = Math.floor(s / 60);
  return `${m}:${String(s % 60).padStart(2, '0')}`;
}

// グローバル再生プレイヤー。
// - 通常時：底部バー（Musicdex 風）。キューの各トラック＝配信内の start〜end 区間
// - 拡大時：全画面（左：動画大画面、右：キュー一覧）。^ / v で切り替え、Esc で縮小
// 動画 iframe は同一 DOM 要素を CSS の fixed 配置だけで移動する
// （別コンテナへ再マウントすると再生が中断されるため）。
export default function PlayerBar() {
  const { queue, index, playing, queueOpen, next, prev, jumpTo, removeAt, setPlaying, setQueueOpen, clear } =
    usePlayerStore();
  const track = queue[index];

  const containerRef = useRef<HTMLDivElement>(null);
  const playerRef = useRef<YouTubePlayerInstance | null>(null);
  const readyRef = useRef(false);
  const currentVideoRef = useRef<string>('');
  const [progress, setProgress] = useState(0); // 区間内の経過秒
  const [expanded, setExpanded] = useState(false);
  // 音量は自前 UI で管理（縮小時の iframe は小さすぎて操作できないためロックする）
  const [volume, setVolume] = useState<number>(() => {
    const saved = Number(localStorage.getItem('setori_player_volume'));
    return Number.isFinite(saved) && saved >= 0 && saved <= 100 ? saved : 80;
  });
  const [muted, setMuted] = useState(false);

  const applyVolume = (v: number, m: boolean) => {
    const p = playerRef.current;
    if (!p || !readyRef.current) return;
    try {
      p.setVolume(v);
      if (m) p.mute();
      else p.unMute();
    } catch {
      /* noop */
    }
  };

  const changeVolume = (v: number) => {
    setVolume(v);
    const m = v === 0 ? true : false;
    setMuted(m);
    localStorage.setItem('setori_player_volume', String(v));
    applyVolume(v, m);
  };

  const toggleMute = () => {
    const m = !muted;
    setMuted(m);
    applyVolume(volume, m);
  };

  // Esc で縮小
  useEffect(() => {
    if (!expanded) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setExpanded(false);
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [expanded]);

  // YT IFrame API のロード（既存の YoutubePlayer と同じ script を共有）
  useEffect(() => {
    const container = containerRef.current;
    if (!track || playerRef.current || !container) return;

    const init = () => {
      const YT = window.YT;
      if (!YT || !YT.Player) {
        setTimeout(init, 100);
        return;
      }
      const origin = window.location.origin;
      playerRef.current = new YT.Player(container, {
        origin,
        width: '100%',
        height: '100%',
        videoId: track.streamId,
        // controls: 1 → 拡大表示時に YouTube ネイティブの操作
        // （全画面・画質・音量など）がそのまま使える
        playerVars: { autoplay: 1, controls: 1, modestbranding: 1, start: Math.floor(track.start), origin },
        events: {
          onReady: () => {
            readyRef.current = true;
            currentVideoRef.current = track.streamId;
            applyVolume(volume, muted);
          },
          onStateChange: (e: YouTubePlayerStateChangeEvent) => {
            // 動画自体が終わった（end 未設定 or 区間が動画末尾）→ 次へ
            if (e.data === window.YT?.PlayerState?.ENDED) {
              usePlayerStore.getState().next();
            }
          },
          onError: () => {
            // 再生不可（限定公開・削除済み等）はスキップ
            usePlayerStore.getState().next();
          },
        },
      });
    };

    if (!window.YT && !document.getElementById('youtube-api')) {
      const script = document.createElement('script');
      script.id = 'youtube-api';
      script.src = 'https://www.youtube.com/iframe_api';
      document.head.appendChild(script);
    }
    init();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [!!track]);

  // アンマウント（キュー空）でインスタンス破棄
  useEffect(() => {
    return () => {
      try {
        playerRef.current?.destroy?.();
      } catch {
        /* noop */
      }
      playerRef.current = null;
      readyRef.current = false;
      currentVideoRef.current = '';
    };
  }, []);

  // トラック切り替え：同一動画なら seek、別動画なら load
  useEffect(() => {
    const p = playerRef.current;
    if (!p || !readyRef.current || !track) return;
    if (currentVideoRef.current === track.streamId) {
      p.seekTo(track.start, true);
      p.playVideo();
    } else {
      currentVideoRef.current = track.streamId;
      p.loadVideoById({ videoId: track.streamId, startSeconds: track.start });
    }
    setProgress(0);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [track?.performanceId]);

  // 再生/一時停止の意図を反映
  useEffect(() => {
    const p = playerRef.current;
    if (!p || !readyRef.current) return;
    try {
      if (playing) p.playVideo();
      else p.pauseVideo();
    } catch {
      /* noop */
    }
  }, [playing]);

  // 区間終端の監視（0.5秒ポーリング）
  useEffect(() => {
    if (!track || !playing) return;
    const interval = setInterval(() => {
      const p = playerRef.current;
      if (!p || !readyRef.current || typeof p.getCurrentTime !== 'function') return;
      const t = p.getCurrentTime();
      setProgress(Math.max(0, t - track.start));
      if (track.end > track.start && t >= track.end - 0.3) {
        usePlayerStore.getState().next();
      }
    }, 500);
    return () => clearInterval(interval);
  }, [track?.performanceId, playing, track]);

  if (!track) return null;

  const duration = track.end > track.start ? track.end - track.start : 0;
  const ratio = duration > 0 ? Math.min(1, progress / duration) : 0;

  const seekTo = (r: number) => {
    const p = playerRef.current;
    if (!p || !readyRef.current || duration <= 0) return;
    p.seekTo(track.start + r * duration, true);
    setProgress(r * duration);
  };

  // 進捗バー（バー/拡大の両方で使用）
  const progressBar = (dark: boolean) => (
    <div className="flex items-center gap-2 flex-1 min-w-0">
      <span className={`text-[11px] font-mono w-9 text-right shrink-0 ${dark ? 'text-gray-400' : 'text-gray-400'}`}>
        {fmt(progress)}
      </span>
      <div
        className={`flex-1 h-1.5 rounded-full cursor-pointer group ${dark ? 'bg-white/20' : 'bg-gray-200'}`}
        onClick={(e) => {
          const rect = e.currentTarget.getBoundingClientRect();
          seekTo((e.clientX - rect.left) / rect.width);
        }}
      >
        <div className="h-full bg-indigo-500 rounded-full relative" style={{ width: `${ratio * 100}%` }}>
          <span className="absolute right-0 top-1/2 -translate-y-1/2 translate-x-1/2 w-3 h-3 bg-indigo-400 rounded-full opacity-0 group-hover:opacity-100 transition-opacity" />
        </div>
      </div>
      <span className="text-[11px] font-mono w-9 shrink-0 text-gray-400">{duration > 0 ? fmt(duration) : '--:--'}</span>
    </div>
  );

  // 音量コントロール（自前 UI。iframe 側の操作はロックしているため）
  const volumeControl = (dark: boolean) => (
    <div className="flex items-center gap-1.5 shrink-0">
      <button
        onClick={toggleMute}
        className={dark ? 'text-gray-400 hover:text-white' : 'text-gray-400 hover:text-gray-700'}
        title={muted || volume === 0 ? 'ミュート解除' : 'ミュート'}
      >
        {muted || volume === 0 ? (
          <svg className="w-5 h-5" fill="currentColor" viewBox="0 0 24 24">
            <path d="M16.5 12c0-1.77-1.02-3.29-2.5-4.03v2.21l2.45 2.45c.03-.2.05-.41.05-.63zm2.5 0c0 .94-.2 1.82-.54 2.64l1.51 1.51C20.63 14.91 21 13.5 21 12c0-4.28-2.99-7.86-7-8.77v2.06c2.89.86 5 3.54 5 6.71zM4.27 3L3 4.27 7.73 9H3v6h4l5 5v-6.73l4.25 4.25c-.67.52-1.42.93-2.25 1.18v2.06c1.38-.31 2.63-.95 3.69-1.81L19.73 21 21 19.73l-9-9L4.27 3zM12 4L9.91 6.09 12 8.18V4z"/>
          </svg>
        ) : (
          <svg className="w-5 h-5" fill="currentColor" viewBox="0 0 24 24">
            <path d="M3 9v6h4l5 5V4L7 9H3zm13.5 3c0-1.77-1.02-3.29-2.5-4.03v8.05c1.48-.73 2.5-2.25 2.5-4.02zM14 3.23v2.06c2.89.86 5 3.54 5 6.71s-2.11 5.85-5 6.71v2.06c4.01-.91 7-4.49 7-8.77s-2.99-7.86-7-8.77z"/>
          </svg>
        )}
      </button>
      <input
        type="range"
        min={0}
        max={100}
        value={muted ? 0 : volume}
        onChange={(e) => changeVolume(Number(e.target.value))}
        className="w-20 h-1 accent-indigo-500 cursor-pointer"
        title={`音量 ${muted ? 0 : volume}`}
      />
    </div>
  );

  // コントロール（前へ/再生/次へ）
  const controls = (size: 'sm' | 'lg') => (
    <div className="flex items-center gap-1 shrink-0">
      <button
        onClick={prev}
        disabled={index === 0}
        className={`${size === 'lg' ? 'p-2.5 text-gray-300 hover:text-white' : 'p-2 text-gray-500 hover:text-gray-900'} disabled:opacity-30`}
        title="前の曲"
      >
        <svg className={size === 'lg' ? 'w-6 h-6' : 'w-5 h-5'} fill="currentColor" viewBox="0 0 24 24"><path d="M6 6h2v12H6zm3.5 6l8.5 6V6z"/></svg>
      </button>
      <button
        onClick={() => setPlaying(!playing)}
        className={`${size === 'lg' ? 'p-3.5' : 'p-2.5'} bg-indigo-600 text-white rounded-full hover:bg-indigo-700 transition-colors`}
        title={playing ? '一時停止' : '再生'}
      >
        {playing ? (
          <svg className={size === 'lg' ? 'w-6 h-6' : 'w-5 h-5'} fill="currentColor" viewBox="0 0 24 24"><path d="M6 19h4V5H6v14zm8-14v14h4V5h-4z"/></svg>
        ) : (
          <svg className={size === 'lg' ? 'w-6 h-6' : 'w-5 h-5'} fill="currentColor" viewBox="0 0 24 24"><path d="M8 5v14l11-7z"/></svg>
        )}
      </button>
      <button
        onClick={next}
        disabled={index >= queue.length - 1}
        className={`${size === 'lg' ? 'p-2.5 text-gray-300 hover:text-white' : 'p-2 text-gray-500 hover:text-gray-900'} disabled:opacity-30`}
        title="次の曲"
      >
        <svg className={size === 'lg' ? 'w-6 h-6' : 'w-5 h-5'} fill="currentColor" viewBox="0 0 24 24"><path d="M6 18l8.5-6L6 6v12zM16 6v12h2V6h-2z"/></svg>
      </button>
    </div>
  );

  return (
    <>
      {/* 動画コンテナ：同一 DOM 要素を fixed 配置の切り替えだけで移動する。
          拡大時は厳密な 16:9（幅と高さの両制約の小さい方）で黒帯を出さない */}
      <div
        className={
          expanded
            ? 'fixed z-[60] top-16 left-2 right-2 h-[36vh] lg:top-20 lg:left-8 lg:right-auto lg:h-auto lg:aspect-video lg:w-[min(calc((100vh-17rem)*1.7778),60vw)] bg-black rounded-lg overflow-hidden [&_iframe]:w-full [&_iframe]:h-full'
            : 'fixed z-[45] bottom-2 left-3 w-32 h-[72px] hidden sm:block bg-black rounded overflow-hidden [&_iframe]:w-full [&_iframe]:h-full'
        }
      >
        <div ref={containerRef} className="w-full h-full" />
        {/* 縮小時は小さすぎて YT コントロールを操作できないためロックし、
            クリックで全画面に拡大する */}
        {!expanded && (
          <button
            className="absolute inset-0 z-10 cursor-pointer"
            onClick={() => setExpanded(true)}
            title="クリックで全画面表示"
          />
        )}
      </div>

      {expanded ? (
        /* ===== 拡大表示（Musicdex 風：左＝動画、右＝キュー） ===== */
        <div className="fixed inset-0 z-50 bg-gray-950 text-white flex flex-col pb-[env(safe-area-inset-bottom)]">
          {/* Header */}
          <div className="h-14 shrink-0 flex items-center justify-between px-4 border-b border-white/10">
            <button
              onClick={() => setExpanded(false)}
              className="flex items-center gap-1.5 px-3 py-1.5 text-sm text-gray-300 hover:text-white hover:bg-white/10 rounded-lg transition-colors"
              title="縮小してページに戻る（Esc）"
            >
              <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
              </svg>
              縮小
            </button>
            <span className="text-sm text-gray-400">再生中 {index + 1}/{queue.length}</span>
            <button
              onClick={() => {
                clear();
                setExpanded(false);
              }}
              className="px-3 py-1.5 text-sm text-gray-400 hover:text-white hover:bg-white/10 rounded-lg transition-colors"
            >
              キューをクリア
            </button>
          </div>

          {/* Body */}
          <div className="flex-1 min-h-0 flex flex-col lg:flex-row">
            {/* 左：動画（fixed の動画コンテナがこの領域に浮いている）＋情報・コントロール。
                幅は動画（16:9）の実寸に合わせ、余白はすべて右のキューに渡す */}
            <div className="shrink-0 lg:w-[calc(min(calc((100vh-17rem)*1.7778),60vw)+4rem)] flex flex-col">
              <div className="h-[36vh] lg:flex-1 mt-2" /> {/* 動画スペース */}
              <div className="px-6 py-4 space-y-3 lg:h-36 shrink-0">
                <div className="min-w-0">
                  <div className="flex flex-wrap items-baseline gap-x-2">
                    {track.songId ? (
                      <Link
                        to={`/songs/${track.songId}`}
                        onClick={() => setExpanded(false)}
                        className="text-lg font-semibold hover:text-indigo-300 truncate"
                      >
                        {track.songName}
                      </Link>
                    ) : (
                      <span className="text-lg font-semibold truncate">{track.songName}</span>
                    )}
                    <ArtistLinks
                      artists={track.artists}
                      fallback={track.artist}
                      className="text-sm text-gray-400"
                      linkClassName="hover:text-indigo-300"
                      onNavigate={() => setExpanded(false)}
                    />
                  </div>
                  <div className="text-sm text-gray-400 truncate mt-0.5">
                    {track.singers.map((s, i) => (
                      <span key={s.id}>
                        {i > 0 && '、'}
                        <Link to={`/singers/${s.id}`} onClick={() => setExpanded(false)} className="hover:text-indigo-300">
                          {s.name}
                        </Link>
                      </span>
                    ))}
                    {track.streamTitle && (
                      <>
                        {track.singers.length > 0 && ' · '}
                        {track.streamDate && (
                          <span className="font-mono">{new Date(track.streamDate).toLocaleDateString('ja-JP')} </span>
                        )}
                        <Link
                          to={`/streams/${track.streamId}`}
                          onClick={() => setExpanded(false)}
                          className="hover:text-indigo-300"
                        >
                          {track.streamTitle}
                        </Link>
                      </>
                    )}
                  </div>
                </div>
                <div className="flex items-center gap-4">
                  {controls('lg')}
                  {progressBar(true)}
                  {volumeControl(true)}
                </div>
              </div>
            </div>

            {/* 右：キュー一覧（残り幅をすべて使う） */}
            <div className="flex-1 min-w-0 min-h-0 border-t lg:border-t-0 lg:border-l border-white/10 flex flex-col">
              <div className="px-4 py-2.5 text-sm font-medium text-gray-300 border-b border-white/10 shrink-0">
                再生キュー（{queue.length}曲）
              </div>
              <div className="flex-1 overflow-y-auto">
                {queue.map((t, i) => (
                  <div
                    key={`${t.performanceId}-${i}`}
                    onClick={() => jumpTo(i)}
                    className={`flex items-center gap-2.5 px-4 py-2.5 cursor-pointer border-b border-white/5 ${
                      i === index ? 'bg-indigo-600/20' : 'hover:bg-white/5'
                    }`}
                  >
                    <span className={`text-xs font-mono w-5 text-right shrink-0 ${i === index ? 'text-indigo-300' : 'text-gray-500'}`}>
                      {i === index ? '▶' : i + 1}
                    </span>
                    {t.artUrl ? (
                      <img src={t.artUrl} alt="" className="w-9 h-9 object-cover rounded shrink-0" />
                    ) : (
                      <span className="w-9 h-9 bg-white/10 rounded shrink-0" />
                    )}
                    <span className="flex-1 min-w-0">
                      <span className="flex items-baseline gap-1.5 min-w-0">
                        <span className={`text-sm truncate ${i === index ? 'text-indigo-200 font-medium' : 'text-gray-100'}`}>
                          {t.songName}
                        </span>
                        {t.artist && <span className="text-[11px] text-gray-500 truncate shrink-0">{t.artist}</span>}
                      </span>
                      <span className="block text-xs text-gray-500 truncate">
                        {t.streamDate && (
                          <span className="font-mono">{new Date(t.streamDate).toLocaleDateString('ja-JP')} · </span>
                        )}
                        {t.streamTitle}
                      </span>
                    </span>
                    <span className="text-xs font-mono text-gray-500 shrink-0">
                      {t.end > t.start ? fmt(t.end - t.start) : '--:--'}
                    </span>
                    <button
                      onClick={(e) => {
                        e.stopPropagation();
                        removeAt(i);
                      }}
                      className="text-gray-600 hover:text-red-400 shrink-0"
                      title="キューから削除"
                    >
                      ✕
                    </button>
                  </div>
                ))}
              </div>
            </div>
          </div>
        </div>
      ) : (
        /* ===== 底部バー ===== */
        <div className="relative shrink-0 bg-white border-t shadow-[0_-2px_8px_rgba(0,0,0,0.06)] z-40 pb-[env(safe-area-inset-bottom)]">
          {/* キューパネル（クイック表示） */}
          {queueOpen && (
            <div className="absolute bottom-full right-2 mb-1 w-[26rem] max-w-[calc(100vw-1rem)] max-h-80 overflow-y-auto bg-white border border-gray-200 rounded-lg shadow-xl">
              <div className="px-3 py-2 border-b flex items-center justify-between sticky top-0 bg-white">
                <span className="text-sm font-medium text-gray-700">再生キュー（{queue.length}曲）</span>
                <span className="flex items-center gap-2">
                  <button
                    onClick={() => {
                      setQueueOpen(false);
                      setExpanded(true);
                    }}
                    className="text-gray-400 hover:text-indigo-600"
                    title="全画面で表示"
                  >
                    <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 15l7-7 7 7" />
                    </svg>
                  </button>
                  <button onClick={() => setQueueOpen(false)} className="text-gray-400 hover:text-gray-600">✕</button>
                </span>
              </div>
              {queue.map((t, i) => (
                <div
                  key={`${t.performanceId}-${i}`}
                  className={`flex items-center gap-2 px-3 py-2 border-b last:border-b-0 cursor-pointer hover:bg-gray-50 ${
                    i === index ? 'bg-indigo-50' : ''
                  }`}
                  onClick={() => jumpTo(i)}
                >
                  <span className={`text-xs font-mono w-5 text-right shrink-0 ${i === index ? 'text-indigo-600' : 'text-gray-400'}`}>
                    {i === index ? '▶' : i + 1}
                  </span>
                  {t.artUrl ? (
                    <img src={t.artUrl} alt="" className="w-8 h-8 object-cover rounded shrink-0" />
                  ) : (
                    <span className="w-8 h-8 bg-gray-100 rounded shrink-0" />
                  )}
                  <span
                    className="flex-1 min-w-0"
                    title={`${t.songName}${t.artist ? ` / ${t.artist}` : ''}\n${
                      t.streamDate ? `${new Date(t.streamDate).toLocaleDateString('ja-JP')} · ` : ''
                    }${t.streamTitle ?? ''}`}
                  >
                    <span className="flex items-baseline gap-1.5 min-w-0">
                      <span className={`text-sm truncate ${i === index ? 'text-indigo-700 font-medium' : 'text-gray-900'}`}>
                        {t.songName}
                      </span>
                      {t.artist && <span className="text-[11px] text-gray-400 truncate shrink-0">{t.artist}</span>}
                    </span>
                    <span className="block text-xs text-gray-400 truncate">
                      {t.streamDate && (
                        <span className="font-mono">{new Date(t.streamDate).toLocaleDateString('ja-JP')} · </span>
                      )}
                      {t.streamTitle}
                    </span>
                  </span>
                  <span className="text-xs font-mono text-gray-400 shrink-0">
                    {t.end > t.start ? fmt(t.end - t.start) : '--:--'}
                  </span>
                  <button
                    onClick={(e) => {
                      e.stopPropagation();
                      removeAt(i);
                    }}
                    className="text-gray-300 hover:text-red-500 shrink-0"
                    title="キューから削除"
                  >
                    ✕
                  </button>
                </div>
              ))}
            </div>
          )}

          {/* モバイルはコントロール以外のタップで全画面表示（Musicdex 風） */}
          <div
            className="flex items-center gap-3 px-3 py-2 cursor-pointer sm:cursor-default"
            onClick={(e) => {
              if (window.matchMedia('(min-width: 640px)').matches) return;
              if ((e.target as HTMLElement).closest('button, a')) return;
              setExpanded(true);
            }}
          >
            {/* 動画のスペース（実映像は fixed のコンテナが浮いている） */}
            <div className="w-32 h-[72px] shrink-0 hidden sm:block" />
            {/* モバイルではアートワーク */}
            <div className="sm:hidden shrink-0">
              {track.artUrl ? (
                <img src={track.artUrl} alt="" className="w-10 h-10 object-cover rounded" />
              ) : (
                <span className="block w-10 h-10 bg-gray-100 rounded" />
              )}
            </div>

            {/* トラック情報 */}
            <div className="min-w-0 flex-1">
              <div className="flex items-center gap-2 min-w-0">
                {track.songId ? (
                  // モバイルではリンク無効（タップ＝全画面表示に統一）
                  <Link to={`/songs/${track.songId}`} title={track.songName} className="text-sm font-medium text-gray-900 hover:text-indigo-600 truncate pointer-events-none sm:pointer-events-auto">
                    {track.songName}
                  </Link>
                ) : (
                  <span className="text-sm font-medium text-gray-900 truncate" title={track.songName}>{track.songName}</span>
                )}
                <ArtistLinks
                  artists={track.artists}
                  fallback={track.artist}
                  className="text-xs text-gray-500 truncate hidden md:inline"
                  linkClassName="hover:text-indigo-600"
                />
              </div>
              <div className="flex items-center gap-2 text-xs text-gray-400 min-w-0">
                {/* モバイルはキュー位置を小さく表示（ボタンではなく情報として） */}
                <span className="sm:hidden font-mono shrink-0">{index + 1}/{queue.length}</span>
                {track.singers.length > 0 && (
                  <span className="truncate shrink-0 pointer-events-none sm:pointer-events-auto">
                    {track.singers.map((s, i) => (
                      <span key={s.id}>
                        {i > 0 && '、'}
                        <Link to={`/singers/${s.id}`} className="hover:text-indigo-600" title={s.name}>
                          {s.name}
                        </Link>
                      </span>
                    ))}
                  </span>
                )}
                {track.streamTitle && (
                  <Link
                    to={`/streams/${track.streamId}`}
                    title={track.streamTitle}
                    className="truncate hover:text-gray-600 hidden lg:inline"
                  >
                    {track.streamTitle}
                  </Link>
                )}
              </div>
            </div>

            {controls('sm')}

            <div className="hidden sm:flex items-center flex-1 max-w-xs shrink-0">{progressBar(false)}</div>

            <div className="hidden md:flex">{volumeControl(false)}</div>

            {/* モバイル：展開できることを示すシェブロン（バー全体がタップ対象） */}
            <svg className="sm:hidden w-4 h-4 text-gray-300 shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 15l7-7 7 7" />
            </svg>

            {/* 拡大・キュー・閉じる（sm 以上のみ。モバイルは拡大＝バータップ、キュー/閉じるは全画面側で操作） */}
            <div className="hidden sm:flex items-center gap-1 shrink-0">
              <button
                onClick={() => setExpanded(true)}
                className="p-2 text-gray-500 hover:text-gray-900"
                title="全画面表示"
              >
                <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 15l7-7 7 7" />
                </svg>
              </button>
              <button
                onClick={() => setQueueOpen(!queueOpen)}
                className={`px-2 py-1.5 rounded-lg text-sm flex items-center gap-1 ${
                  queueOpen ? 'bg-indigo-100 text-indigo-700' : 'text-gray-500 hover:text-gray-900'
                }`}
                title="再生キュー"
              >
                <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 6h16M4 12h16M4 18h10" />
                </svg>
                <span className="text-xs font-mono">{index + 1}/{queue.length}</span>
              </button>
              <button onClick={clear} className="p-2 text-gray-400 hover:text-gray-700" title="プレイヤーを閉じる">
                <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                </svg>
              </button>
            </div>
          </div>
        </div>
      )}
    </>
  );
}
