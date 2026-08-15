import type { MissingSongPayload } from '../api/types';
import type { PerformanceFieldValues } from '../components/PerformanceFields';

// payload を編集欄の値へ。編集画面と同じ形にすることで PerformanceFields を共用できる。
export function toFieldValues(id: string, p: MissingSongPayload): PerformanceFieldValues {
  return {
    id,
    name: p.song_name ?? '',
    nameReading: p.name_reading ?? '',
    artist: p.original_artist ?? '',
    artistReading: p.original_artist_reading ?? '',
    start: p.start_seconds ?? 0,
    end: p.end_seconds ?? 0,
    tags: p.tags ?? [],
    customTags: p.custom_tags ?? [],
    singerIds: p.singer_ids ?? [],
    matchedSongId: p.song_id || null,
    artUrl: p.art_url ?? null,
    itunesId: p.itunes_id ?? null,
  };
}
