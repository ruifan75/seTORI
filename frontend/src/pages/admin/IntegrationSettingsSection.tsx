import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { integrationSettingsApi } from '../../api/client';
import type { SecretFieldStatus } from '../../api/types';
import Loading from '../../components/ui/Loading';
import { useToast } from '../../components/ui/ToastContext';

// 機密項目の定義。key はバックエンドの項目名と一致させる。
// multiline の項目は textarea で出す（1行 input に複数行を貼ると改行が落ちるため）。
const SECRET_FIELDS: { key: string; label: string; help?: string; multiline?: boolean }[] = [
  { key: 'holodex_api_key', label: 'Holodex API キー' },
  { key: 'holodex_editor_token', label: 'Holodex 編集トークン', help: 'seTORI から Holodex へセットリストを書き戻すのに使います' },
  { key: 'youtube_api_key', label: 'YouTube API キー' },
  { key: 'groq_api_key', label: 'Groq API キー（後備）', help: 'AIプロバイダーを1つも登録していないときだけ使われます' },
  { key: 'google_drive_secret', label: 'Google Drive クライアントシークレット', help: 'バックアップの自動アップロード用（TV と限定入力デバイス型）' },
  { key: 'google_signin_secret', label: 'Google ログイン クライアントシークレット', help: 'Google でのログイン用（ウェブアプリケーション型）' },
  {
    key: 'ytdlp_cookies',
    label: 'YouTube cookie（cookies.txt）',
    help: '拍手から歌唱の終了時間を推定するとき、live chat の取得に使います。'
      + 'これが無いと YouTube に BOT 判定されて取得できないことがあります。'
      + 'ブラウザ拡張で書き出した Netscape 形式のファイルの中身をそのまま貼ってください',
    multiline: true,
  },
];

const PLAIN_FIELDS: { key: 'google_drive_client_id' | 'google_signin_client_id'; label: string }[] = [
  { key: 'google_drive_client_id', label: 'Google Drive クライアント ID' },
  { key: 'google_signin_client_id', label: 'Google ログイン クライアント ID' },
];

function StatusBadge({ status }: { status?: SecretFieldStatus }) {
  if (!status?.configured) {
    return <span className="px-2 py-0.5 text-xs rounded bg-gray-100 text-gray-500">未設定</span>;
  }
  if (status.from_env) {
    return (
      <span
        className="px-2 py-0.5 text-xs rounded bg-amber-100 text-amber-800"
        title=".env の値を使っています。ここで保存すると、そちらが優先されます"
      >
        .env {status.hint}
      </span>
    );
  }
  return (
    <span className="px-2 py-0.5 text-xs rounded bg-green-100 text-green-700" title="この画面から保存された値（DB 上で暗号化）">
      設定済み {status.hint}
    </span>
  );
}

// 外部サービスの API キーを管理画面から設定する。
// 値そのものは API から返らないため、入力欄は常に空で始まり、
// 空のまま保存した項目は変更されない。
export default function IntegrationSettingsSection() {
  const { showToast } = useToast();
  const queryClient = useQueryClient();
  const [inputs, setInputs] = useState<Record<string, string>>({});

  const { data, isLoading } = useQuery({
    queryKey: ['settings', 'integrations'],
    queryFn: integrationSettingsApi.get,
  });

  const invalidate = () => queryClient.invalidateQueries({ queryKey: ['settings', 'integrations'] });

  const saveMutation = useMutation({
    mutationFn: () => {
      const secrets: Record<string, string> = {};
      for (const f of SECRET_FIELDS) {
        if (inputs[f.key]?.trim()) secrets[f.key] = inputs[f.key].trim();
      }
      const body: Parameters<typeof integrationSettingsApi.update>[0] = { secrets };
      for (const f of PLAIN_FIELDS) {
        if (inputs[f.key] !== undefined) body[f.key] = inputs[f.key].trim();
      }
      return integrationSettingsApi.update(body);
    },
    onSuccess: () => {
      invalidate();
      setInputs({});
      showToast('連携設定を保存しました（再起動不要で反映されます）', 'success');
    },
    onError: (err: Error) => showToast(`保存に失敗しました: ${err.message}`, 'error'),
  });

  const clearMutation = useMutation({
    mutationFn: (key: string) => integrationSettingsApi.update({ clear: [key] }),
    onSuccess: () => {
      invalidate();
      showToast('削除しました（.env に値があればそちらに戻ります）', 'success');
    },
    onError: (err: Error) => showToast(`削除に失敗しました: ${err.message}`, 'error'),
  });

  if (isLoading) return <Loading />;

  const hasInput = Object.values(inputs).some((v) => v?.trim());

  return (
    <div className="space-y-4">
      <div>
        <h2 className="text-xl font-bold text-gray-900 mb-1">外部サービス連携</h2>
        <p className="text-sm text-gray-500">
          ここで保存した値は DB 上で暗号化され、<code className="text-xs">.env</code> より優先されます。
          保存後は再起動せずに反映されます。
        </p>
      </div>

      {!data?.encryption_enabled && (
        <div className="text-sm text-amber-800 bg-amber-50 border border-amber-200 rounded-lg px-3 py-2">
          <p className="font-medium">暗号鍵が未設定のため、この画面からは保存できません</p>
          <p className="mt-1">
            バックアップは Google Drive へ自動アップロードされるため、鍵の無い状態で機密を DB に置くと
            バックアップの流出がそのままキーの流出になります。サーバーの環境変数に
            <code className="mx-1 text-xs">SETTINGS_ENCRYPTION_KEY</code>
            を設定してから再起動してください（例: <code className="text-xs">openssl rand -base64 32</code>）。
          </p>
        </div>
      )}

      <div className="space-y-3">
        {SECRET_FIELDS.map((f) => {
          const status = data?.secrets[f.key];
          return (
            <div key={f.key} className="flex flex-wrap items-center gap-2">
              <div className="w-full sm:w-72 shrink-0">
                <label className="block text-sm font-medium text-gray-700">{f.label}</label>
                {f.help && <p className="text-xs text-gray-400">{f.help}</p>}
              </div>
              {f.multiline ? (
                <textarea
                  rows={4}
                  spellCheck={false}
                  value={inputs[f.key] ?? ''}
                  onChange={(e) => setInputs((p) => ({ ...p, [f.key]: e.target.value }))}
                  disabled={!data?.encryption_enabled}
                  placeholder={status?.configured ? '変更する場合のみ貼り付け' : '# Netscape HTTP Cookie File ...'}
                  className="flex-1 min-w-[12rem] px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent disabled:bg-gray-50 font-mono text-xs"
                />
              ) : (
                <input
                  type="password"
                  autoComplete="new-password"
                  value={inputs[f.key] ?? ''}
                  onChange={(e) => setInputs((p) => ({ ...p, [f.key]: e.target.value }))}
                  disabled={!data?.encryption_enabled}
                  placeholder={status?.configured ? '変更する場合のみ入力' : '未設定'}
                  className="flex-1 min-w-[12rem] px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent disabled:bg-gray-50"
                />
              )}
              <StatusBadge status={status} />
              {status?.configured && !status.from_env && (
                <button
                  onClick={() => {
                    if (window.confirm(`${f.label} を削除します。よろしいですか？`)) clearMutation.mutate(f.key);
                  }}
                  className="text-gray-400 hover:text-red-600 transition-colors px-1"
                  title="削除（.env に値があればそちらに戻ります）"
                >
                  ×
                </button>
              )}
            </div>
          );
        })}

        {PLAIN_FIELDS.map((f) => {
          const current = data?.plain[f.key] ?? '';
          const fromEnv = data?.plain_from_env[f.key];
          return (
            <div key={f.key} className="flex flex-wrap items-center gap-2">
              <div className="w-full sm:w-72 shrink-0">
                <label className="block text-sm font-medium text-gray-700">{f.label}</label>
                <p className="text-xs text-gray-400">機密ではないので暗号化せず保存します</p>
              </div>
              <input
                type="text"
                value={inputs[f.key] ?? current}
                onChange={(e) => setInputs((p) => ({ ...p, [f.key]: e.target.value }))}
                placeholder="未設定"
                className="flex-1 min-w-[12rem] px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent font-mono text-sm"
              />
              {fromEnv && current && (
                <span className="px-2 py-0.5 text-xs rounded bg-amber-100 text-amber-800" title=".env の値を使っています">
                  .env
                </span>
              )}
            </div>
          );
        })}
      </div>

      <div className="flex items-center gap-3">
        <button
          onClick={() => saveMutation.mutate()}
          disabled={!hasInput || saveMutation.isPending}
          className="px-4 py-2 bg-indigo-600 text-white rounded-lg hover:bg-indigo-700 disabled:bg-gray-300 transition-colors"
        >
          {saveMutation.isPending ? '保存中...' : '保存'}
        </button>
        {hasInput && (
          <button
            onClick={() => setInputs({})}
            className="px-4 py-2 border border-gray-300 rounded-lg hover:bg-gray-50 transition-colors"
          >
            入力を破棄
          </button>
        )}
      </div>
    </div>
  );
}
