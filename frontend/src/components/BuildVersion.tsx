import { useQuery } from '@tanstack/react-query';
import { versionApi } from '../api/client';

// 日時は UTC の ISO で埋め込むので、表示時にローカルへ落とす。
function formatBuiltAt(iso: string): string {
  if (!iso) return '';
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleString();
}

/**
 * 稼働中のビルドを表示する。
 *
 * フロントは自分の commit をビルド時に埋め込み、バックエンドのものは
 * /api/version から取る。compose のレイヤーキャッシュの都合で
 * 「片方だけ入れ替わった」状態が起こり得るので、食い違いは色で知らせる。
 * これが分からないと、直したはずの不具合が消えない理由を追えない。
 */
export default function BuildVersion() {
  const frontCommit = __APP_COMMIT__;
  const frontBuiltAt = __APP_BUILT_AT__;

  const { data: backend, isError } = useQuery({
    queryKey: ['version'],
    queryFn: versionApi.get,
    staleTime: 5 * 60 * 1000,
    retry: false,
  });

  // どちらかが 'dev'（＝埋め込み無しのローカル実行）なら比較しても意味がない。
  const comparable =
    !!backend && frontCommit !== 'dev' && backend.commit !== 'dev';
  const mismatched = comparable && backend.commit !== frontCommit;

  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-sm text-gray-500">
      <span
        className="font-mono"
        title={frontBuiltAt ? `フロントエンドのビルド: ${formatBuiltAt(frontBuiltAt)}` : undefined}
      >
        フロント {frontCommit}
      </span>
      <span
        className="font-mono"
        title={
          backend?.built_at ? `バックエンドのビルド: ${formatBuiltAt(backend.built_at)}` : undefined
        }
      >
        バックエンド {isError ? '取得できません' : (backend?.commit ?? '…')}
      </span>
      {mismatched && (
        <span
          className="rounded bg-amber-100 px-2 py-0.5 text-amber-800"
          title="どちらか片方だけが入れ替わっています。deploy/deploy.sh で再デプロイしてください"
        >
          バージョンが一致していません
        </span>
      )}
    </div>
  );
}
