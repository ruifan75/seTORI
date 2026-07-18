import { useToast } from './ui/Toast';
import { usePlayerStore, type PlayerTrack } from '../store/player';

// 歌唱を再生キューの末尾に追加するアイコンボタン（playlist-add）。
// キューが空のときはそのまま再生が始まる。ラベルは hover の title で示す。
export default function QueueAddButton({ track, className = '' }: { track: PlayerTrack; className?: string }) {
  const { showToast } = useToast();

  const add = () => {
    usePlayerStore.getState().enqueue([track]);
    showToast(`「${track.songName}」をキューに追加しました`, 'success');
  };

  return (
    <button
      onClick={add}
      className={`inline-flex items-center justify-center w-8 h-8 rounded-full text-gray-400 hover:text-indigo-600 hover:bg-indigo-50 transition-colors ${className}`}
      title="キューに追加"
    >
      <svg className="w-5 h-5" fill="currentColor" viewBox="0 0 24 24">
        <path d="M14 10H3v2h11v-2zm0-4H3v2h11V6zm4 8v-4h-2v4h-4v2h4v4h2v-4h4v-2h-4zM3 16h7v-2H3v2z" />
      </svg>
    </button>
  );
}
