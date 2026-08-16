import { useEffect, useState } from 'react';

// 画面幅でレイアウトを切り替えるためのフック。
//
// **境目は Tailwind の lg（1024px）に合わせる。** CSS 側と別の数字を持つと、
// 片方だけ直したときに「JS は横並びのつもり、CSS は縦積み」という形で崩れる。
export function useMediaQuery(query: string): boolean {
  const [matches, setMatches] = useState(() => window.matchMedia(query).matches);

  useEffect(() => {
    const mql = window.matchMedia(query);
    const onChange = () => setMatches(mql.matches);
    onChange(); // query が変わった直後のずれを埋める（同値なら React が止める）
    mql.addEventListener('change', onChange);
    return () => mql.removeEventListener('change', onChange);
  }, [query]);

  return matches;
}

// 報告画面が「縦に積む」側に入るか。lg 未満＝スマホ・小さいタブレット。
export function useIsCompact(): boolean {
  return useMediaQuery('(max-width: 1023px)');
}
