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
