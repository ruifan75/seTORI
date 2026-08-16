import { create } from 'zustand';
import type { ArtistReference, Singer } from '../api/types';

// 再生キューの1トラック＝1歌唱記録（配信内の start〜end 区間）
export interface PlayerTrack {
  performanceId: string;
  streamId: string; // YouTube video ID
  songId?: string;
  songName: string;
  artist: string;
  artists: { id: string; name: string }[]; // 原曲アーティスト（UUID リンク用）
  artUrl?: string;
  singers: { id: string; name: string }[]; // 歌唱チャンネル（リンク用に id も保持）
  streamTitle?: string;
  streamDate?: string; // 配信日（同一曲を複数配信から再生するときの区別用）
  start: number;
  end: number; // 0 の場合は動画終了まで
}

// 配信横断の歌唱を再生トラックへ変換するための最小の形。
// Performance と SongPerformance の両方がこれを満たす（後者は arts を持たないなど差がある）。
export interface PerformanceLike {
  id: string;
  stream_id: string;
  song_id?: string;
  song_name?: string;
  original_artist?: string;
  artists?: ArtistReference[];
  arts?: string;
  singers?: Singer[];
  stream_title?: string;
  stream_date?: string;
  start_seconds: number;
  end_seconds: number;
}

// 配信横断の歌唱を再生トラックへ変換する。
// ホーム・タグ・歌手・プレイリストで同じ形を使う（曲詳細だけは曲側の情報を優先するため独自）。
export function performanceToTrack(p: PerformanceLike): PlayerTrack {
  return {
    performanceId: p.id,
    streamId: p.stream_id,
    songId: p.song_id,
    songName: p.song_name ?? '(不明)',
    artist: p.original_artist ?? '',
    artists: p.artists ?? [],
    artUrl: p.arts,
    singers: p.singers?.map((s) => ({ id: s.id, name: s.name })) ?? [],
    streamTitle: p.stream_title,
    streamDate: p.stream_date,
    start: p.start_seconds,
    end: p.end_seconds,
  };
}

export function performancesToTracks(perfs: PerformanceLike[]): PlayerTrack[] {
  return perfs.map(performanceToTrack);
}

// 報告ダイアログが開いている対象。
//
// performanceId が null なのは「まだ歌唱として登録されていない区間」を報告する場合
// （抜けている曲）。配信は決まっているが対象の歌唱が無いので、この形が要る。
export interface EditorTarget {
  streamId: string; // YouTube 動画 ID
  performanceId: string | null;
}

interface PlayerState {
  queue: PlayerTrack[];
  index: number;
  playing: boolean; // 再生意図（PlayerBar が YT インスタンスへ反映する）
  queueOpen: boolean;

  // editing … 報告ダイアログを開いている状態。**区間の締め切りを外す唯一の旗**。
  //
  // 開始/終了がずれているとき、正しい位置は今の区間の外にある（終了が早すぎる曲は
  // 本当の終わりを聴く前に次へ送られる）。ずれを直すための画面が、ずれた区間に
  // 閉じ込められていては用を成さない。
  //
  // 締め切りは終端監視・ENDED・矢印キーの clamp の 3 か所にあるので、
  // **すべてこの 1 つの旗を見る**こと。片方だけ例外を持つと、
  // 「キーでは出られるのに勝手に次へ送られる」形で壊れる。
  editing: EditorTarget | null;

  // tracks をキューにセットして startIndex から再生開始
  playTracks: (tracks: PlayerTrack[], startIndex?: number) => void;
  // tracks をキュー末尾に追加（空のときはそのまま再生開始）
  enqueue: (tracks: PlayerTrack[]) => void;
  next: () => void;
  prev: () => void;
  jumpTo: (index: number) => void;
  removeAt: (index: number) => void;
  // 歌唱の開始/終了が編集されたとき、キュー上の同じ歌唱にも反映する。
  // これが無いと「終了が早すぎる」を直した直後も、プレイヤーは古い end で次の曲へ送ってしまう。
  updateTrackTiming: (performanceId: string, timing: { start?: number; end?: number }) => void;
  // 報告ダイアログを開く/対象を切り替える。閉じるのは null。
  setEditing: (target: EditorTarget | null) => void;
  // 歌唱を「聴きながら直す」ために開く。**再生していなければ再生してから開く**
  // ── 時間のずれは聴かなければ判断できないので、報告と再生は不可分。
  // キューは壊さない（既にあればそこへ飛び、無ければ次の位置へ差し込む）。
  openReport: (track: PlayerTrack) => void;
  setPlaying: (playing: boolean) => void;
  setQueueOpen: (open: boolean) => void;
  clear: () => void;
}

export const usePlayerStore = create<PlayerState>((set, get) => ({
  queue: [],
  index: 0,
  playing: false,
  queueOpen: false,
  editing: null,

  playTracks: (tracks, startIndex = 0) => {
    if (tracks.length === 0) return;
    set({ queue: tracks, index: Math.min(startIndex, tracks.length - 1), playing: true });
  },

  enqueue: (tracks) => {
    if (tracks.length === 0) return;
    const { queue } = get();
    if (queue.length === 0) {
      set({ queue: tracks, index: 0, playing: true });
    } else {
      set({ queue: [...queue, ...tracks] });
    }
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

  updateTrackTiming: (performanceId, timing) => {
    const { queue } = get();
    if (!queue.some((t) => t.performanceId === performanceId)) return;
    set({
      queue: queue.map((t) =>
        t.performanceId === performanceId
          ? { ...t, start: timing.start ?? t.start, end: timing.end ?? t.end }
          : t
      ),
    });
  },

  setEditing: (target) => set({ editing: target }),

  openReport: (track) => {
    const { queue, index } = get();
    const at = queue.findIndex((t) => t.performanceId === track.performanceId);
    if (at >= 0) {
      set({ index: at, playing: true });
    } else if (queue.length === 0) {
      set({ queue: [track], index: 0, playing: true });
    } else {
      // 現在の曲の直後へ差し込む。末尾に足すと、閉じたあとキューの並びが
      // 「さっき直した曲」で終わることになり、元の流れが分からなくなる
      const next = [...queue.slice(0, index + 1), track, ...queue.slice(index + 1)];
      set({ queue: next, index: index + 1, playing: true });
    }
    set({ editing: { streamId: track.streamId, performanceId: track.performanceId } });
  },

  setPlaying: (playing) => set({ playing }),
  setQueueOpen: (open) => set({ queueOpen: open }),
  // プレイヤーごと閉じるので、開きっぱなしの報告ダイアログも畳む
  clear: () => set({ queue: [], index: 0, playing: false, queueOpen: false, editing: null }),
}));
