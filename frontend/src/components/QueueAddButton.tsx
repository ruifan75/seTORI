import { useRef, useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useLocation, useNavigate } from 'react-router-dom';
import { playlistApi } from '../api/client';
import { useAuthStore } from '../store/auth';
import { useToast } from './ui/ToastContext';
import PlaylistPickerMenu, { PLAYLIST_MENU_WIDTH } from './PlaylistPickerMenu';
import { menuPositionFor, type MenuPosition } from './menuPosition';
import { usePlayerStore, type PlayerTrack } from '../store/player';

type Selection =
  | { track: PlayerTrack; tracks?: never }
  | { track?: never; tracks: PlayerTrack[] };

type Props = Selection & {
  className?: string;
  /** トーストやボタン説明に使う名前。複数曲の場合に渡す */
  description?: string;
  playlistHeading?: string;
  defaultPlaylistName?: string;
};

function addedMessage(
  name: string,
  subject: string,
  itemCount: number,
  added: number,
  skipped: number,
  created: boolean,
): string {
  if (itemCount === 1) {
    if (added === 0) return `「${name}」には${subject}がすでに入っていました`;
    return created
      ? `「${name}」を作成して${subject}を追加しました`
      : `${subject}を「${name}」に追加しました`;
  }
  if (added === 0) return `「${name}」には全部すでに入っていました`;
  const head = created ? `「${name}」を作成して${added}曲追加しました` : `「${name}」に${added}曲追加しました`;
  return skipped > 0 ? `${head}（${skipped}曲は既に入っていました）` : head;
}

// 1曲または複数曲を再生キュー／プレイリストへ追加する共通ボタン。
// 未ログインでも同じ選択メニューを開き、プレイリストを選んだ時だけログインへ案内する。
export default function QueueAddButton({
  track,
  tracks,
  className = '',
  description,
  playlistHeading,
  defaultPlaylistName = '',
}: Props) {
  const { showToast } = useToast();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const location = useLocation();
  const isLoggedIn = useAuthStore((s) => s.status) === 'authenticated';
  const selectedTracks = tracks ?? (track ? [track] : []);
  const subject = description ?? (track ? `「${track.songName}」` : `${selectedTracks.length}曲`);

  const [open, setOpen] = useState(false);
  const [menuPos, setMenuPos] = useState<MenuPosition>({ top: 0, left: 0 });
  const buttonRef = useRef<HTMLButtonElement>(null);

  const addToQueue = () => {
    usePlayerStore.getState().enqueue(selectedTracks);
    showToast(`${subject}をキューに追加しました`, 'success');
    setOpen(false);
  };

  const addMutation = useMutation({
    mutationFn: async (target: { playlistId?: string; playlistName?: string; name?: string }) => {
      const created = target.name ? await playlistApi.create({ name: target.name }) : undefined;
      const playlistId = created?.id ?? target.playlistId!;
      const result = await playlistApi.addItems(playlistId, selectedTracks.map((item) => item.performanceId));
      return {
        ...result,
        playlistId,
        playlistName: created?.name ?? target.playlistName ?? 'プレイリスト',
        created: !!created,
      };
    },
    onSuccess: (result) => {
      queryClient.invalidateQueries({ queryKey: ['playlists'] });
      queryClient.invalidateQueries({ queryKey: ['playlist', result.playlistId] });
      showToast(
        addedMessage(
          result.playlistName,
          subject,
          selectedTracks.length,
          result.added,
          result.skipped,
          result.created,
        ),
        'success',
      );
      setOpen(false);
    },
    onError: (err: Error) => showToast(`追加に失敗しました: ${err.message}`, 'error'),
  });

  const handleClick = () => {
    if (!open) setMenuPos(menuPositionFor(buttonRef.current, PLAYLIST_MENU_WIDTH)); // 開く直前にボタン位置を測る
    setOpen((v) => !v);
  };

  return (
    <>
      <button
        ref={buttonRef}
        type="button"
        onClick={handleClick}
        disabled={selectedTracks.length === 0 || addMutation.isPending}
        className={`inline-flex items-center justify-center w-8 h-8 rounded-full text-gray-400 hover:text-indigo-600 hover:bg-indigo-50 transition-colors ${className}`}
        title={`${subject}をキュー／プレイリストに追加`}
        aria-haspopup="menu"
        aria-expanded={open}
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
          playlistUnavailableAction={!isLoggedIn ? {
            label: 'ログインしてプレイリストに追加',
            onClick: () => navigate('/login', { state: { from: `${location.pathname}${location.search}` } }),
          } : undefined}
          heading={playlistHeading}
          defaultName={defaultPlaylistName}
          onPick={(playlistId, playlistName) => addMutation.mutate({ playlistId, playlistName })}
          onCreate={(name) => addMutation.mutate({ name })}
          busy={addMutation.isPending}
        />
      )}
    </>
  );
}
