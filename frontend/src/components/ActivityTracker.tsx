import { useEffect } from 'react';
import { useLocation } from 'react-router-dom';
import { activityApi } from '../api/client';
import { useAuthStore } from '../store/auth';

// React StrictMode の開発時二重 mount と短時間の同一路遷移を二重計上しない。
let lastRecordKey = '';
let lastRecordAt = 0;

export default function ActivityTracker() {
  const location = useLocation();
  const status = useAuthStore((state) => state.status);
  const userID = useAuthStore((state) => state.user?.id);

  useEffect(() => {
    // 認証復元前に匿名として記録し、その直後に利用者として二重記録するのを避ける。
    if (status === 'loading') return;

    const actor = status === 'authenticated' ? userID ?? 'authenticated' : 'anonymous';
    const key = `${actor}:${location.pathname}`;
    const now = Date.now();
    if (key === lastRecordKey && now - lastRecordAt < 2000) return;
    lastRecordKey = key;
    lastRecordAt = now;

    // pathname だけを送り、query / hash（OAuth code・検索語など）は収集しない。
    void activityApi.recordVisit(location.pathname).catch(() => {
      // アクセス解析の障害で通常のページ表示を壊さない。
    });
  }, [location.pathname, status, userID]);

  return null;
}
