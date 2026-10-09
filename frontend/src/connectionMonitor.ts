import { onlineManager, type QueryClient } from '@tanstack/react-query';
import { connectionSnapshot, probeAPI, setBrowserOnline, subscribeConnection } from './api/connectivity';
import { isTransientFailure } from './queryPolicy';

// API の疎通確認後は、成功済み・非表示・4xx の query や mutation を実行しない。
export function refetchConnectionFailures(client: QueryClient) {
  return client.refetchQueries({ type: 'active', predicate: (query) =>
    query.state.status === 'error' && query.state.fetchStatus === 'idle' &&
    isTransientFailure(query.state.error),
  });
}

export function monitorConnection(client: QueryClient) {
  let timer: ReturnType<typeof setTimeout> | undefined;
  let probe: AbortController | undefined;
  let previous = connectionSnapshot();
  let disposed = false;
  const sync = () => {
    const next = connectionSnapshot();
    // ブラウザの online 復帰は React Query に任せ、二重に refetch しない。
    if (previous.apiUnavailable && !next.apiUnavailable && next.online) {
      void refetchConnectionFailures(client);
    }
    previous = next;
    if (timer) clearTimeout(timer);
    timer = undefined;
    if (next.online && next.apiUnavailable && !probe && !disposed) {
      timer = setTimeout(async () => {
        probe = new AbortController();
        await probeAPI(probe.signal);
        probe = undefined;
        if (!disposed) sync();
      }, 15000);
    }
  };
  const online = () => { setBrowserOnline(true); onlineManager.setOnline(true); };
  const offline = () => { setBrowserOnline(false); onlineManager.setOnline(false); };
  const unsubscribe = subscribeConnection(sync);
  window.addEventListener('online', online);
  window.addEventListener('offline', offline);
  if (navigator.onLine) online(); else offline();
  sync();
  return () => {
    disposed = true;
    unsubscribe();
    window.removeEventListener('online', online);
    window.removeEventListener('offline', offline);
    if (timer) clearTimeout(timer);
    probe?.abort();
  };
}
