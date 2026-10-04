import { createElement } from 'react';
import { createRoot } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import PlayerBar from '/src/components/PlayerBar';
import PerformanceFields from '/src/components/PerformanceFields';
import SettingsPage from '/src/pages/admin/SettingsPage';
import { ToastProvider } from '/src/components/ui/Toast';
import { useAuthStore } from '/src/store/auth';
import { usePlayerStore } from '/src/store/player';

const wait = (ms = 60) => new Promise((resolve) => setTimeout(resolve, ms));
const rect = (el) => {
  if (!el) throw new Error('測定対象が描画されていません');
  const r = el.getBoundingClientRect();
  return { x: r.x, y: r.y, w: r.width, h: r.height, bottom: r.bottom };
};
const query = (selector) => document.querySelector(selector);
const button = (title) => query(`button[title="${title}"]`);

// 実コンポーネントを描画する。外部 API・利用者のデータは使わない。
// YT.Player はローカル iframe に置換し、再生成と再生操作の呼び出しを記録する。
export async function run(tc) {
  const calls = [];
  window.YT = {
    PlayerState: { PLAYING: 1, PAUSED: 2, ENDED: 0 },
    Player: class {
      constructor(el, options) {
        calls.push('create');
        this.frame = document.createElement('iframe');
        this.frame.width = options.width;
        this.frame.height = options.height;
        this.frame.srcdoc = '';
        el.replaceWith(this.frame);
        setTimeout(() => options.events.onReady(), 0);
      }
      playVideo() { calls.push('play'); }
      pauseVideo() { calls.push('pause'); }
      seekTo(t) { calls.push(['seek', t]); }
      loadVideoById(t) { calls.push(['load', t]); }
      destroy() { calls.push('destroy'); this.frame.remove(); }
      getCurrentTime() { return 100; }
      getDuration() { return 3600; }
      getPlayerState() { return 1; }
      setVolume() {}
      mute() {}
      unMute() {}
    },
  };
  const client = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  const tracks = Array.from({ length: 20 }, (_, i) => ({
    performanceId: `performance-${i}`, streamId: 'abcdefghijk', songId: `song-${i}`,
    songName: `曲 ${i}`, artist: '原曲アーティスト', artists: [],
    singers: [{ id: 'singer', name: '歌手' }], streamTitle: '配信タイトル', start: 20, end: 300,
  }));
  client.setQueryData(['stream', 'abcdefghijk'], {
    id: 'abcdefghijk', title: '配信タイトル', duration: 3600, singers: [], tags: [],
    performances: tracks.map((t) => ({
      id: t.performanceId, stream_id: t.streamId, song_id: t.songId, song_name: t.songName,
      original_artist: t.artist, start_seconds: t.start, end_seconds: t.end, singers: [], tags: [],
    })),
  });
  for (const key of ['filter-keywords', 'stream-tags', 'performance-tags', 'tag-rules']) client.setQueryData([key], []);
  client.setQueryData(['version'], { commit: 'dev', built_at: '' });
  client.setQueryData(['ai-providers'], [{
    id: 1, name: 'Groq', model: 'llama-3.3-70b-versatile', enabled: true, timeout_seconds: 60,
    base_url: 'https://example.invalid/v1', has_key: false, key_hint: 'なし',
  }]);
  useAuthStore.setState({ user: { permissions: ['content:edit', 'ai:manage'] }, status: 'authenticated' });
  usePlayerStore.getState().playTracks(tracks);

  const fields = createElement(PerformanceFields, {
    value: { id: 'field', name: '曲', nameReading: '', artist: '原曲アーティスト', artistReading: '',
      start: 20, end: 300, tags: [], customTags: [], singerIds: [], matchedSongId: null,
      artUrl: null, itunesId: null, trackDuration: 280 },
    participants: [], performanceTags: [], onChange() {}, onSelectSong() {}, onTimeChange() {},
    onToggleTag() {}, onApplyEndSource() {},
  });
  const component = tc.mode === 'settings' ? createElement(SettingsPage) : tc.mode === 'fields' ? fields
    : createElement('div', { style: { height: '100vh', display: 'flex', flexDirection: 'column', overflow: 'hidden' } },
      createElement('main', { style: { flex: 1, minHeight: 0, overflowY: 'auto' } }, '背面ページ'), createElement(PlayerBar));
  const host = document.createElement('div');
  host.style.cssText = tc.mode === 'fields' ? 'width:calc(100vw - 102px);margin:24px' : tc.mode === 'settings' ? 'padding:24px' : '';
  document.body.append(host);
  const root = createRoot(host);
  root.render(createElement(QueryClientProvider, { client }, createElement(MemoryRouter, null,
    createElement(ToastProvider, null, component))));
  const readySelector = tc.mode === 'settings' ? 'input[title^="モデルを編集"]'
    : tc.mode === 'fields' ? 'button[title="終了時間から再生"]' : 'iframe';
  for (let i = 0; i < 80 && !query(readySelector); i++) await wait(20);
  if (!query(readySelector)) throw new Error(`fixture の描画が完了しません: ${host.innerHTML.slice(0, 300)} / ${JSON.stringify(calls)}`);
  await wait(80);

  if (tc.mode === 'settings') {
    const forms = [...document.querySelectorAll('input[placeholder="ID（例: singing）"]')].map((el) => {
      const form = el.closest('form');
      return { overflow: form.scrollWidth - form.clientWidth, direction: getComputedStyle(form).flexDirection,
        inputs: [...form.querySelectorAll('input')].map((input) => rect(input)) };
    });
    const model = query('input[title^="モデルを編集"]');
    return { forms, model: rect(model), modelMinWidth: getComputedStyle(model).minWidth,
      modelContainerMaxWidth: getComputedStyle(model.parentElement).maxWidth,
      pageOverflow: document.documentElement.scrollWidth - document.documentElement.clientWidth };
  }
  if (tc.mode === 'fields') return ['この時間から再生', '終了時間から再生'].map((title) => {
    const row = button(title).parentElement;
    return { title, overflow: row.scrollWidth - row.clientWidth, wrap: getComputedStyle(row).flexWrap };
  });

  const frame = query('iframe');
  if (!frame) throw new Error('YT fixture が起動しません');
  const before = JSON.stringify({ queue: usePlayerStore.getState().queue, index: usePlayerStore.getState().index,
    playing: usePlayerStore.getState().playing, calls });
  const popup = () => [...document.querySelectorAll('div')].find((el) =>
    el.className.includes('bottom-full') && el.className.includes('overflow-y-auto'));
  usePlayerStore.getState().setQueueOpen(true);
  await wait();
  const popupRect = { ...rect(popup()), maxHeight: getComputedStyle(popup()).maxHeight,
    lg: matchMedia('(min-width: 1024px)').matches };
  button('全画面表示').click();
  await wait(300);
  const info = query('[class*="lg:h-36"]');
  const queue = query('.flex-1.overscroll-contain.overflow-x-hidden');
  const expanded = { video: rect(frame), queue: rect(queue), info: rect(info),
    scrollHeight: info.scrollHeight, clientHeight: info.clientHeight, overflow: getComputedStyle(info).overflowY,
    overscroll: getComputedStyle(info).overscrollBehaviorY };

  function touch(target) {
    const point = (y) => new Touch({ identifier: 1, target, clientX: 100, clientY: y });
    target.dispatchEvent(new TouchEvent('touchstart', { touches: [point(100)], bubbles: true, cancelable: true }));
    const event = new TouchEvent('touchmove', { touches: [point(170)], bubbles: true, cancelable: true });
    target.dispatchEvent(event);
    const prevented = event.defaultPrevented;
    const transform = button('縮小してページに戻る（Esc）').parentElement.parentElement.style.transform;
    target.dispatchEvent(new TouchEvent('touchcancel', { changedTouches: [point(170)], bubbles: true, cancelable: true }));
    return { prevented, transform };
  }
  const scrollTouch = touch(info.querySelector('a'));
  const queueTouch = touch(queue.firstElementChild);
  const inputTouch = touch(info.querySelector('input'));
  const outsideTouch = touch(document.body);
  const hiddenInput = document.createElement('input');
  document.body.append(hiddenInput);
  const outsideInputTouch = touch(hiddenInput);
  hiddenInput.remove();
  usePlayerStore.getState().setEditing({ streamId: tracks[0].streamId, performanceId: tracks[0].performanceId });
  await wait(180);
  const report = { compact: !!query('button[aria-pressed]'), lg: matchMedia('(min-width: 1024px)').matches,
    video: rect(frame) };
  const resize = [];
  if (tc.resize) for (const width of [1023, 1024, 1023.5, 390, 1440]) {
    window.frameElement.style.width = `${width}px`;
    await new Promise(requestAnimationFrame);
    await new Promise(requestAnimationFrame);
    // matchMedia の change 通知と React の描画は非同期。壊れた境界は待っても揃わない。
    for (let i = 0; i < 30; i++) {
      await wait(20);
      if (!!query('button[aria-pressed]') === !matchMedia('(min-width: 1024px)').matches) break;
    }
    resize.push({ width, compact: !!query('button[aria-pressed]'), lg: matchMedia('(min-width: 1024px)').matches,
      sameFrame: query('iframe') === frame && frame.isConnected, video: rect(frame) });
  }
  const after = JSON.stringify({ queue: usePlayerStore.getState().queue, index: usePlayerStore.getState().index,
    playing: usePlayerStore.getState().playing, calls });
  return { popup: popupRect, expanded, scrollTouch, queueTouch, inputTouch, outsideTouch, outsideInputTouch,
    report, resize, sameFrame: query('iframe') === frame && frame.isConnected, unchanged: before === after, calls };
}
