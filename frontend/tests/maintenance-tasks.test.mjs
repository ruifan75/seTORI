import assert from 'node:assert/strict';
import { test } from 'node:test';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import vm from 'node:vm';
import ts from 'typescript';
import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { QueryClient, QueryObserver } from '@tanstack/react-query';

const require = createRequire(import.meta.url);
function loadTS(path, dependencies = {}) {
  const module = { exports: {} };
  const url = new URL(path, import.meta.url);
  const code = ts.transpileModule(readFileSync(url, 'utf8').replaceAll('import.meta.env', '({})'), {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022, jsx: ts.JsxEmit.ReactJSX },
  }).outputText;
  vm.runInNewContext(code, { module, exports: module.exports, require: (name) => dependencies[name] ?? require(name) }, { filename: url.pathname });
  return module.exports;
}
const jsonCopy = (value) => JSON.parse(JSON.stringify(value));
const task = (kind, status) => ({
  id: 'task-one', kind, status, phase: kind === 'readings_backfill' ? 'songs' : 'ai_scan',
  total: 5, done: 4, succeeded: 1, skipped: 1, failed: 2,
  params: kind === 'duplicate_scan' ? null : {}, failures: [{ target: 'song:uuid', reason: 'quota exceeded' }], message: '一部失敗しました',
  started_at: '2026-10-04T00:00:00Z',
});
const scenarios = [
  { page: 'ReadingsPage', kind: 'readings_backfill', keys: [['readings-stats'], ['artists', 2], ['artist', 'one'], ['songs', 'popular'], ['song', 'one'], ['global-search', 'one']] },
  { page: 'MergeCandidatesPage', kind: 'duplicate_scan', keys: [['song-merge-candidates'], ['song', 'one', 'merge-candidates']] },
];

function pageFixture(scenario) {
  const client = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity } } });
  const states = [], refs = [], effects = [], queries = [], mutations = [], requests = [], toasts = [];
  let stateCursor = 0, refCursor = 0, currentTask = null;
  const react = {
    useState: (initial) => {
      const i = stateCursor++;
      if (!(i in states)) states[i] = initial;
      return [states[i], (next) => { states[i] = next; }];
    },
    useRef: (initial) => {
      const i = refCursor++;
      if (!(i in refs)) refs[i] = { current: initial };
      return refs[i];
    },
    useEffect: (effect) => effects.push(effect),
  };
  const reactQuery = {
    useQueryClient: () => client,
    useQuery: (options) => {
      queries.push(options);
      if (options.queryKey[0] === 'task-progress') return { data: currentTask, isError: false };
      if (options.queryKey[0] === 'readings-stats') return { data: { artists_total: 2, songs_total: 2, artists_needs_fix: 1, songs_needs_fix: 1 } };
      if (options.queryKey[0] === 'tasks') return { data: currentTask ? [currentTask] : [], isError: false };
      if (scenario.page === 'SyncPage') return { data: undefined, isLoading: false };
      return { data: { candidates: [] }, isLoading: false };
    },
    useMutation: (options) => { mutations.push(options); return { isPending: false, mutate() {} }; },
  };
  const api = {
    taskApi: {
      get: async (id) => { requests.push(['get', id]); return currentTask; },
      list: async (limit) => { requests.push(['list', limit]); return [task('chapter_backfill', 'running'), currentTask].filter(Boolean); },
    },
    artistApi: { backfillReadings: async () => ({ task_id: 'task-one', message: '読み補完を開始しました' }) },
    songApi: { scanDuplicates: async () => ({ task_id: 'task-one', message: '走査を開始しました' }) },
    readingApi: {}, itunesApi: {}, holodexApi: {}, batchAnalyzeApi: {}, batchFillApi: {},
    channelApi: {}, autoFillApi: {}, nonSingingApi: {}, restrictionReviewApi: {}, streamApi: {},
  };
  const results = loadTS('../src/utils/taskResults.ts');
  const hook = loadTS('../src/hooks/useTaskProgress.ts', { react, '@tanstack/react-query': reactQuery, '../api/client': api, '../utils/taskResults': results });
  const progress = loadTS('../src/components/TaskProgress.tsx', {
    'react-router-dom': { Link: ({ to, children, ...props }) => createElement('a', { ...props, href: to }, children) },
    '../utils/taskResults': results,
  });
  const page = loadTS(`../src/pages/admin/${scenario.page}.tsx`, {
    react, '@tanstack/react-query': reactQuery,
    '../../api/client': api,
    '../../queryClient': { sameViewer: () => true, viewerID: () => 'fixture' },
    '../../hooks/useViewerState': { useViewerState: react.useState },
    '../../utils/taskResults': results,
    '../../store/auth': { useAuthStore: (select) => select({ user: { permissions: ['content:edit'] }, status: 'authenticated' }), hasPermission: (user, permission) => user.permissions.includes(permission), PERM: { CONTENT_EDIT: 'content:edit' } },
    '../../components/usePerformanceTiming': { formatSeconds: String },
    '../../hooks/useTaskProgress': hook,
    '../../components/TaskProgress': progress,
    '../../utils/matchReason': loadTS('../src/utils/matchReason.ts'),
    '../../components/ui/Loading': { default: () => null },
    '../../components/ui/ToastContext': { useToast: () => ({ showToast: (...args) => toasts.push(args) }) },
  });
  return {
    client, queries, mutations, requests, toasts,
    setTask(value) { currentTask = value; },
    render() {
      stateCursor = refCursor = 0;
      queries.length = mutations.length = effects.length = 0;
      const markup = renderToStaticMarkup(page.default());
      for (const effect of effects) effect();
      return markup;
    },
  };
}

for (const scenario of scenarios) {
  test(`${scenario.page}: start uses task_id, follows progress, and provides history link`, async () => {
    const f = pageFixture(scenario);
    try {
      for (const key of scenario.keys) f.client.setQueryData(key, { total: 1 });
      f.render();
      const mutation = f.mutations[0];
      mutation.onSuccess(await mutation.mutationFn());
      // 開始は完了ではない。ここで成功結果や一覧更新を捏造しない。
      for (const key of scenario.keys) assert.equal(f.client.getQueryState(key).isInvalidated, false);
      assert.equal(f.toasts[0][1], 'info');
      f.setTask(task(scenario.kind, 'running'));
      const markup = f.render();
      const query = f.queries.find((q) => q.queryKey[0] === 'task-progress');
      assert.deepEqual(jsonCopy(query.queryKey), ['task-progress', scenario.kind, 'task-one']);
      await query.queryFn();
      assert.deepEqual(f.requests, [['get', 'task-one']]);
      assert.equal(query.refetchInterval({ state: { data: null } }), 2000);
      assert.equal(query.refetchInterval({ state: { data: task(scenario.kind, 'running') } }), 2000);
      assert.equal(query.refetchInterval({ state: { data: task(scenario.kind, 'done') } }), false);
      assert.match(markup, /href="\/admin\/sync#background-tasks"/);
      assert.match(markup, /実行中/);
      assert.match(markup, /4\/5（成功 1・見送り 1・失敗 2）/);
      assert.match(markup, /quota exceeded/);
      const button = [...markup.matchAll(/<button\b[^>]*>[^<]*<\/button>/g)].map((m) => m[0]).find((s) => s.includes(scenario.kind === 'readings_backfill' ? 'AI補完中' : '走査中'));
      assert.ok(button);
      assert.match(button, / disabled=""/);
    } finally { f.client.clear(); }
  });

  for (const status of ['done', 'failed', 'interrupted', 'cancelled']) {
    test(`${scenario.page}: ${status} invalidates results including inactive cached details`, async () => {
      const f = pageFixture(scenario);
      try {
        for (const key of [...scenario.keys, ['tasks', true], ['settings']]) f.client.setQueryData(key, { total: 1 });
        f.setTask(task(scenario.kind, 'running'));
        f.render();
        for (const key of scenario.keys) assert.equal(f.client.getQueryState(key).isInvalidated, false);
        f.setTask(task(scenario.kind, status));
        f.render();
        await new Promise((resolve) => setImmediate(resolve));
        for (const key of [...scenario.keys, ['tasks', true]]) assert.equal(f.client.getQueryState(key).isInvalidated, true, JSON.stringify(key));
        assert.equal(f.client.getQueryState(['settings']).isInvalidated, false);
      } finally { f.client.clear(); }
    });
  }

  test(`${scenario.page}: revisit resumes a running task of the same kind`, async () => {
    const f = pageFixture(scenario);
    try {
      f.setTask(task(scenario.kind, 'running'));
      const markup = f.render();
      const query = f.queries.find((q) => q.queryKey[0] === 'task-progress');
      assert.equal((await query.queryFn()).kind, scenario.kind);
      assert.deepEqual(f.requests, [['list', 100]]);
      assert.equal(query.refetchInterval({ state: { data: task(scenario.kind, 'running') } }), 2000);
      assert.match(markup, /実行中/);
    } finally { f.client.clear(); }
  });
}

test('API clients keep the original endpoints and return task IDs without synchronously interpreting counts', async () => {
  const requests = [];
  const response = { task_id: 'task-one', message: '開始しました' };
  const fakeApi = {
    interceptors: { request: { use() {} }, response: { use() {} } },
    post: async (path) => { requests.push(['POST', path]); return { data: response }; },
    get: async (path) => { requests.push(['GET', path]); return { data: task('duplicate_scan', 'done') }; },
  };
  const client = loadTS('../src/api/client.ts', { axios: { default: { create: () => fakeApi } }, '../utils/apiError': { apiErrorMessage() {} }, '../queryClient': { sameViewer: () => true, viewerID: () => 'fixture' } });
  assert.equal((await client.artistApi.backfillReadings()).task_id, 'task-one');
  assert.equal((await client.songApi.scanDuplicates()).task_id, 'task-one');
  assert.equal((await client.taskApi.get('task-one')).id, 'task-one');
  assert.deepEqual(requests, [['POST', '/api/ai/backfill-readings'], ['POST', '/api/songs/merge-candidates/scan'], ['GET', '/api/tasks/task-one']]);
});

for (const scenario of scenarios) {
  test(`SyncPage: background history shows ${scenario.kind} and refreshes its results`, async () => {
    const f = pageFixture({ ...scenario, page: 'SyncPage' });
    try {
      for (const key of scenario.keys) f.client.setQueryData(key, { total: 1 });
      f.setTask(task(scenario.kind, 'running'));
      const markup = f.render();
      assert.match(markup, /id="background-tasks"/);
      assert.match(markup, scenario.kind === 'readings_backfill' ? /読み仮名の AI 補完/ : /重複候補の走査/);
      assert.match(markup, scenario.kind === 'readings_backfill' ? /曲名の読み/ : /AI の全件走査/);
      for (const key of scenario.keys) assert.equal(f.client.getQueryState(key).isInvalidated, false);
      f.setTask(task(scenario.kind, 'failed'));
      f.render();
      await new Promise((resolve) => setImmediate(resolve));
      for (const key of scenario.keys) assert.equal(f.client.getQueryState(key).isInvalidated, true, JSON.stringify(key));
    } finally { f.client.clear(); }
  });
}

// 実 QueryObserver が開始した取得を、開始画面と管理の履歴の終了 effect から取り直す。
const refreshScenarios = [...scenarios, ...scenarios.map((scenario) => ({ ...scenario, page: 'SyncPage' }))];
for (const scenario of refreshScenarios) for (const status of ['done', 'failed', 'interrupted', 'cancelled']) {
  for (const cached of [false, true]) test(`${scenario.page}/${scenario.kind}: ${status} refreshes pending results (cached=${cached})`, async () => {
    const f = pageFixture(scenario);
    const before = { revision: 'before' }, after = { revision: 'after' };
    const pendingQueries = scenario.keys.map((key) => {
      let release, calls = 0;
      const pending = new Promise((resolve) => { release = resolve; });
      if (cached) f.client.setQueryData(key, before);
      const observer = new QueryObserver(f.client, { queryKey: key, staleTime: 0,
        queryFn: () => ++calls === 1 ? pending : Promise.resolve(after) });
      const unsubscribe = observer.subscribe(() => {});
      return { key, release, unsubscribe, calls: () => calls };
    });
    try {
      for (const query of pendingQueries) assert.equal(query.calls(), 1);
      f.setTask(task(scenario.kind, 'running')); f.render();
      f.setTask(task(scenario.kind, status)); f.render();
      await new Promise((resolve) => setImmediate(resolve));
      for (const query of pendingQueries) query.release(before);
      await new Promise((resolve) => setImmediate(resolve));
      for (const query of pendingQueries) {
        assert.equal(query.calls(), 2, `${JSON.stringify(query.key)}: 古い初回の取得を再利用した`);
        assert.deepEqual(f.client.getQueryData(query.key), after, `${JSON.stringify(query.key)}: 完了前の結果が残った`);
      }
    } finally {
      for (const query of pendingQueries) { query.release(before); query.unsubscribe(); }
      f.client.clear();
    }
  });
}
