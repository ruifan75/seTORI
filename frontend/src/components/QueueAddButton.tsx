import { useRef, useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { playlistApi } from '../api/client';
import { useAuthStore } from '../store/auth';
import { useToast } from './ui/ToastContext';
import PlaylistPickerMenu, { PLAYLIST_MENU_WIDTH } from './PlaylistPickerMenu';
import { menuPositionFor, type MenuPosition } from './menuPosition';
import { usePlayerStore, type PlayerTrack } from '../store/player';

// 歌唱を再生キューやプレイリストへ追加するアイコンボタン（playlist-add）。
// クリックでメニューを開き、キュー追加とプレイリスト追加を選ばせる。
// 未ログイン時はプレイリストが使えないため、従来どおり即キューへ追加する。
export default function QueueAddButton({ track, className = '' }: { track: PlayerTrack; className?: string }) {
  const { showToast } = useToast();
  const queryClient = useQueryClient();
  const isLoggedIn = useAuthStore((s) => s.status) === 'authenticated';

  const [open, setOpen] = useState(false);
  const [menuPos, setMenuPos] = useState<MenuPosition>({ top: 0, left: 0 });
  const buttonRef = useRef<HTMLButtonElement>(null);

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
      setOpen(false);
    },
    onError: (err: Error) => showToast(`作成に失敗しました: ${err.message}`, 'error'),
  });

  const handleClick = () => {
    if (!isLoggedIn) {
      addToQueue(); // 未ログインならプレイリストは選べないので従来動作
      return;
    }
    if (!open) setMenuPos(menuPositionFor(buttonRef.current, PLAYLIST_MENU_WIDTH)); // 開く直前にボタン位置を測る
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

      {open && (
        <PlaylistPickerMenu
          anchorRef={buttonRef}
          initialPosition={menuPos}
          onClose={() => setOpen(false)}
          leadingAction={{ label: '再生キューに追加', onClick: addToQueue }}
          onPick={(playlistId) => addMutation.mutate(playlistId)}
          onCreate={(name) => createAndAddMutation.mutate(name)}
          busy={addMutation.isPending || createAndAddMutation.isPending}
        />
      )}
    </>
  );
}
