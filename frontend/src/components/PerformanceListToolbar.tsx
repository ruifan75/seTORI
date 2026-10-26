import type { ReactNode } from 'react';
import { usePlayerStore } from '../store/player';
import type { PerformanceListItem } from '../utils/performanceList';

/** 再生対象は渡された項目だけ。ページング／上限制限をラベルに明示する。 */
export default function PerformanceListToolbar({ items, scope, total = items.length, children }: {
  items: PerformanceListItem[];
  scope: 'page' | 'all' | 'shown';
  total?: number;
  children?: ReactNode;
}) {
  const scopeName = { page: 'このページ', all: 'すべて', shown: '表示中' }[scope];
  const play = (shuffle: boolean) => {
    const tracks = items.map((item) => item.track);
    if (shuffle) {
      for (let i = tracks.length - 1; i > 0; i--) {
        const j = Math.floor(Math.random() * (i + 1));
        [tracks[i], tracks[j]] = [tracks[j], tracks[i]];
      }
    }
    usePlayerStore.getState().playTracks(tracks);
  };

  return (
    <div className="flex flex-wrap items-center gap-x-4 gap-y-3" aria-label="歌唱リストの操作">
      <div className="flex flex-wrap items-center gap-2">
        <button type="button" disabled={!items.length} onClick={() => play(false)}
          className="inline-flex min-h-10 items-center gap-2 rounded-full bg-indigo-600 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-indigo-700 disabled:opacity-40">
          <svg className="h-4 w-4" fill="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path d="M8 5v14l11-7z" /></svg>
          {scopeName}を再生
        </button>
        <button type="button" disabled={!items.length} onClick={() => play(true)}
          aria-label={`${scopeName}をランダム再生`}
          title={`${scopeName}の${items.length}件をランダム再生`}
          className="inline-flex min-h-10 items-center gap-2 rounded-full border border-gray-200 bg-white px-3 py-2 text-sm text-gray-600 transition-colors hover:bg-gray-50 disabled:opacity-40">
          <svg className="h-4 w-4" fill="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path d="M10.59 9.17L5.41 4 4 5.41l5.17 5.17 1.42-1.41zM14.5 4l2.04 2.04L4 18.59 5.41 20 17.96 7.46 20 9.5V4h-5.5zm.33 9.41l-1.41 1.41 3.13 3.13L14.5 20H20v-5.5l-2.04 2.04-3.13-3.13z" /></svg>
          ランダム
        </button>
      </div>
      <span className="text-xs tabular-nums text-gray-500">
        {total}件の歌唱{items.length < total && ` · 表示中 ${items.length}件`}
      </span>
      {children && <div className="flex flex-wrap items-center gap-2 sm:ml-auto">{children}</div>}
    </div>
  );
}
