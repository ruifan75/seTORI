import { Navigate, useLocation } from 'react-router-dom';
import { type ReactNode } from 'react';
import { useAuthStore, hasPermission } from '../store/auth';

// RequirePermission はログイン（と任意で特定権限）を要求するルートガード。
// 未ログインなら /login へ、権限不足ならアクセス拒否メッセージを表示する。
export default function RequirePermission({
  permission,
  children,
}: {
  permission?: string;
  children: ReactNode;
}) {
  const location = useLocation();
  const status = useAuthStore((s) => s.status);
  const user = useAuthStore((s) => s.user);

  if (status === 'loading') {
    return <div className="p-8 text-gray-400">読み込み中...</div>;
  }

  if (status !== 'authenticated') {
    return <Navigate to="/login" replace state={{ from: location.pathname }} />;
  }

  if (permission && !hasPermission(user, permission)) {
    return (
      <div className="p-8">
        <h1 className="text-xl font-bold text-gray-900 mb-2">アクセス権限がありません</h1>
        <p className="text-gray-500">このページを表示する権限がありません。管理者にお問い合わせください。</p>
      </div>
    );
  }

  return <>{children}</>;
}
