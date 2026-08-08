import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useNavigate, useLocation, Link } from 'react-router-dom';
import { authApi, startOAuth } from '../api/client';
import { useAuthStore } from '../store/auth';

export default function LoginPage() {
  const navigate = useNavigate();
  const location = useLocation();
  const login = useAuthStore((s) => s.login);

  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  // 設定済みの連携先だけボタンを出す（未設定の provider を押させない）
  const { data: providers = [] } = useQuery({
    queryKey: ['oauth', 'providers'],
    queryFn: authApi.oauthProviders,
    staleTime: Infinity,
  });

  // ログイン前にアクセスしようとしていたページへ戻る（無ければトップ）
  const from = (location.state as { from?: string } | null)?.from ?? '/';

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    setSubmitting(true);
    try {
      await login(username.trim(), password);
      navigate(from, { replace: true });
    } catch (err) {
      setError(err instanceof Error ? err.message : 'ログインに失敗しました');
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="min-h-screen bg-gray-50 flex items-center justify-center px-4">
      <div className="w-full max-w-sm">
        <Link to="/" className="flex items-center justify-center gap-1 mb-8">
          <span className="text-3xl font-bold text-indigo-600 inline-flex items-center">
            seT
            <span className="mx-0 inline-flex h-7 w-7 align-middle translate-y-[1px]">
              <img src="/icon.png" alt="seTORI" className="h-full w-full object-contain" />
            </span>
            RI
          </span>
        </Link>

        <div className="bg-white rounded-xl shadow-sm border p-8">
          <h1 className="text-xl font-bold text-gray-900 mb-1">ログイン</h1>
          <p className="text-sm text-gray-500 mb-6">
            編集にはログインが必要です。閲覧はログインなしで可能です。
          </p>

          <form onSubmit={handleSubmit} className="space-y-4">
            <div>
              <label className="block text-sm font-medium text-gray-700 mb-1">ユーザー名</label>
              <input
                type="text"
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                autoFocus
                autoComplete="username"
                className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
              />
            </div>
            <div>
              <label className="block text-sm font-medium text-gray-700 mb-1">パスワード</label>
              <input
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                autoComplete="current-password"
                className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
              />
            </div>

            {error && (
              <div className="text-sm text-red-600 bg-red-50 border border-red-200 rounded-lg px-3 py-2">
                {error}
              </div>
            )}

            <button
              type="submit"
              disabled={submitting || !username.trim() || !password}
              className="w-full px-4 py-2 text-white font-medium rounded-lg bg-indigo-600 hover:bg-indigo-700 transition-colors disabled:opacity-50"
            >
              {submitting ? 'ログイン中...' : 'ログイン'}
            </button>
          </form>

          {providers.length > 0 && (
            <>
              <div className="flex items-center gap-3 my-5">
                <span className="flex-1 h-px bg-gray-200" />
                <span className="text-xs text-gray-400">または</span>
                <span className="flex-1 h-px bg-gray-200" />
              </div>
              <div className="space-y-2">
                {providers.includes('google') && (
                  <button
                    type="button"
                    onClick={() => {
                      // startOAuth は URL の取得に失敗し得るようになったので握り潰さない
                      // （黙って何も起きないと、押しても無反応にしか見えない）
                      setError(null);
                      startOAuth('google').catch((err) =>
                        setError(err instanceof Error ? err.message : 'Google への接続に失敗しました'),
                      );
                    }}
                    className="w-full flex items-center justify-center gap-2 px-4 py-2 border border-gray-300 rounded-lg hover:bg-gray-50 transition-colors"
                  >
                    <svg className="w-5 h-5" viewBox="0 0 24 24" aria-hidden="true">
                      <path fill="#4285F4" d="M23.06 12.25c0-.85-.08-1.67-.22-2.45H12v4.63h6.2a5.3 5.3 0 01-2.3 3.48v2.9h3.72c2.18-2 3.44-4.96 3.44-8.56z" />
                      <path fill="#34A853" d="M12 23.5c3.11 0 5.72-1.03 7.62-2.79l-3.72-2.89c-1.03.69-2.35 1.1-3.9 1.1-3 0-5.54-2.03-6.45-4.75H1.7v2.98A11.5 11.5 0 0012 23.5z" />
                      <path fill="#FBBC05" d="M5.55 14.17a6.9 6.9 0 010-4.34V6.85H1.7a11.5 11.5 0 000 10.3l3.85-2.98z" />
                      <path fill="#EA4335" d="M12 4.79c1.69 0 3.21.58 4.4 1.72l3.3-3.3C17.71 1.32 15.1.5 12 .5 7.52.5 3.64 3.07 1.7 6.85l3.85 2.98C6.46 7.11 9 4.79 12 4.79z" />
                    </svg>
                    Google でログイン
                  </button>
                )}
              </div>
              <p className="mt-3 text-xs text-gray-400 text-center">
                初めての場合はアカウントが自動で作成されます
              </p>
            </>
          )}
        </div>

        <p className="text-center mt-6">
          <Link to="/" className="text-sm text-gray-500 hover:text-gray-700">
            ← ログインせずに閲覧する
          </Link>
        </p>
      </div>
    </div>
  );
}
