import assert from 'node:assert/strict';
import { test } from 'node:test';
import { fileURLToPath } from 'node:url';
import { createServer } from 'vite';

// 実際の QueryClient にページング等のキーを登録し、共有の無効化関数を呼ぶ。
// ソースのキー一覧の字面だけでは、prefix 指定を変えたときの挙動は分からない。
test('channel visibility invalidates discovery counts and lists', async () => {
  const server = await createServer({
    root: fileURLToPath(new URL('..', import.meta.url)),
    configFile: false,
    server: { middlewareMode: true, ws: false },
    optimizeDeps: { noDiscovery: true, include: [] },
  });
  let queryClient;
  try {
    const module = await server.ssrLoadModule('/src/queryClient.ts');
    queryClient = module.queryClient;
    const keys = [
      ['singers', 'organization'], ['streams', 2], ['tag-streams', 'singing', 1],
      ['random-performances'], ['presets'], ['preset-items', 'x'],
      ['songs', 'popular'], ['songs', 2, 'search'],
      ['song', 'one'], ['song', 'one', 'performances', 2],
      ['tag-performances', 'acoustic', 2], ['artist', 'one', 2, 'name', 'asc'],
      ['global-search', 'one'],
    ];
    const unrelated = ['stream', 'one'];
    for (const key of [...keys, unrelated]) queryClient.setQueryData(key, { count: 1 });
    module.invalidateChannelScopedQueries();
    for (const key of keys) {
      assert.equal(queryClient.getQueryState(key)?.isInvalidated, true, JSON.stringify(key));
    }
    assert.equal(queryClient.getQueryState(unrelated)?.isInvalidated, false);
  } finally {
    queryClient?.clear();
    await server.close();
  }
});
