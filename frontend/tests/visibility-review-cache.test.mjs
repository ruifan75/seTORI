import assert from 'node:assert/strict';
import { test } from 'node:test';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import vm from 'node:vm';
import ts from 'typescript';
import { QueryClient } from '@tanstack/react-query';

const require = createRequire(import.meta.url);
// 実際の TS/TSX を実行し、ページが登録する成功コールバックから実 QueryClient を更新する。
// DOM と通信を使わないよう React の hooks と API だけを差し替える。
function loadTS(url, dependencies, globals = {}) {
  const module = { exports: {} };
  const code = ts.transpileModule(readFileSync(url, 'utf8'), {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022, jsx: ts.JsxEmit.ReactJSX },
  }).outputText;
  vm.runInNewContext(code, {
    module, exports: module.exports, ...globals,
    require: (name) => dependencies[name] ?? require(name),
  }, { filename: url.pathname });
  return module.exports;
}

for (const action of ['apply', 'revert']) {
  test(`${action} invalidates stream visibility across counts, lists and details`, () => {
    const client = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity } } });
    try {
      const keys = [
        ['visibility-review', false, 100], ['visibility-runs'], ['non-singing-candidates'],
        ['stream', 'one', true], ['streams', 2, 'date'], ['stream-search', 'one'],
        ['streams', 'tag-counts', ['singing']], ['stream-tags'], ['tag-streams', 'singing', 2],
        ['performance-tags'], ['tag-performances', 'piano', 2],
        ['singer', 'owner', true], ['singers', 'organization'], ['singer-search', 'owner'],
        ['singerStreams', 'owner', 2], ['singerPerformances', 'owner', 2],
        ['songs', 'popular'], ['song', 'one', 'performances', 2],
        ['artists', 2], ['artist', 'one', 2], ['global-search', 'one'],
        ['random-performances', 'home', 'infinite'], ['presets'], ['preset-items', 'one', 'all'],
      ];
      const unrelated = ['settings', 'integrations'];
      for (const key of [...keys, unrelated]) client.setQueryData(key, { total: 1 });
      const mutations = [];
      const cache = loadTS(new URL('../src/utils/streamVisibilityCache.ts', import.meta.url), {});
      const page = loadTS(new URL('../src/pages/admin/VisibilityReviewPage.tsx', import.meta.url), {
        react: { useState: (value) => [value, () => {}] },
        'react-router-dom': { Link: () => null },
        '@tanstack/react-query': {
          useQueryClient: () => client,
          useQuery: () => ({}),
          useMutation: (options) => { mutations.push(options); return {}; },
        },
        '../../api/client': { visibilityReviewApi: {}, nonSingingApi: {} },
        '../../components/ui/ToastContext': { useToast: () => ({ showToast: () => {} }) },
        '../../utils/streamVisibilityCache': cache,
      });
      page.default();
      mutations[action === 'apply' ? 1 : 2].onSuccess({ changed: 1, reverted: 1, skipped: 0 });
      for (const key of keys) assert.equal(client.getQueryState(key)?.isInvalidated, true, JSON.stringify(key));
      assert.equal(client.getQueryState(unrelated)?.isInvalidated, false);
    } finally {
      client.clear();
    }
  });
}

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

test('visibility review explains value-only undo in preview, confirmation and skipped notification', () => {
  const state = [false, 0, ['one'], { run_id: 'run', count: 1 }];
  let cursor = 0, queryCursor = 0;
  const mutations = [], calls = [], toasts = [], confirmations = [];
  const page = loadTS(new URL('../src/pages/admin/VisibilityReviewPage.tsx', import.meta.url), {
    react: { useState: () => [state[cursor++], () => {}] },
    'react-router-dom': { Link: () => null },
    '@tanstack/react-query': {
      useQueryClient: () => ({ invalidateQueries: () => {} }),
      useQuery: () => queryCursor++ === 0 ? { data: { candidates: [], total: 0 } } : { data: [{ id: 'old', item_count: 2, status: 'applied', created_at: '2026-10-10' }] },
      useMutation: (options) => { const i = mutations.push(options) - 1; return { mutate: (id) => calls.push([i, id]) }; },
    },
    '../../api/client': { visibilityReviewApi: {}, nonSingingApi: {} },
    '../../components/ui/ToastContext': { useToast: () => ({ showToast: (...args) => toasts.push(args) }) },
    '../../utils/streamVisibilityCache': { invalidateStreamVisibilityQueries: () => {} },
  }, { window: { confirm: (message) => { confirmations.push(message); return true; } } });
  const tree = page.default();
  const dialog = elements(tree).find((e) => e.props?.role === 'dialog');
  assert.match(text(dialog), /表示状態がこの実行の変更後の値と同じ配信だけ/);
  assert.match(text(dialog), /同期・解析による他の項目の更新は妨げになりません/);
  elements(tree).find((e) => e.type === 'button' && text(e) === '取り消す').props.onClick();
  assert.deepEqual(confirmations, ['2件の表示変更を取り消します。表示状態が変更後の値と違う配信や、削除された配信は見送ります。']);
  assert.deepEqual(calls, [[2, 'old']]);
  mutations[2].onSuccess({ reverted: 1, skipped: 1 });
  assert.deepEqual(toasts, [['1件を非表示に戻しました。後の表示状態の変更・削除で見送ったもの: 1件', 'info']]);
});
