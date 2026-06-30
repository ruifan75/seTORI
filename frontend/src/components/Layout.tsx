import { Link, Outlet, useLocation } from 'react-router-dom';

const navItems = [
  { path: '/', label: 'ホーム' },
  { path: '/songs', label: '楽曲一覧' },
  { path: '/streams', label: '歌枠一覧' },
  { path: '/singers', label: 'チャンネル一覧' },
];

const adminItems = [
  { path: '/admin/sync', label: '同期' },
  { path: '/admin/settings', label: '設定' },
  { path: '/admin/logs', label: 'ログ' },
];

export default function Layout() {
  const location = useLocation();
  const isStreamDetail = location.pathname.startsWith('/streams/');

  return (
    <div className="h-screen bg-gray-50 flex flex-col overflow-hidden">
      {/* Header */}
      <header className="bg-white shadow-sm border-b shrink-0">
        <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
          <div className="flex justify-between items-center h-16">
            {/* Logo */}
            <Link to="/" className="flex items-center gap-2">
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

            {/* Navigation */}
            <nav className="flex items-center gap-6">
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

              {/* Admin dropdown */}
              <div className="relative group">
                <button className="text-sm font-medium text-gray-600 hover:text-gray-900">
                  管理
                </button>
                <div className="absolute right-0 mt-2 w-40 bg-white rounded-md shadow-lg opacity-0 invisible group-hover:opacity-100 group-hover:visible transition-all duration-200 z-50">
                  <div className="py-1">
                    {adminItems.map((item) => (
                      <Link
                        key={item.path}
                        to={item.path}
                        className="block px-4 py-2 text-sm text-gray-700 hover:bg-gray-100"
                      >
                        {item.label}
                      </Link>
                    ))}
                  </div>
                </div>
              </div>
            </nav>
          </div>
        </div>
      </header>

      {/* Main content */}
      <main
        className={
          isStreamDetail
            ? 'flex-1 w-full max-w-none px-2 sm:px-4 lg:px-6 py-6 overflow-hidden min-h-0'
            : 'flex-1 max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-8 overflow-y-auto'
        }
      >
        <Outlet />
      </main>

      {/* Footer */}
      <footer className="bg-white border-t shrink-0">
        <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-4">
          <p className="text-center text-sm text-gray-500">
            seTORI - VTuber 歌枠セットリスト
          </p>
        </div>
      </footer>
    </div>
  );
}
