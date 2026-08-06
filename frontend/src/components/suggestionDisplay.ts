import type { Suggestion, SuggestionStatus } from '../api/types';

// 提案の中身を見せるための共通部品。
// 管理者のレビュー画面と「自分の提案」画面で同じ見え方にするため、ここに集約する。

export const FIELD_LABELS: Record<string, string> = {
  name: '名前',
  name_reading: '読み',
  original_artist: 'アーティスト',
  original_artist_reading: 'アーティストの読み',
  start_seconds: '開始時間',
  end_seconds: '終了時間',
  song: '曲',
};

export const TYPE_LABELS: Record<string, string> = {
  song: '楽曲',
  artist: 'アーティスト',
  performance: '歌唱',
  stream: '曲の追加',
};

export const STATUS_LABELS: Record<SuggestionStatus, string> = {
  pending: '確認待ち',
  conflict: '要確認',
  approved: '反映済み',
  rejected: '不採用',
};

// 秒数フィールドは M:SS / H:MM:SS でも見せる（6714 だけでは判断できないため）
const TIME_FIELDS = new Set(['start_seconds', 'end_seconds']);

export function formatFieldValue(key: string, value: string): string {
  if (!TIME_FIELDS.has(key)) return value || '（空）';
  const n = Number(value);
  if (!Number.isFinite(n)) return value || '（空）';
  if (n === 0) return '最後まで';
  const h = Math.floor(n / 3600);
  const m = Math.floor((n % 3600) / 60);
  const s = n % 60;
  const clock =
    h > 0
      ? `${h}:${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`
      : `${m}:${String(s).padStart(2, '0')}`;
  return `${clock}（${n}s）`;
}

// この提案が実際に変更するフィールド。
// 未登録曲の追加・曲の差し替えは既存フィールドを触らないので空になる（別途 payload を見せる）。
export function changedKeysOf(s: Suggestion): string[] {
  return Object.keys(s.after).filter((k) => (s.after[k] ?? '') !== (s.before[k] ?? ''));
}

// この提案が処理可能か（差分・payload・差し替え先のいずれかがある）
export function isActionable(s: Suggestion): boolean {
  if (s.kind === 'perf.missing') return !!s.payload;
  if (s.kind === 'perf.meta') return !!s.song_swap;
  return changedKeysOf(s).length > 0;
}

export function detailPathOf(targetType: string, targetID: string, targetKey: string): string {
  if (targetType === 'song') return `/songs/${targetID}`;
  if (targetType === 'artist') return `/artists/${targetID}`;
  if (targetType === 'stream') return `/streams/${targetKey}`;
  return '/songs'; // 歌唱は単独ページを持たない（対象名に配信が入っている）
}
