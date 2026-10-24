import assert from 'node:assert/strict';
import { test } from 'node:test';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import vm from 'node:vm';
import ts from 'typescript';
import { QueryClient, QueryObserver, MutationObserver } from '@tanstack/react-query';

const require = createRequire(import.meta.url);
const stopped = { running: false, total: 1, done: 0, skipped: 1, skipped_ids: ['batch123456'] };
const running = { running: true, total: 1, done: 0, skipped: 0 };

// 実ページが登録した status の取得関数を実行する。React hooks と API だけを差し替え、
// 履歴は実 QueryClient / QueryObserver を使って再取得から表示用のキャッシュまで通す。
function loadTs(path) {
  const module = { exports: {} };
  const file = new URL(path, import.meta.url);
  const code = ts.transpileModule(readFileSync(file, 'utf8'), {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
  }).outputText;
  vm.runInNewContext(code, { module, exports: module.exports, require }, { filename: file.pathname });
  return module.exports;
}

function statusQuery(client, fetchStatus, { mutations = [], toasts = [], cancelFill, cancelAnalyze } = {}) {
  const options = [];
  const module = { exports: {} };
  const dependencies = {
    '../../components/ui/QueryError': { default: () => null },
    react: { useState: (value) => [value, () => {}] },
    'react-router-dom': { Link: () => null },
    '@tanstack/react-query': {
      useQueryClient: () => client,
      useQuery: (value) => { options.push(value); return {}; },
      useMutation: (value) => { mutations.push(value); return {}; },
    },
    '../../api/client': { batchFillApi: { status: fetchStatus, cancel: cancelFill }, batchAnalyzeApi: { cancel: cancelAnalyze } },
    '../../components/ui/ToastContext': { useToast: () => ({ showToast(message, type) { toasts.push({ message, type }); } }) },
    '../../store/auth': { useAuthStore: () => null, hasPermission: () => false, PERM: {} },
    '../../components/usePerformanceTiming': { formatSeconds: () => '' },
    // 型だけの import は消えるので、実体の小さな util はそのまま変換して読む
    '../../utils/taskResults': loadTs('../src/utils/taskResults.ts'),
  };
  const file = new URL('../src/pages/admin/SyncPage.tsx', import.meta.url);
  const code = ts.transpileModule(readFileSync(file, 'utf8'), {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022, jsx: ts.JsxEmit.ReactJSX },
  }).outputText;
  vm.runInNewContext(code, { module, exports: module.exports, require: (name) => dependencies[name] ?? require(name) }, { filename: file.pathname });
  module.exports.default();
  return options.find(option => option.queryKey[0] === 'batch-fill-status');
}

function historyFixture() {
  const client = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  const before = { runs: [{ id: 'run', status: 'running', streams_total: 0, streams_done: 0, skipped_stream_ids: [] }] };
  const after = { runs: [{ id: 'run', status: 'done', streams_total: 1, streams_done: 0, skipped_stream_ids: ['batch123456'] }] };
  const key = ['batch-fill-runs'];
  client.setQueryData(key, before);
  client.setQueryData(['unrelated'], 'keep');
  let fetches = 0;
  const observer = new QueryObserver(client, { queryKey: key, queryFn: async () => { fetches++; return after; } });
  const unsubscribe = observer.subscribe(() => {});
  return { client, key, before, after, fetches: () => fetches, close: () => { unsubscribe(); client.clear(); } };
}

for (const label of ['実行中から完了', '最初のポーリング前に完了']) {
  test(`${label}したら、全件見送りの最終履歴を再取得する`, async () => {
    const f = historyFixture();
    try {
      const statuses = label === '実行中から完了' ? [running, stopped] : [stopped];
      const query = statusQuery(f.client, async () => statuses.shift());
      if (label === '実行中から完了') {
        assert.strictEqual(await query.queryFn(), running);
        assert.equal(f.fetches(), 0, '実行中は既存の履歴ポーリングに任せる');
      }
      assert.strictEqual(await query.queryFn(), stopped);
      await new Promise(resolve => setImmediate(resolve));
      assert.equal(f.fetches(), 1, '終了時に最後の履歴を取得する');
      assert.deepEqual(f.client.getQueryData(f.key), f.after, '見送り ID と件数を表示するキャッシュを更新する');
      assert.equal(f.client.getQueryState(['unrelated']).isInvalidated, false);
      assert.equal(query.refetchInterval({ state: { data: { running: false } } }), false);
    } finally { f.close(); }
  });
}

test('停止状態が続いても、次の短い実行が完了した履歴を取得する', async () => {
  const f = historyFixture();
  try {
    const query = statusQuery(f.client, async () => stopped);
    assert.strictEqual(await query.queryFn(), stopped);
    await new Promise(resolve => setImmediate(resolve));
    assert.equal(f.fetches(), 1);
    // 短い処理では running=true を一度も観測しない。同じ status の再取得でも履歴を更新する。
    f.client.setQueryData(f.key, f.before);
    assert.strictEqual(await query.queryFn(), stopped);
    await new Promise(resolve => setImmediate(resolve));
    assert.equal(f.fetches(), 2);
    assert.deepEqual(f.client.getQueryData(f.key), f.after);
  } finally { f.close(); }
});

test('status の取得失敗を完了と扱わず、取得済みの履歴を保つ', async () => {
  const f = historyFixture();
  try {
    const failure = new Error('status unavailable');
    const query = statusQuery(f.client, async () => { throw failure; });
    await assert.rejects(query.queryFn, error => error === failure);
    assert.equal(f.fetches(), 0);
    assert.deepEqual(f.client.getQueryData(f.key), f.before);
  } finally { f.close(); }
});

for (const action of ['cancelFill', 'cancelAnalyze']) {
  test(`${action}: 停止要求に失敗したら具体的な理由を表示する`, async () => {
    const client = new QueryClient();
    try {
      const failure = new Error('停止する権限がありません');
      const cancel = async () => { throw failure; };
      const mutations = [], toasts = [];
      statusQuery(client, async () => stopped, { mutations, toasts, [action]: cancel });
      const mutation = mutations.find(options => options.mutationFn === cancel);
      assert.ok(mutation, 'ページが実際に登録した停止処理を使う');
      const observer = new MutationObserver(client, mutation);
      await assert.rejects(observer.mutate(), error => error === failure);
      assert.deepEqual(toasts, [{ message: '停止できません: 停止する権限がありません', type: 'error' }]);
    } finally { client.clear(); }
  });
}

for (const cached of [false, true]) {
  test(`完了を取得した時点で古い履歴が取得中でも最終履歴を表示する（キャッシュ=${cached}）`, async () => {
    const client = new QueryClient({ defaultOptions: { queries: { staleTime: 0, retry: false } } });
    const key = ['batch-fill-runs'];
    const before = { runs: [{ id: 'run', status: 'running', skipped_stream_ids: [] }] };
    const after = { runs: [{ id: 'run', status: 'done', skipped_stream_ids: ['batch123456'] }] };
    if (cached) client.setQueryData(key, before);
    let release;
    const oldRequest = new Promise(resolve => { release = resolve; });
    let fetches = 0;
    const observer = new QueryObserver(client, { queryKey: key, queryFn: () => ++fetches === 1 ? oldRequest : Promise.resolve(after) });
    const unsubscribe = observer.subscribe(() => {});
    try {
      assert.equal(fetches, 1, 'ページに入ったときの履歴取得を先に開始する');
      const query = statusQuery(client, async () => stopped);
      assert.strictEqual(await query.queryFn(), stopped);
      // 最初の取得は完了前の DB を読んでいて、status より遅く届く。
      release(before);
      await new Promise(resolve => setImmediate(resolve));
      assert.equal(fetches, 2, '完了前の取得とは別に最終履歴を取り直す');
      assert.deepEqual(client.getQueryData(key), after, '遅い古い応答で完了後の履歴を上書きしない');
    } finally { release(before); unsubscribe(); client.clear(); }
  });
}
