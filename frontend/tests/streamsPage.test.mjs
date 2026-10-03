import assert from 'node:assert/strict';
import { after, test } from 'node:test';
import { createServer } from 'vite';
import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { MemoryRouter } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';

// Vite で実際の TSX と依存を読み、取得済みの空データで画面を描く。HTTP 通信はしない。
const server = await createServer({ server: { middlewareMode: true, hmr: false, ws: false }, optimizeDeps: { noDiscovery: true, include: [] }, appType: 'custom' });
after(() => server.close());
const { default: StreamsPage } = await server.ssrLoadModule('/src/pages/StreamsPage.tsx');
function render(tags) {
  const client = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  client.setQueryData(['streams', 1, 'date', 'desc', tags], {
    streams: [], pagination: { page: 1, limit: 20, total: 0, total_pages: 0 },
  });
  client.setQueryData(['stream-tags'], [{ id: 'singing', display_name: '歌枠', color: '#E91E63' }]);
  client.setQueryData(['streams', 'tag-counts', tags], {});
  const params = new URLSearchParams();
  for (const tag of tags) params.append('tag', tag);
  try {
    return renderToStaticMarkup(createElement(QueryClientProvider, { client },
      createElement(MemoryRouter, { initialEntries: [`/streams?${params}`] }, createElement(StreamsPage))));
  } finally { client.clear(); }
}

test('未知・削除済みのタグでも、結果 0 件から絞り込みを解除できる', () => {
  assert.match(render(['deleted-tag']), /<button[^>]*>絞り込みを解除<\/button>/);
});
test('既知のタグを選んで 0 件でも解除できる', () => {
  assert.match(render(['singing']), /aria-pressed="true"/);
  assert.match(render(['singing']), /絞り込みを解除/);
});
test('絞り込みがなければ解除ボタンは出さない', () => {
  assert.doesNotMatch(render([]), /絞り込みを解除/);
});
