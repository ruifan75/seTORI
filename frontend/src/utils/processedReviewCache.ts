import type { QueryClient } from '@tanstack/react-query';

// 処理済み状態を表示・絞り込み・自動処理の残件に使うキャッシュを、実行と撤回で更新する。
export function invalidateProcessedReviewQueries(client: QueryClient) {
  for (const key of [
    'processed-review', 'processed-runs', 'stream', 'streams', 'stream-search',
    'singerStreams', 'batch-fill-status', 'batch-analyze-status', 'autoFillSettings',
  ]) void client.invalidateQueries({ queryKey: [key] });
}
