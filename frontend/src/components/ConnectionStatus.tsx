import { useEffect, useState, useSyncExternalStore } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { connectionSnapshot, probeAPI, subscribeConnection } from '../api/connectivity';
import { monitorConnection } from '../connectionMonitor';

export default function ConnectionStatus() {
  const client = useQueryClient();
  const { online, apiUnavailable } = useSyncExternalStore(subscribeConnection, connectionSnapshot, connectionSnapshot);
  const [checking, setChecking] = useState(false);
  useEffect(() => monitorConnection(client), [client]);
  if (online && !apiUnavailable) return null;
  return <div role="status" className="shrink-0 border-b border-amber-300 bg-amber-50 px-4 py-2 text-sm text-amber-900 flex flex-wrap items-center justify-center gap-2">
    <span>{online ? 'サーバーに接続できません。復帰を待っています。' : 'オフラインです。接続が戻ると表示中の情報を取り直します。'}</span>
    {online && <button type="button" disabled={checking} className="underline disabled:opacity-50" onClick={async () => {
      setChecking(true);
      try { await probeAPI(); } finally { setChecking(false); }
    }}>{checking ? '確認中…' : '接続を確認'}</button>}
  </div>;
}
