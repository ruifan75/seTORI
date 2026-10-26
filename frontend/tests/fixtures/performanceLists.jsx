import { createElement, useEffect } from 'react';
import { createRoot } from 'react-dom/client';
import { MemoryRouter, Routes, Route, useLocation } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import ChannelDetailPage from '/src/pages/ChannelDetailPage';
import SongDetailPage from '/src/pages/SongDetailPage';
import PlaylistDetailPage from '/src/pages/PlaylistDetailPage';
import PresetPlaylistPage from '/src/pages/PresetPlaylistPage';
import TagPage from '/src/pages/TagPage';
import { ToastProvider } from '/src/components/ui/Toast';
import { useAuthStore } from '/src/store/auth';
import { usePlayerStore } from '/src/store/player';
import { channelApi, playlistApi, presetPlaylistApi } from '/src/api/client';

const wait = () => new Promise((resolve) => setTimeout(resolve, 25));
const cover = `data:image/svg+xml,${encodeURIComponent('<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 120 120"><defs><linearGradient id="g" x2="1" y2="1"><stop stop-color="#8b5cf6"/><stop offset="1" stop-color="#c4b5fd"/></linearGradient></defs><rect width="120" height="120" fill="url(#g)"/><circle cx="60" cy="60" r="36" fill="none" stroke="white" stroke-opacity=".5" stroke-width="2"/><circle cx="60" cy="60" r="6" fill="white"/></svg>')}`;

function LocationProbe() {
  const location = useLocation();
  useEffect(() => { window.fixtureLocation = location.pathname + location.search; }, [location]);
  return null;
}

// 実際の5ページを合成データで開く。ネットワーク・本番の資料・実際の書き込みは使わない。
export async function run(tc) {
  const editor = tc.role === 'editor';
  const authenticated = tc.role !== 'anonymous';
  useAuthStore.setState({ user: authenticated ? { id: 'viewer', permissions: editor ? ['content:edit'] : [], username: 'Viewer' } : null,
    status: authenticated ? 'authenticated' : 'anonymous' });
  usePlayerStore.setState({ queue: [], index: 0, playing: false, editing: null });
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, retryOnMount: false, staleTime: Infinity } } });
  const channel = { id: 'channel', name: 'デモチャンネル', english_name: 'Demo Channel', photo_url: cover,
    stream_count: 12, performance_count: 43, is_hidden: false, members_only_stream_count: 0, can_edit_metadata: false };
  const guest = { id: 'guest', name: 'ゲストシンガー', photo_url: cover };
  const song = { id: 'song', name: '夜明けのメロディー', original_artist: 'Demo Artist', name_reading: 'よあけのめろでぃー',
    original_artist_reading: 'でもあーてぃすと', artists: [{ id: 'artist', name: 'Demo Artist' }], arts: cover,
    performance_count: 43, itunes_ids: [] };
  const performances = [
    { id: 'p1', stream_id: 'stream-one', song_id: 'song', song_name: song.name, start_seconds: 65, end_seconds: 305,
      stream_title: '【歌枠】秋の夜に、ゆっくり歌います / Acoustic Night', singers: [channel], tags: [] },
    { id: 'p2', stream_id: 'stream-two', song_id: 'song', song_name: song.name, start_seconds: 3800, end_seconds: 4072,
      stream_title: '【コラボ歌枠】ふたりで歌う、とっておきのセットリスト', singers: [channel, guest],
      tags: [{ id: 'acoustic', display_name: 'Acoustic', color: '#7c3aed' }], custom_tags: ['コラボ'] },
    { id: 'p3', stream_id: 'stream-three', song_id: 'other-song', song_name: 'とても長い曲名のテスト — いつかまたこの場所で会える日まで',
      start_seconds: 900, end_seconds: 0, stream_title: '配信名が長い場合も一覧の操作を押せることを確かめるための配信タイトル', singers: [channel], tags: [] },
  ].map((p, index) => ({ original_artist: song.original_artist, artists: song.artists, arts: index === 2 ? undefined : cover,
    thumbnail_url: cover, stream_date: '2026-10-18T10:00:00Z', custom_tags: [],
    youtube_url: `https://www.youtube.com/watch?v=${p.stream_id}&t=${p.start_seconds}`, is_restricted: editor && index === 1, ...p }));
  const pagination = { page: 2, limit: 20, total: 43, total_pages: 3 };
  const calls = [];
  let playlistItems = [...performances];
  const playlist = { id: 'playlist', name: '夜に聴きたい歌', description: '好きな歌唱を、ひとつのプレイリストに。', owner_name: 'Demo Listener',
    is_owner: editor && tc.page !== 'shared', item_count: 3, visibility: 'public' };
  let preset = { key: 'collab', name: 'コラボ', description: 'ふたり以上で歌った曲を集めました。',
    item_count: tc.presetState === 'empty' ? 0 : 43, is_following: tc.following ?? false };
  // SongPerformance は arts を持たない。曲詳細は曲情報が親にあり、行の曲情報も欠け得る。
  const histories = performances.map(({ arts: _arts, ...p }) => p);
  client.setQueryData(['singer', 'channel', editor], channel);
  client.setQueryData(['organizations'], { organizations: [] });
  client.setQueryData(['singerPerformances', 'channel', 2, 'date', 'desc'], { singer: channel, performances: histories, pagination });
  channelApi.getPerformances = async (...args) => {
    calls.push(['sort', ...args]);
    return { singer: channel, performances: histories, pagination: { ...pagination, page: 1 } };
  };
  client.setQueryData(['song', 'song', 'performances', 2], { song,
    performances: histories.map(({ song_id: _id, song_name: _name, original_artist: _artist, artists: _artists, ...p }) => p), pagination });
  client.setQueryData(['song', 'song', 'merge-candidates'], { candidates: [] });
  client.setQueryData(['playlist', 'playlist', false], playlist);
  client.setQueryData(['playlist', 'playlist', 'items', false], { performances: playlistItems });
  client.setQueryData(['playlist', 'shared', true], playlist);
  client.setQueryData(['playlist', 'shared', 'items', true], { performances: playlistItems });
  playlistApi.items = async () => ({ performances: playlistItems });
  playlistApi.get = async () => playlist;
  playlistApi.listMine = async () => ({ playlists: [playlist] });
  playlistApi.reorder = async (id, ids) => {
    calls.push(['reorder', id, ids]);
    playlistItems = ids.map((key) => performances.find((p) => p.id === key));
  };
  playlistApi.removeItem = async (id, performanceId) => {
    calls.push(['remove', id, performanceId]);
    playlistItems = playlistItems.filter((p) => p.id !== performanceId);
  };
  client.setQueryData(['presets', 'detail', 'collab'], preset);
  presetPlaylistApi.get = async () => preset;
  presetPlaylistApi.follow = async (key) => {
    calls.push(['follow', key]);
    preset = { ...preset, is_following: true };
  };
  presetPlaylistApi.unfollow = async (key) => {
    calls.push(['unfollow', key]);
    preset = { ...preset, is_following: false };
  };
  if (tc.presetState === 'loading') presetPlaylistApi.items = () => new Promise(() => {});
  else if (tc.presetState === 'error') presetPlaylistApi.items = async () => { throw new Error('fixture items failure'); };
  else client.setQueryData(['preset-items', 'collab', 'all'], { performances: tc.presetState === 'empty' ? [] : performances });
  client.setQueryData(['performance-tags'], [{ id: 'acoustic', display_name: 'Acoustic', color: '#7c3aed' }]);
  client.setQueryData(['tag-performances', 'acoustic', 2], { performances, pagination });
  const urls = { channel: '/channels/channel?tab=performances&ppage=2', song: '/songs/song?page=2',
    playlist: '/playlists/playlist', shared: '/shared/playlists/shared', preset: '/playlists/preset/collab', tag: '/tags/performance/acoustic?page=2' };
  const host = document.createElement('main');
  host.style.cssText = 'max-width:1120px;margin:auto;padding:24px 16px';
  document.body.style.background = '#f9fafb';
  document.body.append(host);
  const root = createRoot(host);
  root.render(createElement(QueryClientProvider, { client }, createElement(MemoryRouter, { initialEntries: [urls[tc.page]] },
    createElement(ToastProvider, null, createElement(LocationProbe), createElement(Routes, null,
      createElement(Route, { path: '/channels/:id', element: createElement(ChannelDetailPage) }),
      createElement(Route, { path: '/songs/:id', element: createElement(SongDetailPage) }),
      createElement(Route, { path: '/playlists/:id', element: createElement(PlaylistDetailPage) }),
      createElement(Route, { path: '/shared/playlists/:slug', element: createElement(PlaylistDetailPage, { shared: true }) }),
      createElement(Route, { path: '/playlists/preset/:key', element: createElement(PresetPlaylistPage) }),
      createElement(Route, { path: '/tags/:kind/:id', element: createElement(TagPage) }),
      createElement(Route, { path: '*', element: createElement('p', null, 'リンク先') }))))));
  if (tc.presetState) {
    const followButton = () => document.querySelector('button[title^="フォロー"]');
    for (let i = 0; i < 100 && (!document.querySelector('h1') || (tc.presetState === 'error' && !document.querySelector('[role="alert"]'))); i++) await wait();
    const button = followButton();
    const before = { present: !!button, enabled: !!button && !button.disabled, title: button?.title };
    button?.click();
    for (let i = 0; i < 100 && button && followButton()?.title === before.title; i++) await wait();
    const result = { before, afterTitle: followButton()?.title, calls, text: host.textContent,
      hasList: !!document.querySelector('[aria-label="歌唱リスト"]'),
      pageOverflow: Math.max(0, document.documentElement.scrollWidth - innerWidth) };
    root.unmount(); client.clear();
    return result;
  }
  for (let i = 0; i < 100 && !document.querySelector('[aria-label="歌唱リスト"]'); i++) await wait();
  const list = document.querySelector('[aria-label="歌唱リスト"]');
  if (!list) throw new Error(`一覧が描画されません: ${document.body.innerText}`);
  await wait();
  const rows = [...list.children];
  const rect = (element) => {
    const r = element.getBoundingClientRect();
    return { x: r.x, y: r.y, width: r.width, height: r.height, right: r.right, bottom: r.bottom };
  };
  const snapshot = {
    pageOverflow: Math.max(0, document.documentElement.scrollWidth - innerWidth),
    rows: rows.map((row) => ({ rect: rect(row), overflow: Math.max(0, row.scrollWidth - row.clientWidth),
      text: row.textContent, links: [...row.querySelectorAll('a')].map((a) => a.getAttribute('href')),
      controls: [...row.querySelectorAll('button')].map((b) => rect(b)) })),
    toolbar: document.querySelector('[aria-label="歌唱リストの操作"]').textContent,
  };
  if (tc.preview) return snapshot;

  // 同曲別歌唱を選び、選んだ performance だけが現在行になることを確認。
  rows[1].querySelector('button').click(); await wait();
  const player = () => { const p = usePlayerStore.getState(); return { ids: p.queue.map((t) => t.performanceId), index: p.index, playing: p.playing, track: p.queue[p.index] }; };
  snapshot.played = player();
  snapshot.currentRows = rows.map((row) => row.getAttribute('aria-current'));
  rows[1].querySelector('button').click(); await wait();
  snapshot.paused = player();
  rows[1].querySelector('button').click(); await wait();
  snapshot.resumed = player();

  const more = (row) => row.querySelector('button[aria-controls], button[aria-label$="その他の操作"]');
  rows[1].scrollIntoView({ block: 'end' }); await wait();
  more(rows[1]).click(); await wait();
  const menu = document.querySelector('[role="group"][aria-label$="の操作"]');
  snapshot.menu = { rect: rect(menu), text: menu.textContent, youtube: menu.querySelector('a').getAttribute('href') };
  document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })); await wait();
  snapshot.closedWithFocus = !document.querySelector('[role="group"][aria-label$="の操作"]') && document.activeElement === more(rows[1]);
  more(rows[1]).click(); await wait();
  document.querySelector('[role="group"][aria-label$="の操作"] button').click(); await wait();
  snapshot.report = usePlayerStore.getState().editing;
  snapshot.reportTrack = player().track;
  usePlayerStore.setState({ editing: null });

  // ＋は再生列とは独立してキュー末尾へ追加する。
  rows[0].querySelector('button[aria-haspopup="menu"]').click(); await wait();
  [...document.querySelectorAll('button')].find((b) => b.textContent === '再生キューに追加').click(); await wait();
  snapshot.added = player();

  const toolbar = document.querySelector('[aria-label="歌唱リストの操作"]');
  toolbar.querySelector('button').click(); await wait();
  snapshot.playAll = player();
  toolbar.querySelector('button[aria-label$="ランダム再生"]').click(); await wait();
  snapshot.shuffled = player();

  if (tc.page === 'channel') {
    const select = toolbar.querySelector('select');
    snapshot.sortVisible = rect(select).width > 0;
    select.value = 'song'; select.dispatchEvent(new Event('change', { bubbles: true })); await wait(); await wait();
    snapshot.sortLocation = window.fixtureLocation;
  }
  if (tc.page === 'playlist' && editor) {
    more(rows[0]).click(); await wait();
    snapshot.firstMoveDisabled = [...document.querySelectorAll('button')].find((b) => b.textContent === '上へ移動').disabled;
    [...document.querySelectorAll('button')].find((b) => b.textContent === '下へ移動').click(); await wait(); await wait();
    const secondRow = document.querySelector('[aria-label="歌唱リスト"]').children[1];
    more(secondRow).click(); await wait();
    [...document.querySelectorAll('button')].find((b) => b.textContent === 'プレイリストから削除').click(); await wait(); await wait();
  }
  snapshot.calls = calls;
  // 各リンクの遷移先は上で測定。配信リンクを実際に押してもキューを変えない。
  const beforeLink = JSON.stringify(player());
  document.querySelector('[aria-label="歌唱リスト"] a[href^="/streams/"]').click(); await wait();
  snapshot.linkDidNotPlay = beforeLink === JSON.stringify(player());
  snapshot.linkLocation = window.fixtureLocation;
  root.unmount(); client.clear();
  return snapshot;
}
