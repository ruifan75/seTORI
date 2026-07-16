import { useEffect, useRef, useState } from 'react';
import { youtubePlayerSeekTo } from './YoutubePlayer';

// 秒数を M:SS / H:MM:SS 形式に
function fmt(sec: number): string {
  const s = Math.max(0, Math.round(sec));
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const r = s % 60;
  return h > 0 ? `${h}:${String(m).padStart(2, '0')}:${String(r).padStart(2, '0')}` : `${m}:${String(r).padStart(2, '0')}`;
}

const RANGE = 6; // ±6 秒

// TimestampTweaker 開始/終了時間の ±6 秒微調整（Holodex の RelativeTimestampEditor 相当）。
// 上段：試聴バー（クリックでその位置から再生、赤いラインは現在の再生位置）
// 下段：1秒刻みの目盛り付きスライダー。ドラッグ中は +N s と確定後の時刻をバブル表示し、
// 離すと確定してプレイヤーが自動シーク（end は3秒前から聴いて締めを確認）。
export default function TimestampTweaker({
  value,
  mode,
  onChange,
  currentTime,
}: {
  value: number;
  mode: 'start' | 'end';
  onChange: (newValue: number) => void;
  currentTime?: number | null;
}) {
  const [draft, setDraft] = useState(value);
  const [dragging, setDragging] = useState(false);
  const trackRef = useRef<HTMLDivElement>(null);

  // 外部から value が変わったら中央にリセット
  useEffect(() => {
    setDraft(value);
  }, [value]);

  const min = Math.max(0, value - RANGE);
  const max = value + RANGE;
  const span = max - min;
  const diff = draft - value;
  const pct = (v: number) => ((v - min) / span) * 100;

  const posFromEvent = (e: { clientX: number }): number => {
    const rect = trackRef.current?.getBoundingClientRect();
    if (!rect) return draft;
    const ratio = Math.min(1, Math.max(0, (e.clientX - rect.left) / rect.width));
    return Math.round(min + ratio * span); // 1秒スナップ
  };

  const commit = (v: number) => {
    if (v !== value) {
      onChange(v);
    }
    // 確定位置を試聴：start はそこから、end は 3 秒前から聴いて締めを確認
    youtubePlayerSeekTo(mode === 'start' ? v : Math.max(0, v - 3));
  };

  // 再生位置が窓内にあるときだけ進捗を描画
  const progress =
    currentTime != null && currentTime >= min && currentTime <= max ? pct(currentTime) : null;

  return (
    <div className="mt-2 select-none">
      {/* 試聴バー：クリックでその位置から再生 */}
      <div
        className="relative h-2 rounded-full bg-gray-100 cursor-pointer group/strip"
        title="クリックでこの位置から再生"
        onClick={(e) => youtubePlayerSeekTo(min + ((e.clientX - e.currentTarget.getBoundingClientRect().left) / e.currentTarget.getBoundingClientRect().width) * span)}
      >
        {progress !== null && (
          <>
            <div className="absolute inset-y-0 left-0 rounded-full bg-red-200" style={{ width: `${progress}%` }} />
            <div className="absolute top-1/2 -translate-y-1/2 w-0.5 h-3 bg-red-500 rounded" style={{ left: `${progress}%` }} />
          </>
        )}
        <span className="absolute -top-4 right-0 text-[10px] text-gray-300 opacity-0 group-hover/strip:opacity-100 transition-opacity">
          クリックで再生
        </span>
      </div>

      {/* 目盛り付きスライダー */}
      <div
        ref={trackRef}
        className="relative h-7 cursor-ew-resize touch-none"
        onPointerDown={(e) => {
          e.currentTarget.setPointerCapture(e.pointerId);
          setDragging(true);
          setDraft(posFromEvent(e));
        }}
        onPointerMove={(e) => {
          if (dragging) setDraft(posFromEvent(e));
        }}
        onPointerUp={(e) => {
          if (!dragging) return;
          setDragging(false);
          commit(posFromEvent(e));
        }}
        tabIndex={0}
        role="slider"
        aria-valuemin={min}
        aria-valuemax={max}
        aria-valuenow={draft}
        onKeyDown={(e) => {
          if (e.key === 'ArrowLeft') setDraft((d) => Math.max(min, d - 1));
          if (e.key === 'ArrowRight') setDraft((d) => Math.min(max, d + 1));
        }}
        onKeyUp={(e) => {
          if (e.key === 'ArrowLeft' || e.key === 'ArrowRight') commit(draft);
        }}
      >
        {/* トラック */}
        <div className="absolute left-0 right-0 top-1/2 -translate-y-1/2 h-1 rounded bg-gray-200" />

        {/* 1秒刻みの目盛り（中央 = 現在値は強調） */}
        {Array.from({ length: 2 * RANGE + 1 }, (_, i) => {
          const v = value - RANGE + i;
          if (v < min) return null;
          const isCenter = v === value;
          return (
            <div
              key={i}
              className={`absolute top-1/2 -translate-y-1/2 -translate-x-1/2 w-px ${
                isCenter ? 'h-4 bg-indigo-400 w-0.5' : 'h-2 bg-gray-300'
              }`}
              style={{ left: `${pct(v)}%` }}
            />
          );
        })}

        {/* サム（つまみ）＋バブル */}
        <div
          className="absolute top-1/2 -translate-y-1/2 -translate-x-1/2"
          style={{ left: `${pct(draft)}%` }}
        >
          <div
            className={`w-4 h-4 rounded-full border-2 bg-white shadow transition-transform ${
              dragging ? 'scale-125 border-indigo-600' : 'border-indigo-500'
            }`}
          />
          {/* 差分バブル：ドラッグ中 or 中央からズレているとき */}
          {(dragging || diff !== 0) && (
            <div className="absolute bottom-full left-1/2 -translate-x-1/2 mb-1 px-1.5 py-0.5 rounded bg-indigo-600 text-white text-[11px] font-mono whitespace-nowrap shadow">
              {diff === 0 ? fmt(draft) : `${diff > 0 ? '+' : ''}${diff}s → ${fmt(draft)}`}
            </div>
          )}
        </div>
      </div>

      {/* 目盛りラベル */}
      <div className="flex justify-between text-[10px] font-mono text-gray-400 -mt-1">
        <span>-{RANGE}s</span>
        <span className="text-gray-500">{fmt(value)}</span>
        <span>+{RANGE}s</span>
      </div>
    </div>
  );
}
