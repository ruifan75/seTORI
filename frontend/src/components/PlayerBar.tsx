import { useEffect, useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import { usePlayerStore } from '../store/player';

// 秒 → M:SS
function fmt(sec: number): string {
  const s = Math.max(0, Math.floor(sec));
  const m = Math.floor(s / 60);
  return `${m}:${String(s % 60).padStart(2, '0')}`;
}

// グローバル再生バー（Musicdex 風）。
// キューの各トラック＝配信内の start〜end 区間。区間終端で自動的に次へ進む。
// YouTube 規約上プレイヤーは可視である必要があるため、バー左に小さく動画を表示する。
export default function PlayerBar() {
  const { queue, index, playing, queueOpen, next, prev, jumpTo, removeAt, setPlaying, setQueueOpen, clear } =
    usePlayerStore();
  const track = queue[index];

  const containerRef = useRef<HTMLDivElement>(null);
  const playerRef = useRef<any>(null);
  const readyRef = useRef(false);
  const currentVideoRef = useRef<string>('');
  const [progress, setProgress] = useState(0); // 区間内の経過秒

  // YT IFrame API のロード（既存の YoutubePlayer と同じ script を共有）
  useEffect(() => {
    if (!track || playerRef.current || !containerRef.current) return;

    const init = () => {
      const YT = (window as any).YT;
      if (!YT || !YT.Player) {
        setTimeout(init, 100);
        return;
      }
      playerRef.current = new YT.Player(containerRef.current, {
        width: '128',
        height: '72',
        videoId: track.streamId,
        playerVars: { autoplay: 1, controls: 0, modestbranding: 1, start: Math.floor(track.start) },
        events: {
          onReady: () => {
            readyRef.current = true;
            currentVideoRef.current = track.streamId;
          },
          onStateChange: (e: any) => {
            // 動画自体が終わった（end 未設定 or 区間が動画末尾）→ 次へ
            if (e.data === (window as any).YT?.PlayerState?.ENDED) {
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

    if (!(window as any).YT && !document.getElementById('youtube-api')) {
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

  return (
    <div className="relative shrink-0 bg-white border-t shadow-[0_-2px_8px_rgba(0,0,0,0.06)] z-40">
      {/* キューパネル */}
      {queueOpen && (
        <div className="absolute bottom-full right-2 mb-1 w-[26rem] max-w-[calc(100vw-1rem)] max-h-80 overflow-y-auto bg-white border border-gray-200 rounded-lg shadow-xl">
          <div className="px-3 py-2 border-b flex items-center justify-between sticky top-0 bg-white">
            <span className="text-sm font-medium text-gray-700">再生キュー（{queue.length}曲）</span>
            <span className="flex items-center gap-3">
              <Link
                to="/queue"
                onClick={() => setQueueOpen(false)}
                className="text-xs text-indigo-600 hover:text-indigo-800"
                title="キューを全画面で表示"
              >
                拡大表示 →
              </Link>
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
                {/* 同一曲を複数配信から再生するケースでは配信タイトル・日付が識別子になる */}
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

      <div className="flex items-center gap-3 px-3 py-2">
        {/* YT ミニプレイヤー（規約上可視必須） */}
        <div className="w-32 h-[72px] shrink-0 rounded overflow-hidden bg-black hidden sm:block">
          <div ref={containerRef} />
        </div>
        {/* モバイルでは動画の代わりにアートワーク */}
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
              <Link to={`/songs/${track.songId}`} title={track.songName} className="text-sm font-medium text-gray-900 hover:text-indigo-600 truncate">
                {track.songName}
              </Link>
            ) : (
              <span className="text-sm font-medium text-gray-900 truncate" title={track.songName}>{track.songName}</span>
            )}
            {track.artist && (
              <Link
                to={`/artists?search=${encodeURIComponent(track.artist)}`}
                title={`アーティスト: ${track.artist}`}
                className="text-xs text-gray-500 hover:text-indigo-600 truncate hidden md:inline"
              >
                {track.artist}
              </Link>
            )}
          </div>
          <div className="flex items-center gap-2 text-xs text-gray-400 min-w-0">
            {track.singers.length > 0 && (
              <span className="truncate shrink-0">
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

        {/* コントロール */}
        <div className="flex items-center gap-1 shrink-0">
          <button
            onClick={prev}
            disabled={index === 0}
            className="p-2 text-gray-500 hover:text-gray-900 disabled:opacity-30"
            title="前の曲"
          >
            <svg className="w-5 h-5" fill="currentColor" viewBox="0 0 24 24"><path d="M6 6h2v12H6zm3.5 6l8.5 6V6z"/></svg>
          </button>
          <button
            onClick={() => setPlaying(!playing)}
            className="p-2.5 bg-indigo-600 text-white rounded-full hover:bg-indigo-700 transition-colors"
            title={playing ? '一時停止' : '再生'}
          >
            {playing ? (
              <svg className="w-5 h-5" fill="currentColor" viewBox="0 0 24 24"><path d="M6 19h4V5H6v14zm8-14v14h4V5h-4z"/></svg>
            ) : (
              <svg className="w-5 h-5" fill="currentColor" viewBox="0 0 24 24"><path d="M8 5v14l11-7z"/></svg>
            )}
          </button>
          <button
            onClick={next}
            disabled={index >= queue.length - 1}
            className="p-2 text-gray-500 hover:text-gray-900 disabled:opacity-30"
            title="次の曲"
          >
            <svg className="w-5 h-5" fill="currentColor" viewBox="0 0 24 24"><path d="M6 18l8.5-6L6 6v12zM16 6v12h2V6h-2z"/></svg>
          </button>
        </div>

        {/* 進捗（区間内） */}
        <div className="hidden sm:flex items-center gap-2 flex-1 max-w-xs shrink-0">
          <span className="text-[11px] font-mono text-gray-400 w-9 text-right">{fmt(progress)}</span>
          <div
            className="flex-1 h-1.5 bg-gray-200 rounded-full cursor-pointer group"
            onClick={(e) => {
              const rect = e.currentTarget.getBoundingClientRect();
              seekTo((e.clientX - rect.left) / rect.width);
            }}
          >
            <div className="h-full bg-indigo-500 rounded-full relative" style={{ width: `${ratio * 100}%` }}>
              <span className="absolute right-0 top-1/2 -translate-y-1/2 translate-x-1/2 w-3 h-3 bg-indigo-600 rounded-full opacity-0 group-hover:opacity-100 transition-opacity" />
            </div>
          </div>
          <span className="text-[11px] font-mono text-gray-400 w-9">{duration > 0 ? fmt(duration) : '--:--'}</span>
        </div>

        {/* キュー・閉じる */}
        <div className="flex items-center gap-1 shrink-0">
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
  );
}
