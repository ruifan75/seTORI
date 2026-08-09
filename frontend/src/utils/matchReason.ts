// 照合の根拠（SongMatchService の MatchReason）を画面用の文言にする。
//
// 同じ文言を統合候補のレビューと配信編集の両方で使う。ずれると
// 「同じ判定なのに画面ごとに違う説明が出る」ことになり、レビューの判断がぶれる。
export const MATCH_REASON_LABELS: Record<string, string> = {
  itunes_id: 'iTunes ID が一致',
  exact: '曲名・アーティストが完全一致',
  title_artist: '曲名もアーティストも一致',
  title_primary: '曲名一致・アーティストの主体が一致',
  title_overlap: '曲名一致・アーティスト名が部分的に共通',
  same_title: '曲名が同じ',
  title_mismatch: '曲名は一致・アーティストが違う（別名義の可能性）',
  title_ambiguous: '同じ曲名の曲が複数ある',
  title_only: '曲名は一致・アーティスト未記入',
  fuzzy_title: '曲名が似ている',
  song_alias: '学習済みの別表記',
  artist_alias: '登録済みの別名義で一致',
};

export function matchReasonLabel(reason?: string): string {
  if (!reason) return '';
  return MATCH_REASON_LABELS[reason] ?? reason;
}
