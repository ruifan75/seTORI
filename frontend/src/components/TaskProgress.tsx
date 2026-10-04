import { Link } from 'react-router-dom';
import type { TaskRun } from '../api/types';
import { TASK_PHASE_LABELS, TASK_STATUS_LABELS } from '../utils/taskResults';

export default function TaskProgress({ task, isError, waiting }: { task?: TaskRun | null; isError: boolean; waiting: boolean }) {
  return (
    <div className="text-sm space-y-1 break-words" aria-live="polite">
      {isError ? <p className="text-red-600">進捗の取得に失敗しました。背景処理の記録を確認してください。</p> : task ? (
        <>
          <p className={task.status === 'failed' ? 'text-red-600' : 'text-gray-700'}>
            {TASK_STATUS_LABELS[task.status]} {task.phase && `・${TASK_PHASE_LABELS[task.phase] ?? task.phase}`}：
            {task.done}/{task.total}（成功 {task.succeeded}・見送り {task.skipped}・失敗 {task.failed}）
          </p>
          {task.status !== 'running' && task.message && <p>{task.message}</p>}
          {task.failures.length > 0 && <details>
            <summary className="cursor-pointer text-red-600">失敗の理由</summary>
            <ul className="mt-1 space-y-1 max-h-40 overflow-y-auto">
              {task.failures.map((f, i) => <li key={i} className="break-all">{f.target}：{f.reason}</li>)}
            </ul>
          </details>}
        </>
      ) : waiting ? <p>実行記録を取得しています…</p> : null}
      <Link className="text-indigo-600 underline" to="/admin/sync#background-tasks">背景処理の記録を見る</Link>
    </div>
  );
}
