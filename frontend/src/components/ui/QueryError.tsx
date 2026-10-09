import axios from 'axios';
import { useSyncExternalStore } from 'react';
import { connectionSnapshot, subscribeConnection } from '../../api/connectivity';

// 通信失敗を空データや404へ読み替えず、エラー詳細・設定値も表示しない。
export default function QueryError({ error, onRetry }: { error: unknown; onRetry: () => unknown }) {
  const { online } = useSyncExternalStore(subscribeConnection, connectionSnapshot, connectionSnapshot);
  const status = axios.isAxiosError(error) ? error.response?.status : undefined;
  const message = status === 401 ? 'ログインが必要です。' : status === 403
    ? 'この情報を表示する権限がありません。' : '取得に失敗しました。';
  return <div role="alert" className="my-3 rounded border border-red-200 bg-red-50 p-4 text-sm text-red-700">
    <p>{message}</p>
    <button type="button" disabled={!online} onClick={() => { void onRetry(); }} className="mt-2 underline disabled:opacity-50">再取得</button>
  </div>;
}
