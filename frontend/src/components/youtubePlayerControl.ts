import type { YouTubePlayerInstance } from '../types/youtube';

// 画面には YouTube プレイヤーが 2 つ存在しうる。
//   page … 歌枠編集ページ・審査ページに埋め込まれたプレイヤー
//   bar  … 全ページ共通のグローバル再生バー
//
// **どちらを操作するかは呼び出し側が明示する。** 「最後にアクティブだった方」の
// ような暗黙の選択にすると、編集ページで両方が生きているときに飛び先が変わる。
//
// null は「このツリーには対応するプレイヤーが無い」（別の配信を再生中、
// そもそもプレイヤーが出ていない等）。再生位置の取り込みも試聴もできないので、
// 呼んでも何も起きない。呼び出し側が毎回 if を書かなくて済むよう、ここで吸収する。
export type PlayerScope = 'page' | 'bar';

const instances: Record<PlayerScope, YouTubePlayerInstance | null> = {
  page: null,
  bar: null,
};

// null を渡すと参照を捨てる。壊れたプレイヤーを作り直すとき、古い参照を
// 残したままにすると seek や getCurrentTime が死んだ iframe を触りにいく。
export function setPlayerInstance(scope: PlayerScope, player: YouTubePlayerInstance | null) {
  instances[scope] = player;
}

export function playerSeekTo(scope: PlayerScope | null, seconds: number) {
  if (!scope) return;
  const player = instances[scope];
  if (!player) return;
  try {
    player.seekTo(Math.max(0, seconds), true);
    player.playVideo();
  } catch {
    // 破棄直後のプレイヤーは呼び出しで投げることがある
  }
}

// 動画内の絶対再生位置（秒）。プレイヤーが無ければ null。
export function playerGetCurrentTime(scope: PlayerScope | null): number | null {
  if (!scope) return null;
  try {
    return instances[scope]?.getCurrentTime() ?? null;
  } catch {
    // 再生が止まった直後のプレイヤーは呼び出しで投げることがある
    return null;
  }
}

// 一時停止。歌枠編集ページのように**プレイヤーが 2 つ生きている画面**で、
// 片方を前に出すときに他方を黙らせるために要る（同じ動画が二重に鳴る）。
export function playerPause(scope: PlayerScope | null) {
  if (!scope) return;
  try {
    instances[scope]?.pauseVideo();
  } catch {
    // 破棄直後のプレイヤーは呼び出しで投げることがある
  }
}

// 動画全体の長さ（秒）。読み込み前は 0 を返すので、その場合も null に寄せる
// （0 を長さとして扱うと時間軸の幅が 0 になる）。
export function playerGetDuration(scope: PlayerScope | null): number | null {
  if (!scope) return null;
  try {
    const d = instances[scope]?.getDuration() ?? 0;
    return d > 0 ? d : null;
  } catch {
    return null;
  }
}
