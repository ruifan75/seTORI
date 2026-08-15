import { useCallback, useEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { playlistApi } from '../api/client';
import { useAuthStore } from '../store/auth';
import { useToast } from './ui/ToastContext';
import { usePlayerStore, type PlayerTrack } from '../store/player';

// 歌唱を再生キューやプレイリストへ追加するアイコンボタン（playlist-add）。
// クリックでメニューを開き、キュー追加とプレイリスト追加を選ばせる。
// 未ログイン時はプレイリストが使えないため、従来どおり即キューへ追加する。
export default function QueueAddButton({ track, className = '' }: { track: PlayerTrack; className?: string }) {
  const { showToast } = useToast();
  const queryClient = useQueryClient();
  const isLoggedIn = useAuthStore((s) => s.status) === 'authenticated';

  const [open, setOpen] = useState(false);
  const [creatingName, setCreatingName] = useState('');
  const [menuPos, setMenuPos] = useState({ top: 0, left: 0 });
  const buttonRef = useRef<HTMLButtonElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);

  const MENU_WIDTH = 256;

  // メニューはビューポート基準（fixed）で body 直下に描く。
  // このボタンはホームの横スクロール列（overflow-x-auto）の中にも置かれるため、
  // 通常の absolute だと祖先に切り取られて見えなくなる。
  const updatePosition = useCallback(() => {
    const rect = buttonRef.current?.getBoundingClientRect();
    if (!rect) return;
    // 既定はボタン右端に右揃え。画面外へはみ出す場合は内側へ寄せる。
    const left = Math.min(
      Math.max(8, rect.right - MENU_WIDTH),
      window.innerWidth - MENU_WIDTH - 8,
    );
    setMenuPos({ top: rect.bottom + 4, left });
  }, []);

  // メニュー外クリック・Esc で閉じる。スクロールやリサイズでは位置を追従させる。
  useEffect(() => {
    if (!open) return;
    const onPointerDown = (e: MouseEvent) => {
      const target = e.target as Node;
      if (buttonRef.current?.contains(target) || menuRef.current?.contains(target)) return;
      setOpen(false);
    };
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(false);
    };
    document.addEventListener('mousedown', onPointerDown);
    document.addEventListener('keydown', onKeyDown);
    window.addEventListener('resize', updatePosition);
    window.addEventListener('scroll', updatePosition, true); // 祖先のスクロールも拾う
    return () => {
      document.removeEventListener('mousedown', onPointerDown);
      document.removeEventListener('keydown', onKeyDown);
      window.removeEventListener('resize', updatePosition);
      window.removeEventListener('scroll', updatePosition, true);
    };
  }, [open, updatePosition]);

  // メニューを開いたときだけ自分のプレイリストを取る
  const playlists = useQuery({
    queryKey: ['playlists', 'mine'],
    queryFn: playlistApi.listMine,
    enabled: open && isLoggedIn,
  });

  const addToQueue = () => {
    usePlayerStore.getState().enqueue([track]);
    showToast(`「${track.songName}」をキューに追加しました`, 'success');
    setOpen(false);
  };

  const addMutation = useMutation({
    mutationFn: (playlistId: string) => playlistApi.addItem(playlistId, track.performanceId),
    onSuccess: (_data, playlistId) => {
      queryClient.invalidateQueries({ queryKey: ['playlists'] });
      queryClient.invalidateQueries({ queryKey: ['playlist', playlistId] });
      showToast(`「${track.songName}」を追加しました`, 'success');
      setOpen(false);
    },
    onError: (err: Error) => showToast(`追加に失敗しました: ${err.message}`, 'error'),
  });

  // 新規プレイリストを作ってそのまま1曲目として追加する
  const createAndAddMutation = useMutation({
    mutationFn: async (name: string) => {
      const created = await playlistApi.create({ name });
      await playlistApi.addItem(created.id, track.performanceId);
      return created;
    },
    onSuccess: (created) => {
      queryClient.invalidateQueries({ queryKey: ['playlists'] });
      showToast(`「${created.name}」を作成して追加しました`, 'success');
      setCreatingName('');
      setOpen(false);
    },
    onError: (err: Error) => showToast(`作成に失敗しました: ${err.message}`, 'error'),
  });

  const handleClick = () => {
    if (!isLoggedIn) {
      addToQueue(); // 未ログインならプレイリストは選べないので従来動作
      return;
    }
    if (!open) updatePosition(); // 開く直前にボタン位置を測る
    setOpen((v) => !v);
  };

  return (
    <>
      <button
        ref={buttonRef}
        onClick={handleClick}
        className={`inline-flex items-center justify-center w-8 h-8 rounded-full text-gray-400 hover:text-indigo-600 hover:bg-indigo-50 transition-colors ${className}`}
        title={isLoggedIn ? 'キュー／プレイリストに追加' : 'キューに追加'}
      >
        <svg className="w-5 h-5" fill="currentColor" viewBox="0 0 24 24">
          <path d="M14 10H3v2h11v-2zm0-4H3v2h11V6zm4 8v-4h-2v4h-4v2h4v4h2v-4h4v-2h-4zM3 16h7v-2H3v2z" />
        </svg>
      </button>

      {open && createPortal(
        <div
          ref={menuRef}
          style={{ position: 'fixed', top: menuPos.top, left: menuPos.left, width: MENU_WIDTH }}
          className="z-50 bg-white border border-gray-200 rounded-lg shadow-lg py-1 text-left">
          <button
            onClick={addToQueue}
            className="w-full px-3 py-2 text-left text-sm text-gray-700 hover:bg-gray-50 transition-colors"
          >
            再生キューに追加
          </button>

          <div className="border-t border-gray-100 my-1" />
          <p className="px-3 py-1 text-xs font-medium text-gray-400">プレイリストに追加</p>

          {playlists.isLoading ? (
            <p className="px-3 py-2 text-sm text-gray-400">読み込み中...</p>
          ) : (playlists.data?.playlists.length ?? 0) === 0 ? (
            <p className="px-3 py-2 text-sm text-gray-400">まだプレイリストがありません</p>
          ) : (
            <ul className="max-h-48 overflow-y-auto">
              {playlists.data!.playlists.map((pl) => (
                <li key={pl.id}>
                  <button
                    onClick={() => addMutation.mutate(pl.id)}
                    disabled={addMutation.isPending}
                    className="w-full px-3 py-2 text-left text-sm text-gray-700 hover:bg-gray-50 disabled:text-gray-300 transition-colors flex items-center justify-between gap-2"
                  >
                    <span className="truncate">{pl.name}</span>
                    <span className="text-xs text-gray-400 shrink-0">{pl.item_count}</span>
                  </button>
                </li>
              ))}
            </ul>
          )}

          <div className="border-t border-gray-100 my-1" />
          <form
            onSubmit={(e) => {
              e.preventDefault();
              if (creatingName.trim()) createAndAddMutation.mutate(creatingName.trim());
            }}
            className="px-2 py-1"
          >
            <input
              value={creatingName}
              onChange={(e) => setCreatingName(e.target.value)}
              placeholder="新しいプレイリスト名"
              className="w-full px-2 py-1 text-sm border border-gray-300 rounded focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
            />
          </form>
        </div>,
        document.body,
      )}
    </>
  );
}
