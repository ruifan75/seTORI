import { useEffect } from 'react';
import { BrowserRouter, Routes, Route } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { ToastProvider } from './components/ui/Toast';
import Layout from './components/Layout';
import RequirePermission from './components/RequirePermission';
import { useAuthStore, PERM } from './store/auth';
import HomePage from './pages/HomePage';
import LoginPage from './pages/LoginPage';
import SongsPage from './pages/SongsPage';
import SongDetailPage from './pages/SongDetailPage';
import StreamsPage from './pages/StreamsPage';
import StreamDetailPage from './pages/StreamDetailPage';
import SingersPage from './pages/SingersPage';
import SingerDetailPage from './pages/SingerDetailPage';
import TagPage from './pages/TagPage';
import SyncPage from './pages/admin/SyncPage';
import SettingsPage from './pages/admin/SettingsPage';
import LogsPage from './pages/admin/LogsPage';
import UsersPage from './pages/admin/UsersPage';

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 1000 * 60 * 5, // 5 minutes
      retry: 1,
    },
  },
});

function App() {
  // 起動時に保存済みトークンからログイン状態を復元
  const init = useAuthStore((s) => s.init);
  useEffect(() => {
    init();
  }, [init]);

  return (
    <QueryClientProvider client={queryClient}>
      <ToastProvider>
        <BrowserRouter>
          <Routes>
            <Route path="/login" element={<LoginPage />} />
            <Route path="/" element={<Layout />}>
              <Route index element={<HomePage />} />
              <Route path="songs" element={<SongsPage />} />
              <Route path="songs/:id" element={<SongDetailPage />} />
              <Route path="streams" element={<StreamsPage />} />
              <Route path="streams/:id" element={<StreamDetailPage />} />
              <Route path="singers" element={<SingersPage />} />
              <Route path="singers/:id" element={<SingerDetailPage />} />
              <Route path="tags/:kind/:id" element={<TagPage />} />
              <Route
                path="admin/sync"
                element={<RequirePermission permission={PERM.SYNC_RUN}><SyncPage /></RequirePermission>}
              />
              <Route
                path="admin/settings"
                element={<RequirePermission permission={PERM.CONTENT_EDIT}><SettingsPage /></RequirePermission>}
              />
              <Route
                path="admin/logs"
                element={<RequirePermission permission={PERM.LOGS_VIEW}><LogsPage /></RequirePermission>}
              />
              <Route
                path="admin/users"
                element={<RequirePermission permission={PERM.USERS_MANAGE}><UsersPage /></RequirePermission>}
              />
            </Route>
          </Routes>
        </BrowserRouter>
      </ToastProvider>
    </QueryClientProvider>
  );
}

export default App;
