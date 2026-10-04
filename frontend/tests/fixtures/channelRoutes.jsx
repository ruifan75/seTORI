import { createElement, useEffect } from 'react';
import { createRoot } from 'react-dom/client';
import { MemoryRouter, Routes, Route, useLocation, useNavigate, useNavigationType } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import Layout from '/src/components/Layout';
import LegacyChannelRedirect from '/src/components/LegacyChannelRedirect';
import { ToastProvider } from '/src/components/ui/Toast';
import { useAuthStore } from '/src/store/auth';
import { usePlayerStore } from '/src/store/player';

const wait = () => new Promise((resolve) => setTimeout(resolve, 20));
function Probe() {
  const location = useLocation();
  const type = useNavigationType();
  return createElement('pre', { id: 'location' }, JSON.stringify({
    pathname: location.pathname, search: location.search, hash: location.hash, type,
  }));
}
function Controls() {
  const navigate = useNavigate();
  useEffect(() => { window.navigate = navigate; }, [navigate]);
  return null;
}

// Layout / PlayerBar / Navigate は実物。到着先のデータ取得と YT.Player は合成する。
export async function run(tc) {
  const frames = [], calls = [];
  window.YT = {
    PlayerState: { PLAYING: 1, PAUSED: 2, ENDED: 0 },
    Player: class {
      constructor(el, options) {
        calls.push('create');
        this.frame = document.createElement('iframe');
        this.frame.srcdoc = ''; this.frame.width = options.width; this.frame.height = options.height;
        el.replaceWith(this.frame); frames.push(this.frame);
        setTimeout(() => options.events.onReady({ target: this }), 0);
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
  useAuthStore.setState({ user: null, status: 'unauthenticated' });
  usePlayerStore.setState({ queue: [{ performanceId: 'performance', streamId: 'abcdefghijk',
    songId: 'song', songName: '曲', artist: '原曲アーティスト', singers: [{ id: 'channel', name: 'チャンネル' }],
    start: 60, end: 240 }], index: 0, playing: true });
  const queue = () => JSON.stringify({ queue: usePlayerStore.getState().queue, index: usePlayerStore.getState().index,
    playing: usePlayerStore.getState().playing });
  const initialQueue = queue();
  const host = document.createElement('div'); document.body.append(host);
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  createRoot(host).render(createElement(QueryClientProvider, { client },
    createElement(MemoryRouter, { initialEntries: ['/entry'] },
      createElement(ToastProvider, null, createElement(Controls),
        createElement(Routes, null, createElement(Route, { path: '/', element: createElement(Layout) },
          createElement(Route, { path: 'entry', element: createElement(Probe) }),
          ...tc.routes.map(({ path, element }) => createElement(Route, { key: path, path,
            element: createElement(element === 'LegacyChannelRedirect' ? LegacyChannelRedirect : Probe) }))))))));
  for (let i = 0; i < 100 && (!window.navigate || frames.length !== 1); i++) await wait();
  if (!window.navigate || frames.length !== 1) throw new Error('初期化失敗');
  await wait();
  const frame = frames[0];
  const initialCalls = JSON.stringify(calls);
  window.navigate(tc.old);
  let location;
  for (let i = 0; i < 100; i++) {
    const value = document.getElementById('location')?.textContent;
    if (value) { location = JSON.parse(value); if (location.pathname.startsWith('/channels')) break; }
    await wait();
  }
  const iframeSame = frames.length === 1 && frame.isConnected;
  const queueSame = initialQueue === queue();
  const playerCallsSame = initialCalls === JSON.stringify(calls);
  window.navigate(-1);
  for (let i = 0; i < 100 && !document.getElementById('location')?.textContent.includes('"/entry"'); i++) await wait();
  return { location, iframeSame, queueSame, playerCallsSame,
    back: JSON.parse(document.getElementById('location')?.textContent ?? '{}').pathname };
}
