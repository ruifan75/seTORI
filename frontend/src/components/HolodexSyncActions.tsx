import { hasPermission, PERM, useAuthStore } from '../store/auth';

interface Props {
  onDownload: () => void;
  onUpload: () => void;
  downloading: boolean;
  uploading: boolean;
}

// 配信編集の操作タブ。読み取りと運用者名義の書き込みを別々の権限で表示する。
export default function HolodexSyncActions({ onDownload, onUpload, downloading, uploading }: Props) {
  const user = useAuthStore((s) => s.user);
  const canDownload = hasPermission(user, PERM.SYNC_RUN);
  const canUpload = hasPermission(user, PERM.HOLODEX_UPLOAD);
  if (!canDownload && !canUpload) return null;

  return (
    <div>
      <p className="text-xs font-medium text-gray-400 mb-1.5">Holodex 同期</p>
      <div className="flex flex-wrap gap-2">
        {canDownload && (
          <button
            onClick={onDownload}
            disabled={downloading}
            className="px-3 py-1.5 text-sm bg-indigo-50 text-indigo-700 border border-indigo-200 font-medium rounded-lg hover:bg-indigo-100 transition-colors disabled:opacity-50"
          >
            {downloading ? '同期中...' : 'Holodex から同期'}
          </button>
        )}
        {canUpload && (
          <button
            onClick={onUpload}
            disabled={uploading}
            title="seTORI のセットリストを Holodex に書き込みます（外部サービスへの反映）"
            className="px-3 py-1.5 text-sm bg-amber-50 text-amber-700 border border-amber-300 font-medium rounded-lg hover:bg-amber-100 transition-colors disabled:opacity-50"
          >
            {uploading ? 'Holodex へ同期中...' : 'seTORI から Holodex へ同期'}
          </button>
        )}
      </div>
    </div>
  );
}
