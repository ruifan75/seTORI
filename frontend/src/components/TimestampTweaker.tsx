import { useEffect, useState } from 'react';
import { youtubePlayerSeekTo } from './YoutubePlayer';

// 秒数を M:SS 形式に
function fmt(sec: number): string {
  const s = Math.max(0, Math.round(sec));
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const r = s % 60;
  return h > 0 ? `${h}:${String(m).padStart(2, '0')}:${String(r).padStart(2, '0')}` : `${m}:${String(r).padStart(2, '0')}`;
}

// TimestampTweaker 開始/終了時間の ±6 秒微調整スライダー（Holodex 風）。
// ドラッグ中は差分（+2s / -3s）を表示し、離した時に確定 → プレイヤーが該当位置へ
// 自動シークして試聴できる（end モードは終了 3 秒前から聴いて締めを確認）。
export default function TimestampTweaker({
  value,
  mode,
  onChange,
}: {
  value: number;
  mode: 'start' | 'end';
  onChange: (newValue: number) => void;
}) {
  const RANGE = 6;
  const [draft, setDraft] = useState(value);

  // 外部から value が変わったら中央にリセット
  useEffect(() => {
    setDraft(value);
  }, [value]);

  const min = Math.max(0, value - RANGE);
  const max = value + RANGE;
  const diff = draft - value;

  const commit = () => {
    if (draft !== value) {
      onChange(draft);
    }
    // 確定位置を試聴：start はそこから、end は 3 秒前から聴いて締めを確認
    youtubePlayerSeekTo(mode === 'start' ? draft : Math.max(0, draft - 3));
  };

  return (
    <div className="flex items-center gap-2 mt-1.5">
      <span className="text-[10px] text-gray-400 font-mono w-8 text-right shrink-0">-{RANGE}s</span>
      <input
        type="range"
        min={min}
        max={max}
        step={1}
        value={draft}
        onChange={(e) => setDraft(Number(e.target.value))}
        onMouseUp={commit}
        onTouchEnd={commit}
        onKeyUp={(e) => {
          if (e.key === 'ArrowLeft' || e.key === 'ArrowRight') commit();
        }}
        className="flex-1 h-1.5 accent-indigo-600 cursor-pointer"
        title="ドラッグで微調整（離すと確定して試聴）"
      />
      <span className="text-[10px] text-gray-400 font-mono w-8 shrink-0">+{RANGE}s</span>
      <span
        className={`text-xs font-mono w-20 text-right shrink-0 ${
          diff !== 0 ? 'text-indigo-600 font-semibold' : 'text-gray-400'
        }`}
      >
        {diff !== 0 ? `${diff > 0 ? '+' : ''}${diff}s → ${fmt(draft)}` : fmt(value)}
      </span>
    </div>
  );
}
