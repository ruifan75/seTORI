import { Fragment, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link } from 'react-router-dom';
import { holodexApi, batchAnalyzeApi, batchFillApi, singerApi, availabilityApi, autoFillApi } from '../../api/client';
import { useToast } from '../../components/ui/ToastContext';
import { useAuthStore, hasPermission, PERM } from '../../store/auth';
import { formatSeconds } from '../../components/usePerformanceTiming';

// 一括分析のモード定義（バックエンドの BatchMode* と対応）
const BATCH_MODES = [
  {
    value: 'unanalyzed',
    label: '未分析のみ',
    description: '分析結果が一度も無い配信だけを処理（処理済みフラグは問わない）。最も軽い。',
  },
  {
    value: 'unprocessed',
    label: '未処理すべて',
    description: '未処理（ユーザー未確認）の配信をすべて処理。キャッシュ済みは秒で通過。',
  },
  {
    value: 'refresh',
    label: 'コメント再取得',
    description: '未処理配信のコメントを取得し直してから分析。新しいコメントが増えた配信だけ AI が再実行される。',
  },
  {
    value: 'reanalyze',
    label: 'すべて再分析（force）',
    description:
      '対象のすべての配信を最新の解析ロジックで作り直す（分析済みも含む）。誤検出フィルタ強化後に古いキャッシュを更新したいとき用。AI を呼び直すため時間がかかります。',
  },
] as const;

export default function SyncPage() {
  const [channelId, setChannelId] = useState('');
  const [videoId, setVideoId] = useState('');
  const [syncMode, setSyncMode] = useState<'new' | 'all'>('new');
  const [batchMode, setBatchMode] = useState<string>('unanalyzed');
  const [batchSingerId, setBatchSingerId] = useState<string>(''); // '' = 全チャンネル
  // 非表示配信の扱い。既定は従来どおり「除く」──通常運用で雑談・ゲーム配信を
  // 毎回 AI にかけないため。非表示を回すのは抽出規則を変えた後の棚卸しという別の作業。
  const [batchHidden, setBatchHidden] = useState<'all' | 'true' | 'false'>('false');
  const { showToast } = useToast();
  const queryClient = useQueryClient();

  // チャンネル選択用の歌手一覧（名前順）。
  // 一覧で非表示にしたチャンネルも同期対象には出す（隠したいのは一覧の場所だけで、
  // 配信の取り込みまで止めたいわけではないため）。
  const { data: singerList } = useQuery({
    queryKey: ['singers-for-batch'],
    queryFn: () => singerApi.list(1, 300, 'name', 'asc', true),
    staleTime: 5 * 60 * 1000,
  });
  const singers = singerList?.singers ?? [];

  // 一括分析：実行中は 3 秒ごとに進捗をポーリング
  // 一括セットリスト作成（歌唱を直接作るので、プレ分析とは別物）
  const [fillMode, setFillMode] = useState('unprocessed');
  // 対象チャンネルは複数選べる。既定は「そのチャンネルが所有する配信だけ」で、
  // ゲスト参加した他人の配信まで巻き込まないようにしてある。
  const [fillSingerIds, setFillSingerIds] = useState<string[]>([]);
  const [fillIncludeCollabs, setFillIncludeCollabs] = useState(false);
  // 「入力元に無い」の内訳を開いている実行（一度に 1 つ）
  const [openGapRun, setOpenGapRun] = useState<string | null>(null);

  // 再生可否の取得：実行中は 3 秒ごとに進捗をポーリング（一括分析と同じ形）
  const [availConcurrency, setAvailConcurrency] = useState(2);
  const [availRecheck, setAvailRecheck] = useState(false);
  const { data: availStatus } = useQuery({
    queryKey: ['availability-backfill-status'],
    queryFn: availabilityApi.status,
    refetchInterval: (q) => (q.state.data?.running ? 3000 : false),
  });
  const availStart = useMutation({
    mutationFn: () => availabilityApi.backfill(availConcurrency, availRecheck),
    onSuccess: (r) => showToast(`再生可否の取得を開始しました（対象 ${r.targets} 件）`, 'success'),
    onError: (e: Error) => showToast(e.message, 'error'),
    // **失敗しても取り直す。** 二重起動で弾かれたときは、手元の status が古い
    // （running=false）まま polling も止まっているので、toast で「実行中」と言われても
    // 進捗も停止ボタンも出ない。取り直せば running=true を拾って停止できる。
    onSettled: () => queryClient.invalidateQueries({ queryKey: ['availability-backfill-status'] }),
  });
  const availCancel = useMutation({
    mutationFn: availabilityApi.cancel,
    onSuccess: () => {
      showToast('停止を要求しました（実行中のものが終わり次第止まります）', 'success');
      queryClient.invalidateQueries({ queryKey: ['availability-backfill-status'] });
    },
    onError: (e: Error) => showToast(e.message, 'error'),
  });

  const { data: fillStatus } = useQuery({
    queryKey: ['batch-fill-status'],
    queryFn: batchFillApi.status,
    refetchInterval: (q) => (q.state.data?.running ? 3000 : false),
  });
  const { data: fillRuns } = useQuery({
    queryKey: ['batch-fill-runs'],
    queryFn: () => batchFillApi.listRuns(10),
    refetchInterval: fillStatus?.running ? 5000 : false,
  });
  const startFillMutation = useMutation({
    mutationFn: () => batchFillApi.start(fillMode, fillSingerIds, fillIncludeCollabs),
    onSuccess: () => {
      showToast('一括セットリスト作成を開始しました', 'success');
      queryClient.invalidateQueries({ queryKey: ['batch-fill-status'] });
    },
    onError: (err: Error) => showToast(`開始できません: ${err.message}`, 'error'),
  });
  const cancelFillMutation = useMutation({
    mutationFn: batchFillApi.cancel,
    onSuccess: () => showToast('停止を要求しました', 'info'),
  });
  const revertFillMutation = useMutation({
    mutationFn: (runId: string) => batchFillApi.revert(runId),
    onSuccess: (res) => {
      showToast(res.message, 'success');
      queryClient.invalidateQueries({ queryKey: ['batch-fill-runs'] });
    },
    onError: (err: Error) => showToast(`撤回に失敗: ${err.message}`, 'error'),
  });

  const { data: batchStatus } = useQuery({
    queryKey: ['batch-analyze-status'],
    queryFn: batchAnalyzeApi.status,
    refetchInterval: (query) => (query.state.data?.running ? 3000 : false),
  });

  const startBatchMutation = useMutation({
    mutationFn: () => batchAnalyzeApi.start(batchMode, batchSingerId, batchHidden),
    onSuccess: () => {
      showToast('一括分析を開始しました（バックグラウンドで実行されます）', 'success');
      queryClient.invalidateQueries({ queryKey: ['batch-analyze-status'] });
    },
    onError: (err: Error) => showToast(err.message, 'error'),
  });

  const cancelBatchMutation = useMutation({
    mutationFn: batchAnalyzeApi.cancel,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['batch-analyze-status'] }),
  });

  const syncChannelMutation = useMutation({
    mutationFn: () => holodexApi.syncChannel({ 
      channel_id: channelId,
      force_update: syncMode === 'all'
    }),
    onSuccess: (data) => {
      const message = data.message || `同期完了: ${data.synced_count}件 (新規: ${data.new_streams.length}, 更新: ${data.updated.length})`;
      showToast(message, 'success');
    },
    onError: (err: Error) => {
      showToast(`同期エラー: ${err.message}`, 'error');
    },
  });

  const syncVideoMutation = useMutation({
    mutationFn: () => holodexApi.syncVideo(videoId),
    onSuccess: () => {
      showToast('動画の同期が完了しました', 'success');
    },
    onError: (err: Error) => {
      showToast(`同期エラー: ${err.message}`, 'error');
    },
  });

  const handleSyncChannel = (e: React.FormEvent) => {
    e.preventDefault();
    if (channelId.trim()) {
      syncChannelMutation.mutate();
    }
  };

  const handleSyncVideo = (e: React.FormEvent) => {
    e.preventDefault();
    if (videoId.trim()) {
      syncVideoMutation.mutate();
    }
  };

  return (
    <div className="space-y-6">
      <h1 className="text-3xl font-bold text-gray-900">Holodex 同期</h1>

      <AutoFillTargets />
      <AutoFillSchedule />

      {/* Sync by Channel */}
      <div className="bg-white rounded-lg shadow-sm border p-6">
        <h2 className="text-xl font-bold text-gray-900 mb-4">チャンネルから同期</h2>
        <p className="text-gray-500 mb-4">
          YouTube Channel ID を入力して、そのチャンネルの歌枠を同期します。
        </p>

        <form onSubmit={handleSyncChannel} className="space-y-4">
          <div>
            <label htmlFor="channelId" className="block text-sm font-medium text-gray-700 mb-1">
              Channel ID
            </label>
            <input
              type="text"
              id="channelId"
              value={channelId}
              onChange={(e) => setChannelId(e.target.value)}
              placeholder="UCeqIMtLuGc3YgwkhEaG8oDg"
              className="w-full px-4 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
            />
          </div>

          <div>
            <label htmlFor="syncMode" className="block text-sm font-medium text-gray-700 mb-1">
              同期モード
            </label>
            <select
              id="syncMode"
              value={syncMode}
              onChange={(e) => setSyncMode(e.target.value as 'new' | 'all')}
              className="w-full px-4 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
            >
              <option value="new">新しい動画のみ同期</option>
              <option value="all">すべての動画を同期（更新を含む）</option>
            </select>
          </div>

          <button
            type="submit"
            disabled={syncChannelMutation.isPending || !channelId.trim()}
            className="px-4 py-2 bg-indigo-600 text-white font-medium rounded-lg hover:bg-indigo-700 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
          >
            {syncChannelMutation.isPending ? '同期中...' : '同期開始'}
          </button>
        </form>

        {/* 進捗：API が同期処理のため、リアルタイムの進捗は表示できない */}
        {syncChannelMutation.isPending && (
          <div className="mt-4 p-4 bg-blue-50 border border-blue-200 rounded-lg">
            <h3 className="font-medium text-blue-800 mb-2">同期中...</h3>
            <p className="text-sm text-blue-700">
              データを同期しています。しばらくお待ちください...
            </p>
            <div className="mt-2 w-full bg-blue-200 rounded-full h-2">
              <div className="bg-blue-600 h-2 rounded-full animate-pulse w-full" />
            </div>
          </div>
        )}

        {/* Result */}
        {syncChannelMutation.isSuccess && (
          <div className="mt-4 p-4 bg-green-50 border border-green-200 rounded-lg">
            <h3 className="font-medium text-green-800 mb-2">同期完了</h3>
            <ul className="text-sm text-green-700 space-y-1">
              <li>同期数: {syncChannelMutation.data.synced_count}</li>
              <li>新規: {syncChannelMutation.data.new_streams.length}</li>
              <li>更新: {syncChannelMutation.data.updated.length}</li>
              <li>スキップ: {syncChannelMutation.data.skipped.length}</li>
              {syncChannelMutation.data.message && (
                <li className="mt-2 pt-2 border-t border-green-300">{syncChannelMutation.data.message}</li>
              )}
            </ul>
          </div>
        )}

      </div>

      {/* Sync by Video */}
      <div className="bg-white rounded-lg shadow-sm border p-6">
        <h2 className="text-xl font-bold text-gray-900 mb-4">動画から同期</h2>
        <p className="text-gray-500 mb-4">
          YouTube Video ID を入力して、その動画を同期します。
        </p>

        <form onSubmit={handleSyncVideo} className="space-y-4">
          <div>
            <label htmlFor="videoId" className="block text-sm font-medium text-gray-700 mb-1">
              Video ID
            </label>
            <input
              type="text"
              id="videoId"
              value={videoId}
              onChange={(e) => setVideoId(e.target.value)}
              placeholder="vak2WG1TomU"
              className="w-full px-4 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
            />
          </div>

          <button
            type="submit"
            disabled={syncVideoMutation.isPending || !videoId.trim()}
            className="px-4 py-2 bg-indigo-600 text-white font-medium rounded-lg hover:bg-indigo-700 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
          >
            {syncVideoMutation.isPending ? '同期中...' : '同期開始'}
          </button>
        </form>

        {/* Result */}
        {syncVideoMutation.isSuccess && (
          <div className="mt-4 p-4 bg-green-50 border border-green-200 rounded-lg">
            <h3 className="font-medium text-green-800 mb-2">同期完了</h3>
            <ul className="text-sm text-green-700 space-y-1">
              <li>同期数: {syncVideoMutation.data.synced_count}</li>
              {syncVideoMutation.data.new_streams.length > 0 && <li>新規追加されました</li>}
              {syncVideoMutation.data.updated.length > 0 && <li>更新されました</li>}
            </ul>
          </div>
        )}

      </div>

      {/* Batch pre-analysis */}
      <div className="bg-white rounded-lg shadow-sm border p-6">
        <h2 className="text-xl font-bold text-gray-900 mb-2">配信の一括プレ分析</h2>
        <p className="text-gray-500 mb-4">
          コメントの抽出 → AI 正規化 → 拍手 end 検出をまとめて実行し、結果をキャッシュします
          （setlist の自動作成は行いません。確認・保存は編集画面で行います）。
          AI プロバイダーの冷却・レート制限には自動で対応します。
        </p>

        {/* モード選択 */}
        <div className="space-y-2 mb-4">
          {BATCH_MODES.map((m) => (
            <label
              key={m.value}
              className={`flex items-start gap-3 p-3 border rounded-lg cursor-pointer transition-colors ${
                batchMode === m.value ? 'border-indigo-400 bg-indigo-50' : 'border-gray-200 hover:border-gray-300'
              }`}
            >
              <input
                type="radio"
                name="batchMode"
                value={m.value}
                checked={batchMode === m.value}
                onChange={() => setBatchMode(m.value)}
                className="mt-1 accent-indigo-600"
                disabled={batchStatus?.running}
              />
              <span>
                <span className="block text-sm font-medium text-gray-900">{m.label}</span>
                <span className="block text-xs text-gray-500">{m.description}</span>
              </span>
            </label>
          ))}
        </div>

        {/* 対象チャンネルの絞り込み */}
        <div className="mb-4">
          <label htmlFor="batch-singer" className="block text-sm font-medium text-gray-900 mb-1">
            対象チャンネル
          </label>
          <select
            id="batch-singer"
            value={batchSingerId}
            onChange={(e) => setBatchSingerId(e.target.value)}
            disabled={batchStatus?.running}
            className="w-full max-w-md px-3 py-2 text-sm border border-gray-300 rounded-lg bg-white focus:border-indigo-400 focus:ring-1 focus:ring-indigo-400 disabled:opacity-50"
          >
            <option value="">すべてのチャンネル</option>
            {singers.map((sg) => (
              <option key={sg.id} value={sg.id}>
                {sg.name}
              </option>
            ))}
          </select>
          <p className="mt-1 text-xs text-gray-500">
            選んだチャンネルが参加した配信だけを対象にします（オーナー／コラボ参加どちらも含む）。
          </p>
        </div>

        {/* 非表示配信の扱い */}
        <div className="mb-4">
          <label htmlFor="batch-hidden" className="block text-sm font-medium text-gray-900 mb-1">
            非表示の配信
          </label>
          <select
            id="batch-hidden"
            value={batchHidden}
            onChange={(e) => setBatchHidden(e.target.value as 'all' | 'true' | 'false')}
            disabled={batchStatus?.running}
            className="w-full max-w-md px-3 py-2 text-sm border border-gray-300 rounded-lg bg-white focus:border-indigo-400 focus:ring-1 focus:ring-indigo-400 disabled:opacity-50"
          >
            <option value="false">対象にしない（既定）</option>
            <option value="true">非表示だけを対象にする</option>
            <option value="all">両方を対象にする</option>
          </select>
          <p className="mt-1 text-xs text-gray-500">
            非表示は雑談・ゲーム配信が大半なので通常は対象外です。抽出の規則を変えたあと、
            誤って非表示にした歌枠が無いか棚卸しするときだけ「非表示だけ」を選びます
            （結果は抽出（comment_songs）に入るだけで、歌唱記録は作られません）。
          </p>
        </div>

        {batchStatus?.running ? (
          <div className="rounded-lg border border-indigo-200 bg-indigo-50 p-3 text-sm flex flex-wrap items-center gap-3">
            <svg className="animate-spin h-4 w-4 text-indigo-500 shrink-0" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
              <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
              <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
            </svg>
            <span className="font-medium text-indigo-700">
              一括分析中（{BATCH_MODES.find((m) => m.value === batchStatus.mode)?.label ?? batchStatus.mode}
              {batchStatus.hidden === 'true' ? ' / 非表示だけ' : batchStatus.hidden === 'all' ? ' / 非表示も含む' : ''}
              {batchStatus.singer_id
                ? ` / ${singers.find((sg) => sg.id === batchStatus.singer_id)?.name ?? batchStatus.singer_id}`
                : ' / 全チャンネル'}
              ）{' '}
              {batchStatus.done + batchStatus.failed + (batchStatus.deferred ?? 0)}/{batchStatus.total}
            </span>
            {batchStatus.current && <span className="text-gray-500 truncate max-w-md">{batchStatus.current}</span>}
            <button
              onClick={() => cancelBatchMutation.mutate()}
              className="ml-auto text-xs text-gray-500 hover:text-gray-800 underline shrink-0"
            >
              キャンセル
            </button>
          </div>
        ) : (
          <div className="flex flex-wrap items-center gap-3">
            <button
              onClick={() => startBatchMutation.mutate()}
              disabled={startBatchMutation.isPending}
              className="px-4 py-2 text-sm bg-indigo-600 text-white font-medium rounded-lg hover:bg-indigo-700 transition-colors disabled:opacity-50"
            >
              一括分析を開始
            </button>
            {batchStatus && batchStatus.total > 0 && (
              <span className="text-sm text-gray-500">
                前回: {batchStatus.message}（成功 {batchStatus.done} 件
                {batchStatus.failed > 0 && `・失敗 ${batchStatus.failed} 件`}
                {/* 見送りは失敗ではない。次の実行で拾われることまで書かないと
                    「取りこぼした」と読まれる */}
                {(batchStatus.deferred ?? 0) > 0 &&
                  `・live chat 待ちで見送り ${batchStatus.deferred} 件（次回やり直します）`}
                ）
                {batchStatus.failed > 0 && batchStatus.failed_ids && (
                  <span className="text-xs text-gray-400" title={batchStatus.failed_ids.join(', ')}>
                    {' '}（{batchStatus.failed_ids.slice(0, 3).join(', ')}
                    {batchStatus.failed_ids.length > 3 ? ' …' : ''}）
                  </span>
                )}
              </span>
            )}
          </div>
        )}
      </div>

      {/* 再生可否の取得（会限・削除済みの判定材料。issue #3） */}
      <div className="bg-white rounded-lg shadow-sm border p-6">
        <h2 className="text-xl font-bold text-gray-900 mb-2">再生可否の取得</h2>
        <p className="text-gray-500 mb-4">
          yt-dlp で <code className="text-xs bg-gray-100 px-1 rounded">availability</code> を調べ、
          会限（メンバー限定）や削除済みの配信を判別します。判別できると、プレイヤーを描く前に
          理由を出せるようになります。
        </p>
        <p className="text-gray-500 mb-4 text-sm">
          既定は<strong>未記録の配信だけ</strong>が対象なので、途中で止めても記録済みのぶんは残り、
          そのまま続きから再開できます。
          <br />
          {/* recheck には checkpoint が無い。WHERE が
              「未記録 OR (public かつ埋め込み可)」なので、記録済みでも毎回対象に戻る。
              止めて再開すると先頭から掛け直すことになる。 */}
          <span className="text-amber-800">
            再調査（下のチェック）を使うときは続きから再開できません
          </span>
          ── 記録済みの弱い判定も毎回対象に戻るので、止めて再開すると先頭からやり直しになります。
        </p>

        {/* **実行中は触らせない。** backend が使う値は開始時に固定される一方、
            status はそれを返さないので、ここの表示は「走っている条件」ではない
            （再読み込みすると既定値に戻る）。編集できると現在の条件と読めてしまう。 */}
        <div className="flex flex-wrap items-center gap-3 mb-4">
          {availStatus?.running && (
            <span className="w-full text-xs text-gray-500">
              実行中は変更できません（下の値は次回の設定で、いま走っている条件とは限りません）
            </span>
          )}
          <label className="flex items-center gap-2 text-sm">
            並列
            <input
              type="number"
              min={1}
              max={8}
              value={availConcurrency}
              disabled={availStatus?.running}
              onChange={(e) => setAvailConcurrency(Math.max(1, Math.min(8, Number(e.target.value) || 1)))}
              className="w-16 px-2 py-1 border rounded disabled:bg-gray-100 disabled:text-gray-400"
            />
          </label>
          <label className="flex items-center gap-2 text-sm cursor-pointer">
            <input
              type="checkbox"
              checked={availRecheck}
              disabled={availStatus?.running}
              onChange={(e) => setAvailRecheck(e.target.checked)}
              className="w-4 h-4"
            />
            調査済みの弱い判定も対象にする
            <span className="text-xs text-gray-500">
              （<code className="bg-gray-100 px-1 rounded">public</code> は「反証が無かった」という結論なので、会限を取りこぼしている可能性がある）
            </span>
          </label>
        </div>

        <div className="flex items-center gap-3">
          <button
            type="button"
            onClick={() => availStart.mutate()}
            disabled={availStatus?.running || availStart.isPending}
            className="px-4 py-2 bg-indigo-600 text-white rounded-lg hover:bg-indigo-700 disabled:opacity-50"
          >
            {availStatus?.running ? '実行中…' : '取得を開始'}
          </button>
          {availStatus?.running && (
            <button
              type="button"
              onClick={() => availCancel.mutate()}
              disabled={availCancel.isPending}
              className="px-4 py-2 border border-red-300 text-red-700 rounded-lg hover:bg-red-50 disabled:opacity-50"
            >
              停止
            </button>
          )}
        </div>

        {availStatus && availStatus.total > 0 && (
          <div className="mt-4 space-y-2">
            <div className="h-2 bg-gray-100 rounded overflow-hidden">
              <div
                className="h-full bg-indigo-500 transition-all"
                style={{ width: `${Math.round((availStatus.done / availStatus.total) * 100)}%` }}
              />
            </div>
            <div className="flex flex-wrap gap-x-4 gap-y-1 text-sm text-gray-700">
              <span>{availStatus.done} / {availStatus.total}</span>
              <span className="text-green-700">記録 {availStatus.saved}</span>
              {/* failed は「再試行が要る」件数。error の有無ではない（動画が消えていた場合は記録済み＝saved） */}
              <span className={availStatus.failed > 0 ? 'text-amber-700 font-medium' : 'text-gray-500'}>
                未記録 {availStatus.failed}
              </span>
              {availStatus.cancelled && <span className="text-gray-500">（停止しました）</span>}
            </div>

            {/* **最後のエラーを出すのが要点。** 1300 件が同じ理由で失敗しているとき、
                log を見に行かないと気付けなかった（cookie 失効など）。 */}
            {availStatus.last_error && (
              <div className="text-xs bg-amber-50 border border-amber-200 text-amber-900 rounded p-2">
                <div className="font-medium mb-0.5">最後のエラー</div>
                <div className="break-all">{availStatus.last_error}</div>
                {availStatus.failed >= 5 && (
                  <div className="mt-1">
                    同じ理由が続いている場合は、配信ごとの問題ではなく設定の問題かもしれません
                    （管理→設定の YouTube cookie の失効など）。停止して確認してください。
                  </div>
                )}
              </div>
            )}
          </div>
        )}
      </div>


      {/* 一括セットリスト作成 */}
      <div className="bg-white rounded-lg shadow p-6">
        <h2 className="text-xl font-bold text-gray-900 mb-2">一括セットリスト作成</h2>
        <p className="text-sm text-gray-500 mb-4">
          源（Holodex 優先、無ければコメント）から歌唱を自動で作ります。
          <span className="font-medium text-gray-700">上のプレ分析と違い、歌唱（performances）に直接書き込みます。</span>
          決めきれないものは人の審査（修正提案）へ回り、実行単位でまとめて撤回できます。
        </p>

        <div className="flex flex-wrap items-end gap-3 mb-4">
          <label className="text-sm">
            <span className="block text-gray-700 mb-1">対象</span>
            <select
              value={fillMode}
              onChange={(e) => setFillMode(e.target.value)}
              disabled={fillStatus?.running}
              className="border border-gray-300 rounded-lg px-3 py-2"
            >
              <option value="unprocessed">歌唱がまだ無い配信だけ</option>
              <option value="force">入力元を持つ配信すべて（既存と違う分は審査へ）</option>
            </select>
          </label>
          <label className="text-sm">
            <span className="block text-gray-700 mb-1">
              チャンネル
              <span className="ml-1 text-xs text-gray-400">（Ctrl / ⌘ で複数選択・未選択なら全部）</span>
            </span>
            <select
              multiple
              size={5}
              value={fillSingerIds}
              onChange={(e) =>
                setFillSingerIds(Array.from(e.target.selectedOptions, (o) => o.value))
              }
              disabled={fillStatus?.running}
              className="border border-gray-300 rounded-lg px-3 py-2 min-w-56"
            >
              {singers.map((sg) => (
                <option key={sg.id} value={sg.id}>{sg.name}</option>
              ))}
            </select>
          </label>
          <label className="text-sm flex items-center gap-2 pb-2" title="既定では、選んだチャンネルが所有する配信だけを対象にします">
            <input
              type="checkbox"
              checked={fillIncludeCollabs}
              onChange={(e) => setFillIncludeCollabs(e.target.checked)}
              disabled={fillStatus?.running || fillSingerIds.length === 0}
              className="accent-indigo-600"
            />
            <span className={fillSingerIds.length === 0 ? 'text-gray-400' : 'text-gray-700'}>
              ゲスト参加した配信も含む
            </span>
          </label>
          {fillStatus?.running ? (
            <button
              onClick={() => cancelFillMutation.mutate()}
              className="px-4 py-2 rounded-lg bg-red-50 text-red-700 border border-red-200 hover:bg-red-100"
            >
              停止
            </button>
          ) : (
            <button
              onClick={() => startFillMutation.mutate()}
              disabled={startFillMutation.isPending}
              className="px-4 py-2 rounded-lg bg-indigo-600 text-white hover:bg-indigo-700 disabled:opacity-50"
            >
              自動で埋める
            </button>
          )}
        </div>

        {fillStatus?.running && (
          <div className="mb-4 rounded-lg bg-indigo-50 border border-indigo-100 p-3 text-sm">
            <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
              <span className="font-medium text-indigo-900">
                {/* 段によって待ち時間の意味が違うので、どこに居るかを出す */}
                {fillStatus.phase === 'ai'
                  ? 'AI に判定を問い合わせ中'
                  : fillStatus.phase === 'write'
                    ? '歌唱を作成中'
                    : '配信を読み込み中'}
              </span>
              <span className="text-indigo-700">{fillStatus.done}/{fillStatus.total}</span>
              {fillStatus.current && (
                <span className="text-gray-500 truncate max-w-xs">{fillStatus.current}</span>
              )}
              <span className="text-gray-600">
                作成 {fillStatus.created} ／ 審査 {fillStatus.review}
                {fillStatus.ai_asked > 0 && ` ／ AI ${fillStatus.ai_asked} 行`}
              </span>
            </div>
          </div>
        )}

        {/* 実行の履歴。撤回はここから */}
        {(fillRuns?.runs?.length ?? 0) > 0 && (
          <div className="overflow-x-auto">
            <table className="min-w-full text-sm">
              <thead>
                <tr className="text-left text-gray-500 border-b">
                  <th className="py-2 pr-3">実行</th>
                  <th className="py-2 pr-3">対象</th>
                  <th className="py-2 pr-3">作成</th>
                  <th className="py-2 pr-3">審査</th>
                  <th className="py-2 pr-3" title="DB にあるが、今回の入力元には出てこなかった歌唱">
                    入力元に無い
                  </th>
                  <th className="py-2 pr-3">状態</th>
                  <th className="py-2"></th>
                </tr>
              </thead>
              <tbody>
                {fillRuns!.runs.map((run) => (
                  <Fragment key={run.id}>
                    <tr className="border-b last:border-0">
                      <td className="py-2 pr-3 whitespace-nowrap text-gray-600">
                        {new Date(run.started_at).toLocaleString('ja-JP', {
                          month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false,
                        })}
                        {run.started_by_name && <span className="ml-1 text-gray-400">{run.started_by_name}</span>}
                      </td>
                      <td className="py-2 pr-3 text-gray-600">
                        {run.mode === 'force' ? 'すべて' : '歌唱なし'}
                        {run.singer_id && (
                          <span className="ml-1 text-gray-400">
                            {/* 複数チャンネルはカンマ区切りで記録されている */}
                            {run.singer_id
                              .split(',')
                              .map((id) => singers.find((sg) => sg.id === id)?.name ?? id)
                              .join('・')}
                          </span>
                        )}
                      </td>
                      <td className="py-2 pr-3 font-medium text-gray-800">{run.songs_created}</td>
                      <td className="py-2 pr-3 text-amber-700">{run.songs_review}</td>
                      <td className="py-2 pr-3">
                        {run.songs_gap > 0 ? (
                          <button
                            onClick={() => setOpenGapRun(openGapRun === run.id ? null : run.id)}
                            className="text-gray-600 underline hover:text-gray-900"
                            title="DB にあるが、今回の入力元には出てこなかった歌唱を一覧する"
                          >
                            {run.songs_gap}
                          </button>
                        ) : (
                          <span className="text-gray-300">—</span>
                        )}
                      </td>
                      <td className="py-2 pr-3 text-gray-500" title={run.message}>
                        {{ running: '実行中', done: '完了', cancelled: '中止', failed: '失敗', reverted: '撤回済み' }[run.status]}
                      </td>
                      <td className="py-2 text-right">
                        {run.songs_created > 0 && run.status !== 'reverted' && (
                          <button
                            onClick={() => revertFillMutation.mutate(run.id)}
                            disabled={revertFillMutation.isPending}
                            title="この実行が作った歌唱をまとめて削除します"
                            className="text-red-600 hover:text-red-800 disabled:opacity-50"
                          >
                            撤回
                          </button>
                        )}
                      </td>
                    </tr>
                    {openGapRun === run.id && (
                      <tr className="border-b last:border-0">
                        <td colSpan={7} className="py-2 pr-3 bg-gray-50">
                          <GapList runId={run.id} />
                        </td>
                      </tr>
                    )}
                  </Fragment>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  );
}

// GapList は「DB にあるが、その実行の入力元には出てこなかった」歌唱を並べる。
//
// **これらは審査待ちとして積んでいない。** 源（Holodex のセットリストもコメントも）は
// 欠けているのが普通なので、欠落 1 件ごとに待ち行列を作ると人が処理できない量になり、
// しかも「入力元に無い」だけでは何をすべきか決まらない（消すべきとは限らない）。
// 気付けるようにはしておきたいので、実行履歴から辿れる形にだけしてある。
function GapList({ runId }: { runId: string }) {
  const { data, isLoading } = useQuery({
    queryKey: ['batch-fill-gaps', runId],
    queryFn: () => batchFillApi.gaps(runId),
  });

  if (isLoading) return <p className="text-xs text-gray-400">読み込み中…</p>;
  const gaps = data?.gaps ?? [];
  if (gaps.length === 0) {
    // 実行後に歌唱が消えていれば記録も消える（CASCADE）ので、件数と合わないことがある
    return <p className="text-xs text-gray-400">該当する歌唱は残っていません</p>;
  }

  // 配信ごとにまとめる（1 配信に何曲も落ちることが多い）
  const byStream = new Map<string, typeof gaps>();
  for (const g of gaps) {
    const list = byStream.get(g.stream_id) ?? [];
    list.push(g);
    byStream.set(g.stream_id, list);
  }

  return (
    <div className="space-y-2">
      <p className="text-xs text-gray-500">
        すでに登録されている歌唱のうち、この実行で読んだ入力元には出てこなかったものです。
        入力元の取りこぼしのことも、登録が誤っていることもあるので、自動では何もしていません。
      </p>
      {[...byStream.entries()].map(([streamId, list]) => (
        <div key={streamId} className="text-xs">
          <Link to={`/streams/${streamId}`} className="text-indigo-600 hover:text-indigo-900">
            {list[0].stream_title || streamId}
          </Link>
          <ul className="mt-0.5 ml-3 flex flex-wrap gap-x-3 gap-y-0.5 text-gray-600">
            {list.map((g) => (
              <li key={g.performance_id}>
                <span className="font-mono text-gray-400">{formatSeconds(g.start_seconds)}</span>{' '}
                {g.song_name}
              </li>
            ))}
          </ul>
        </div>
      ))}
    </div>
  );
}


// AutoFillTargets は自動処理が有効なチャンネルの一覧。
//
// **1 か所で見えて、ここから外せること**が目的。登録はチャンネルページから
// 個別にやるが、「今どれが自動で動いているか」を知るのに 148 件を見て回るのでは
// 運用にならない。0 件のときも節ごと消さずに「無効」と出す ── 消すと
// 「そんな仕組みは無い」と読めてしまう。
function AutoFillTargets() {
  const queryClient = useQueryClient();
  const { showToast } = useToast();
  // **この画面は sync:run で開けるが、この API は content:edit を要求する。**
  // 権限で中身が変わるどころか、権限が無ければ 403 になる。
  const canEdit = hasPermission(useAuthStore((st) => st.user), PERM.CONTENT_EDIT);
  const authStatus = useAuthStore((st) => st.status);

  const { data, isLoading, isError, error } = useQuery({
    // **権限を鍵に入れる。** ログアウトしても QueryClient は消えないので、
    // 固定の鍵だと content:edit の利用者が取った一覧が、5 分以内に
    // ログインした sync:run だけの利用者に見えてしまう
    // （応答には会限の方針と本数も入っている）。
    queryKey: ['autoFillTargets', canEdit],
    queryFn: singerApi.listAutoFill,
    enabled: canEdit && authStatus !== 'loading',
  });

  const stop = useMutation({
    mutationFn: (id: string) => singerApi.setAutoFill(id, false),
    onSuccess: (_d, id) => {
      queryClient.invalidateQueries({ queryKey: ['autoFillTargets'] }); // prefix 一致で権限別の鍵も拾う
      queryClient.invalidateQueries({ queryKey: ['singer', id] });
      showToast('自動処理の対象から外しました', 'success');
    },
    onError: (err: Error) => showToast(err.message, 'error'),
  });

  const targets = data?.singers ?? [];

  // 権限が無いなら節ごと出さない（取得もしない）。
  if (!canEdit) return null;

  return (
    <div className="bg-white rounded-lg shadow-sm border p-6">
      <h2 className="text-xl font-bold text-gray-900 mb-2">自動処理の対象</h2>
      {/* **今は旗を保存するだけ**で、定期実行はまだ入っていない（issue #35 の ③）。
          「取り込みます」と書くと、有効にしたのに何も起きないのを
          正常稼働と誤認させる */}
      <p className="text-gray-500 mb-4 text-sm">
        ここに登録したチャンネルが、将来の自動処理（定期同期 → コメント解析 → 歌単作成）の
        対象になります。<strong>定期実行はまだ動いていません</strong>（登録だけ先にできます）。
        動き出したあとも、確信の無いものと未登録の曲は<strong>審査へ回り</strong>、
        <strong>処理完了のチェックは自動では付きません</strong>。
        登録は各チャンネルのページから。
      </p>

      {isLoading ? (
        <p className="text-gray-400 text-sm">読み込み中...</p>
      ) : isError ? (
        // **取得失敗を「対象なし」と言わない。** 運用の設定なので、
        // 「全部無効」と読めてしまうと止まっているのか壊れているのか分からない
        <p className="text-red-600 text-sm">
          対象の取得に失敗しました（{(error as Error)?.message ?? '不明なエラー'}）。
          一覧が空という意味ではありません。
        </p>
      ) : targets.length === 0 ? (
        <p className="text-gray-400 text-sm">
          対象はありません。チャンネルページの「自動処理」から登録できます。
        </p>
      ) : (
        <ul className="divide-y border rounded-lg">
          {targets.map((sg) => (
            <li key={sg.id} className="flex items-center justify-between gap-3 px-4 py-2">
              <Link to={`/singers/${sg.id}`} className="text-indigo-600 hover:underline truncate">
                {sg.name}
              </Link>
              <button
                onClick={() => stop.mutate(sg.id)}
                disabled={stop.isPending}
                title="このチャンネルを自動処理の対象から外す"
                className="shrink-0 px-2 py-1 text-xs text-gray-600 border border-gray-300 rounded-full hover:text-red-600 hover:border-red-300 disabled:opacity-50"
              >
                外す
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}


// AutoFillSchedule は自動処理の定期実行の設定と手動実行。
//
// **既定は無効。** 外部 API と AI を自動で叩くので、設定しない限り動かない。
// 手動実行は設定が無効でも走る ── 有効にする前に何が起きるか確かめられないと、
// いきなり自動で回すことになる。
function AutoFillSchedule() {
  const queryClient = useQueryClient();
  const { showToast } = useToast();
  const canEdit = hasPermission(useAuthStore((st) => st.user), PERM.CONTENT_EDIT);
  const authStatus = useAuthStore((st) => st.status);

  const { data: settings, isError } = useQuery({
    queryKey: ['autoFillSettings', canEdit],
    queryFn: autoFillApi.getSettings,
    enabled: canEdit && authStatus !== 'loading',
  });

  const [interval, setInterval] = useState<number | null>(null);
  const [refreshDays, setRefreshDays] = useState<number | null>(null);

  const save = useMutation({
    mutationFn: (next: { enabled: boolean; interval: number; refreshDays: number }) =>
      autoFillApi.updateSettings(next.enabled, next.interval, next.refreshDays),
    onSuccess: (data) => {
      queryClient.setQueryData(['autoFillSettings', canEdit], data);
      showToast(data.enabled ? '自動処理を有効にしました' : '自動処理を止めました', 'success');
    },
    onError: (err: Error) => showToast(err.message, 'error'),
  });

  const runNow = useMutation({
    mutationFn: autoFillApi.run,
    onSuccess: (res) => {
      queryClient.invalidateQueries({ queryKey: ['autoFillSettings'] });
      queryClient.invalidateQueries({ queryKey: ['batchFillStatus'] });
      showToast(
        `同期 ${res.synced} 件 / コメント取り直し ${res.refreshed} 件` +
          (res.note ? `（${res.note}）` : ''),
        'success',
      );
    },
    // 実行中（409）も含めてそのまま出す。「既に走っている」は失敗ではないので
    // 文言はバックエンドのものを見せる
    onError: (err: Error) => showToast(err.message, 'error'),
  });

  if (!canEdit) return null;

  const effInterval = interval ?? settings?.interval_hours ?? 6;
  const effRefresh = refreshDays ?? settings?.refresh_days ?? 30;

  return (
    <div className="bg-white rounded-lg shadow-sm border p-6">
      <h2 className="text-xl font-bold text-gray-900 mb-2">自動処理の定期実行</h2>
      <p className="text-gray-500 mb-4 text-sm">
        上で登録したチャンネルを定期的に処理します：
        <strong>同期 → 歌単が空の配信のコメント取り直し → 歌単作成</strong>。
        確信の無いものと未登録の曲は<strong>審査へ回り</strong>、
        <strong>処理完了のチェックは自動では付きません</strong>。
      </p>

      {isError ? (
        <p className="text-red-600 text-sm">設定の取得に失敗しました（無効という意味ではありません）。</p>
      ) : (
        <>
          <div className="flex flex-wrap items-end gap-4">
            <label className="flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                checked={settings?.enabled ?? false}
                onChange={(e) =>
                  save.mutate({ enabled: e.target.checked, interval: effInterval, refreshDays: effRefresh })
                }
                disabled={save.isPending}
                className="w-4 h-4 rounded border-gray-300"
              />
              定期実行を有効にする
            </label>

            <label className="flex items-center gap-2 text-sm">
              実行間隔
              <input
                type="number"
                min={1}
                max={168}
                value={effInterval}
                onChange={(e) => setInterval(Number(e.target.value))}
                className="w-20 px-2 py-1 border border-gray-300 rounded"
              />
              時間
            </label>

            <label className="flex items-center gap-2 text-sm">
              コメント取り直しの範囲
              <input
                type="number"
                min={1}
                max={365}
                value={effRefresh}
                onChange={(e) => setRefreshDays(Number(e.target.value))}
                className="w-20 px-2 py-1 border border-gray-300 rounded"
              />
              日以内
            </label>

            <button
              onClick={() =>
                save.mutate({ enabled: settings?.enabled ?? false, interval: effInterval, refreshDays: effRefresh })
              }
              disabled={save.isPending}
              className="px-3 py-1.5 text-sm bg-indigo-600 text-white rounded-lg hover:bg-indigo-700 disabled:opacity-50"
            >
              保存
            </button>

            <button
              onClick={() => runNow.mutate()}
              disabled={runNow.isPending}
              title="設定が無効でも 1 回だけ走らせます（有効にする前の確認用）"
              className="px-3 py-1.5 text-sm border border-gray-300 rounded-lg hover:border-indigo-300 disabled:opacity-50"
            >
              {runNow.isPending ? '実行中...' : '今すぐ 1 回実行'}
            </button>
          </div>

          {/* **間隔を短くする側の代償を書いておく。** live chat がまだ無い配信は
              次の実行でやり直すので、間隔が短いほど同じ配信を何度も触る */}
          <p className="text-xs text-gray-400 mt-3">
            間隔を短くすると、配信直後で live chat がまだ取得できない配信を何度も処理し直します。
          </p>

          {settings?.last_run_at && (
            <p className="text-sm text-gray-500 mt-3">
              前回: {new Date(settings.last_run_at).toLocaleString('ja-JP')}
              {settings.last_run_note && `（${settings.last_run_note}）`}
              {settings.last_run_error && (
                <span className="text-red-600"> エラー: {settings.last_run_error}</span>
              )}
            </p>
          )}
        </>
      )}
    </div>
  );
}
