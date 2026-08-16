import { useRef, useState } from 'react';
import { playerSeekTo } from './youtubePlayerControl';
import { usePlayerScope } from './playerScope';
import { usePlayerTime } from './usePlayerTime';
import { formatTimeInput } from '../utils/timeFormat';

// 歌唱の区間（開始〜終了）を時間軸の上で直す。
//
// **TimestampTweaker とは前提が逆**。あちらは「値はだいたい合っている」前提の
// ±6 秒の微調整で、こちらは「値が分単位でずれているかもしれない」前提の作り直し。
// 窓は自分で広げられ、区間の外へも自由に出られる。
//
// 前後の歌唱を薄い帯で背景に描くのが要点。区間の外へ出られるということは
// 隣の曲へ食い込めるということなので、**どこまで行くと隣に当たるか**が
// 見えていないと直したつもりで壊せる。
//
// ドラッグ中はシークしない（連続シークは重く、音も途切れて判断できない）。
// 離した時点で確定し、start はそこから・end は 3 秒前から試聴する
// ── TimestampTweaker と同じ規則にしてある。
//
// **タッチでは細いハンドルをつまめない**ので、tapTarget を渡すと
// 「トラックのどこを押してもそのハンドルが動く」モードになる。
// 押した位置＝聴きたい位置＝境界の候補なので、移動と試聴を分ける必要が無い。

export interface RangeNeighbour {
  id: string;
  label: string;
  start: number;
  end: number;
}

const ZOOMS = [
  { key: 30, label: '±30秒' },
  { key: 120, label: '±2分' },
  { key: 0, label: '全体' }, // 0 = 動画全体
] as const;

type Handle = 'start' | 'end';

export default function RangeEditor({
  start,
  end,
  duration,
  neighbours = [],
  tapTarget = null,
  onChange,
}: {
  start: number;
  end: number; // 0 = 動画の最後まで
  duration: number | null; // 動画全体の長さ（取れなければ null）
  neighbours?: RangeNeighbour[];
  // タッチ向け：トラックのタップでこのハンドルを動かす（null ならタップは試聴のみ）
  tapTarget?: Handle | null;
  onChange: (patch: { start?: number; end?: number }) => void;
}) {
  const scope = usePlayerScope();
  const current = usePlayerTime(scope);
  const [zoom, setZoom] = useState<number>(120);
  // ドラッグ中の値。確定するまで onChange は呼ばない
  const [drag, setDrag] = useState<{ handle: Handle; value: number } | null>(null);
  const trackRef = useRef<HTMLDivElement>(null);

  // end = 0（動画の最後まで）は時間軸の上では「動画の終わり」。
  // 長さが取れないときだけ、便宜的に開始 +5 分を右端の目安にする
  const segEnd = end > start ? end : (duration ?? start + 300);
  const shownStart = drag?.handle === 'start' ? drag.value : start;
  const shownEnd = drag?.handle === 'end' ? drag.value : segEnd;

  const max = duration ?? segEnd + 600;
  const win =
    zoom === 0
      ? { min: 0, max }
      : { min: Math.max(0, start - zoom), max: Math.min(max, segEnd + zoom) };
  const span = Math.max(1, win.max - win.min);
  const pct = (v: number) => ((v - win.min) / span) * 100;
  const clampToWindow = (v: number) => Math.min(win.max, Math.max(win.min, v));

  const valueFromX = (clientX: number): number => {
    const rect = trackRef.current?.getBoundingClientRect();
    if (!rect) return win.min;
    const ratio = Math.min(1, Math.max(0, (clientX - rect.left) / rect.width));
    return Math.round(win.min + ratio * span);
  };

  // 掴んだハンドルを動かす。開始と終了は追い越せない（1 秒は空ける）
  const moveHandle = (handle: Handle, raw: number): number => {
    const v = clampToWindow(Math.max(0, raw));
    return handle === 'start' ? Math.min(v, segEnd - 1) : Math.max(v, start + 1);
  };

  const commit = (handle: Handle, value: number) => {
    if (handle === 'start') {
      if (value !== start) onChange({ start: value });
      playerSeekTo(scope, value);
    } else {
      if (value !== end) onChange({ end: value });
      playerSeekTo(scope, Math.max(0, value - 3)); // 締めを確認する
    }
  };

  const startDrag = (handle: Handle) => (e: React.PointerEvent) => {
    e.stopPropagation();
    e.currentTarget.setPointerCapture(e.pointerId);
    setDrag({ handle, value: handle === 'start' ? start : segEnd });
  };

  const onPointerMove = (e: React.PointerEvent) => {
    if (!drag) return;
    setDrag({ handle: drag.handle, value: moveHandle(drag.handle, valueFromX(e.clientX)) });
  };

  const endDrag = (e: React.PointerEvent) => {
    if (!drag) return;
    const value = moveHandle(drag.handle, valueFromX(e.clientX));
    setDrag(null);
    commit(drag.handle, value);
  };

  // 窓の中に見えている隣の歌唱だけ描く
  const visibleNeighbours = neighbours.filter((n) => n.end > win.min && n.start < win.max);

  return (
    <div className="select-none">
      <div className="flex items-center justify-between mb-1">
        <span className="text-xs text-gray-500">区間</span>
        <div className="flex gap-1">
          {ZOOMS.map((z) => (
            <button
              key={z.key}
              type="button"
              onClick={() => setZoom(z.key)}
              className={`px-2 py-0.5 text-[11px] rounded border transition-colors ${
                zoom === z.key
                  ? 'bg-indigo-600 border-indigo-600 text-white'
                  : 'bg-white border-gray-300 text-gray-600 hover:bg-indigo-50'
              }`}
            >
              {z.label}
            </button>
          ))}
        </div>
      </div>

      {/* 時間軸本体。クリック（ハンドル以外）はその位置から試聴 */}
      <div
        ref={trackRef}
        className={`relative rounded-lg bg-gray-100 cursor-pointer touch-none ${tapTarget ? 'h-16' : 'h-14'}`}
        title={tapTarget ? `タップで${tapTarget === 'start' ? '開始' : '終了'}をここにする` : 'クリックでこの位置から再生'}
        onPointerMove={onPointerMove}
        onPointerUp={endDrag}
        onPointerCancel={endDrag}
        onClick={(e) => {
          if (drag) return;
          const at = valueFromX(e.clientX);
          // タップ先が決まっていればハンドルを動かす（commit が試聴もする）。
          // 決まっていなければ従来どおりその位置を聴くだけ
          if (tapTarget) commit(tapTarget, moveHandle(tapTarget, at));
          else playerSeekTo(scope, at);
        }}
      >
        {/* 前後の歌唱。ここへ食い込んでいないかを見るためのもの */}
        {visibleNeighbours.map((n) => (
          <div
            key={n.id}
            className="absolute top-0 bottom-0 bg-gray-200/80 border-x border-gray-300 overflow-hidden"
            style={{
              left: `${pct(Math.max(n.start, win.min))}%`,
              width: `${pct(Math.min(n.end, win.max)) - pct(Math.max(n.start, win.min))}%`,
            }}
            title={`${n.label}（${formatTimeInput(n.start)}〜）`}
          >
            <span className="absolute inset-x-1 top-1 text-[10px] text-gray-500 truncate">{n.label}</span>
          </div>
        ))}

        {/* 選択中の区間 */}
        <div
          className="absolute top-0 bottom-0 bg-indigo-500/25 border-x-2 border-indigo-500"
          style={{
            left: `${pct(clampToWindow(shownStart))}%`,
            width: `${pct(clampToWindow(shownEnd)) - pct(clampToWindow(shownStart))}%`,
          }}
        />

        {/* 現在の再生位置 */}
        {current != null && current >= win.min && current <= win.max && (
          <div
            className="absolute top-0 bottom-0 w-0.5 bg-red-500 pointer-events-none"
            style={{ left: `${pct(current)}%` }}
          >
            <span className="absolute -top-0.5 left-1/2 -translate-x-1/2 w-1.5 h-1.5 rounded-full bg-red-500" />
          </div>
        )}

        {/* ドラッグ中の時刻。**ハンドルに貼らずトラックの子として出す** ──
            ハンドルは端で枠の外へ半分出るので、そこに文字を置くと横スクロールが
            生まれる。位置は 8〜92% に丸めて必ず枠の中に収める */}
        {drag && (
          <div
            className="absolute -top-0.5 -translate-x-1/2 px-1.5 py-0.5 rounded bg-indigo-600 text-white text-[10px] font-mono whitespace-nowrap shadow pointer-events-none"
            style={{ left: `${Math.min(92, Math.max(8, pct(clampToWindow(drag.value))))}%` }}
          >
            {drag.handle === 'start' ? '開始' : '終了'} {formatTimeInput(drag.value)}
          </div>
        )}

        {/* ハンドル。指で掴めるよう見た目より広い当たり判定を持たせる */}
        {(['start', 'end'] as Handle[]).map((handle) => {
          const value = handle === 'start' ? shownStart : shownEnd;
          const active = drag?.handle === handle;
          const at = pct(clampToWindow(value));
          return (
            <div
              key={handle}
              role="slider"
              aria-label={handle === 'start' ? '開始' : '終了'}
              aria-valuemin={win.min}
              aria-valuemax={win.max}
              aria-valuenow={value}
              tabIndex={0}
              onPointerDown={startDrag(handle)}
              onKeyDown={(e) => {
                if (e.key !== 'ArrowLeft' && e.key !== 'ArrowRight') return;
                e.preventDefault();
                // キーボードは 1 秒刻み。押すたびに確定して試聴する
                const step = e.key === 'ArrowRight' ? 1 : -1;
                commit(handle, moveHandle(handle, (handle === 'start' ? start : segEnd) + step));
              }}
              className={`absolute top-0 bottom-0 -translate-x-1/2 cursor-ew-resize touch-none flex items-center justify-center ${
                tapTarget ? 'w-11' : 'w-6'
              }`}
              style={{ left: `${at}%` }}
            >
              <span
                className={`rounded-full shadow transition-transform ${
                  tapTarget === handle ? 'w-2 h-11 bg-indigo-600' : 'w-1.5 h-8 bg-indigo-600'
                } ${active ? 'scale-y-110 bg-indigo-700' : ''} ${
                  tapTarget && tapTarget !== handle ? 'opacity-40' : ''
                }`}
              />
            </div>
          );
        })}
      </div>

      {/* 窓の両端の時刻 */}
      <div className="flex justify-between text-[10px] font-mono text-gray-400 mt-0.5">
        <span>{formatTimeInput(win.min)}</span>
        <span>{formatTimeInput(win.max)}</span>
      </div>
    </div>
  );
}
