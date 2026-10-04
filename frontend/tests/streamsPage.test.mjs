import assert from 'node:assert/strict';
import { after, test } from 'node:test';
import { createServer } from 'vite';
import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { MemoryRouter } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { AxiosError } from 'axios';

// Vite で実際の TSX と依存を読み、取得済みの空データで画面を描く。HTTP 通信はしない。
const server = await createServer({ server: { middlewareMode: true, hmr: false, ws: false }, optimizeDeps: { noDiscovery: true, include: [] }, appType: 'custom' });
after(() => server.close());
const { default: StreamsPage } = await server.ssrLoadModule('/src/pages/StreamsPage.tsx');
function render(tags, { expectedTags = tags, error } = {}) {
  const client = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false, retryOnMount: false } } });
  const listKey = ['streams', 1, 'date', 'desc', expectedTags];
  if (error) {
    client.getQueryCache().build(client, { queryKey: listKey }).setState({
      status: 'error', fetchStatus: 'idle', error,
    });
  } else {
    client.setQueryData(listKey, {
      streams: [], pagination: { page: 1, limit: 20, total: 0, total_pages: 0 },
    });
  }
  client.setQueryData(['stream-tags'], [{ id: 'singing', display_name: '歌枠', color: '#E91E63' }]);
  client.setQueryData(['streams', 'tag-counts', expectedTags], {});
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

test('空白・重複を除いた API の条件でチップを選択し、同じ条件の結果を表示する', () => {
  const html = render([' singing ', 'singing', '', '  '], { expectedTags: ['singing'] });
  assert.match(html, /aria-pressed="true"/);
  assert.match(html, /条件に合う配信がありません/);
});

test('空の条件だけなら、絞り込みなしの結果を表示する', () => {
  const html = render(['', '  '], { expectedTags: [] });
  assert.doesNotMatch(html, /絞り込みを解除/);
  assert.match(html, /配信がありません/);
});

test('20 種を超える条件の API エラーを表示し、解除できる', () => {
  const error = new AxiosError('Request failed', 'ERR_BAD_REQUEST');
  error.response = { status: 400, data: { error: '配信タグは20個まで指定できます' } };
  const html = render(Array.from({ length: 21 }, (_, i) => `tag_${i}`), { error });
  assert.match(html, /role="alert"[^>]*>配信タグは20個まで指定できます<\/div>/);
  assert.match(html, /絞り込みを解除/);
  assert.doesNotMatch(html, /件の配信|条件に合う配信がありません/);
});

test('通信に失敗したら、空の一覧や件数として表示しない', () => {
  const html = render([], { error: new Error('network error') });
  assert.match(html, /role="alert"[^>]*>配信一覧を取得できませんでした<\/div>/);
  assert.doesNotMatch(html, /件の配信|配信がありません/);
});
