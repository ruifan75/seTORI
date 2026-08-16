import { useRef, useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useLocation, useNavigate } from 'react-router-dom';
import { presetPlaylistApi } from '../api/client';
import type { PresetPlaylist } from '../api/types';
import { useToast } from './ui/ToastContext';
import PlaylistPickerMenu, { PLAYLIST_MENU_WIDTH } from './PlaylistPickerMenu';
import { menuPositionFor, type MenuPosition } from './menuPosition';
import { useAuthStore } from '../store/auth';
import { performancesToTracks, usePlayerStore } from '../store/player';

interface Props {
  preset: PresetPlaylist;
  /** 押すとプリセットを頭から再生する。渡さなければ再生ボタンを出さない */
  onPlayAll?: () => void;
  playDisabled?: boolean;
}

// addedMessage は「何曲入ったか」をそのまま伝える。
// 既に入っていた曲は飛ばすので、押した曲数と増えた曲数は一致しないことがある。
function addedMessage(name: string, added: number, skipped: number, created: boolean): string {
  if (added === 0) {
    return `「${name}」には全部すでに入っていました`;
  }
  const head = created ? `「${name}」を作成して${added}曲追加しました` : `「${name}」に${added}曲追加しました`;
  return skipped > 0 ? `${head}（${skipped}曲は既に入っていました）` : head;
}

/**
 * プリセットプレイリストの操作（再生・キュー／プレイリストへ追加・フォロー）。
 *
 * 未ログインでもボタンは出す。隠すと「できること」に気づけないので、
 * キュー追加はそのまま使え、プレイリスト追加やフォローを選んだ時点でログインへ案内する。
 */
export default function PresetActions({ preset, onPlayAll, playDisabled }: Props) {
  const { showToast } = useToast();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const location = useLocation();
  const isAuthenticated = useAuthStore((state) => state.status === 'authenticated');

  const [pickerOpen, setPickerOpen] = useState(false);
  const [pickerPos, setPickerPos] = useState<MenuPosition>({ top: 0, left: 0 });
  const addButtonRef = useRef<HTMLButtonElement>(null);

  const goToLogin = () => navigate('/login', { state: { from: `${location.pathname}${location.search}` } });

  const followMutation = useMutation({
    mutationFn: () => (preset.is_following
      ? presetPlaylistApi.unfollow(preset.key)
      : presetPlaylistApi.follow(preset.key)),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['presets'] });
      showToast(preset.is_following ? 'フォローを解除しました' : `「${preset.name}」をフォローしました`, 'success');
    },
    onError: () => showToast('フォローの更新に失敗しました', 'error'),
  });

  const addMutation = useMutation({
    mutationFn: (target: { playlistId?: string; name?: string }) =>
      presetPlaylistApi.addToPlaylist(preset.key, target),
    onSuccess: (result) => {
      queryClient.invalidateQueries({ queryKey: ['playlists'] });
      queryClient.invalidateQueries({ queryKey: ['playlist', result.playlist.id] });
      setPickerOpen(false);
      showToast(addedMessage(result.playlist.name, result.added, result.skipped, result.created), 'success', {
        label: '開く',
        onClick: () => navigate(`/playlists/${result.playlist.id}`),
      });
    },
    onError: (err: Error) => showToast(`追加に失敗しました: ${err.message}`, 'error'),
  });

  const enqueueMutation = useMutation({
    mutationFn: () => presetPlaylistApi.items(preset.key),
    onSuccess: (result) => {
      const tracks = performancesToTracks(result.performances);
      usePlayerStore.getState().enqueue(tracks);
      setPickerOpen(false);
      showToast(`「${preset.name}」の${tracks.length}曲をキューに追加しました`, 'success');
    },
    onError: () => showToast('キューに追加する曲を読み込めませんでした', 'error'),
  });

  const openPicker = () => {
    if (!pickerOpen) setPickerPos(menuPositionFor(addButtonRef.current, PLAYLIST_MENU_WIDTH)); // 開く直前に測る
    setPickerOpen((v) => !v);
  };

  const iconButton = 'inline-flex items-center justify-center w-8 h-8 text-gray-500 border rounded-full hover:text-indigo-600 hover:bg-indigo-50 transition-colors disabled:opacity-60';

  return (
    <>
      {onPlayAll && (
        <button
          onClick={onPlayAll}
          disabled={playDisabled}
          className="inline-flex items-center justify-center w-8 h-8 bg-indigo-600 text-white rounded-full hover:bg-indigo-700 transition-colors disabled:opacity-60"
          title={`「${preset.name}」を再生`}
        >
          <svg className="w-4 h-4 ml-0.5" fill="currentColor" viewBox="0 0 24 24"><path d="M8 5v14l11-7z" /></svg>
        </button>
      )}

      <button
        ref={addButtonRef}
        onClick={openPicker}
        disabled={addMutation.isPending || enqueueMutation.isPending || preset.item_count === 0}
        className={iconButton}
        title={`今の${preset.item_count}曲をキュー／プレイリストに追加`}
        aria-haspopup="menu"
        aria-expanded={pickerOpen}
      >
        {addMutation.isPending || enqueueMutation.isPending ? (
          <svg className="w-4 h-4 animate-spin" fill="none" viewBox="0 0 24 24">
            <circle className="opacity-25" cx="12" cy="12" r="9" stroke="currentColor" strokeWidth="3" />
            <path className="opacity-75" fill="currentColor" d="M12 3a9 9 0 0 1 9 9h-3a6 6 0 0 0-6-6V3Z" />
          </svg>
        ) : (
          // 1曲の追加（QueueAddButton）と同じ playlist-add の絵柄にする。
          // 増える曲数が違うだけで、選ばせるもの（どのプレイリストへ入れるか）は同じなので
          <svg className="w-5 h-5" fill="currentColor" viewBox="0 0 24 24">
            <path d="M14 10H3v2h11v-2zm0-4H3v2h11V6zm4 8v-4h-2v4h-4v2h4v4h2v-4h4v-2h-4zM3 16h7v-2H3v2z" />
          </svg>
        )}
      </button>

      {pickerOpen && (
        <PlaylistPickerMenu
          anchorRef={addButtonRef}
          initialPosition={pickerPos}
          onClose={() => setPickerOpen(false)}
          leadingAction={{ label: '再生キューに追加', onClick: () => enqueueMutation.mutate() }}
          playlistUnavailableAction={!isAuthenticated ? {
            label: 'ログインしてプレイリストに追加',
            onClick: goToLogin,
          } : undefined}
          heading={`${preset.item_count}曲を追加`}
          defaultName={preset.name}
          busy={addMutation.isPending || enqueueMutation.isPending}
          onPick={(playlistId) => addMutation.mutate({ playlistId })}
          onCreate={(name) => addMutation.mutate({ name })}
        />
      )}

      <button
        onClick={() => (isAuthenticated ? followMutation.mutate() : goToLogin())}
        disabled={followMutation.isPending}
        className={preset.is_following
          ? 'inline-flex items-center justify-center w-8 h-8 text-indigo-600 border border-indigo-200 bg-indigo-50 rounded-full hover:bg-indigo-100 transition-colors disabled:opacity-60'
          : iconButton}
        title={isAuthenticated
          ? (preset.is_following ? 'フォローを解除する' : 'フォローしてマイリストに並べる（中身は自動で最新になります）')
          : 'フォローするにはログインが必要です'}
      >
        <svg className="w-4 h-4" fill={preset.is_following ? 'currentColor' : 'none'} stroke="currentColor" strokeWidth={2} viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" d="M5 5a2 2 0 0 1 2-2h10a2 2 0 0 1 2 2v16l-7-4-7 4V5Z" />
        </svg>
      </button>
    </>
  );
}
