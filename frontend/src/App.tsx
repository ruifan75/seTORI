import { BrowserRouter, Routes, Route } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { ToastProvider } from './components/ui/Toast';
import Layout from './components/Layout';
import HomePage from './pages/HomePage';
import SongsPage from './pages/SongsPage';
import SongDetailPage from './pages/SongDetailPage';
import StreamsPage from './pages/StreamsPage';
import StreamDetailPage from './pages/StreamDetailPage';
import SingersPage from './pages/SingersPage';
import SingerDetailPage from './pages/SingerDetailPage';
import SyncPage from './pages/admin/SyncPage';

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 1000 * 60 * 5, // 5 minutes
      retry: 1,
    },
  },
});

function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <ToastProvider>
        <BrowserRouter>
          <Routes>
            <Route path="/" element={<Layout />}>
              <Route index element={<HomePage />} />
              <Route path="songs" element={<SongsPage />} />
              <Route path="songs/:id" element={<SongDetailPage />} />
              <Route path="streams" element={<StreamsPage />} />
              <Route path="streams/:id" element={<StreamDetailPage />} />
              <Route path="singers" element={<SingersPage />} />
              <Route path="singers/:id" element={<SingerDetailPage />} />
              <Route path="admin/sync" element={<SyncPage />} />
            </Route>
          </Routes>
        </BrowserRouter>
      </ToastProvider>
    </QueryClientProvider>
  );
}

export default App;
