import type { PerformanceTag, Song } from '../api/types';
import { performanceToTrack, type PerformanceLike, type PlayerTrack } from '../store/player';

/** 配信横断の一覧で表示する1回の歌唱。PlayerTrack と表示情報の出どころをそろえる。 */
export interface PerformanceListItem {
  track: PlayerTrack;
  thumbnailUrl?: string;
  youtubeUrl: string;
  tags: PerformanceTag[];
  restricted: boolean;
}

type Source = PerformanceLike & {
  thumbnail_url?: string;
  youtube_url: string;
  tags: PerformanceTag[];
  custom_tags?: string[];
  is_restricted?: boolean;
};

// 曲詳細の応答は曲情報を親に持つ。表示・追加・報告のすべてに同じ補完を使う。
export function toPerformanceListItem(performance: Source, song?: Song): PerformanceListItem {
  return {
    track: performanceToTrack(song ? {
      ...performance,
      song_id: song.id,
      song_name: song.name,
      original_artist: song.original_artist,
      artists: song.artists,
      arts: song.arts,
    } : performance),
    thumbnailUrl: performance.thumbnail_url,
    youtubeUrl: performance.youtube_url,
    tags: [
      ...performance.tags,
      ...(performance.custom_tags ?? []).map((name) => ({ id: `custom:${name}`, display_name: name, color: '#6B7280' })),
    ],
    restricted: performance.is_restricted === true,
  };
}
