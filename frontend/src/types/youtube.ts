export interface YouTubePlayerInstance {
  destroy(): void;
  getCurrentTime(): number;
  // 動画全体の長さ（秒）。読み込み前は 0 を返す
  getDuration(): number;
  getPlayerState(): number;
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

// onError の data はエラーコード。2=パラメータ不正 / 5=HTML5 / 100=動画が無い /
// 101・150=埋め込みが許可されていない（会限もここに来る）
export interface YouTubePlayerErrorEvent extends YouTubePlayerEvent {
  data: number;
}

interface YouTubePlayerOptions {
  origin?: string;
  width?: string;
  height?: string;
  // 省略時は空のプレイヤーを生成し、後から loadVideoById で読み込む
  videoId?: string;
  playerVars?: Record<string, string | number>;
  events?: {
    onReady?: (event: YouTubePlayerEvent) => void;
    onStateChange?: (event: YouTubePlayerStateChangeEvent) => void;
    onError?: (event: YouTubePlayerErrorEvent) => void;
  };
}

interface YouTubeNamespace {
  Player: new (element: HTMLElement, options: YouTubePlayerOptions) => YouTubePlayerInstance;
  PlayerState?: {
    ENDED: number;
    PLAYING: number;
    PAUSED: number;
  };
}

declare global {
  interface Window {
    YT?: YouTubeNamespace;
  }
}
