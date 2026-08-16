import { useEffect, useRef } from 'react';
import { formatTimeInput } from '../utils/timeFormat';
import type { Performance } from '../api/types';

// 報告ダイアログの中に出す「この配信の曲」一覧。対象を選び直すためのもの。
//
// **再生キューではなく配信のセットリストを出す。** 誤りに気づくのは次の曲へ
// 送られた直後が多いので「直前の曲」へ戻れることが要るが、キューは
// プレイリスト由来だと配信を跨いでいて、その配信の直前の曲が入っていない。
// 直したい相手は常に「同じ配信の隣の曲」なので、そちらを出す。
//
// 選ぶとプレイヤーがその曲へ飛ぶ（親が処理する）。
export default function SetlistStrip({
  performances,
  currentId,
  onSelect,
  onAddMissing,
}: {
  performances: Performance[];
  currentId: string | null;
  onSelect: (performanceId: string) => void;
  onAddMissing?: () => void;
}) {
  // 選ばれている曲まで送る。16 曲ある配信の 9 曲目を直しているのに
  // 一覧が先頭を映していると、隣の曲へ移る導線として使えない
  const selectedRef = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    selectedRef.current?.scrollIntoView({ block: 'nearest' });
    // 一覧が届いたタイミングでも送る。対象は最初から決まっているので
    // currentId だけを見ていると、読み込み後に一度も動かない
  }, [currentId, performances.length]);

  return (
    <div className="flex flex-col min-h-0">
      <div className="px-3 py-2 text-xs font-medium text-gray-500 border-b shrink-0">
        この配信の曲 <span className="font-mono text-gray-400">{performances.length}</span>
      </div>
      <div className="flex-1 overflow-y-auto overscroll-contain divide-y">
        {performances.length === 0 && (
          <p className="px-3 py-3 text-xs text-gray-400">歌唱が登録されていません</p>
        )}
        {performances.map((p, i) => {
          const selected = p.id === currentId;
          return (
            <button
              key={p.id}
              ref={selected ? selectedRef : undefined}
              type="button"
              onClick={() => onSelect(p.id)}
              aria-current={selected}
              className={`w-full text-left px-3 py-2 flex items-baseline gap-2 min-w-0 transition-colors ${
                selected ? 'bg-indigo-50' : 'hover:bg-gray-50'
              }`}
            >
              <span
                className={`text-[11px] font-mono w-5 text-right shrink-0 ${
                  selected ? 'text-indigo-600' : 'text-gray-400'
                }`}
              >
                {selected ? '▶' : i + 1}
              </span>
              <span className="min-w-0 flex-1">
                <span
                  className={`block text-sm truncate ${
                    selected ? 'text-indigo-900 font-medium' : 'text-gray-900'
                  }`}
                >
                  {p.song_name || '(曲名なし)'}
                </span>
                {p.original_artist && (
                  <span className="block text-[11px] text-gray-400 truncate">{p.original_artist}</span>
                )}
              </span>
              <span className="text-[11px] font-mono text-gray-400 shrink-0">
                {formatTimeInput(p.start_seconds)}
              </span>
            </button>
          );
        })}
      </div>
      {onAddMissing && (
        <button
          type="button"
          onClick={onAddMissing}
          className="shrink-0 border-t px-3 py-2 text-left text-xs text-indigo-600 hover:bg-indigo-50"
        >
          ＋ ここに登録されていない曲がある
        </button>
      )}
    </div>
  );
}
