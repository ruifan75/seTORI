import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Link, Outlet, useLocation, useNavigate } from 'react-router-dom';
import { useAuthStore, hasPermission, PERM } from '../store/auth';
import { suggestionApi } from '../api/client';
import GlobalSearch from './GlobalSearch';
import PlayerBar from './PlayerBar';

const navItems = [
  { path: '/', label: 'ホーム' },
  { path: '/songs', label: '楽曲一覧' },
  { path: '/artists', label: 'アーティスト' },
  { path: '/streams', label: '歌枠一覧' },
  { path: '/singers', label: 'チャンネル一覧' },
];

const adminItems = [
  { path: '/admin/suggestions', label: '修正提案', permission: PERM.CONTENT_EDIT },
  { path: '/admin/sync', label: '同期', permission: PERM.SYNC_RUN },
  { path: '/admin/settings', label: '設定', permission: PERM.CONTENT_EDIT },
  { path: '/admin/logs', label: 'ログ', permission: PERM.LOGS_VIEW },
  { path: '/admin/users', label: 'ユーザー', permission: PERM.USERS_MANAGE },
];

export default function Layout() {
  const location = useLocation();
  const navigate = useNavigate();
  const isStreamDetail = location.pathname.startsWith('/streams/');

  const status = useAuthStore((s) => s.status);
  const user = useAuthStore((s) => s.user);
  const logout = useAuthStore((s) => s.logout);

  const visibleAdminItems = adminItems.filter((item) => hasPermission(user, item.permission));
  const canReviewSuggestions = hasPermission(user, PERM.CONTENT_EDIT);

  // 未処理の修正提案数（バッジ表示用）。権限があるときだけ取得し、定期更新する。
  const { data: pendingSuggestions } = useQuery({
    queryKey: ['suggestions', 'count'],
    queryFn: () => suggestionApi.count(),
    enabled: canReviewSuggestions,
    refetchInterval: 60000,
  });
  const pendingCount = pendingSuggestions ?? 0;

  // 開いた場所の location.key を保持することで、ページ遷移時は自動的に閉じる。
  const [menuLocationKey, setMenuLocationKey] = useState<string | null>(null);
  const [expandedSearchLocationKey, setExpandedSearchLocationKey] = useState<string | null>(null);
  const menuOpen = menuLocationKey === location.key;
  const desktopSearchExpanded = expandedSearchLocationKey === location.key;

  const handleLogout = async () => {
    await logout();
    navigate('/');
  };

  return (
    <div className="h-screen bg-gray-50 flex flex-col overflow-hidden">
      {/* Header */}
      <header className="relative z-30 bg-white shadow-sm border-b shrink-0">
        <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
          <div className="flex items-center h-16">
            {/* Logo */}
            <Link to="/" className="flex shrink-0 items-center gap-2">
              <span className="text-2xl font-bold text-indigo-600 inline-flex items-center">
                seT
                <span className="mx-0 inline-flex h-6 w-6 align-middle translate-y-[1px]">
                  <img
                    src="/icon.png"
                    alt="seTORI bird"
                    className="h-full w-full object-contain"
                  />
                </span>
                RI
              </span>
            </Link>

            {/* Desktop navigation */}
            <nav className="ml-6 hidden min-w-0 flex-1 items-center lg:flex">
              <GlobalSearch
                key={`desktop-search-${location.key}`}
                expandable
                onExpandedChange={(expanded) => setExpandedSearchLocationKey(expanded ? location.key : null)}
              />

              <div
                aria-hidden={desktopSearchExpanded}
                inert={desktopSearchExpanded ? true : undefined}
                className={`flex shrink-0 items-center gap-3 whitespace-nowrap transition-[max-width,margin,opacity] duration-300 ease-out motion-reduce:transition-none xl:gap-6 ${
                  desktopSearchExpanded
                    ? 'pointer-events-none ml-0 max-w-0 overflow-hidden opacity-0'
                    : 'ml-3 max-w-[64rem] opacity-100'
                }`}
              >
                {navItems.map((item) => (
                  <Link
                    key={item.path}
                    to={item.path}
                    className={`text-sm font-medium transition-colors ${
                      location.pathname === item.path
                        ? 'text-indigo-600'
                        : 'text-gray-600 hover:text-gray-900'
                    }`}
                  >
                    {item.label}
                  </Link>
                ))}

                {/* Admin dropdown（アクセス可能な項目がある場合のみ表示） */}
                {visibleAdminItems.length > 0 && (
                  <div className="relative group">
                    <button className="text-sm font-medium text-gray-600 hover:text-gray-900 inline-flex items-center gap-1">
                      管理
                      {pendingCount > 0 && (
                        <span className="inline-flex items-center justify-center min-w-[18px] h-[18px] px-1 rounded-full text-[10px] font-bold bg-red-500 text-white">
                          {pendingCount > 99 ? '99+' : pendingCount}
                        </span>
                      )}
                    </button>
                    <div className="absolute right-0 mt-2 w-40 bg-white rounded-md shadow-lg opacity-0 invisible group-hover:opacity-100 group-hover:visible transition-all duration-200 z-50">
                      <div className="py-1">
                        {visibleAdminItems.map((item) => (
                          <Link
                            key={item.path}
                            to={item.path}
                            className="flex items-center justify-between px-4 py-2 text-sm text-gray-700 hover:bg-gray-100"
                          >
                            <span>{item.label}</span>
                            {item.path === '/admin/suggestions' && pendingCount > 0 && (
                              <span className="inline-flex items-center justify-center min-w-[18px] h-[18px] px-1 rounded-full text-[10px] font-bold bg-red-500 text-white">
                                {pendingCount > 99 ? '99+' : pendingCount}
                              </span>
                            )}
                          </Link>
                        ))}
                      </div>
                    </div>
                  </div>
                )}

                {/* 認証エリア */}
                <div className="flex items-center gap-3 pl-3 border-l border-gray-200">
                  {status === 'authenticated' && user ? (
                    <>
                      <span className="text-sm text-gray-600" title={`ロール: ${user.role}`}>
                        {user.display_name || user.username}
                        <span className="ml-1 text-xs text-gray-400">({user.role})</span>
                      </span>
                      <button
                        onClick={handleLogout}
                        className="text-sm font-medium text-gray-500 hover:text-gray-800"
                      >
                        ログアウト
                      </button>
                    </>
                  ) : status === 'anonymous' ? (
                    <Link
                      to="/login"
                      className="text-sm font-medium px-3 py-1.5 rounded-lg bg-indigo-600 text-white hover:bg-indigo-700 transition-colors"
                    >
                      ログイン
                    </Link>
                  ) : null}
                </div>
              </div>
            </nav>

            {/* Mobile hamburger */}
            <button
              onClick={() => setMenuLocationKey((key) => key === location.key ? null : location.key)}
              className="ml-auto lg:hidden p-2 -mr-2 text-gray-600 hover:text-gray-900"
              aria-label="メニュー"
              aria-expanded={menuOpen}
            >
              {menuOpen ? (
                <svg className="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                </svg>
              ) : (
                <svg className="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 6h16M4 12h16M4 18h16" />
                </svg>
              )}
            </button>
          </div>
        </div>

        {/* Mobile menu panel */}
        {menuOpen && (
          <nav className="lg:hidden border-t bg-white">
            <div className="px-4 py-3 space-y-1">
              <div className="pb-2">
                <GlobalSearch />
              </div>
              {navItems.map((item) => (
                <Link
                  key={item.path}
                  to={item.path}
                  className={`block px-3 py-2 rounded-lg text-base font-medium ${
                    location.pathname === item.path
                      ? 'bg-indigo-50 text-indigo-600'
                      : 'text-gray-700 hover:bg-gray-50'
                  }`}
                >
                  {item.label}
                </Link>
              ))}

              {visibleAdminItems.length > 0 && (
                <>
                  <div className="pt-2 pb-1 px-3 text-xs font-medium text-gray-400 uppercase">管理</div>
                  {visibleAdminItems.map((item) => (
                    <Link
                      key={item.path}
                      to={item.path}
                      className={`flex items-center justify-between px-3 py-2 rounded-lg text-base font-medium ${
                        location.pathname === item.path
                          ? 'bg-indigo-50 text-indigo-600'
                          : 'text-gray-700 hover:bg-gray-50'
                      }`}
                    >
                      <span>{item.label}</span>
                      {item.path === '/admin/suggestions' && pendingCount > 0 && (
                        <span className="inline-flex items-center justify-center min-w-[18px] h-[18px] px-1 rounded-full text-[10px] font-bold bg-red-500 text-white">
                          {pendingCount > 99 ? '99+' : pendingCount}
                        </span>
                      )}
                    </Link>
                  ))}
                </>
              )}

              {/* 認証エリア */}
              <div className="border-t mt-2 pt-3 pb-1 px-3">
                {status === 'authenticated' && user ? (
                  <div className="flex items-center justify-between gap-3">
                    <span className="text-sm text-gray-600">
                      {user.display_name || user.username}
                      <span className="ml-1 text-xs text-gray-400">({user.role})</span>
                    </span>
                    <button
                      onClick={handleLogout}
                      className="text-sm font-medium text-gray-500 hover:text-gray-800"
                    >
                      ログアウト
                    </button>
                  </div>
                ) : status === 'anonymous' ? (
                  <div className="flex justify-end">
                    <Link
                      to="/login"
                      className="text-sm font-medium px-3 py-1.5 rounded-lg bg-indigo-600 text-white hover:bg-indigo-700 transition-colors"
                    >
                      ログイン
                    </Link>
                  </div>
                ) : null}
              </div>
            </div>
          </nav>
        )}
      </header>

      {/* Main content
          スクロールコンテナ（main）は常に全幅にし、幅制限（max-w-7xl）は内側の div で行う。
          main 自体に max-w を付けるとスクロールバーの出現時にコンテンツ幅が変わり、
          フィルタ切替などでレイアウトがガタつくため。scrollbar-gutter でバー分を常に確保する。 */}
      <main
        className={
          isStreamDetail
            ? 'flex-1 w-full max-w-none px-2 sm:px-4 lg:px-6 py-6 overflow-hidden min-h-0'
            : 'flex-1 overflow-y-auto [scrollbar-gutter:stable_both-edges]'
        }
      >
        {isStreamDetail ? (
          <Outlet />
        ) : (
          <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-6 sm:py-8">
            <Outlet />
          </div>
        )}
      </main>

      {/* グローバル再生バー（キューがあるときのみ表示、ページ遷移しても再生継続） */}
      <PlayerBar />
    </div>
  );
}
