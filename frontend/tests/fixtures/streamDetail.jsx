import { createElement } from 'react';
import { createRoot } from 'react-dom/client';
import { MemoryRouter, Routes, Route } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import StreamDetailPage from '/src/pages/StreamDetailPage';
import PlayerBar from '/src/components/PlayerBar';
import { ToastProvider } from '/src/components/ui/Toast';
import { useAuthStore } from '/src/store/auth';
import { usePlayerStore } from '/src/store/player';
import { streamApi } from '/src/api/client';

const wait = (ms = 50) => new Promise((resolve) => setTimeout(resolve, ms));
const button = (text) => [...document.querySelectorAll('main button')].find((el) => el.textContent.trim() === text);

// API・YouTube は合成データで置き換え、ページ、子コンポーネント、CSS は実物を使う。
export async function run(tc) {
  Date.now = () => 1788220800000;
  const calls = [];
  const frames = [];
  let ready = 0;
  window.YT = {
    PlayerState: { PLAYING: 1, PAUSED: 2, ENDED: 0 },
    Player: class {
      constructor(target, options) {
        calls.push(['create', options.videoId]);
        this.frame = document.createElement('iframe');
        this.frame.width = options.width; this.frame.height = options.height; this.frame.srcdoc = '';
        const el = typeof target === 'string' ? document.getElementById(target) : target;
        el.replaceWith(this.frame); frames.push(this.frame);
        setTimeout(() => { options.events.onReady({ target: this }); ready++; }, 0);
      }
      playVideo() { calls.push('play'); }
      pauseVideo() { calls.push('pause'); }
      seekTo(t) { calls.push(['seek', t]); }
      loadVideoById(t) { calls.push(['load', t]); }
      destroy() { calls.push('destroy'); this.frame.remove(); }
      getCurrentTime() { return 100; }
      getDuration() { return 3600; }
      getPlayerState() { return 1; }
      setVolume() {} mute() {} unMute() {}
    },
  };
  const permissions = tc.role === 'editor' ? ['content:edit']
    : tc.role === 'both' ? ['content:edit', 'restricted:view', 'holodex:upload']
      : tc.role === 'restricted' ? ['restricted:view'] : [];
  const canEdit = permissions.includes('content:edit');
  const canRead = !tc.restricted || permissions.includes('restricted:view');
  useAuthStore.setState({ user: tc.role === 'anonymous' ? null : { id: 'viewer', permissions },
    status: tc.role === 'anonymous' ? 'unauthenticated' : 'authenticated' });
  const photo = 'data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7';
  const singers = [{ id: 'channel', name: 'チャンネル主', photo_url: photo }, { id: 'vocalist', name: '歌った人', photo_url: photo }];
  const vocalists = [...singers, ...Array.from({ length: 4 }, (_, i) => ({ id: `guest-${i}`, name: `ゲスト ${i}`, english_name: `Guest ${i}`, photo_url: photo }))];
  const performances = canRead ? [0, 1].map((i) => ({
    id: `performance-${i}`, stream_id: 'abcdefghijk', song_id: `song-${i}`,
    song_name: tc.restricted ? `秘匿曲 ${i}` : `曲 ${i}`, original_artist: '原曲アーティスト',
    start_seconds: i * 240 + 60, end_seconds: i * 240 + 240, end_source: 'comment',
    singers: vocalists, tags: [{ id: 'acoustic', display_name: '弾き語り', color: '#123456' }], artists: [], custom_tags: ['合成タグ'],
  })) : [];
  const sourceSong = { name: '入力元の曲', original_artist: '原曲アーティスト',
    start_seconds: 60, end_seconds: 240, singer_ids: ['vocalist'], tags: ['acoustic'] };
  const commentSong = { name: 'コメントの曲', original_artist: '原曲アーティスト',
    start: 60, end: 240, tags: ['acoustic'], is_end_time_estimated: false };
  const stream = {
    id: 'abcdefghijk', title: '配信タイトル', stream_date: '2026-09-01T00:00:00Z', duration_seconds: 3600,
    singers, participants: singers, channel_owner: singers[0], performances,
    tags: tc.restricted ? [{ id: 'members_only', display_name: '会限', color: '#abcdef' }]
      : [{ id: 'singing', display_name: '歌枠', color: '#abcdef' }],
    is_restricted: tc.restricted, restriction_basis: tc.restricted ? 'unknown' : 'none',
    ...(canEdit ? { is_processed: false, is_hidden: false, chapter_count: 1,
      has_comment_raw: canRead, comment_songs_analyzed_at: '2026-09-01T00:00:00Z',
      holodex_timeline_songs: canRead ? [sourceSong] : [],
      comment_timeline_songs: canRead ? [commentSong] : [],
      chapter_timeline_songs: canRead ? [commentSong] : [] } : {}),
  };
  streamApi.get = async () => stream;
  const client = new QueryClient({ defaultOptions: { queries: {
    staleTime: Infinity, retry: false, refetchOnMount: false, refetchOnWindowFocus: false,
  } } });
  client.setQueryData(['stream', stream.id, canEdit], stream);
  client.setQueryData(['raw-comments', stream.id], { comments: [], total: 0 });
  client.setQueryData(['stream-tags'], [
    { id: 'singing', display_name: '歌枠', color: '#abcdef' },
    { id: 'members_only', display_name: '会限', color: '#abcdef' },
  ]);
  client.setQueryData(['performance-tags'], [{ id: 'acoustic', display_name: '弾き語り', color: '#123456' }]);
  const tracks = performances.map((p) => ({ performanceId: p.id, streamId: p.stream_id, songId: p.song_id,
    songName: p.song_name, artist: p.original_artist, singers: p.singers, start: p.start_seconds, end: p.end_seconds }));
  usePlayerStore.setState({ queue: tracks, index: 0, playing: true });
  const queue = () => JSON.stringify({ queue: usePlayerStore.getState().queue, index: usePlayerStore.getState().index });
  const initialQueue = queue();
  const host = document.createElement('div');
  host.style.cssText = 'height:100vh;display:flex;flex-direction:column'; document.body.append(host);
  const root = createRoot(host);
  root.render(createElement(QueryClientProvider, { client },
    createElement(MemoryRouter, { initialEntries: ['/streams/abcdefghijk'] },
      createElement(ToastProvider, null,
        createElement('main', { className: 'flex-1 w-full max-w-none px-2 sm:px-4 lg:px-6 py-6 min-h-0 overflow-y-auto overscroll-y-contain min-[1300px]:overflow-hidden' },
          createElement(Routes, null, createElement(Route, { path: '/streams/:id', element: createElement(StreamDetailPage) }))),
        createElement(PlayerBar)))));
  for (let i = 0; i < 100 && !document.querySelector('main h1'); i++) await wait(20);
  if (!document.querySelector('main h1')) throw new Error('配信詳細が描画されません');
  const expectedFrames = (tc.restricted ? 0 : 1) + (tracks.length ? 1 : 0);
  for (let i = 0; i < 100 && (frames.length !== expectedFrames || ready !== expectedFrames); i++) await wait(20);
  if (frames.length !== expectedFrames || ready !== expectedFrames) throw new Error('プレイヤーの準備が完了しません');
  await wait(80);
  const originalFrames = [...frames];
  function tree(node) {
    if (node.nodeType === Node.TEXT_NODE) return node.textContent;
    if (node.nodeType !== Node.ELEMENT_NODE) return null;
    return [node.tagName, [...node.attributes].map((a) => [a.name, a.value]).sort(),
      ...(['INPUT', 'SELECT', 'TEXTAREA'].includes(node.tagName) ? [{ value: node.value, checked: node.checked ?? null }] : []),
      [...node.childNodes].map(tree).filter((n) => n !== null)];
  }
  const snapshots = {};
  function snapshot(name) {
    snapshots[name] = JSON.stringify(tree(document.querySelector('main')));
  }
  snapshot('view');
  if (tc.w >= 1024 && canRead) {
    button('+3').click(); await wait(60); snapshot('vocalists');
    [...document.querySelectorAll('main h3')].find((el) => el.textContent === 'ボーカル一覧')
      .parentElement.querySelector('button').click(); await wait(60);
  }
  if (canEdit) {
    document.querySelector('button[title="タグを編集"]').click(); await wait(60); snapshot('tag-picker');
    document.querySelector('button[title="タグを編集"]').click();
    document.querySelector('button[title="参加チャンネルを編集"]').click(); await wait(60); snapshot('participants');
    document.querySelector('button[title="参加チャンネルを編集"]').click(); await wait(60);
    document.querySelector('button[title="セットリストを編集"]').click(); await wait(120);
    snapshot('edit-actions');
    for (const text of ['Holodex', 'コメント', 'チャプター', '手動', '生コメント', '操作']) {
      const el = button(text); if (!el) throw new Error(`タブがありません: ${text}`);
      el.click(); await wait(90); snapshot(text);
    }
    for (const width of [1023, 1024, 390, 1440]) {
      window.frameElement.style.width = `${width}px`;
      await new Promise(requestAnimationFrame); await wait(80);
    }
    button('キャンセル').click(); await wait(90); snapshot('cancel');
  }
  return { snapshots, iframeSame: frames.length === originalFrames.length && originalFrames.every((f) => f.isConnected),
    queueSame: initialQueue === queue(), calls, playing: usePlayerStore.getState().playing,
    texts: { title: document.querySelector('main h1').textContent, setlist: document.querySelector('main h2').textContent } };
}
