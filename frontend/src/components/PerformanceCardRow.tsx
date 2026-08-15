import { useCallback, useEffect, useRef, useState } from 'react';
import type { Performance } from '../api/types';
import PerformanceCard from './PerformanceCard';

interface Props {
  performances: Performance[];
  /** 押した歌唱から連続再生する（並びの何番目かを渡す） */
  onPlayFrom: (index: number) => void;
}

/**
 * 歌唱カードの横スクロール列（配信カードの StreamCardRow と同じ見せ方）。
 * ホームの「おすすめ」だけは追加読み込みを矢印に載せているため別実装のままにしてある。
 */
export default function PerformanceCardRow({ performances, onPlayFrom }: Props) {
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
  }, [performances.length, updateScrollState]);

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
        {performances.map((perf, i) => (
          <PerformanceCard key={perf.id} performance={perf} onPlay={() => onPlayFrom(i)} />
        ))}
      </div>

      {scrollState.canGoLeft && (
        <button
          type="button"
          onClick={() => scroll(-1)}
          aria-label="前の曲を表示"
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
          aria-label="次の曲を表示"
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
