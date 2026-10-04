import assert from 'node:assert/strict';
import { test } from 'node:test';
import { fileURLToPath } from 'node:url';
import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { createServer } from 'vite';

test('compact は lg の補集合で、小数幅にも隙間を作らない', async () => {
  const server = await createServer({ root: fileURLToPath(new URL('..', import.meta.url)),
    configFile: false, server: { middlewareMode: true, ws: false },
    optimizeDeps: { noDiscovery: true, include: [] } });
  const previous = Object.getOwnPropertyDescriptor(globalThis, 'window');
  try {
    const { useIsCompact } = await server.ssrLoadModule('/src/components/useMediaQuery.ts');
    // CSS メディアクエリの結果は期待値として固定する。フックの実装からは生成しない。
    for (const c of [
      { width: 390, lg: false, max1023: true, compact: true },
      { width: 1023, lg: false, max1023: true, compact: true },
      { width: 1023.5, lg: false, max1023: false, compact: true },
      { width: 1024, lg: true, max1023: false, compact: false },
      { width: 1440, lg: true, max1023: false, compact: false },
    ]) {
      globalThis.window = { matchMedia(query) {
        const matches = { '(min-width: 1024px)': c.lg, '(max-width: 1023px)': c.max1023 }[query];
        assert.equal(typeof matches, 'boolean', `未定義の境界: ${query}`);
        return { matches };
      } };
      function Probe() { return useIsCompact() ? 'compact' : 'wide'; }
      assert.equal(renderToStaticMarkup(createElement(Probe)), c.compact ? 'compact' : 'wide', `${c.width}px`);
    }
  } finally {
    if (previous) Object.defineProperty(globalThis, 'window', previous);
    else delete globalThis.window;
    await server.close();
  }
});
