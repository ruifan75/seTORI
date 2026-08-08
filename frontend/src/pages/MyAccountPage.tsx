import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link } from 'react-router-dom';
import { authApi, startOAuth } from '../api/client';
import { useAuthStore } from '../store/auth';
import Loading from '../components/ui/Loading';
import { useToast } from '../components/ui/ToastContext';

// 自分のアカウント。外部アカウント連携の追加と解除をここで行う。
//
// 連携追加は startOAuth をログイン中に呼ぶだけでよい（サーバーが
// Authorization ヘッダーを見て「新規ログイン」ではなく「連携追加」と判定する）。

const PROVIDER_LABELS: Record<string, string> = {
  google: 'Google',
};

function providerLabel(provider: string): string {
  return PROVIDER_LABELS[provider] ?? provider;
}

export default function MyAccountPage() {
  const me = useAuthStore((s) => s.user);
  const { showToast } = useToast();
  const queryClient = useQueryClient();

  const { data: identities = [], isLoading } = useQuery({
    queryKey: ['oauth', 'identities'],
    queryFn: authApi.oauthIdentities,
  });

  // 設定済みの連携先だけを候補にする（未設定の provider を押させない）
  const { data: providers = [] } = useQuery({
    queryKey: ['oauth', 'providers'],
    queryFn: authApi.oauthProviders,
    staleTime: Infinity,
  });

  const unlinkMutation = useMutation({
    mutationFn: (provider: string) => authApi.unlinkOAuth(provider),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['oauth', 'identities'] });
      showToast('連携を解除しました', 'success');
    },
    // 最後のログイン手段を失う解除はバックエンドが拒否する。理由をそのまま出す。
    onError: (err: Error) => showToast(err.message, 'error'),
  });

  const linked = new Set(identities.map((i) => i.provider));
  const linkable = providers.filter((p) => !linked.has(p));

  const handleLink = (provider: string) => {
    startOAuth(provider).catch((err: unknown) =>
      showToast(err instanceof Error ? err.message : '連携を開始できませんでした', 'error'),
    );
  };

  return (
    <div className="space-y-6">
      <h1 className="text-3xl font-bold text-gray-900">アカウント</h1>

      <div className="bg-white rounded-lg shadow-sm border p-6">
        <h2 className="text-xl font-bold text-gray-900 mb-4">利用者</h2>
        <dl className="grid grid-cols-[auto,1fr] gap-x-6 gap-y-2 text-sm">
          <dt className="text-gray-500">ユーザー名</dt>
          <dd className="text-gray-900">{me?.username}</dd>
          {me?.display_name && (
            <>
              <dt className="text-gray-500">表示名</dt>
              <dd className="text-gray-900">{me.display_name}</dd>
            </>
          )}
          <dt className="text-gray-500">ロール</dt>
          <dd className="text-gray-900">{me?.role}</dd>
        </dl>
        <p className="mt-4 text-sm text-gray-500">
          自分が出した修正提案は{' '}
          <Link to="/my/suggestions" className="text-indigo-600 hover:underline">
            こちら
          </Link>
          。
        </p>
      </div>

      <div className="bg-white rounded-lg shadow-sm border p-6">
        <h2 className="text-xl font-bold text-gray-900 mb-2">外部アカウント連携</h2>
        <p className="text-gray-500 mb-6 text-sm">
          連携すると、そのアカウントでログインできるようになります。
          ログイン手段が 1 つも無くなる解除はできません。
        </p>

        {isLoading ? (
          <Loading />
        ) : (
          <div className="space-y-3">
            {identities.map((identity) => (
              <div
                key={identity.id}
                className="flex flex-wrap items-center justify-between gap-3 border rounded-lg px-4 py-3"
              >
                <div className="min-w-0">
                  <p className="font-medium text-gray-900">{providerLabel(identity.provider)}</p>
                  {identity.email && (
                    <p className="text-sm text-gray-500 truncate">{identity.email}</p>
                  )}
                </div>
                <button
                  onClick={() => unlinkMutation.mutate(identity.provider)}
                  disabled={unlinkMutation.isPending}
                  className="text-sm font-medium text-red-600 hover:text-red-700 disabled:text-gray-400"
                  title={`${providerLabel(identity.provider)} の連携を解除する`}
                >
                  解除
                </button>
              </div>
            ))}

            {identities.length === 0 && (
              <p className="text-gray-400 text-sm">連携しているアカウントはありません</p>
            )}

            {linkable.length > 0 && (
              <div className="pt-2 flex flex-wrap gap-2">
                {linkable.map((provider) => (
                  <button
                    key={provider}
                    onClick={() => handleLink(provider)}
                    className="px-4 py-2 text-sm font-medium border border-gray-300 rounded-lg hover:bg-gray-50 transition-colors"
                  >
                    {providerLabel(provider)} と連携する
                  </button>
                ))}
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
