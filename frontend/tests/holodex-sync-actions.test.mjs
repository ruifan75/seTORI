import assert from 'node:assert/strict';
import { test } from 'node:test';
import { fileURLToPath } from 'node:url';
import { createServer } from 'vite';
import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';

// 操作タブが実際に使うコンポーネントを描画する。権限の helper だけの検査にはしない。
test('Holodex download and upload buttons use separate permissions', async () => {
  const server = await createServer({
    root: fileURLToPath(new URL('..', import.meta.url)),
    configFile: false,
    esbuild: { jsx: 'automatic' },
    server: { middlewareMode: true, ws: false },
    optimizeDeps: { noDiscovery: true, include: [] },
  });
  try {
    const { default: Actions } = await server.ssrLoadModule('/src/components/HolodexSyncActions.tsx');
    const { useAuthStore, PERM } = await server.ssrLoadModule('/src/store/auth.ts');
    assert.equal(PERM.HOLODEX_UPLOAD, 'holodex:upload');
    const cases = [
      { name: 'anonymous', permissions: null, labels: [] },
      { name: 'viewer', permissions: [], labels: [] },
      { name: 'content only', permissions: ['content:edit'], labels: [] },
      { name: 'default editor', permissions: ['content:edit', 'sync:run', 'logs:view'], labels: ['Holodex から同期'] },
      { name: 'sync only', permissions: ['sync:run'], labels: ['Holodex から同期'] },
      { name: 'upload only', permissions: ['holodex:upload'], labels: ['seTORI から Holodex へ同期'] },
      { name: 'upload editor', permissions: ['content:edit', 'holodex:upload'], labels: ['seTORI から Holodex へ同期'] },
      { name: 'both', permissions: ['sync:run', 'holodex:upload'], labels: ['Holodex から同期', 'seTORI から Holodex へ同期'] },
      { name: 'admin', permissions: ['*'], labels: ['Holodex から同期', 'seTORI から Holodex へ同期'] },
    ];
    for (const c of cases) {
      const user = c.permissions === null ? null : { permissions: c.permissions };
      useAuthStore.setState({ user });
      // SSR は Zustand の初期 snapshot を読むので、その fixture も揃える。
      useAuthStore.getInitialState().user = user;
      const props = { onDownload() {}, onUpload() {}, downloading: false, uploading: false };
      const markup = renderToStaticMarkup(createElement(Actions, props));
      const buttons = [...markup.matchAll(/<button\b([^>]*)>([^<]*)<\/button>/g)];
      assert.deepEqual(buttons.map((b) => b[2]), c.labels, c.name);
      for (const b of buttons) assert.doesNotMatch(b[1], /\sdisabled=""/, c.name);
    }
    useAuthStore.setState({ user: { permissions: ['*'] } });
    useAuthStore.getInitialState().user = useAuthStore.getState().user;
    const pending = renderToStaticMarkup(createElement(Actions, {
      onDownload() {}, onUpload() {}, downloading: true, uploading: true,
    }));
    const buttons = [...pending.matchAll(/<button\b([^>]*)>([^<]*)<\/button>/g)];
    assert.deepEqual(buttons.map((b) => b[2]), ['同期中...', 'Holodex へ同期中...']);
    for (const b of buttons) assert.match(b[1], /\sdisabled=""/);
  } finally {
    await server.close();
  }
});

// 管理ページ本体を描画し、カタログから追加されたキーと保存済みの権限を
// checkbox が反映することを確認する。保存 API は Go のロール更新テストで扱う。
test('role editor offers and retains the Holodex upload permission', async () => {
  const server = await createServer({
    root: fileURLToPath(new URL('..', import.meta.url)),
    configFile: false,
    esbuild: { jsx: 'automatic' },
    server: { middlewareMode: true, ws: false },
    optimizeDeps: { noDiscovery: true, include: [] },
  });
  const { QueryClient, QueryClientProvider } = await import('@tanstack/react-query');
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  try {
    const { default: UsersPage } = await server.ssrLoadModule('/src/pages/admin/UsersPage.tsx');
    const { ToastProvider } = await server.ssrLoadModule('/src/components/ui/Toast.tsx');
    client.setQueryData(['users'], []);
    client.setQueryData(['permissions'], [
      { key: '*', description: '全権限' },
      { key: 'sync:run', description: '読み取り同期' },
      { key: 'holodex:upload', description: 'Holodex への送信' },
    ]);
    for (const c of [
      { permissions: ['content:edit', 'sync:run'], checked: false, disabled: false },
      { permissions: ['holodex:upload'], checked: true, disabled: false },
      { permissions: ['sync:run', 'holodex:upload'], checked: true, disabled: false },
      { permissions: [], checked: false, disabled: false },
      { permissions: ['*'], checked: true, disabled: true },
    ]) {
      client.setQueryData(['roles'], [{
        id: 'fixture', name: 'fixture', description: '', is_system: false, permissions: c.permissions,
      }]);
      const markup = renderToStaticMarkup(createElement(QueryClientProvider, { client },
        createElement(ToastProvider, null, createElement(UsersPage))));
      const labels = [...markup.matchAll(/<label\b[^>]*>(.*?)<\/label>/gs)]
        .filter((label) => /<code\b[^>]*>holodex:upload<\/code>/.test(label[1]));
      assert.equal(labels.length, 1, JSON.stringify(c.permissions));
      const input = labels[0][1].match(/<input\b[^>]*>/)?.[0];
      assert.ok(input);
      assert.equal(/\schecked=""/.test(input), c.checked, JSON.stringify(c.permissions));
      assert.equal(/\sdisabled=""/.test(input), c.disabled, JSON.stringify(c.permissions));
    }
  } finally {
    client.clear();
    await server.close();
  }
});
