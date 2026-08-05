import { useEffect, useRef, useState } from 'react';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';
import { useAuthStore } from '../store/auth';

// OAuth コールバックの受け口。バックエンドから引き換えコード付きで戻ってくるので、
// それをセッショントークンに替えてログイン状態にする。
// 引き換えコードは1回限り・60秒で失効するため、URL に残っても長くは使えない。
export default function OAuthCallbackPage() {
  const [params] = useSearchParams();
  const navigate = useNavigate();
  const loginWithOAuthCode = useAuthStore((s) => s.loginWithOAuthCode);

  const [error, setError] = useState<string | null>(params.get('error'));
  // React 18 の StrictMode では effect が2回走る。引き換えは1回限りなので
  // 2回目で「無効なコード」エラーにならないよう、実行済みを覚えておく。
  const redeemed = useRef(false);

  useEffect(() => {
    const code = params.get('code');
    if (!code || redeemed.current) return;
    redeemed.current = true;

    loginWithOAuthCode(code)
      .then(() => navigate('/', { replace: true }))
      .catch((err: Error) => setError(err.message || 'ログインに失敗しました'));
  }, [params, loginWithOAuthCode, navigate]);

  if (error) {
    return (
      <div className="min-h-[60vh] flex items-center justify-center px-4">
        <div className="text-center space-y-4 max-w-sm">
          <p className="text-gray-900 font-medium">ログインできませんでした</p>
          <p className="text-sm text-gray-500">{error}</p>
          <Link
            to="/login"
            className="inline-block px-4 py-2 bg-indigo-600 text-white rounded-lg hover:bg-indigo-700 transition-colors"
          >
            ログイン画面へ戻る
          </Link>
        </div>
      </div>
    );
  }

  return (
    <div className="min-h-[60vh] flex items-center justify-center">
      <p className="text-gray-500">ログイン処理中...</p>
    </div>
  );
}
