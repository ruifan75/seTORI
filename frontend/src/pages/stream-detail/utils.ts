import type { CommentSong, EndSource } from '../../api/types';
import type { EditableSong } from './types';

// AI 正規化後に重複楽曲をマージ
// 条件：name が完全一致 + artist が完全一致 + start の時間差 ≤ 30s
export function mergeDuplicateSongs(songs: EditableSong[]): EditableSong[] {
  const result: EditableSong[] = [];

  for (const song of songs) {
    let merged = false;
    for (let i = 0; i < result.length; i++) {
      const existing = result[i];
      if (
        existing.name === song.name &&
        existing.artist === song.artist &&
        Math.abs(existing.start - song.start) <= 30
      ) {
        // Merge: 情報がより完全な方を保持
        const hasRealEnd = (s: EditableSong) => s.end > 0 && !s.isEndTimeEstimated;
        const preferSong = hasRealEnd(song) && !hasRealEnd(existing);

        // 吸収された側の元の名称を記録（AI 変更前の名称）
        const absorbedName = preferSong
          ? (existing.aiNormalizedName || existing.originalName)
          : (song.aiNormalizedName || song.originalName);
        const prevMerged = existing.mergedFrom || [];

        if (preferSong) {
          result[i] = {
            ...song,
            tags: [...new Set([...song.tags, ...existing.tags])],
            matchedSongId: song.matchedSongId || existing.matchedSongId,
            artUrl: song.artUrl || existing.artUrl,
            itunesId: song.itunesId ?? existing.itunesId,
            itunesFromDb: song.itunesId != null ? song.itunesFromDb : existing.itunesFromDb,
            trackDuration: song.trackDuration ?? existing.trackDuration,
            nameReading: song.nameReading || existing.nameReading,
            artistReading: song.artistReading || existing.artistReading,
            mergedFrom: [...prevMerged, absorbedName],
          };
        } else {
          result[i] = {
            ...existing,
            tags: [...new Set([...existing.tags, ...song.tags])],
            matchedSongId: existing.matchedSongId || song.matchedSongId,
            artUrl: existing.artUrl || song.artUrl,
            itunesId: existing.itunesId ?? song.itunesId,
            itunesFromDb: existing.itunesId != null ? existing.itunesFromDb : song.itunesFromDb,
            trackDuration: existing.trackDuration ?? song.trackDuration,
            nameReading: existing.nameReading || song.nameReading,
            artistReading: existing.artistReading || song.artistReading,
            mergedFrom: [...prevMerged, absorbedName],
          };
        }
        merged = true;
        break;
      }
    }
    if (!merged) {
      result.push(song);
    }
  }

  return result;
}

/**
 * endSourceForSourceSong は入力元から読み込んだ曲の終了時間の由来を決める。
 * バックエンドの endSourceForComment / endSourceForChapter と同じ規則にしてある
 * ── 経路によって違う値が入ると、確度で絞り込む問い合わせが当てにならなくなる。
 */
export function endSourceForSourceSong(song: CommentSong, source: 'comment' | 'chapter'): EndSource | undefined {
  if (song.end <= 0) return undefined;
  if (song.chat_end && song.chat_end === song.end) return 'chat';
  // チャプターの end は次の章節の開始なので、拍手で埋まっていない限り推定値でしかない
  if (source === 'chapter') return 'next_start';
  return song.is_end_time_estimated ? undefined : 'comment';
}

export function formatTime(seconds: number): string {
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  const s = seconds % 60;

  if (h > 0) {
    return `${h}:${m.toString().padStart(2, '0')}:${s.toString().padStart(2, '0')}`;
  }
  return `${m}:${s.toString().padStart(2, '0')}`;
}
