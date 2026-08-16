import { usePlayerStore } from '../store/player';
import { useAuthStore, hasPermission, PERM } from '../store/auth';

// 再生中に「この曲の情報が違う」と気づいた人が、その場で直しにいく入口。
//
// **ここはボタン 1 つだけ。** 以前は小さなパネルを開いて「今の曲／直前の曲」の
// 開始・終了をワンタップで送れるようにしていたが、送れるのは押した瞬間の
// 再生位置だけで、その再生位置は区間の外へ出られなかった（区間の終わりに
// 来ると次の曲へ送られるため）。つまり**ずれが大きいほど直せない**という、
// 逆になっている導線だった。
//
// 代わりに区間の締め切りを外した編集画面（PerformanceReportDialog）を開く。
// 対象の選び直し（直前の曲を直す）も、曲の差し替えも、未登録曲の報告も
// そこに集約する。
export default function PlaybackFeedback({ dark = false }: { dark?: boolean }) {
  const queue = usePlayerStore((s) => s.queue);
  const index = usePlayerStore((s) => s.index);
  const setEditing = usePlayerStore((s) => s.setEditing);
  const canEdit = hasPermission(
    useAuthStore((s) => s.user),
    PERM.CONTENT_EDIT
  );

  const current = queue[index];
  if (!current) return null;

  const label = canEdit ? 'この曲の情報を直す' : 'この曲の情報の誤りを報告';

  return (
    <button
      onClick={() => setEditing({ streamId: current.streamId, performanceId: current.performanceId })}
      className={`p-2 rounded-lg ${
        dark ? 'text-gray-400 hover:text-white hover:bg-white/10' : 'text-gray-400 hover:text-gray-700'
      }`}
      title={label}
      aria-label={label}
    >
      <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path
          strokeLinecap="round"
          strokeLinejoin="round"
          strokeWidth={2}
          d="M3 21v-4m0 0V5a2 2 0 012-2h6.5l1 1H21l-3 6 3 6h-8.5l-1-1H5a2 2 0 00-2 2zm9-13.5V9"
        />
      </svg>
    </button>
  );
}
