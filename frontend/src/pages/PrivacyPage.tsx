import { useQuery } from '@tanstack/react-query';
import { activityApi } from '../api/client';

export default function PrivacyPage() {
  const { data } = useQuery({ queryKey: ['activity', 'policy'], queryFn: activityApi.policy });
  const retentionDays = data?.retention_days ?? 30;

  return (
    <article className="mx-auto max-w-3xl rounded-lg border bg-white p-6 shadow-sm sm:p-8">
      <h1 className="text-3xl font-bold text-gray-900">プライバシー</h1>
      <p className="mt-4 text-gray-600">
        seTORI では、サービスの運用状況の把握、不正利用への対応、ログインの保護のため、
        アクセスに関する最小限の情報を記録します。
      </p>

      <section className="mt-7">
        <h2 className="text-lg font-semibold text-gray-900">記録する情報</h2>
        <ul className="mt-2 list-disc space-y-1 pl-6 text-gray-600">
          <li>IP アドレス</li>
          <li>ログイン中の場合は seTORI の利用者アカウント</li>
          <li>表示したページの pathname、表示日時、表示回数</li>
          <li>ブラウザが送信する User-Agent</li>
        </ul>
        <p className="mt-3 text-sm text-gray-500">
          URL の query string、referrer、ページに入力した内容はアクセス記録に保存しません。
        </p>
      </section>

      <section className="mt-7">
        <h2 className="text-lg font-semibold text-gray-900">保存期間と閲覧</h2>
        <p className="mt-2 text-gray-600">
          アクセス記録は日単位で集約し、{retentionDays} 日後に自動削除します。完全な IP アドレスを閲覧できるのは、
          利用者管理権限を持つ運営者だけです。通常の管理画面では IP の一部をマスクして表示します。
        </p>
        <p className="mt-3 text-sm text-gray-500">
          削除済みの記録が、障害復旧用バックアップのローテーションが終わるまで一時的に残る場合があります。
        </p>
      </section>

      <section className="mt-7">
        <h2 className="text-lg font-semibold text-gray-900">ログイン情報</h2>
        <p className="mt-2 text-gray-600">
          ログイン状態を維持するため、ブラウザのローカルストレージにセッショントークンを保存します。
          サーバー側にはトークンそのものではなく、そのハッシュだけを保存します。
        </p>
      </section>

      <p className="mt-8 text-xs text-gray-400">最終更新: 2026年8月16日</p>
    </article>
  );
}
