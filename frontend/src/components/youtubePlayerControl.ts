import type { YouTubePlayerInstance } from '../types/youtube';

let playerInstance: YouTubePlayerInstance | null = null;

export function setYoutubePlayerInstance(player: YouTubePlayerInstance) {
  playerInstance = player;
}

export function youtubePlayerSeekTo(seconds: number) {
  playerInstance?.seekTo(seconds, true);
  playerInstance?.playVideo();
}

export function youtubePlayerGetCurrentTime(): number | null {
  return playerInstance?.getCurrentTime() ?? null;
}

// グローバル再生バー（PlayerBar）の YT インスタンス。
// 上の playerInstance（歌枠編集ページの埋め込みプレイヤー）とは別に持つ。
// 同じ変数を共有すると、編集ページで両方が生きているときに
// TimestampTweaker のシークが意図しない側へ飛ぶため。
let barInstance: YouTubePlayerInstance | null = null;

export function setPlayerBarInstance(player: YouTubePlayerInstance | null) {
  barInstance = player;
}

// 再生バーが再生中の動画内の絶対位置（秒）。再生していなければ null。
export function playerBarGetCurrentTime(): number | null {
  try {
    return barInstance?.getCurrentTime() ?? null;
  } catch {
    return null;
  }
}
