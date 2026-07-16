import { create } from 'zustand';

// 再生キューの1トラック＝1演唱記録（配信内の start〜end 区間）
export interface PlayerTrack {
  performanceId: string;
  streamId: string; // YouTube video ID
  songId?: string;
  songName: string;
  artist: string;
  artUrl?: string;
  singers: { id: string; name: string }[]; // 歌唱チャンネル（リンク用に id も保持）
  streamTitle?: string;
  streamDate?: string; // 配信日（同一曲を複数配信から再生するときの区別用）
  start: number;
  end: number; // 0 の場合は動画終了まで
}

interface PlayerState {
  queue: PlayerTrack[];
  index: number;
  playing: boolean; // 再生意図（PlayerBar が YT インスタンスへ反映する）
  queueOpen: boolean;

  // tracks をキューにセットして startIndex から再生開始
  playTracks: (tracks: PlayerTrack[], startIndex?: number) => void;
  next: () => void;
  prev: () => void;
  jumpTo: (index: number) => void;
  removeAt: (index: number) => void;
  setPlaying: (playing: boolean) => void;
  setQueueOpen: (open: boolean) => void;
  clear: () => void;
}

export const usePlayerStore = create<PlayerState>((set, get) => ({
  queue: [],
  index: 0,
  playing: false,
  queueOpen: false,

  playTracks: (tracks, startIndex = 0) => {
    if (tracks.length === 0) return;
    set({ queue: tracks, index: Math.min(startIndex, tracks.length - 1), playing: true });
  },

  next: () => {
    const { queue, index } = get();
    if (index + 1 < queue.length) {
      set({ index: index + 1, playing: true });
    } else {
      set({ playing: false }); // キューの最後で停止
    }
  },

  prev: () => {
    const { index } = get();
    if (index > 0) set({ index: index - 1, playing: true });
  },

  jumpTo: (i) => {
    const { queue } = get();
    if (i >= 0 && i < queue.length) set({ index: i, playing: true });
  },

  removeAt: (i) => {
    const { queue, index } = get();
    const next = queue.filter((_, j) => j !== i);
    if (next.length === 0) {
      set({ queue: [], index: 0, playing: false, queueOpen: false });
      return;
    }
    let newIndex = index;
    if (i < index) newIndex = index - 1;
    else if (i === index) newIndex = Math.min(index, next.length - 1);
    set({ queue: next, index: newIndex });
  },

  setPlaying: (playing) => set({ playing }),
  setQueueOpen: (open) => set({ queueOpen: open }),
  clear: () => set({ queue: [], index: 0, playing: false, queueOpen: false }),
}));
