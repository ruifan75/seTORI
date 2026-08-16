import { usePlayerStore, type PlayerTrack } from '../store/player';
import { useAuthStore, hasPermission, PERM } from '../store/auth';

// 歌唱 1 件の「情報が違う」入口。**一覧の行に置くのはこれ 1 つだけ**にする。
//
// 以前は行ごとに「歌唱時間を編集」と「別の曲に差し替え」の 2 つを並べていたが、
// 利用者から見れば同じ「この曲の情報が違う」で、押す前にどちらの誤りかを
// 分類させていたことになる。押した先の 1 画面で時間も曲も歌った人も直せる。
//
// 押すとその歌唱を再生してから開く（時間のずれは聴かなければ判断できない）。
export default function ReportButton({
  track,
  className = 'inline-flex items-center justify-center w-8 h-8 rounded-full text-gray-400 hover:text-indigo-600 hover:bg-indigo-50 transition-colors',
}: {
  track: PlayerTrack;
  className?: string;
}) {
  const openReport = usePlayerStore((s) => s.openReport);
  const canEdit = hasPermission(
    useAuthStore((s) => s.user),
    PERM.CONTENT_EDIT
  );
  const label = canEdit ? 'この曲の情報を直す' : 'この曲の情報の誤りを報告';

  return (
    <button onClick={() => openReport(track)} className={className} title={label} aria-label={label}>
      <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
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
