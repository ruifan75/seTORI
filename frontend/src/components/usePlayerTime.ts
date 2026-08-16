import { useEffect, useState } from 'react';
import { playerGetCurrentTime, type PlayerScope } from './youtubePlayerControl';

// 再生位置は**見る側が自分で追う**。
//
// 以前は親が測って prop で配っていたが、親が再レンダーしない限り値が固定される。
// 実際、赤い再生位置の線は開いた瞬間の場所に貼り付いたままになっていた
// （PlaybackFeedback はパネルを閉じた時点でポーリングを止め、SongDetailPage は
// JSX の中で 1 回読むだけだった）。測る責任を「表示する部品」へ寄せる。
//
// 250ms なのは ±6 秒の窓では 1 秒が幅の 8% にあたり、目に見えて飛ぶため。
const INTERVAL_MS = 250;

export function usePlayerTime(scope: PlayerScope | null): number | null {
  // 初回はレンダー時に読む（effect 内の同期 setState を避ける）
  const [time, setTime] = useState<number | null>(() => playerGetCurrentTime(scope));

  useEffect(() => {
    if (!scope) return; // 返り値側で null にするので、ここで setState はしない
    const timer = setInterval(() => setTime(playerGetCurrentTime(scope)), INTERVAL_MS);
    return () => clearInterval(timer);
  }, [scope]);

  return scope ? time : null;
}
