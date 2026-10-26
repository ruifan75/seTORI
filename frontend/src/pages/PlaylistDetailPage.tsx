import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { playlistApi } from '../api/client';
import type { Performance, PlaylistVisibility } from '../api/types';
import Loading from '../components/ui/Loading';
import PerformanceList from '../components/PerformanceList';
import PerformanceListToolbar from '../components/PerformanceListToolbar';
import { toPerformanceListItem } from '../utils/performanceList';
import VisibilityBadge from '../components/VisibilityBadge';
import { useToast } from '../components/ui/ToastContext';

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
  const listItems = items.map((perf) => toPerformanceListItem(perf));
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

      {/* 所有者の管理操作 */}
      {isOwner && (
        <div className="flex flex-wrap items-center gap-2">
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
        </div>
      )}

      {itemsQuery.isError ? (
        <p role="alert" className="py-8 text-center text-red-700">歌唱リストを取得できませんでした</p>
      ) : itemsQuery.isLoading ? <Loading /> : listItems.length === 0 ? (
        <p className="rounded-xl border bg-white py-12 text-center text-gray-500">まだ曲がありません。楽曲や歌枠のページの「＋」ボタンから追加できます。</p>
      ) : (
        <section className="space-y-4" aria-label="収録曲">
          <PerformanceListToolbar items={listItems} scope="all" />
          <PerformanceList items={listItems} numbered actionsFor={isOwner ? (item, index) => [
            { label: '上へ移動', onClick: () => move(index, -1), disabled: index === 0 || reorderMutation.isPending || removeItemMutation.isPending },
            { label: '下へ移動', onClick: () => move(index, 1), disabled: index === items.length - 1 || reorderMutation.isPending || removeItemMutation.isPending },
            { label: 'プレイリストから削除', onClick: () => removeItemMutation.mutate(item.track.performanceId), disabled: reorderMutation.isPending || removeItemMutation.isPending, destructive: true },
          ] : undefined} />
        </section>
      )}
    </div>
  );
}
