import type { QueryClient } from '@tanstack/react-query';
import type { TaskRun } from '../api/types';

export type MaintenanceTaskKind = 'readings_backfill' | 'duplicate_scan';

export const TASK_LABELS: Record<string, string> = {
  chapter_backfill: 'チャプターの取得',
  stream_prepare: '同期後の準備',
  chat_end_backfill: '拍手 end の埋め直し',
  readings_backfill: '読み仮名の AI 補完',
  duplicate_scan: '重複候補の走査',
};
export const TASK_PHASE_LABELS: Record<string, string> = {
  chapters: '章節取得', analysis: 'プレ分析', artists: 'アーティストの読み',
  songs: '曲名の読み', titles: '曲名キーの走査', ai_scan: 'AI の全件走査',
};
export const TASK_STATUS_LABELS: Record<string, string> = {
  running: '実行中', done: '完了', failed: '失敗', interrupted: '中断', cancelled: '停止',
};

// 一部失敗・中断でも保存できた行はあるので、すべての終了状態で取り直す。
export async function invalidateTaskResults(client: QueryClient, task: Pick<TaskRun, 'kind' | 'status'>) {
  if (task.status === 'running') return;
  const keys = task.kind === 'readings_backfill'
    ? ['readings-stats', 'artists', 'artist', 'songs', 'song', 'global-search']
    : task.kind === 'duplicate_scan' ? ['song-merge-candidates', 'song'] : [];
  // キャッシュがまだ無い初回取得も止める。invalidate だけだと古い取得を再利用する。
  await Promise.all(keys.map((key) => client.cancelQueries({ queryKey: [key] })));
  await Promise.all(keys.map((key) => client.invalidateQueries({ queryKey: [key] })));
}
