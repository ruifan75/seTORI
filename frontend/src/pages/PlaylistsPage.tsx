import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link } from 'react-router-dom';
import { playlistApi } from '../api/client';
import type { PlaylistVisibility } from '../api/types';
import { useAuthStore } from '../store/auth';
import Loading from '../components/ui/Loading';
import { useToast } from '../components/ui/ToastContext';
import VisibilityBadge from '../components/VisibilityBadge';

export default function PlaylistsPage() {
  const status = useAuthStore((s) => s.status);
  const isLoggedIn = status === 'authenticated';
  const { showToast } = useToast();
  const queryClient = useQueryClient();

  const [creating, setCreating] = useState(false);
  const [newName, setNewName] = useState('');
  const [newVisibility, setNewVisibility] = useState<PlaylistVisibility>('private');

  const mine = useQuery({
    queryKey: ['playlists', 'mine'],
    queryFn: playlistApi.listMine,
    enabled: isLoggedIn,
  });

  const publicLists = useQuery({
    queryKey: ['playlists', 'public'],
    queryFn: () => playlistApi.listPublic(),
  });

  const createMutation = useMutation({
    mutationFn: () => playlistApi.create({ name: newName, visibility: newVisibility }),
    onSuccess: (created) => {
      queryClient.invalidateQueries({ queryKey: ['playlists'] });
      setCreating(false);
      setNewName('');
      setNewVisibility('private');
      showToast(`「${created.name}」を作成しました`, 'success');
    },
    onError: (err: Error) => showToast(`作成に失敗しました: ${err.message}`, 'error'),
  });

  return (
    <div className="space-y-8">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h1 className="text-3xl font-bold text-gray-900">プレイリスト</h1>
        {isLoggedIn && !creating && (
          <button
            onClick={() => setCreating(true)}
            className="px-4 py-2 bg-indigo-600 text-white rounded-lg hover:bg-indigo-700 transition-colors"
          >
            新規作成
          </button>
        )}
      </div>

      {creating && (
        <form
          onSubmit={(e) => {
            e.preventDefault();
            if (newName.trim()) createMutation.mutate();
          }}
          className="bg-white border border-gray-200 rounded-lg p-4 space-y-3"
        >
          <div>
            <label className="block text-sm font-medium text-gray-700 mb-1">プレイリスト名</label>
            <input
              autoFocus
              value={newName}
              onChange={(e) => setNewName(e.target.value)}
              className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
              placeholder="例：お気に入りのバラード"
            />
          </div>
          <div>
            <label className="block text-sm font-medium text-gray-700 mb-1">公開範囲</label>
            <select
              value={newVisibility}
              onChange={(e) => setNewVisibility(e.target.value as PlaylistVisibility)}
              className="px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
            >
              <option value="private">非公開（自分だけ）</option>
              <option value="unlisted">限定公開（リンクを知っている人だけ）</option>
              <option value="public">公開（一覧に掲載）</option>
            </select>
          </div>
          <div className="flex gap-2">
            <button
              type="submit"
              disabled={!newName.trim() || createMutation.isPending}
              className="px-4 py-2 bg-indigo-600 text-white rounded-lg hover:bg-indigo-700 disabled:bg-gray-300 transition-colors"
            >
              {createMutation.isPending ? '作成中...' : '作成'}
            </button>
            <button
              type="button"
              onClick={() => { setCreating(false); setNewName(''); }}
              className="px-4 py-2 border border-gray-300 rounded-lg hover:bg-gray-50 transition-colors"
            >
              キャンセル
            </button>
          </div>
        </form>
      )}

      {/* 自分のプレイリスト */}
      <section className="space-y-3">
        <h2 className="text-xl font-semibold text-gray-900">マイプレイリスト</h2>
        {!isLoggedIn ? (
          <p className="text-gray-500">
            <Link to="/login" className="text-indigo-600 hover:underline">ログイン</Link>
            するとプレイリストを作成できます。
          </p>
        ) : mine.isLoading ? (
          <Loading />
        ) : (mine.data?.playlists.length ?? 0) === 0 ? (
          <p className="text-gray-500">まだプレイリストがありません。曲の「＋」ボタンから追加できます。</p>
        ) : (
          <ul className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {mine.data!.playlists.map((pl) => (
              <li key={pl.id}>
                <Link
                  to={`/playlists/${pl.id}`}
                  className="block bg-white border border-gray-200 rounded-lg p-4 hover:border-indigo-300 transition-colors"
                >
                  <div className="flex items-start justify-between gap-2">
                    <span className="font-medium text-gray-900 truncate">{pl.name}</span>
                    <VisibilityBadge visibility={pl.visibility} />
                  </div>
                  <span className="block mt-1 text-sm text-gray-500">{pl.item_count} 曲</span>
                </Link>
              </li>
            ))}
          </ul>
        )}
      </section>

      {/* 公開プレイリスト */}
      <section className="space-y-3">
        <h2 className="text-xl font-semibold text-gray-900">公開プレイリスト</h2>
        {publicLists.isLoading ? (
          <Loading />
        ) : (publicLists.data?.playlists.length ?? 0) === 0 ? (
          <p className="text-gray-500">公開されているプレイリストはまだありません。</p>
        ) : (
          <ul className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {publicLists.data!.playlists.map((pl) => (
              <li key={pl.id}>
                <Link
                  to={`/playlists/${pl.id}`}
                  className="block bg-white border border-gray-200 rounded-lg p-4 hover:border-indigo-300 transition-colors"
                >
                  <span className="block font-medium text-gray-900 truncate">{pl.name}</span>
                  <span className="block mt-1 text-sm text-gray-500">
                    {pl.owner_name} · {pl.item_count} 曲
                  </span>
                </Link>
              </li>
            ))}
          </ul>
        )}
      </section>
    </div>
  );
}
