import { useEffect } from 'react';
import { BrowserRouter, Routes, Route } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { ToastProvider } from './components/ui/Toast';
import Layout from './components/Layout';
import RequirePermission from './components/RequirePermission';
import { useAuthStore, PERM } from './store/auth';
import HomePage from './pages/HomePage';
import LoginPage from './pages/LoginPage';
import OAuthCallbackPage from './pages/OAuthCallbackPage';
import SongsPage from './pages/SongsPage';
import SongDetailPage from './pages/SongDetailPage';
import StreamsPage from './pages/StreamsPage';
import StreamDetailPage from './pages/StreamDetailPage';
import SingersPage from './pages/SingersPage';
import SingerDetailPage from './pages/SingerDetailPage';
import TagPage from './pages/TagPage';
import ArtistsPage from './pages/ArtistsPage';
import ArtistDetailPage from './pages/ArtistDetailPage';
import SearchPage from './pages/SearchPage';
import PlaylistsPage from './pages/PlaylistsPage';
import MySuggestionsPage from './pages/MySuggestionsPage';
import MyAccountPage from './pages/MyAccountPage';
import PlaylistDetailPage from './pages/PlaylistDetailPage';
import PresetPlaylistPage from './pages/PresetPlaylistPage';
import SyncPage from './pages/admin/SyncPage';
import SettingsPage from './pages/admin/SettingsPage';
import LogsPage from './pages/admin/LogsPage';
import UsersPage from './pages/admin/UsersPage';
import SuggestionsPage from './pages/admin/SuggestionsPage';
import MergeCandidatesPage from './pages/admin/MergeCandidatesPage';
import MissingTagsPage from './pages/admin/MissingTagsPage';
import OrganizationsPage from './pages/admin/OrganizationsPage';
import BackupPage from './pages/admin/BackupPage';

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
            {/* OAuth の戻り先。Layout の外に置き、認証確立まで何も描かない */}
            <Route path="/login/oauth" element={<OAuthCallbackPage />} />
            <Route path="/" element={<Layout />}>
              <Route index element={<HomePage />} />
              <Route path="songs" element={<SongsPage />} />
              <Route path="songs/:id" element={<SongDetailPage />} />
              <Route path="streams" element={<StreamsPage />} />
              <Route path="streams/:id" element={<StreamDetailPage />} />
              <Route path="singers" element={<SingersPage />} />
              <Route path="singers/:id" element={<SingerDetailPage />} />
              <Route path="tags/:kind/:id" element={<TagPage />} />
              <Route path="artists" element={<ArtistsPage />} />
              <Route path="artists/:id" element={<ArtistDetailPage />} />
              <Route path="search" element={<SearchPage />} />
              <Route path="playlists" element={<PlaylistsPage />} />
              {/* プリセット（運営が用意した歌単）。:id より前に置かなくても静的な
                  preset セグメントが優先されるが、読む順として先に並べておく */}
              <Route path="playlists/preset/:key" element={<PresetPlaylistPage />} />
              <Route path="playlists/:id" element={<PlaylistDetailPage />} />
              {/* 限定公開の共有リンク（未ログインでも開ける） */}
              <Route path="shared/playlists/:slug" element={<PlaylistDetailPage shared />} />
              {/* 自分が出した提案（要ログイン。権限は不要） */}
              <Route
                path="my/suggestions"
                element={<RequirePermission><MySuggestionsPage /></RequirePermission>}
              />
              {/* 外部アカウント連携の追加・解除（要ログイン。権限は不要） */}
              <Route
                path="my/account"
                element={<RequirePermission><MyAccountPage /></RequirePermission>}
              />
              <Route
                path="admin/sync"
                element={<RequirePermission permission={PERM.SYNC_RUN}><SyncPage /></RequirePermission>}
              />
              <Route
                path="admin/settings"
                element={<RequirePermission permission={PERM.CONTENT_EDIT}><SettingsPage /></RequirePermission>}
              />
              <Route
                path="admin/suggestions"
                element={<RequirePermission permission={PERM.CONTENT_EDIT}><SuggestionsPage /></RequirePermission>}
              />
              <Route
                path="admin/merge-candidates"
                element={<RequirePermission permission={PERM.CONTENT_EDIT}><MergeCandidatesPage /></RequirePermission>}
              />
              <Route
                path="admin/missing-tags"
                element={<RequirePermission permission={PERM.CONTENT_EDIT}><MissingTagsPage /></RequirePermission>}
              />
              <Route
                path="admin/organizations"
                element={<RequirePermission permission={PERM.CONTENT_EDIT}><OrganizationsPage /></RequirePermission>}
              />
              <Route
                path="admin/logs"
                element={<RequirePermission permission={PERM.LOGS_VIEW}><LogsPage /></RequirePermission>}
              />
              <Route
                path="admin/users"
                element={<RequirePermission permission={PERM.USERS_MANAGE}><UsersPage /></RequirePermission>}
              />
              <Route
                path="admin/backups"
                element={<RequirePermission permission={PERM.BACKUP_MANAGE}><BackupPage /></RequirePermission>}
              />
            </Route>
          </Routes>
        </BrowserRouter>
      </ToastProvider>
    </QueryClientProvider>
  );
}

export default App;
