import { useEffect, useRef, useState } from 'react';
import { usePlayerStore, type PlayerTrack } from '../store/player';
import PerformanceTimingDialog from './PerformanceTimingDialog';
import MissingSongDialog from './MissingSongDialog';
import SongSwapDialog from './SongSwapDialog';
import LoginToSuggest from './LoginToSuggest';
import { usePerformanceTiming, formatSeconds, type TimingTarget } from './usePerformanceTiming';

// 再生中に「タイムスタンプがずれている」と気づいた人がその場で直せる導線。
//
// 設計方針：
// - ダイアログで問い直さない。ボタンを押したその瞬間の再生位置が、その人の考える正しい位置。
// - 「今の曲」と「直前の曲」を並べて出す。次へ送られた直後に気づくことが多く、
//   どちらの話かをこちらで推測すると外すため、選ばせる。
// - 直前の曲が別の配信のものなら、再生位置を使う操作は意味を持たないので出さない
//   （その場合は「詳しく直す」だけ）。
export default function PlaybackFeedback({
  getCurrentTime,
  dark = false,
}: {
  getCurrentTime: () => number | null;
  dark?: boolean; // 全画面表示（暗い背景）で使うときの色味
}) {
  const queue = usePlayerStore((s) => s.queue);
  const index = usePlayerStore((s) => s.index);
  const { canEdit, canSubmit, submit } = usePerformanceTiming();

  const [open, setOpen] = useState(false);
  const [now, setNow] = useState<number | null>(null);
  const [dialogFor, setDialogFor] = useState<PlayerTrack | null>(null);
  // 未登録曲の報告ダイアログ。開いた瞬間の再生位置を開始時間の初期値にする
  const [missingAt, setMissingAt] = useState<number | null>(null);
  // 曲の差し替え（「この曲ではない」）ダイアログの対象
  const [swapFor, setSwapFor] = useState<PlayerTrack | null>(null);
  const wrapRef = useRef<HTMLDivElement>(null);

  const current = queue[index];
  const previous = index > 0 ? queue[index - 1] : undefined;

  // パネルを開いている間だけ再生位置を追う（閉じている間のポーリングは無駄）。
  // 開いた瞬間の値は toggle 側で入れる（effect 内での同期 setState を避けるため）。
  useEffect(() => {
    if (!open) return;
    const timer = setInterval(() => setNow(getCurrentTime()), 500);
    return () => clearInterval(timer);
  }, [open, getCurrentTime]);

  // パネル外のクリック・Esc で閉じる
  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (!wrapRef.current?.contains(e.target as Node)) setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(false);
    };
    document.addEventListener('mousedown', onDown);
    window.addEventListener('keydown', onKey);
    return () => {
      document.removeEventListener('mousedown', onDown);
      window.removeEventListener('keydown', onKey);
    };
  }, [open]);

  if (!current) return null;

  const toTarget = (t: PlayerTrack): TimingTarget => ({
    performanceId: t.performanceId,
    songName: t.songName,
    start: t.start,
    end: t.end,
  });

  // ワンタップ送信。押した瞬間の再生位置をそのまま採用する。
  const capture = async (track: PlayerTrack, field: 'start' | 'end') => {
    const t = getCurrentTime();
    if (t == null) return;
    const value = Math.round(t);
    const ok = await submit(toTarget(track), field === 'start' ? { start: value } : { end: value });
    if (ok) setOpen(false);
  };

  return (
    <div className="relative" ref={wrapRef}>
      <button
        onClick={() => {
          if (!open) setNow(getCurrentTime());
          setOpen((v) => !v);
        }}
        className={`p-2 rounded-lg ${
          open
            ? 'text-indigo-600 bg-indigo-50'
            : dark
              ? 'text-gray-400 hover:text-white hover:bg-white/10'
              : 'text-gray-400 hover:text-gray-700'
        }`}
        title={canEdit ? 'タイムスタンプを直す' : 'タイムスタンプの誤りを報告'}
        aria-label={canEdit ? 'タイムスタンプを直す' : 'タイムスタンプの誤りを報告'}
      >
        <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path
            strokeLinecap="round"
            strokeLinejoin="round"
            strokeWidth={2}
            d="M3 21v-4m0 0V5a2 2 0 012-2h6.5l1 1H21l-3 6 3 6h-8.5l-1-1H5a2 2 0 00-2 2zm9-13.5V9"
          />
        </svg>
      </button>

      {open && (
        <div className="absolute bottom-full right-0 mb-2 w-[22rem] max-w-[calc(100vw-2rem)] bg-white rounded-xl shadow-xl border p-3 z-50">
          {!canSubmit ? (
            <LoginToSuggest message="タイムスタンプの誤りを報告するにはログインが必要です。" />
          ) : (
            <>
          <p className="text-xs text-gray-500 mb-2">
            {now != null ? (
              <>
                再生位置 <span className="font-mono text-gray-700">{formatSeconds(now)}</span> を、
                選んだ曲の開始／終了として{canEdit ? '保存' : '提案'}します
              </>
            ) : (
              'どの曲のどこがずれていますか？'
            )}
          </p>

          <FeedbackRow
            heading="今の曲"
            track={current}
            canUseNow={now != null}
            onCapture={(field) => capture(current, field)}
            onDetail={() => {
              setDialogFor(current);
              setOpen(false);
            }}
            onSwap={() => {
              setSwapFor(current);
              setOpen(false);
            }}
          />

          {previous && (
            <FeedbackRow
              heading="直前の曲"
              track={previous}
              // 別配信の曲には現在の再生位置を当てはめられない
              canUseNow={now != null && previous.streamId === current.streamId}
              sameStream={previous.streamId === current.streamId}
              onCapture={(field) => capture(previous, field)}
              onDetail={() => {
                setDialogFor(previous);
                setOpen(false);
              }}
              onSwap={() => {
                setSwapFor(previous);
                setOpen(false);
              }}
            />
          )}

          {/* 既存の曲の話ではなく「ここに曲が登録されていない」という指摘 */}
          <div className="border-t pt-2 mt-2">
            <button
              onClick={() => {
                setMissingAt(Math.round(getCurrentTime() ?? current.start));
                setOpen(false);
              }}
              className="text-xs text-indigo-600 hover:bg-indigo-50 rounded-lg px-2 py-1 -ml-2"
            >
              ここに登録されていない曲がある
            </button>
          </div>
            </>
          )}
        </div>
      )}

      {dialogFor && (
        <PerformanceTimingDialog
          target={toTarget(dialogFor)}
          subtitle={dialogFor.streamTitle}
          currentTime={dialogFor.streamId === current.streamId ? getCurrentTime() : null}
          onClose={() => setDialogFor(null)}
        />
      )}

      {swapFor && (
        <SongSwapDialog
          performanceId={swapFor.performanceId}
          currentSongName={swapFor.songName}
          subtitle={swapFor.streamTitle}
          onClose={() => setSwapFor(null)}
        />
      )}

      {missingAt !== null && (
        <MissingSongDialog
          streamId={current.streamId}
          streamTitle={current.streamTitle}
          startSeconds={missingAt}
          onClose={() => setMissingAt(null)}
        />
      )}
    </div>
  );
}

function FeedbackRow({
  heading,
  track,
  canUseNow,
  sameStream = true,
  onCapture,
  onDetail,
  onSwap,
}: {
  heading: string;
  track: PlayerTrack;
  canUseNow: boolean;
  sameStream?: boolean;
  onCapture: (field: 'start' | 'end') => void;
  onDetail: () => void;
  onSwap: () => void;
}) {
  return (
    <div className="border-t first:border-t-0 pt-2 mt-2 first:pt-0 first:mt-0">
      <div className="flex items-baseline gap-2 min-w-0">
        <span className="text-[11px] font-medium text-gray-400 shrink-0">{heading}</span>
        <span className="text-sm text-gray-900 truncate" title={track.songName}>
          {track.songName}
        </span>
        <span className="text-[11px] font-mono text-gray-400 shrink-0 ml-auto">
          {formatSeconds(track.start)}–{track.end === 0 ? '最後' : formatSeconds(track.end)}
        </span>
      </div>

      <div className="flex flex-wrap gap-1.5 mt-1.5">
        <button
          onClick={() => onCapture('start')}
          disabled={!canUseNow}
          className="px-2 py-1 text-xs rounded-lg bg-gray-100 text-gray-700 hover:bg-gray-200 disabled:opacity-40 disabled:cursor-not-allowed"
          title="開始はいま聞こえている位置だ、と報告する"
        >
          開始はここ
        </button>
        <button
          onClick={() => onCapture('end')}
          disabled={!canUseNow}
          className="px-2 py-1 text-xs rounded-lg bg-gray-100 text-gray-700 hover:bg-gray-200 disabled:opacity-40 disabled:cursor-not-allowed"
          title="終了はいま聞こえている位置だ、と報告する"
        >
          終了はここ
        </button>
        <button
          onClick={onDetail}
          className="px-2 py-1 text-xs rounded-lg text-indigo-600 hover:bg-indigo-50"
        >
          詳しく直す
        </button>
        <button
          onClick={onSwap}
          className="px-2 py-1 text-xs rounded-lg text-indigo-600 hover:bg-indigo-50"
          title="この区間は別の曲だと報告する"
        >
          曲が違う
        </button>
      </div>

      {!sameStream && (
        <p className="text-[11px] text-gray-400 mt-1">
          別の配信の曲のため、再生位置は使えません
        </p>
      )}
    </div>
  );
}
