import type { QueryClient } from '@tanstack/react-query';

// 配信の表示を変えると詳細の状態だけでなく、発見面の歌唱一覧・件数も変わる。
// おすすめ等の staleTime: Infinity のキャッシュも、実行・取り消しの両方で失効させる。
export function invalidateStreamVisibilityQueries(client: QueryClient) {
  for (const key of [
    'stream', 'streams', 'stream-search', 'stream-tags',
    'tag-streams', 'performance-tags', 'tag-performances',
    'singer', 'singers', 'singer-search', 'singerStreams', 'singerPerformances',
    'songs', 'song', 'artists', 'artist', 'global-search',
    'random-performances', 'presets', 'preset-items',
  ]) {
    void client.invalidateQueries({ queryKey: [key] });
  }
}
