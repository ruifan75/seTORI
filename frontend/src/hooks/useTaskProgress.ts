import { useEffect, useRef } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { taskApi } from '../api/client';
import { invalidateTaskResults } from '../utils/taskResults';
import type { MaintenanceTaskKind } from '../utils/taskResults';

// 起動した実行を追う。再訪時は最新の同種の実行を拾い、実行中ならポーリングを再開する。
export function useTaskProgress(kind: MaintenanceTaskKind, taskId: string | null) {
  const client = useQueryClient();
  const completed = useRef<string | null>(null);
  const query = useQuery({
    queryKey: ['task-progress', kind, taskId],
    queryFn: async () => taskId ? taskApi.get(taskId) :
      (await taskApi.list(100)).find((task) => task.kind === kind) ?? null,
    staleTime: 0,
    refetchInterval: (q) => (!!taskId && !q.state.data) || q.state.data?.status === 'running' ? 2000 : false,
  });
  useEffect(() => {
    const task = query.data;
    if (!task || task.status === 'running' || completed.current === task.id) return;
    completed.current = task.id;
    invalidateTaskResults(client, task);
    void client.invalidateQueries({ queryKey: ['tasks'] });
  }, [query.data, client]);
  return { ...query, isRunning: query.data?.status === 'running' || (!!taskId && !query.data) };
}
