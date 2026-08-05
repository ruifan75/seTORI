import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { playlistApi } from '../api/client';
import type { Performance, PlaylistVisibility } from '../api/types';
import { usePlayerStore, performancesToTracks } from '../store/player';
import Loading from '../components/ui/Loading';
import QueueAddButton from '../components/QueueAddButton';
import ArtistLinks from '../components/ArtistLinks';
import VisibilityBadge from '../components/VisibilityBadge';
import { useToast } from '../components/ui/ToastContext';

function formatTime(seconds: number): string {
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  const s = seconds % 60;
  return h > 0
    ? `${h}:${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`
    : `${m}:${String(s).padStart(2, '0')}`;
}

// プレイリスト詳細。/playlists/:id と、共有リンクの /shared/playlists/:slug の両方で使う。
// 共有リンク経由は閲覧専用（編集操作は所有者判定で隠れる）。
export default function PlaylistDetailPage({ shared = false }: { shared?: boolean }) {
  const { id, slug } = useParams<{ id?: string; slug?: string }>();
  const key = shared ? slug : id;
  const navigate = useNavigate();
  const { showToast } = useToast();
  const queryClient = useQueryClient();

  const [editing, setEditing] = useState(false);
  const [editName, setEditName] = useState('');
  const [editDescription, setEditDescription] = useState('');

  const playlistQuery = useQuery({
    queryKey: ['playlist', key, shared],
    queryFn: () => (shared ? playlistApi.getShared(slug!) : playlistApi.get(id!)),
    enabled: !!key,
    retry: false,
  });

  const itemsQuery = useQuery({
    queryKey: ['playlist', key, 'items', shared],
    queryFn: () => (shared ? playlistApi.sharedItems(slug!) : playlistApi.items(id!)),
    enabled: !!key,
    retry: false,
  });

  const playlist = playlistQuery.data;
  const items: Performance[] = itemsQuery.data?.performances ?? [];
  const isOwner = playlist?.is_owner ?? false;

  const invalidate = () => {
    queryClient.invalidateQueries({ queryKey: ['playlist', key] });
    queryClient.invalidateQueries({ queryKey: ['playlists'] });
  };

  const updateMutation = useMutation({
    mutationFn: (body: { name?: string; description?: string; visibility?: PlaylistVisibility }) =>
      playlistApi.update(playlist!.id, body),
    onSuccess: () => {
      invalidate();
      setEditing(false);
      showToast('更新しました', 'success');
    },
    onError: (err: Error) => showToast(`更新に失敗しました: ${err.message}`, 'error'),
  });

  const removeItemMutation = useMutation({
    mutationFn: (performanceId: string) => playlistApi.removeItem(playlist!.id, performanceId),
    onSuccess: () => {
      invalidate();
      showToast('プレイリストから削除しました', 'success');
    },
    onError: (err: Error) => showToast(`削除に失敗しました: ${err.message}`, 'error'),
  });

  const reorderMutation = useMutation({
    mutationFn: (performanceIds: string[]) => playlistApi.reorder(playlist!.id, performanceIds),
    onSuccess: invalidate,
    onError: (err: Error) => showToast(`並び替えに失敗しました: ${err.message}`, 'error'),
  });

  const deleteMutation = useMutation({
    mutationFn: () => playlistApi.remove(playlist!.id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['playlists'] });
      showToast('プレイリストを削除しました', 'success');
      navigate('/playlists');
    },
    onError: (err: Error) => showToast(`削除に失敗しました: ${err.message}`, 'error'),
  });

  const playFrom = (startIndex: number) => {
    usePlayerStore.getState().playTracks(performancesToTracks(items), startIndex);
  };

  const playShuffled = () => {
    const tracks = performancesToTracks(items);
    for (let i = tracks.length - 1; i > 0; i--) {
      const j = Math.floor(Math.random() * (i + 1));
      [tracks[i], tracks[j]] = [tracks[j], tracks[i]];
    }
    usePlayerStore.getState().playTracks(tracks, 0);
  };

  // 1つ上/下へ移動。並び順は performance ID の配列としてサーバーへ送る。
  const move = (index: number, direction: -1 | 1) => {
    const target = index + direction;
    if (target < 0 || target >= items.length) return;
    const ids = items.map((p) => p.id);
    [ids[index], ids[target]] = [ids[target], ids[index]];
    reorderMutation.mutate(ids);
  };

  const copyShareLink = async () => {
    if (!playlist?.share_slug) return;
    const url = `${window.location.origin}/shared/playlists/${playlist.share_slug}`;
    try {
      await navigator.clipboard.writeText(url);
      showToast('共有リンクをコピーしました', 'success');
    } catch {
      // クリップボードが使えない環境（http や権限拒否）ではリンクを直接見せる
      showToast(url, 'info');
    }
  };

  if (playlistQuery.isLoading) return <Loading />;

  if (playlistQuery.isError || !playlist) {
    return (
      <div className="text-center py-16 space-y-3">
        <p className="text-gray-900 font-medium">プレイリストが見つかりません</p>
        <p className="text-sm text-gray-500">
          削除されたか、非公開に設定されている可能性があります。
        </p>
        <Link to="/playlists" className="inline-block text-indigo-600 hover:underline">
          プレイリスト一覧へ
        </Link>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      {/* ヘッダー */}
      <div className="space-y-3">
        {editing ? (
          <form
            onSubmit={(e) => {
              e.preventDefault();
              updateMutation.mutate({ name: editName, description: editDescription });
            }}
            className="space-y-3"
          >
            <input
              autoFocus
              value={editName}
              onChange={(e) => setEditName(e.target.value)}
              className="w-full text-2xl font-bold px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
            />
            <textarea
              value={editDescription}
              onChange={(e) => setEditDescription(e.target.value)}
              rows={2}
              placeholder="説明（任意）"
              className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
            />
            <div className="flex gap-2">
              <button
                type="submit"
                disabled={!editName.trim() || updateMutation.isPending}
                className="px-4 py-2 bg-indigo-600 text-white rounded-lg hover:bg-indigo-700 disabled:bg-gray-300 transition-colors"
              >
                保存
              </button>
              <button
                type="button"
                onClick={() => setEditing(false)}
                className="px-4 py-2 border border-gray-300 rounded-lg hover:bg-gray-50 transition-colors"
              >
                キャンセル
              </button>
            </div>
          </form>
        ) : (
          <>
            <div className="flex flex-wrap items-center gap-3">
              <h1 className="text-3xl font-bold text-gray-900">{playlist.name}</h1>
              <VisibilityBadge visibility={playlist.visibility} />
              {isOwner && (
                <button
                  onClick={() => {
                    setEditName(playlist.name);
                    setEditDescription(playlist.description);
                    setEditing(true);
                  }}
                  className="text-gray-400 hover:text-indigo-600 transition-colors"
                  title="名前と説明を編集"
                >
                  <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z" />
                  </svg>
                </button>
              )}
            </div>
            {playlist.description && <p className="text-gray-600">{playlist.description}</p>}
            <p className="text-sm text-gray-500">
              {playlist.owner_name} · {playlist.item_count} 曲
            </p>
          </>
        )}
      </div>

      {/* 操作 */}
      <div className="flex flex-wrap items-center gap-2">
        <button
          onClick={() => playFrom(0)}
          disabled={items.length === 0}
          className="px-4 py-2 bg-indigo-600 text-white rounded-lg hover:bg-indigo-700 disabled:bg-gray-300 transition-colors"
        >
          再生
        </button>
        <button
          onClick={playShuffled}
          disabled={items.length === 0}
          className="px-4 py-2 border border-gray-300 rounded-lg hover:bg-gray-50 disabled:text-gray-300 transition-colors"
        >
          ランダム再生
        </button>

        {isOwner && (
          <>
            <select
              value={playlist.visibility}
              onChange={(e) => updateMutation.mutate({ visibility: e.target.value as PlaylistVisibility })}
              className="px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
              title="公開範囲を変更"
            >
              <option value="private">非公開（自分だけ）</option>
              <option value="unlisted">限定公開（リンクのみ）</option>
              <option value="public">公開（一覧に掲載）</option>
            </select>

            {playlist.visibility !== 'private' && (
              <button
                onClick={copyShareLink}
                className="px-3 py-2 border border-gray-300 rounded-lg hover:bg-gray-50 transition-colors"
                title="共有リンクをコピー"
              >
                共有リンク
              </button>
            )}

            <button
              onClick={() => {
                if (window.confirm(`「${playlist.name}」を削除します。よろしいですか？`)) {
                  deleteMutation.mutate();
                }
              }}
              className="ml-auto px-3 py-2 text-red-600 border border-red-200 rounded-lg hover:bg-red-50 transition-colors"
            >
              削除
            </button>
          </>
        )}
      </div>

      {/* 収録曲 */}
      {itemsQuery.isLoading ? (
        <Loading />
      ) : items.length === 0 ? (
        <p className="text-gray-500 py-8 text-center">
          まだ曲がありません。楽曲や歌枠のページの「＋」ボタンから追加できます。
        </p>
      ) : (
        <ul className="space-y-2">
          {items.map((perf, index) => (
            <li
              key={perf.id}
              className="flex items-center gap-3 bg-white border border-gray-200 rounded-lg px-3 py-2"
            >
              <span className="w-6 text-right text-xs font-mono text-gray-400 shrink-0">{index + 1}</span>
              {perf.arts ? (
                <img src={perf.arts} alt="" className="w-10 h-10 object-cover rounded shrink-0" />
              ) : (
                <span className="w-10 h-10 bg-gray-200 rounded shrink-0" />
              )}
              <div className="flex-1 min-w-0">
                <Link
                  to={`/songs/${perf.song_id}`}
                  className="block font-medium text-gray-900 truncate hover:text-indigo-600"
                >
                  {perf.song_name}
                </Link>
                <span className="block text-sm text-gray-500 truncate">
                  <ArtistLinks artists={perf.artists} fallback={perf.original_artist} />
                </span>
              </div>
              <span className="hidden sm:block text-xs font-mono text-gray-400 shrink-0">
                {formatTime(perf.start_seconds)}
              </span>

              <button
                onClick={() => playFrom(index)}
                className="inline-flex items-center justify-center w-8 h-8 rounded-full text-gray-400 hover:text-indigo-600 hover:bg-indigo-50 transition-colors shrink-0"
                title="ここから再生"
              >
                <svg className="w-5 h-5" fill="currentColor" viewBox="0 0 24 24">
                  <path d="M8 5v14l11-7z" />
                </svg>
              </button>

              <QueueAddButton track={performancesToTracks([perf])[0]} className="shrink-0" />

              {isOwner && (
                <>
                  <span className="flex flex-col shrink-0">
                    <button
                      onClick={() => move(index, -1)}
                      disabled={index === 0 || reorderMutation.isPending}
                      className="text-gray-300 hover:text-indigo-600 disabled:opacity-30 transition-colors leading-none"
                      title="上へ移動"
                    >
                      ▲
                    </button>
                    <button
                      onClick={() => move(index, 1)}
                      disabled={index === items.length - 1 || reorderMutation.isPending}
                      className="text-gray-300 hover:text-indigo-600 disabled:opacity-30 transition-colors leading-none"
                      title="下へ移動"
                    >
                      ▼
                    </button>
                  </span>
                  <button
                    onClick={() => removeItemMutation.mutate(perf.id)}
                    className="inline-flex items-center justify-center w-8 h-8 rounded-full text-gray-400 hover:text-red-600 hover:bg-red-50 transition-colors shrink-0"
                    title="プレイリストから削除"
                  >
                    <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                    </svg>
                  </button>
                </>
              )}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
