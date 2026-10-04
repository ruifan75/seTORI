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
function loadTS(url, dependencies) {
  const module = { exports: {} };
  const code = ts.transpileModule(readFileSync(url, 'utf8'), {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022, jsx: ts.JsxEmit.ReactJSX },
  }).outputText;
  vm.runInNewContext(code, {
    module, exports: module.exports,
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
