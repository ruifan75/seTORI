import assert from 'node:assert/strict';
import { test } from 'node:test';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import vm from 'node:vm';
import ts from 'typescript';
import { QueryClient } from '@tanstack/react-query';

const require = createRequire(import.meta.url);
function load(relative, dependencies = {}, globals = {}) {
  const filename = new URL(`../${relative}`, import.meta.url);
  const source = readFileSync(filename, 'utf8').replaceAll('import.meta.env', '({})');
  const module = { exports: {} };
  const code = ts.transpileModule(source, { compilerOptions: {
    module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022, jsx: ts.JsxEmit.ReactJSX, esModuleInterop: true,
  }, fileName: filename.pathname }).outputText;
  vm.runInNewContext(code, { module, exports: module.exports, URLSearchParams,
    require: (name) => dependencies[name] ?? require(name), ...globals }, { filename: filename.pathname });
  return module.exports;
}
const plain = (value) => JSON.parse(JSON.stringify(value));

test('処理済み API は絞り込みと変更先・run ID を保持する', async () => {
  const axios = require('axios');
  const requests = [];
  const instance = axios.create({ adapter: async (config) => {
    requests.push({ method: config.method, url: config.url, body: config.data == null ? null : JSON.parse(config.data) });
    return { data: { runs: [], count: 2 }, status: 200, statusText: 'OK', headers: {}, config };
  } });
  const { processedReviewApi: api } = load('src/api/client.ts', { axios: { ...axios, create: () => instance } });
  await api.list({ is_processed: false, q: '歌枠', channel_id: 'guest', tags: ['singing', 'members_only'], hidden: 'true', has_performances: 'false', from: '2026-10-01T00:00:00Z', until: '2026-11-01T00:00:00Z' }, 100);
  await api.preview(['one', 'two'], false);
  await api.apply('run'); await api.revert('run'); await api.runs();
  assert.deepEqual(requests, [
    { method: 'get', url: '/api/processed-review?is_processed=false&offset=100&limit=100&q=%E6%AD%8C%E6%9E%A0&channel_id=guest&hidden=true&has_performances=false&from=2026-10-01T00%3A00%3A00Z&until=2026-11-01T00%3A00%3A00Z&tag=singing&tag=members_only', body: null },
    { method: 'post', url: '/api/processed-review/preview', body: { stream_ids: ['one', 'two'], is_processed: false } },
    { method: 'post', url: '/api/processed-review/runs/run/apply', body: null },
    { method: 'post', url: '/api/processed-review/runs/run/revert', body: null },
    { method: 'get', url: '/api/processed-review/runs', body: null },
  ]);
});

function elements(tree) {
  if (!tree || typeof tree !== 'object') return [];
  if (Array.isArray(tree)) return tree.flatMap(elements);
  return [tree, ...elements(tree.props?.children)];
}
function text(tree) {
  if (tree == null || typeof tree === 'boolean') return '';
  if (typeof tree !== 'object') return String(tree);
  if (Array.isArray(tree)) return tree.map(text).join('');
  return text(tree.props?.children);
}
function harness() {
  const state = [], mutations = [], queries = [], requests = [], toasts = [];
  let cursor = 0, mutationCursor = 0, listener, viewer = 'editor';
  const client = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity } } });
  const cache = load('src/utils/processedReviewCache.ts');
  const dates = load('src/utils/processedReviewDate.ts');
  const api = {
    list: () => {}, runs: () => {},
    preview: async (ids, after) => { requests.push(['preview', plain(ids), after]); return { run_id: 'run', count: ids.length, is_processed: after }; },
    apply: async (id) => { requests.push(['apply', id]); return { changed: 1 }; },
    revert: async (id) => { requests.push(['revert', id]); return { reverted: 1, skipped: 1 }; },
  };
  const context = {
    onViewerChange: (fn) => { listener = fn; return () => {}; },
    sameViewer: (key) => key === viewer,
    viewerID: () => viewer,
  };
  const window = { confirm: () => true };
  const page = load('src/pages/admin/ProcessedReviewPage.tsx', {
    react: { useState: (initial) => {
      const index = cursor++; if (!(index in state)) state[index] = initial;
      return [state[index], (value) => { state[index] = typeof value === 'function' ? value(state[index]) : value; }];
    }, useEffect: (fn) => { fn(); } },
    'react-router-dom': { Link: () => null },
    '@tanstack/react-query': { useQueryClient: () => client,
      useQuery: (opts) => {
        queries.push(opts);
        if (opts.queryKey[0] === 'processed-review') return { data: { total: 2, candidates: ['one', 'two'].map((id) => ({ id, title: id, stream_date: '2026-10-01', is_processed: !state[0].is_processed, is_hidden: true })) } };
        if (opts.queryKey[0] === 'processed-runs') return { data: [{ id: 'old', status: 'applied', item_count: 2, after_processed: false, created_at: '2026-10-01' }] };
        return {};
      },
      useMutation: (options) => { const index = mutationCursor++; mutations[index] = options; return { isPending: false, mutate: (args) => requests.push(['mutate', index, plain(args)]) }; },
    },
    '../../api/client': { processedReviewApi: api, channelApi: {}, tagApi: {} },
    '../../components/ui/ToastContext': { useToast: () => ({ showToast: (...args) => toasts.push(args) }) },
    '../../queryClient': context,
    '../../utils/processedReviewCache': cache,
    '../../utils/processedReviewDate': dates,
  }, { window });
  return { state, mutations, requests, toasts, queries, client, window,
    render: () => { cursor = 0; mutationCursor = 0; queries.length = 0; return page.default(); },
    changeViewer: () => { viewer = 'other'; listener(); },
  };
}
const button = (tree, label) => elements(tree).find((e) => e.type === 'button' && text(e) === label);

for (const after of [false, true]) test(`選択→確認→実行・取り消し（変更先 ${after}）`, async () => {
  const h = harness();
  try {
    let tree = h.render();
    assert.match(text(tree), /自動処理の停止条件/);
    assert.equal(button(tree, after ? '処理済みを付ける' : '処理済みを外す'), undefined, '確認前に実行ボタンを出さない');
    const target = elements(tree).find((e) => e.type === 'select' && e.props.value === 'true');
    target.props.onChange({ target: { value: String(after) } }); tree = h.render();
    elements(tree).find((e) => e.type === 'input' && e.props.type === 'checkbox').props.onChange({ target: { checked: true } });
    tree = h.render();
    button(tree, '選んだ1件の変更内容を確認').props.onClick();
    assert.deepEqual(h.requests, [['mutate', 0, { ids: ['one'], after, startedAs: 'editor' }]]);
    const args = h.requests.at(-1)[2];
    const preview = await h.mutations[0].mutationFn(args); h.mutations[0].onSuccess(preview, args); tree = h.render();
    const dialog = elements(tree).find((e) => e.props?.role === 'dialog');
    assert.match(text(dialog), after ? /1件の「未処理」を「処理済み」に/ : /1件の「処理済み」を「未処理」に/);
    assert.match(text(dialog), after ? /今後の自動処理の対象から外れます/ : /解析・歌唱作成が再び/);
    assert.match(text(dialog), /処理済み状態がこの実行の変更後の値と同じ配信だけ/);
    assert.match(text(dialog), /同期・解析による他の項目の更新は妨げになりません/);
    assert.equal(elements(tree).find((e) => e.type === 'fieldset').props.disabled, true);
    assert.equal(button(tree, '取り消す').props.disabled, true, '確認中に別 run の撤回を割り込ませない');
    button(tree, after ? '処理済みを付ける' : '処理済みを外す').props.onClick();
    const applyArgs = h.requests.at(-1)[2];
    assert.deepEqual(applyArgs, { id: 'run', startedAs: 'editor' });
    await h.mutations[1].mutationFn(applyArgs); h.mutations[1].onSuccess({ changed: 1 }, applyArgs); tree = h.render();
    assert.equal(elements(tree).find((e) => e.props?.role === 'dialog'), undefined);
    assert.deepEqual(plain(h.state[2]), []);
    h.window.confirm = () => false; const before = h.requests.length;
    button(tree, '取り消す').props.onClick(); assert.equal(h.requests.length, before, '撤回も確認が必要');
    h.window.confirm = (message) => {
      assert.equal(message, '2件の処理済み変更を取り消します。処理済み状態が変更後の値と違う配信や、削除された配信は見送ります。自動処理の結果は戻しません。');
      return true;
    }; button(tree, '取り消す').props.onClick();
    const revertArgs = h.requests.at(-1)[2]; await h.mutations[2].mutationFn(revertArgs);
    h.mutations[2].onSuccess({ reverted: 1, skipped: 1 }, revertArgs);
    assert.deepEqual(h.requests.filter((r) => r[0] !== 'mutate'), [['preview', ['one'], after], ['apply', 'run'], ['revert', 'old']]);
    assert.deepEqual(h.toasts.at(-1), ['1件を戻しました。後の処理済み状態の変更・削除で見送ったもの: 1件', 'info']);
  } finally { h.client.clear(); }
});

for (const action of ['apply', 'revert']) test(`${action} が処理済みを使う詳細・一覧・残件を失効させる`, () => {
  const h = harness();
  const keys = [['processed-review', true, 100], ['processed-runs'], ['stream', 'one', true], ['streams', 2], ['stream-search', 'one'], ['singerStreams', 'owner', 2, 'false'], ['batch-fill-status'], ['batch-analyze-status'], ['autoFillSettings', true]];
  try {
    for (const key of [...keys, ['settings']]) h.client.setQueryData(key, { total: 1 });
    h.render(); h.mutations[action === 'apply' ? 1 : 2].onSuccess({ changed: 1, reverted: 1, skipped: 0 }, { startedAs: 'editor' });
    for (const key of keys) assert.equal(h.client.getQueryState(key).isInvalidated, true, JSON.stringify(key));
    assert.equal(h.client.getQueryState(['settings']).isInvalidated, false);
  } finally { h.client.clear(); }
});

test('利用者変更で選択・確認を捨て、遅れた成功・失敗を state/通知へ戻さない', () => {
  const h = harness();
  try {
    h.render(); h.state[2] = ['one']; h.state[3] = { run_id: 'run', count: 1, is_processed: true };
    h.changeViewer();
    assert.deepEqual(plain(h.state[2]), []); assert.equal(h.state[3], null);
    for (const options of h.mutations) {
      const before = JSON.stringify(h.state);
      options.onSuccess({ run_id: 'stale', count: 1, changed: 1, reverted: 1, skipped: 0 }, { startedAs: 'editor' });
      options.onError(new Error('stale'), { startedAs: 'editor' });
      assert.equal(JSON.stringify(h.state), before, '古い要求が破棄済みの確認や選択を変更した');
      assert.deepEqual(h.toasts, []);
    }
  } finally { h.client.clear(); }
});

test('確認失敗・実行失敗は通知し、実行失敗では選び直せる', () => {
  const h = harness();
  try {
    h.render(); h.state[3] = { run_id: 'run', count: 1, is_processed: true };
    h.mutations[0].onError(new Error('preview failed'), { startedAs: 'editor' });
    h.mutations[1].onError(new Error('conflict'), { startedAs: 'editor' });
    assert.equal(h.state[3], null);
    assert.deepEqual(h.toasts, [['preview failed', 'error'], ['conflict', 'error']]);
  } finally { h.client.clear(); }
});

test('終了日を含める日付境界はタイムゾーンと夏時間を保つ', () => {
  const prev = process.env.TZ;
  try {
    process.env.TZ = 'Asia/Taipei';
    const { reviewDateBoundary } = load('src/utils/processedReviewDate.ts');
    assert.equal(reviewDateBoundary('', true), '');
    assert.equal(reviewDateBoundary('2026-10-03', false), '2026-10-02T16:00:00.000Z');
    assert.equal(reviewDateBoundary('2026-10-03', true), '2026-10-03T16:00:00.000Z');
    process.env.TZ = 'America/New_York';
    assert.equal(reviewDateBoundary('2026-03-08', true), '2026-03-09T04:00:00.000Z');
  } finally { if (prev === undefined) delete process.env.TZ; else process.env.TZ = prev; }
});
