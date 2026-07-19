import { useEffect, useRef, useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { backupApi } from '../../api/client';
import { useToast } from '../../components/ui/ToastContext';
import type { BackupFileInfo, BackupSettings, DriveFile, DriveDeviceAuth } from '../../api/types';

function formatBytes(bytes: number): string {
  if (!bytes) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB'];
  let v = bytes;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v.toFixed(v >= 100 || i === 0 ? 0 : 1)} ${units[i]}`;
}

function formatDate(iso?: string | null): string {
  if (!iso) return '—';
  const d = new Date(iso);
  if (isNaN(d.getTime())) return '—';
  return d.toLocaleString('ja-JP', {
    year: 'numeric', month: '2-digit', day: '2-digit',
    hour: '2-digit', minute: '2-digit',
  });
}

// ========== アイコン（ボタンは icon + hover tooltip 方針） ==========

function DownloadIcon() {
  return (
    <svg className="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
      <path strokeLinecap="round" strokeLinejoin="round" d="M4 16v2a2 2 0 002 2h12a2 2 0 002-2v-2M7 10l5 5 5-5M12 15V3" />
    </svg>
  );
}

function RestoreIcon() {
  return (
    <svg className="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
      <path strokeLinecap="round" strokeLinejoin="round" d="M3 12a9 9 0 109-9 9.75 9.75 0 00-6.74 2.74L3 8" />
      <path strokeLinecap="round" strokeLinejoin="round" d="M3 3v5h5M12 7v5l4 2" />
    </svg>
  );
}

function TrashIcon() {
  return (
    <svg className="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
      <path strokeLinecap="round" strokeLinejoin="round" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
    </svg>
  );
}

function CloudIcon({ className = 'w-5 h-5' }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
      <path strokeLinecap="round" strokeLinejoin="round" d="M3 15a4 4 0 004 4h9a5 5 0 10-.1-9.999A5.002 5.002 0 103 15z" />
    </svg>
  );
}

// RestoreTarget リストア確認モーダルの対象。
type RestoreTarget =
  | { kind: 'local'; name: string }
  | { kind: 'drive'; id: string; name: string }
  | { kind: 'upload'; file: File };

// ConfirmRestoreModal リストアは破壊的操作のため明示的な確認を挟む。
function ConfirmRestoreModal({ target, busy, onConfirm, onCancel }: {
  target: RestoreTarget;
  busy: boolean;
  onConfirm: () => void;
  onCancel: () => void;
}) {
  const label =
    target.kind === 'local' ? target.name :
    target.kind === 'drive' ? `${target.name}（Google Drive）` :
    `${target.file.name}（アップロード）`;
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center">
      <div className="absolute inset-0 bg-black/40" onClick={busy ? undefined : onCancel} />
      <div className="relative bg-white rounded-lg shadow-xl border p-6 w-full max-w-md mx-4">
        <h3 className="text-lg font-bold text-gray-900 mb-2">データベースを復元しますか？</h3>
        <p className="text-sm text-gray-600 mb-1">
          復元元: <span className="font-mono text-gray-900 break-all">{label}</span>
        </p>
        <ul className="text-sm text-gray-600 list-disc pl-5 my-3 space-y-1">
          <li>現在のデータベースはこのバックアップの内容で<span className="text-red-600 font-medium">完全に置き換えられます</span></li>
          <li>直前の状態は自動でローカルに安全バックアップされます（*_prerestore.dump）</li>
          <li>復元中は他の操作を行わないでください（数秒〜数十秒）</li>
        </ul>
        <div className="flex justify-end gap-2 mt-4">
          <button
            onClick={onCancel}
            disabled={busy}
            className="px-4 py-1.5 text-sm text-gray-700 border border-gray-300 rounded-lg hover:bg-gray-50 disabled:opacity-50"
          >
            キャンセル
          </button>
          <button
            onClick={onConfirm}
            disabled={busy}
            className="px-4 py-1.5 text-sm text-white font-medium rounded-lg bg-red-600 hover:bg-red-700 disabled:opacity-50"
          >
            {busy ? '復元中...' : '復元する'}
          </button>
        </div>
      </div>
    </div>
  );
}

// GoogleDriveSection Google Drive 連携（デバイスフロー認可 + Drive 上のバックアップ一覧）。
function GoogleDriveSection({ onRestoreRequest }: {
  onRestoreRequest: (target: RestoreTarget) => void;
}) {
  const queryClient = useQueryClient();
  const { showToast } = useToast();

  const { data: status } = useQuery({ queryKey: ['backup-status'], queryFn: backupApi.status });
  const gdrive = status?.gdrive;

  const [deviceAuth, setDeviceAuth] = useState<DriveDeviceAuth | null>(null);
  const pollTimer = useRef<number | null>(null);

  const stopPolling = () => {
    if (pollTimer.current !== null) {
      window.clearInterval(pollTimer.current);
      pollTimer.current = null;
    }
  };

  // 連携済みのときだけ Drive 上のファイル一覧を取得
  const { data: driveFiles = [], isLoading: driveLoading, error: driveError } = useQuery({
    queryKey: ['backup-drive-files'],
    queryFn: backupApi.gdriveFiles,
    enabled: !!gdrive?.connected,
  });

  const startMutation = useMutation({
    mutationFn: backupApi.gdriveAuthStart,
    onSuccess: (auth) => {
      setDeviceAuth(auth);
      // interval 秒ごとに承認状況を確認（expires_in で打ち切り）
      const startedAt = Date.now();
      stopPolling();
      pollTimer.current = window.setInterval(async () => {
        if (Date.now() - startedAt > auth.expires_in * 1000) {
          stopPolling();
          setDeviceAuth(null);
          showToast('認可コードの有効期限が切れました。もう一度お試しください', 'error');
          return;
        }
        try {
          const result = await backupApi.gdriveAuthPoll(auth.device_code);
          if (result.connected) {
            stopPolling();
            setDeviceAuth(null);
            showToast(`Google Drive と連携しました（${result.gdrive.email ?? ''}）`, 'success');
            queryClient.invalidateQueries({ queryKey: ['backup-status'] });
            queryClient.invalidateQueries({ queryKey: ['backup-drive-files'] });
          }
        } catch (e) {
          stopPolling();
          setDeviceAuth(null);
          showToast(`連携エラー: ${e instanceof Error ? e.message : String(e)}`, 'error');
        }
      }, Math.max(auth.interval, 5) * 1000);
    },
    onError: (err: Error) => showToast(err.message, 'error'),
  });

  const disconnectMutation = useMutation({
    mutationFn: backupApi.gdriveDisconnect,
    onSuccess: () => {
      showToast('Google Drive 連携を解除しました', 'success');
      queryClient.invalidateQueries({ queryKey: ['backup-status'] });
    },
    onError: (err: Error) => showToast(err.message, 'error'),
  });

  const deleteMutation = useMutation({
    mutationFn: backupApi.gdriveDeleteFile,
    onSuccess: () => {
      showToast('Drive 上のバックアップを削除しました', 'success');
      queryClient.invalidateQueries({ queryKey: ['backup-drive-files'] });
    },
    onError: (err: Error) => showToast(`削除エラー: ${err.message}`, 'error'),
  });

  // アンマウント時にポーリングを止める
  useEffect(() => stopPolling, []);

  return (
    <div className="bg-white rounded-lg shadow-sm border p-6">
      <div className="flex items-center gap-2 mb-2">
        <CloudIcon className="w-6 h-6 text-indigo-500" />
        <h2 className="text-xl font-bold text-gray-900">Google Drive 連携</h2>
      </div>
      <p className="text-gray-500 mb-4">
        バックアップを Google Drive（フォルダ「seTORI Backups」）へ自動アップロードします。
        アクセス範囲はこのアプリが作成したファイルのみ（drive.file）です。
      </p>

      {!gdrive?.configured && (
        <div className="text-sm text-gray-600 bg-gray-50 border border-gray-200 rounded-lg p-4 space-y-2">
          <p className="font-medium text-gray-800">セットアップ（.env に OAuth クライアントを設定）</p>
          <ol className="list-decimal pl-5 space-y-1">
            <li>Google Cloud Console で プロジェクトを作成し、Google Drive API を有効化</li>
            <li>OAuth 同意画面を設定（テストユーザーに自分の Google アカウントを追加）</li>
            <li>「認証情報」→ OAuth クライアント ID を作成（種類: <span className="font-mono">テレビと入力が限られたデバイス</span>）</li>
            <li>backend/.env に <span className="font-mono">GOOGLE_OAUTH_CLIENT_ID</span> と <span className="font-mono">GOOGLE_OAUTH_CLIENT_SECRET</span> を設定して再起動</li>
          </ol>
        </div>
      )}

      {gdrive?.configured && !gdrive.connected && !deviceAuth && (
        <button
          onClick={() => startMutation.mutate()}
          disabled={startMutation.isPending}
          className="px-4 py-2 text-sm text-white font-medium rounded-lg bg-indigo-600 hover:bg-indigo-700 disabled:opacity-50"
        >
          {startMutation.isPending ? '開始中...' : 'Google アカウントと連携'}
        </button>
      )}

      {deviceAuth && (
        <div className="text-sm bg-indigo-50 border border-indigo-200 rounded-lg p-4 space-y-2">
          <p className="text-gray-700">
            ブラウザで{' '}
            <a href={deviceAuth.verification_url} target="_blank" rel="noreferrer" className="text-indigo-600 underline font-medium">
              {deviceAuth.verification_url}
            </a>{' '}
            を開き、次のコードを入力してください：
          </p>
          <div className="text-2xl font-mono font-bold tracking-widest text-indigo-700 text-center py-2 bg-white rounded border border-indigo-200">
            {deviceAuth.user_code}
          </div>
          <p className="text-gray-500 flex items-center gap-2">
            <svg className="animate-spin h-4 w-4 text-indigo-500" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
              <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
              <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
            </svg>
            承認を待っています...
            <button
              onClick={() => { stopPolling(); setDeviceAuth(null); }}
              className="ml-auto text-gray-500 hover:text-gray-700 underline"
            >
              キャンセル
            </button>
          </p>
        </div>
      )}

      {gdrive?.connected && (
        <div>
          <div className="flex items-center gap-2 mb-4">
            <span className="inline-flex items-center gap-1.5 px-2.5 py-1 text-sm bg-green-100 text-green-800 border border-green-200 rounded-full">
              <span className="w-2 h-2 rounded-full bg-green-500" />
              連携中{gdrive.email ? `: ${gdrive.email}` : ''}
            </span>
            <button
              onClick={() => disconnectMutation.mutate()}
              disabled={disconnectMutation.isPending}
              className="ml-auto text-sm text-red-600 hover:text-red-800 disabled:opacity-50"
              title="連携を解除（Drive 上のファイルは残ります）"
            >
              連携解除
            </button>
          </div>

          <h3 className="text-sm font-semibold text-gray-700 mb-2">Drive 上のバックアップ</h3>
          {driveLoading ? (
            <p className="text-sm text-gray-400">読み込み中...</p>
          ) : driveError ? (
            <p className="text-sm text-red-600">{driveError instanceof Error ? driveError.message : '取得に失敗しました'}</p>
          ) : driveFiles.length === 0 ? (
            <p className="text-sm text-gray-400">バックアップがまだありません（バックアップ実行時に自動アップロードされます）</p>
          ) : (
            <div className="divide-y border rounded-lg">
              {driveFiles.map((f: DriveFile) => (
                <div key={f.id} className="flex items-center gap-3 px-3 py-2 text-sm">
                  <CloudIcon className="w-4 h-4 text-gray-400 shrink-0" />
                  <span className="font-mono text-gray-800 truncate">{f.name}</span>
                  <span className="text-gray-400 shrink-0">{f.size ? formatBytes(f.size) : ''}</span>
                  <span className="text-gray-400 shrink-0 hidden sm:inline">{formatDate(f.createdTime)}</span>
                  <span className="flex-1" />
                  <button
                    onClick={() => onRestoreRequest({ kind: 'drive', id: f.id, name: f.name })}
                    className="p-1.5 text-amber-600 hover:bg-amber-50 rounded"
                    title="このバックアップから復元"
                  >
                    <RestoreIcon />
                  </button>
                  <button
                    onClick={() => deleteMutation.mutate(f.id)}
                    disabled={deleteMutation.isPending}
                    className="p-1.5 text-red-500 hover:bg-red-50 rounded disabled:opacity-50"
                    title="Drive から削除"
                  >
                    <TrashIcon />
                  </button>
                </div>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  );
}

// SettingsForm 自動バックアップ設定（マウント時のサーバー値を初期値にする）。
function SettingsForm({ initial }: { initial: BackupSettings }) {
  const queryClient = useQueryClient();
  const { showToast } = useToast();

  const [autoEnabled, setAutoEnabled] = useState(initial.auto_enabled);
  const [intervalHours, setIntervalHours] = useState(initial.interval_hours);
  const [retentionLocal, setRetentionLocal] = useState(initial.retention_local);
  const [retentionDrive, setRetentionDrive] = useState(initial.retention_drive);
  const [driveUpload, setDriveUpload] = useState(initial.drive_upload);

  const settingsMutation = useMutation({
    mutationFn: backupApi.updateSettings,
    onSuccess: () => {
      showToast('設定を保存しました', 'success');
      queryClient.invalidateQueries({ queryKey: ['backup-status'] });
    },
    onError: (err: Error) => showToast(`設定の保存失敗: ${err.message}`, 'error'),
  });

  const saveSettings = () => {
    settingsMutation.mutate({
      auto_enabled: autoEnabled,
      interval_hours: intervalHours,
      retention_local: retentionLocal,
      retention_drive: retentionDrive,
      drive_upload: driveUpload,
    });
  };

  return (
    <div className="flex flex-wrap items-center gap-x-6 gap-y-3 text-sm">
      <label className="flex items-center gap-2">
        <input
          type="checkbox"
          checked={autoEnabled}
          onChange={(e) => setAutoEnabled(e.target.checked)}
          className="h-4 w-4"
        />
        <span className="text-gray-700 font-medium">自動バックアップを有効化</span>
      </label>
      <label className="flex items-center gap-1.5 text-gray-600">
        間隔
        <input
          type="number" min={1} max={720}
          value={intervalHours}
          onChange={(e) => setIntervalHours(parseInt(e.target.value) || 24)}
          className="w-16 px-2 py-1 border border-gray-300 rounded text-right font-mono"
        />
        時間
      </label>
      <label className="flex items-center gap-1.5 text-gray-600" title="ローカルに残す世代数">
        ローカル保持
        <input
          type="number" min={1} max={100}
          value={retentionLocal}
          onChange={(e) => setRetentionLocal(parseInt(e.target.value) || 7)}
          className="w-14 px-2 py-1 border border-gray-300 rounded text-right font-mono"
        />
        件
      </label>
      <label className="flex items-center gap-1.5 text-gray-600" title="Google Drive に残す世代数">
        Drive 保持
        <input
          type="number" min={1} max={100}
          value={retentionDrive}
          onChange={(e) => setRetentionDrive(parseInt(e.target.value) || 14)}
          className="w-14 px-2 py-1 border border-gray-300 rounded text-right font-mono"
        />
        件
      </label>
      <label className="flex items-center gap-2 text-gray-600" title="バックアップ作成時に Google Drive へアップロードする（要連携）">
        <input
          type="checkbox"
          checked={driveUpload}
          onChange={(e) => setDriveUpload(e.target.checked)}
          className="h-4 w-4"
        />
        Drive アップロード
      </label>
      <button
        onClick={saveSettings}
        disabled={settingsMutation.isPending}
        className="px-4 py-1.5 text-white font-medium rounded-lg bg-indigo-600 hover:bg-indigo-700 disabled:opacity-50"
      >
        保存
      </button>
    </div>
  );
}

export default function BackupPage() {
  const queryClient = useQueryClient();
  const { showToast } = useToast();
  const fileInputRef = useRef<HTMLInputElement>(null);

  const { data: status, isLoading } = useQuery({ queryKey: ['backup-status'], queryFn: backupApi.status });

  const [restoreTarget, setRestoreTarget] = useState<RestoreTarget | null>(null);

  const invalidateStatus = () => queryClient.invalidateQueries({ queryKey: ['backup-status'] });

  const createMutation = useMutation({
    mutationFn: backupApi.create,
    onSuccess: (r) => {
      const drive = r.drive_uploaded ? '（Drive にもアップロード済み）' : r.drive_error ? `（Drive アップロード失敗: ${r.drive_error}）` : '';
      showToast(`バックアップを作成しました: ${r.name} ${drive}`, r.drive_error ? 'error' : 'success');
      invalidateStatus();
      queryClient.invalidateQueries({ queryKey: ['backup-drive-files'] });
    },
    onError: (err: Error) => showToast(`バックアップ失敗: ${err.message}`, 'error'),
  });

  const restoreMutation = useMutation({
    mutationFn: async (target: RestoreTarget) => {
      if (target.kind === 'local') return backupApi.restore(target.name);
      if (target.kind === 'drive') return backupApi.gdriveRestoreFile(target.id);
      return backupApi.restoreUpload(target.file);
    },
    onSuccess: (r) => {
      showToast(r.message, 'success');
      setRestoreTarget(null);
      // DB 全体が入れ替わったため、キャッシュを全て破棄
      queryClient.invalidateQueries();
    },
    onError: (err: Error) => showToast(`リストア失敗: ${err.message}`, 'error'),
  });

  const deleteMutation = useMutation({
    mutationFn: backupApi.delete,
    onSuccess: () => {
      showToast('バックアップを削除しました', 'success');
      invalidateStatus();
    },
    onError: (err: Error) => showToast(`削除エラー: ${err.message}`, 'error'),
  });

  const handleDownload = async (name: string) => {
    try {
      const blob = await backupApi.downloadBlob(name);
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = name;
      a.click();
      URL.revokeObjectURL(url);
    } catch (e) {
      showToast(`ダウンロード失敗: ${e instanceof Error ? e.message : String(e)}`, 'error');
    }
  };

  const handleUploadRestore = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    e.target.value = ''; // 同じファイルを再選択できるようにリセット
    if (!file) return;
    setRestoreTarget({ kind: 'upload', file });
  };

  const settings = status?.settings;

  return (
    <div className="space-y-6">
      <h1 className="text-3xl font-bold text-gray-900">バックアップ</h1>

      {/* 自動バックアップ設定 */}
      <div className="bg-white rounded-lg shadow-sm border p-6">
        <h2 className="text-xl font-bold text-gray-900 mb-2">自動バックアップ</h2>
        <p className="text-gray-500 mb-4">
          設定した間隔で pg_dump によるバックアップを自動実行し、保持数を超えた古い世代は自動削除します。
          Google Drive 連携中はクラウドにもアップロードされます。
        </p>

        {settings ? (
          <SettingsForm initial={settings} />
        ) : (
          <p className="text-gray-400 text-sm">読み込み中...</p>
        )}

        {settings && (
          <p className="text-sm text-gray-500 mt-4">
            前回のバックアップ: {formatDate(settings.last_backup_at)}
            {settings.last_backup_status && (
              <span className={settings.last_backup_status.startsWith('成功') ? 'text-green-600 ml-2' : 'text-red-600 ml-2'}>
                {settings.last_backup_status}
              </span>
            )}
          </p>
        )}
      </div>

      {/* ローカルバックアップ */}
      <div className="bg-white rounded-lg shadow-sm border p-6">
        <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2 mb-2">
          <h2 className="text-xl font-bold text-gray-900 whitespace-nowrap">ローカルバックアップ</h2>
          <div className="flex flex-wrap gap-2">
            <button
              onClick={() => fileInputRef.current?.click()}
              className="px-3 py-1.5 text-sm text-gray-700 border border-gray-300 rounded-lg hover:bg-gray-50"
              title="ダンプファイル（.dump）をアップロードして復元"
            >
              ファイルから復元
            </button>
            <input
              ref={fileInputRef}
              type="file"
              accept=".dump"
              onChange={handleUploadRestore}
              className="hidden"
            />
            <button
              onClick={() => createMutation.mutate()}
              disabled={createMutation.isPending}
              className="px-4 py-1.5 text-sm text-white font-medium rounded-lg bg-indigo-600 hover:bg-indigo-700 disabled:opacity-50"
            >
              {createMutation.isPending ? 'バックアップ中...' : '今すぐバックアップ'}
            </button>
          </div>
        </div>
        <p className="text-gray-500 mb-4">
          サーバー上（backend/backups/）に保存されたバックアップです。復元前には現在の状態が自動で安全バックアップされます。
        </p>

        {isLoading ? (
          <p className="text-gray-400">読み込み中...</p>
        ) : !status?.backups.length ? (
          <p className="text-sm text-gray-400">バックアップがまだありません</p>
        ) : (
          <div className="divide-y border rounded-lg">
            {status.backups.map((b: BackupFileInfo) => (
              <div key={b.name} className="flex items-center gap-3 px-3 py-2 text-sm">
                <span className="font-mono text-gray-800 truncate">{b.name}</span>
                {b.name.includes('prerestore') && (
                  <span className="px-1.5 py-0.5 text-xs bg-amber-100 text-amber-800 rounded shrink-0" title="復元直前の自動安全バックアップ">
                    復元前
                  </span>
                )}
                <span className="text-gray-400 shrink-0">{formatBytes(b.size)}</span>
                <span className="text-gray-400 shrink-0 hidden sm:inline">{formatDate(b.modified_at)}</span>
                <span className="flex-1" />
                <button
                  onClick={() => handleDownload(b.name)}
                  className="p-1.5 text-indigo-600 hover:bg-indigo-50 rounded"
                  title="ダウンロード"
                >
                  <DownloadIcon />
                </button>
                <button
                  onClick={() => setRestoreTarget({ kind: 'local', name: b.name })}
                  className="p-1.5 text-amber-600 hover:bg-amber-50 rounded"
                  title="このバックアップから復元"
                >
                  <RestoreIcon />
                </button>
                <button
                  onClick={() => deleteMutation.mutate(b.name)}
                  disabled={deleteMutation.isPending}
                  className="p-1.5 text-red-500 hover:bg-red-50 rounded disabled:opacity-50"
                  title="削除"
                >
                  <TrashIcon />
                </button>
              </div>
            ))}
          </div>
        )}
      </div>

      {/* Google Drive 連携 */}
      <GoogleDriveSection onRestoreRequest={setRestoreTarget} />

      {restoreTarget && (
        <ConfirmRestoreModal
          target={restoreTarget}
          busy={restoreMutation.isPending}
          onConfirm={() => restoreMutation.mutate(restoreTarget)}
          onCancel={() => setRestoreTarget(null)}
        />
      )}
    </div>
  );
}
