import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useLocation, useNavigate } from 'react-router-dom';
import { presetPlaylistApi } from '../api/client';
import type { PresetPlaylist } from '../api/types';
import { useToast } from './ui/ToastContext';
import { useAuthStore } from '../store/auth';

interface Props {
  preset: PresetPlaylist;
  /** 押すとプリセットを頭から再生する。渡さなければ再生ボタンを出さない */
  onPlayAll?: () => void;
  playDisabled?: boolean;
}

/**
 * プリセットプレイリストの操作（再生・コピー・フォロー）。
 *
 * 未ログインでもボタンは出す。隠すと「できること」に気づけないので、
 * 押した時点でログインへ案内する（修正提案の LoginToSuggest と同じ考え方）。
 */
export default function PresetActions({ preset, onPlayAll, playDisabled }: Props) {
  const { showToast } = useToast();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const location = useLocation();
  const isAuthenticated = useAuthStore((state) => state.status === 'authenticated');

  const goToLogin = () => navigate('/login', { state: { from: location.pathname } });

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

  const copyMutation = useMutation({
    mutationFn: () => presetPlaylistApi.copy(preset.key),
    onSuccess: (playlist) => {
      queryClient.invalidateQueries({ queryKey: ['playlists'] });
      showToast(`「${playlist.name}」としてコピーしました（${playlist.item_count}曲）`, 'success', {
        label: '開く',
        onClick: () => navigate(`/playlists/${playlist.id}`),
      });
    },
    onError: () => showToast('コピーに失敗しました', 'error'),
  });

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
        onClick={() => (isAuthenticated ? copyMutation.mutate() : goToLogin())}
        disabled={copyMutation.isPending}
        className={iconButton}
        title={isAuthenticated
          ? `今の${preset.item_count}曲を自分のプレイリストへコピー（以後は自分で編集できます）`
          : 'コピーするにはログインが必要です'}
      >
        {copyMutation.isPending ? (
          <svg className="w-4 h-4 animate-spin" fill="none" viewBox="0 0 24 24">
            <circle className="opacity-25" cx="12" cy="12" r="9" stroke="currentColor" strokeWidth="3" />
            <path className="opacity-75" fill="currentColor" d="M12 3a9 9 0 0 1 9 9h-3a6 6 0 0 0-6-6V3Z" />
          </svg>
        ) : (
          <svg className="w-4 h-4" fill="none" stroke="currentColor" strokeWidth={2} viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" d="M8 7V5a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2h-2M5 21h9a2 2 0 0 0 2-2v-9a2 2 0 0 0-2-2H5a2 2 0 0 0-2 2v9a2 2 0 0 0 2 2Z" />
          </svg>
        )}
      </button>

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
