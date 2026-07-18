export interface YouTubePlayerInstance {
  destroy(): void;
  getCurrentTime(): number;
  loadVideoById(options: { videoId: string; startSeconds?: number }): void;
  mute(): void;
  pauseVideo(): void;
  playVideo(): void;
  seekTo(seconds: number, allowSeekAhead: boolean): void;
  setVolume(volume: number): void;
  unMute(): void;
}

export interface YouTubePlayerEvent {
  target: YouTubePlayerInstance;
}

export interface YouTubePlayerStateChangeEvent extends YouTubePlayerEvent {
  data: number;
}

interface YouTubePlayerOptions {
  origin?: string;
  width?: string;
  height?: string;
  videoId: string;
  playerVars?: Record<string, string | number>;
  events?: {
    onReady?: (event: YouTubePlayerEvent) => void;
    onStateChange?: (event: YouTubePlayerStateChangeEvent) => void;
    onError?: (event: YouTubePlayerEvent) => void;
  };
}

interface YouTubeNamespace {
  Player: new (element: HTMLElement, options: YouTubePlayerOptions) => YouTubePlayerInstance;
  PlayerState?: {
    ENDED: number;
  };
}

declare global {
  interface Window {
    YT?: YouTubeNamespace;
  }
}
