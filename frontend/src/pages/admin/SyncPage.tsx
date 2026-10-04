import { invalidateTaskResults, TASK_LABELS, TASK_PHASE_LABELS } from '../../utils/taskResults';
import { Fragment, useEffect, useRef, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link } from 'react-router-dom';
import { holodexApi, batchAnalyzeApi, batchFillApi, channelApi, autoFillApi, nonSingingApi, restrictionReviewApi, streamApi, taskApi } from '../../api/client';
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
  const [batchChannelId, setBatchChannelId] = useState<string>(''); // '' = 全チャンネル
  // 非表示配信の扱い。既定は従来どおり「除く」──通常運用で雑談・ゲーム配信を
  // 毎回 AI にかけないため。非表示を回すのは抽出規則を変えた後の棚卸しという別の作業。
  const [batchHidden, setBatchHidden] = useState<'all' | 'true' | 'false'>('false');
  const { showToast } = useToast();
  const queryClient = useQueryClient();

  // 選択用のチャンネル一覧（名前順）。
  // 一覧で非表示にしたチャンネルも同期対象には出す（隠したいのは一覧の場所だけで、
  // 配信の取り込みまで止めたいわけではないため）。
  const { data: channelList } = useQuery({
    queryKey: ['singers-for-batch'],
    queryFn: () => channelApi.list(1, 300, 'name', 'asc', true),
    staleTime: 5 * 60 * 1000,
  });
  const channels = channelList?.singers ?? [];

  // 一括分析：実行中は 3 秒ごとに進捗をポーリング
  // 一括セットリスト作成（歌唱を直接作るので、プレ分析とは別物）
  const [fillMode, setFillMode] = useState('unprocessed');
  // 対象チャンネルは複数選べる。既定は「そのチャンネルが所有する配信だけ」で、
  // ゲスト参加した他人の配信まで巻き込まないようにしてある。
  const [fillChannelIds, setFillChannelIds] = useState<string[]>([]);
  const [fillIncludeCollabs, setFillIncludeCollabs] = useState(false);
  // 「入力元に無い」の内訳を開いている実行（一度に 1 つ）
  const [openGapRun, setOpenGapRun] = useState<string | null>(null);
  const [openSkippedRun, setOpenSkippedRun] = useState<string | null>(null);


  const { data: fillStatus } = useQuery({
    queryKey: ['batch-fill-status'],
    queryFn: async () => {
      const status = await batchFillApi.status();
      // 完了で両方のポーリングが止まる前に、見送り ID を含む最終履歴を取り直す。
      // 短い実行では running=true を観測しないこともあるので、遷移だけで判定しない。
      if (!status.running) {
        // 初回の履歴取得中は invalidate だけではその取得が再利用される。
        // 完了前の応答を採用しないよう、止めてから最終履歴を取り直す。
        await queryClient.cancelQueries({ queryKey: ['batch-fill-runs'] });
        void queryClient.invalidateQueries({ queryKey: ['batch-fill-runs'] });
      }
      return status;
    },
    refetchInterval: (q) => (q.state.data?.running ? 3000 : false),
  });
  const { data: fillRuns } = useQuery({
    queryKey: ['batch-fill-runs'],
    queryFn: () => batchFillApi.listRuns(10),
    refetchInterval: fillStatus?.running ? 5000 : false,
  });
  const startFillMutation = useMutation({
    mutationFn: () => batchFillApi.start(fillMode, fillChannelIds, fillIncludeCollabs),
    onSuccess: () => {
      showToast('一括セットリスト作成を開始しました', 'success');
      queryClient.invalidateQueries({ queryKey: ['batch-fill-status'] });
    },
    onError: (err: Error) => showToast(`開始できません: ${err.message}`, 'error'),
  });
  const cancelFillMutation = useMutation({
    mutationFn: batchFillApi.cancel,
    onSuccess: () => showToast('停止を要求しました', 'info'),
    onError: (err: Error) => showToast(`停止できません: ${err.message}`, 'error'),
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
    mutationFn: () => batchAnalyzeApi.start(batchMode, batchChannelId, batchHidden),
    onSuccess: () => {
      showToast('一括分析を開始しました（バックグラウンドで実行されます）', 'success');
      queryClient.invalidateQueries({ queryKey: ['batch-analyze-status'] });
    },
    onError: (err: Error) => showToast(err.message, 'error'),
  });

  const cancelBatchMutation = useMutation({
    mutationFn: batchAnalyzeApi.cancel,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['batch-analyze-status'] }),
    onError: (err: Error) => showToast(`停止できません: ${err.message}`, 'error'),
  });

  const syncChannelMutation = useMutation({
    mutationFn: () => holodexApi.syncChannel({ 
      channel_id: channelId,
      force_update: syncMode === 'all'
    }),
    onSuccess: (data) => {
      const message = data.message || `同期完了: ${data.synced_count}件 (新規: ${data.new_streams.length}, 更新: ${data.updated.length})`;
      queryClient.invalidateQueries({ queryKey: ['restriction-review'] });
      showToast(message, 'success');
    },
    onError: (err: Error) => {
      showToast(`同期エラー: ${err.message}`, 'error');
    },
  });

  const syncVideoMutation = useMutation({
    mutationFn: () => holodexApi.syncVideo(videoId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['restriction-review'] });
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
      <NonSingingCandidates />
      <RestrictionReview />
      <BackgroundTasks />

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
            value={batchChannelId}
            onChange={(e) => setBatchChannelId(e.target.value)}
            disabled={batchStatus?.running}
            className="w-full max-w-md px-3 py-2 text-sm border border-gray-300 rounded-lg bg-white focus:border-indigo-400 focus:ring-1 focus:ring-indigo-400 disabled:opacity-50"
          >
            <option value="">すべてのチャンネル</option>
            {channels.map((sg) => (
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
                ? ` / ${channels.find((sg) => sg.id === batchStatus.singer_id)?.name ?? batchStatus.singer_id}`
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
                {/* **状態を変えたことは必ず出す。** 出さないと、いつの間にか
                    処理済みが増えていて理由が分からなくなる */}
                {(batchStatus.marked_processed ?? 0) > 0 &&
                  `・曲が無いので処理済みにした ${batchStatus.marked_processed} 件`}
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

      {/* 一括セットリスト作成 */}
      <div className="bg-white rounded-lg shadow p-6">
        <h2 className="text-xl font-bold text-gray-900 mb-2">一括セットリスト作成</h2>
        <p className="text-sm text-gray-500 mb-4">
          源（Holodex 優先、無ければコメント）から歌唱を自動で作ります。
          <span className="font-medium text-gray-700">上のプレ分析と違い、歌唱（performances）に直接書き込みます。</span>
          決めきれないものは人の審査（修正提案）へ回り、実行単位でまとめて撤回できます。
        </p>

        <div className="flex flex-wrap items-end gap-3 mb-4">
          <label className="text-sm max-sm:w-full max-sm:min-w-0">
            <span className="block text-gray-700 mb-1">対象</span>
            <select
              value={fillMode}
              onChange={(e) => setFillMode(e.target.value)}
              disabled={fillStatus?.running}
              className="border border-gray-300 rounded-lg px-3 py-2 max-sm:w-full"
            >
              {/* 「処理済みを除く」を書かないと、force との違いが
                  「歌唱の有無だけ」に見えて、なぜ対象に出てこないのか分からなくなる */}
              <option value="unprocessed">歌唱が無く、まだ処理済みでない配信</option>
              <option value="force">入力元を持つ配信すべて（処理済みも含む・違う分は審査へ）</option>
            </select>
          </label>
          <label className="text-sm max-sm:w-full max-sm:min-w-0">
            <span className="block text-gray-700 mb-1">
              チャンネル
              <span className="ml-1 text-xs text-gray-400">（Ctrl / ⌘ で複数選択・未選択なら全部）</span>
            </span>
            <select
              multiple
              size={5}
              value={fillChannelIds}
              onChange={(e) =>
                setFillChannelIds(Array.from(e.target.selectedOptions, (o) => o.value))
              }
              disabled={fillStatus?.running}
              className="border border-gray-300 rounded-lg px-3 py-2 min-w-56 max-sm:min-w-0 max-sm:w-full"
            >
              {channels.map((sg) => (
                <option key={sg.id} value={sg.id}>{sg.name}</option>
              ))}
            </select>
          </label>
          <label className="text-sm flex items-center gap-2 pb-2" title="既定では、選んだチャンネルが所有する配信だけを対象にします">
            <input
              type="checkbox"
              checked={fillIncludeCollabs}
              onChange={(e) => setFillIncludeCollabs(e.target.checked)}
              disabled={fillStatus?.running || fillChannelIds.length === 0}
              className="accent-indigo-600"
            />
            <span className={fillChannelIds.length === 0 ? 'text-gray-400' : 'text-gray-700'}>
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
                {/* 「扱った」に数えない配信。done/total だけだと飛ばしたことが見えない */}
                {(fillStatus.skipped ?? 0) > 0 && (
                  <span className="text-amber-700"> ／ 飛ばした {fillStatus.skipped}</span>
                )}
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
                  <th className="py-2 pr-3" title="入力元を確定できずに今回は扱わなかった配信（live chat 待ちなど）。次の実行で拾う">
                    飛ばした
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
                              .map((id) => channels.find((sg) => sg.id === id)?.name ?? id)
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
                      <td className="py-2 pr-3">
                        {(run.skipped_stream_ids?.length ?? 0) > 0 ? (
                          <button
                            onClick={() => setOpenSkippedRun(openSkippedRun === run.id ? null : run.id)}
                            className="text-amber-700 underline hover:text-amber-900"
                            title="飛ばした配信を一覧する"
                          >
                            {run.skipped_stream_ids.length}
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
                        <td colSpan={8} className="py-2 pr-3 bg-gray-50">
                          <GapList runId={run.id} />
                        </td>
                      </tr>
                    )}
                    {openSkippedRun === run.id && (
                      <tr className="border-b last:border-0">
                        <td colSpan={8} className="py-2 pr-3 bg-gray-50">
                          <ul className="flex flex-wrap gap-x-3 gap-y-1 text-xs">
                            {run.skipped_stream_ids.map((sid) => (
                              <li key={sid}>
                                <Link to={`/streams/${sid}`} className="text-indigo-600 hover:underline">
                                  {sid}
                                </Link>
                              </li>
                            ))}
                          </ul>
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
    queryFn: channelApi.listAutoFill,
    enabled: canEdit && authStatus !== 'loading',
  });

  const stop = useMutation({
    mutationFn: (id: string) => channelApi.setAutoFill(id, false),
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
      {/* 登録したチャンネルは下の定期実行の対象になる。実行するかどうかと
          間隔はそちらの設定側（既定は無効） */}
      <p className="text-gray-500 mb-4 text-sm">
        ここに登録したチャンネルが、下の定期実行の対象になります（同期 → コメント取り直し →
        歌単作成）。確信の無いものと未登録の曲は<strong>審査へ回り</strong>、
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
              <Link to={`/channels/${sg.id}`} className="text-indigo-600 hover:underline truncate">
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

  const { data: settings, isError, isLoading } = useQuery({
    queryKey: ['autoFillSettings', canEdit],
    queryFn: autoFillApi.getSettings,
    enabled: canEdit && authStatus !== 'loading',
  });

  const [interval, setInterval] = useState<number | null>(null);
  const [refreshDays, setRefreshDays] = useState<number | null>(null);

  const save = useMutation({
    mutationFn: (next: { enabled: boolean; interval: number; refreshDays: number; includeCollabs: boolean }) =>
      autoFillApi.updateSettings({
        enabled: next.enabled,
        interval_hours: next.interval,
        refresh_days: next.refreshDays,
        include_collabs: next.includeCollabs,
      }),
    onSuccess: (data) => {
      queryClient.setQueryData(['autoFillSettings', canEdit], data);
      // **サーバーが丸めた値を画面へ戻す。** ローカル state を残すと、
      // 999 を保存してサーバーが 168 に丸めても画面には 999 が出たままになる。
      setInterval(null);
      setRefreshDays(null);
      showToast(data.enabled ? '自動処理を有効にしました' : '自動処理を止めました', 'success');
    },
    onError: (err: Error) => showToast(err.message, 'error'),
  });

  const runNow = useMutation({
    mutationFn: autoFillApi.run,
    onSuccess: (res) => {
      queryClient.invalidateQueries({ queryKey: ['autoFillSettings'] });
      queryClient.invalidateQueries({ queryKey: ['batch-fill-status'] });
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
  // 客串の旗は即時保存（有効の旗と同じ）なので、ローカル state を持たない。
  const effCollabs = settings?.include_collabs ?? false;

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
      ) : isLoading || !settings ? (
        // **読み込み前に触らせない。** 既定値（無効・6・30）が入ったフォームで
        // 「保存」を押せると、有効にしてある設定を既定値で上書きできてしまう。
        <p className="text-gray-400 text-sm">読み込み中...</p>
      ) : (
        <>
          <div className="flex flex-wrap items-end gap-4">
            <label className="flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                checked={settings?.enabled ?? false}
                onChange={(e) =>
                  save.mutate({
                    enabled: e.target.checked,
                    interval: effInterval,
                    refreshDays: effRefresh,
                    includeCollabs: effCollabs,
                  })
                }
                disabled={save.isPending}
                className="w-4 h-4 rounded border-gray-300"
              />
              定期実行を有効にする
            </label>

            <label
              className="flex items-center gap-2 text-sm"
              title="登録チャンネルがゲスト参加しただけの配信も対象にします。参加チャンネルが複数なので、歌単は全部審査へ回ります"
            >
              <input
                type="checkbox"
                checked={effCollabs}
                onChange={(e) =>
                  save.mutate({
                    enabled: settings?.enabled ?? false,
                    interval: effInterval,
                    refreshDays: effRefresh,
                    includeCollabs: e.target.checked,
                  })
                }
                disabled={save.isPending}
                className="w-4 h-4 rounded border-gray-300"
              />
              参加した配信（客串）も対象にする
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
                save.mutate({
                  enabled: settings?.enabled ?? false,
                  interval: effInterval,
                  refreshDays: effRefresh,
                  includeCollabs: effCollabs,
                })
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
          {/* 客串は参加チャンネルが複数あるので、一括作成は誰が歌ったかを決めず全行を審査へ回す。
              mention されただけの告知・企画枠も入るが、審査で落とせる */}
          {effCollabs && (
            <p className="text-xs text-gray-400 mt-1">
              客串の配信は参加チャンネルが複数あるため、作った歌単は全部審査へ回ります。
              mention されただけの告知や企画枠も対象に入るので、審査で落としてください。
            </p>
          )}

          {settings?.last_skipped_at && (
            <p className="text-sm text-amber-700 mt-3">
              前回の見送り: {new Date(settings.last_skipped_at).toLocaleString('ja-JP')}
              {settings.last_skip_note && `（${settings.last_skip_note}）`}
            </p>
          )}

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


// BackgroundTasks は backfill・準備・AI 整備の実行記録（issue #22）。
//
// 以前は curl で叩いて投げっぱなし、進捗も失敗も log だけだった。log はメモリ上の
// 直近 1000 件なので、長い実行は自分の進捗行で失敗行を押し流す。ここでは実行ごとに
// 成功・見送り・失敗を分けて出し、**失敗の理由を後から引ける**ようにする
// （cookie を直して再実行すべきかの判断材料）。
function BackgroundTasks() {
  const queryClient = useQueryClient();
  const { showToast } = useToast();
  const canEdit = hasPermission(useAuthStore((st) => st.user), PERM.CONTENT_EDIT);
  const authStatus = useAuthStore((st) => st.status);
  const completedTasks = useRef(new Set<string>());
  const [openTask, setOpenTask] = useState<string | null>(null);
  const [prepareChannel, setPrepareChannel] = useState('');
  const { data: prepareChannels } = useQuery({ queryKey: ['singers-for-prepare'], queryFn: () => channelApi.list(1, 300, 'name', 'asc', true), enabled: canEdit });
  const prepare = useMutation({
    mutationFn: () => taskApi.prepare(prepareChannel),
    onSuccess: () => { queryClient.invalidateQueries({ queryKey: ['tasks'] }); showToast('準備を開始しました', 'success'); },
    onError: (err: Error) => showToast(err.message, 'error'),
  });
  const cancel = useMutation({ mutationFn: taskApi.cancel, onSuccess: () => showToast('停止を要求しました。処理中の1件が終わると停止します', 'info'), onError: (err: Error) => showToast(err.message, 'error') });

  const { data: tasks, isError } = useQuery({
    queryKey: ['tasks', canEdit],
    queryFn: () => taskApi.list(10),
    enabled: canEdit && authStatus !== 'loading',
    // 走っている間だけ追う
    refetchInterval: (q) => (q.state.data?.some((t) => t.status === 'running') ? 3000 : false),
  });

  useEffect(() => {
    for (const task of tasks ?? []) {
      if (task.status === 'running' || completedTasks.current.has(task.id)) continue;
      completedTasks.current.add(task.id);
      void invalidateTaskResults(queryClient, task);
    }
  }, [tasks, queryClient]);

  const start = useMutation({
    mutationFn: (kind: 'chapter' | 'chat_end') =>
      kind === 'chapter' ? taskApi.startChapterBackfill(3) : taskApi.startChatEndBackfill(3),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['tasks'] });
      showToast('開始しました', 'success');
    },
    // 409（同じ処理が実行中）もバックエンドの文言をそのまま出す
    onError: (err: Error) => showToast(err.message, 'error'),
  });

  if (!canEdit) return null;

  return (
    <div id="background-tasks" className="bg-white rounded-lg shadow-sm border p-6">
      <h2 className="text-xl font-bold text-gray-900 mb-2">背景処理</h2>
      <div className="mb-4 space-y-2">
        <p className="text-sm text-gray-600">同期後の準備：所有する表示中・未処理の配信の章節を取得し、コメントを取り直してプレ分析します。会限・秘匿の配信は対象外です。各取得・解析前に状態を確認します。歌唱の保存は編集画面で確認して行います。</p>
        <label className="text-sm max-sm:block">対象チャンネル <select value={prepareChannel} onChange={(e) => setPrepareChannel(e.target.value)} className="border rounded px-2 py-1 max-sm:block max-sm:w-full">
          <option value="">チャンネルを選択</option>
          {prepareChannels?.singers.map((s) => <option key={s.id} value={s.id}>{s.name}</option>)}
        </select></label>
        <button onClick={() => prepare.mutate()} disabled={!prepareChannel || prepare.isPending} className="ml-2 max-sm:ml-0 px-3 py-1.5 bg-indigo-600 text-white rounded disabled:opacity-50">同期後の準備を開始</button>
      </div>
      <p className="text-gray-500 mb-4 text-sm">
        yt-dlp を使う一括取得です。どちらも時間がかかり、YouTube に BOT 判定されると全件失敗します
        （そのときは管理→設定で cookies.txt を更新してから再実行）。一括セットリスト作成は
        yt-dlp を呼ばないので、チャプターを入力元に使うなら<strong>先にここで取得</strong>しておきます。
      </p>
      <div className="flex flex-wrap gap-2 mb-4">
        <button
          onClick={() => start.mutate('chapter')}
          disabled={start.isPending}
          title="チャプターを未取得の配信について、yt-dlp で目次を取得します"
          className="px-3 py-1.5 text-sm border border-gray-300 rounded-lg hover:border-indigo-300 disabled:opacity-50"
        >
          チャプターを取得
        </button>
        <button
          onClick={() => start.mutate('chat_end')}
          disabled={start.isPending}
          title="解析済みの配信について、live chat の拍手から曲の終了時刻を埋め直します"
          className="px-3 py-1.5 text-sm border border-gray-300 rounded-lg hover:border-indigo-300 disabled:opacity-50"
        >
          拍手 end を埋め直す
        </button>
      </div>

      {isError ? (
        <p className="text-red-600 text-sm">記録の取得に失敗しました。</p>
      ) : (tasks?.length ?? 0) === 0 ? (
        <p className="text-gray-400 text-sm">まだ実行の記録がありません。</p>
      ) : (
        <ul className="divide-y border rounded-lg text-sm">
          {tasks!.map((t) => (
            <li key={t.id} className="px-4 py-2">
              <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
                <span className="font-medium text-gray-800">{TASK_LABELS[t.kind] ?? t.kind}</span>
                <span className="text-gray-500">
                  {new Date(t.started_at).toLocaleString('ja-JP', {
                    month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false,
                  })}
                  {t.started_by_name && ` ${t.started_by_name}`}
                </span>
                <span
                  className={
                    t.status === 'running'
                      ? 'text-indigo-700'
                      : t.status === 'done'
                        ? 'text-gray-600'
                        : 'text-red-600'
                  }
                >
                  {{ running: '実行中', done: '完了', failed: '失敗', interrupted: '中断', cancelled: '停止' }[t.status] ?? t.status}
                </span>
                <span className="text-gray-600">
                  {t.done}/{t.total}（成功 {t.succeeded}・見送り {t.skipped}・
                  <span className={t.failed > 0 ? 'text-red-600' : ''}>失敗 {t.failed}</span>）
                </span>
                {t.phase && <span>{TASK_PHASE_LABELS[t.phase] ?? t.phase}{typeof t.params?.singer_id === 'string' && ` / ${t.params.singer_id}`}</span>}
                {t.status === 'running' && t.kind === 'stream_prepare' && <button onClick={() => cancel.mutate(t.id)} disabled={cancel.isPending} className="underline">停止</button>}
                {t.failures.length > 0 && (
                  <button
                    onClick={() => setOpenTask(openTask === t.id ? null : t.id)}
                    className="text-red-600 underline hover:text-red-800"
                  >
                    失敗の理由
                  </button>
                )}
              </div>
              {t.message && t.status !== 'running' && <div className="text-xs text-gray-400 mt-0.5">{t.message}</div>}
              {openTask === t.id && (
                <ul className="mt-2 space-y-0.5 text-xs bg-gray-50 rounded p-2 max-h-60 overflow-y-auto">
                  {t.failures.map((f, i) => (
                    <li key={i} className="flex gap-2">
                      {['chapter_backfill', 'chat_end_backfill', 'stream_prepare'].includes(t.kind) ? (
                        <Link to={`/streams/${f.target}`} className="text-indigo-600 hover:underline shrink-0">{f.target}</Link>
                      ) : <span className="break-all text-gray-700">{f.target}</span>}
                      <span className="text-gray-600 break-all">{f.reason}</span>
                    </li>
                  ))}
                  {t.failed > t.failures.length && (
                    <li className="text-gray-400">ほか {t.failed - t.failures.length} 件（記録は直近の {t.failures.length} 件まで）</li>
                  )}
                </ul>
              )}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

// RestrictionReview は公開の裁定と現在の会限判定が食い違う配信（issue #26）。
// 控えが無い旧裁定も含むため、検出と裁定の前後関係は断定しない。
//
// 人の裁定は自動判定に勝つので、配信者が後から会限へ変えても公開のまま、
// 何の知らせも無かった。**自動で伏せ直さない**（裁定の意味が無くなる）──
// 食い違いを並べて人に決めさせる。配信を開かなくても気付けるよう、ここに置く。
//
// どちらのボタンも裁定を書き直す。書き直した時点の判定が控えられるので一覧から消え、
// 「公開のまま」を選んだときは、現在の自動判定を確認済みとして控える。
function RestrictionReview() {
  const queryClient = useQueryClient();
  const { showToast } = useToast();
  const canEdit = hasPermission(useAuthStore((st) => st.user), PERM.CONTENT_EDIT);
  const authStatus = useAuthStore((st) => st.status);

  const { data, isLoading, isError } = useQuery({
    queryKey: ['restriction-review', canEdit],
    queryFn: () => restrictionReviewApi.list(100),
    enabled: canEdit && authStatus !== 'loading',
  });

  const decide = useMutation({
    mutationFn: ({ id, restricted }: { id: string; restricted: boolean }) =>
      streamApi.update(id, { is_restricted: restricted }),
    onSuccess: (_data, { id, restricted }) => {
      queryClient.invalidateQueries({ queryKey: ['restriction-review'] });
      queryClient.invalidateQueries({ queryKey: ['stream', id] });
      showToast(restricted ? '非公開にしました' : '公開のままにしました（確認済み）', 'success');
    },
    onError: (err: Error) => showToast(err.message, 'error'),
  });

  if (!canEdit) return null;
  const items = data?.items ?? [];
  // 該当が無いときは場所を取らない（ほとんどの時間は 0 件のはず）。
  // 取得の失敗は 0 件と区別して出す。
  if (!isLoading && !isError && items.length === 0) return null;

  return (
    <div className="bg-white rounded-lg shadow-sm border border-red-200 p-6">
      <h2 className="text-xl font-bold text-gray-900 mb-2">公開の裁定を見直す配信</h2>
      <p className="text-gray-500 mb-4 text-sm">
        「公開してよい」という裁定が残る一方で、現在は<strong>会限として検出されている</strong>配信です。
        裁定時点の判定が不明な以前の裁定も含みます。裁定は自動判定より優先されるので、
        このままだとセットリストは<strong>公開のまま</strong>です。
      </p>
      {isLoading ? (
        <p className="text-gray-400 text-sm">読み込み中...</p>
      ) : isError ? (
        <p className="text-red-600 text-sm">取得に失敗しました（該当が無いという意味ではありません）。</p>
      ) : (
        <ul className="divide-y border rounded-lg">
          {items.map((item) => (
            <li key={item.id} className="flex items-center justify-between gap-3 px-4 py-2 max-sm:flex-col max-sm:items-start">
              <div className="min-w-0 max-sm:w-full">
                <Link to={`/streams/${item.id}`} className="text-indigo-600 hover:underline truncate block max-sm:whitespace-normal max-sm:break-words">
                  {item.title}
                </Link>
                <div className="text-xs text-gray-400 flex flex-wrap items-center gap-2">
                  <span>{new Date(item.stream_date).toLocaleDateString('ja-JP')}</span>
                  {/* 控えが無い＝この仕組みより前の裁定。会限と知っていて公開したのかもしれない */}
                  {item.basis_unknown && <span>裁定の時点の判定は不明（以前の裁定）</span>}
                </div>
              </div>
              <div className="flex shrink-0 gap-2 text-xs">
                <button
                  type="button"
                  disabled={decide.isPending}
                  onClick={() => decide.mutate({ id: item.id, restricted: true })}
                  className="px-2 py-1 rounded bg-red-600 text-white hover:bg-red-700 disabled:opacity-50"
                >
                  非公開にする
                </button>
                <button
                  type="button"
                  disabled={decide.isPending}
                  onClick={() => decide.mutate({ id: item.id, restricted: false })}
                  className="px-2 py-1 rounded border text-gray-700 hover:bg-gray-50 disabled:opacity-50"
                >
                  公開のまま
                </button>
              </div>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

// NonSingingCandidates は「非表示だが現行規則で曲が出た」配信の一覧。
//
// **自動で非表示は解除しない。** 2026-08-29 に手で見直したときの実測では、
// 誤判定は両方向にあった（雑談が歌枠と判定される／本物の歌枠が隠れる）ので、
// 自動で解くと雑談が発見面へ出る。ここは候補を並べて人に決めさせる場所。
//
// **差分は保存しない**（毎回計算する）。記録するのは否定だけ ──
// 「見たが歌回ではない」を残さないと同じ配信が毎回出続け、作業一覧として使えなくなる。
function NonSingingCandidates() {
  const queryClient = useQueryClient();
  const { showToast } = useToast();
  const canEdit = hasPermission(useAuthStore((st) => st.user), PERM.CONTENT_EDIT);
  const authStatus = useAuthStore((st) => st.status);

  // **却下したものも見えること。** 一覧から消えるだけで戻せないと、
  // 誤って却下した配信が二度と出てこない（CLAUDE.md §7.7）。
  const [showDismissed, setShowDismissed] = useState(false);

  const { data, isLoading, isError } = useQuery({
    queryKey: ['nonSingingCandidates', canEdit, showDismissed],
    queryFn: () => nonSingingApi.list(100, showDismissed),
    enabled: canEdit && authStatus !== 'loading',
  });

  const dismiss = useMutation({
    mutationFn: (id: string) => nonSingingApi.dismiss(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['nonSingingCandidates'] });
      showToast('歌回ではないと記録しました（「判断済み」から取り消せます）', 'success');
    },
    onError: (err: Error) => showToast(err.message, 'error'),
  });

  const restore = useMutation({
    mutationFn: (id: string) => nonSingingApi.restore(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['nonSingingCandidates'] });
      showToast('判断を取り消しました（候補に戻ります）', 'success');
    },
    onError: (err: Error) => showToast(err.message, 'error'),
  });

  if (!canEdit) return null;

  const candidates = data?.candidates ?? [];

  return (
    <div className="bg-white rounded-lg shadow-sm border p-6">
      <div className="flex items-center justify-between mb-2">
        <h2 className="text-xl font-bold text-gray-900">見直しが要る配信</h2>
        <button
          onClick={() => setShowDismissed((v) => !v)}
          className="text-xs text-gray-500 hover:text-indigo-600 underline"
        >
          {showDismissed ? '候補に戻る' : '判断済みを見る'}
        </button>
      </div>
      <p className="text-gray-500 mb-4 text-sm">
        <strong>非表示なのに、コメントから曲が抽出できた</strong>配信です。
        本当は歌枠なのに隠れている可能性がありますが、実況メモや雑談のタイムスタンプが
        曲として拾われることも多いので、<strong>自動では解除しません</strong>。
        中身を見て、歌枠なら配信ページで非表示を解除してください。
      </p>

      {isLoading ? (
        <p className="text-gray-400 text-sm">読み込み中...</p>
      ) : isError ? (
        <p className="text-red-600 text-sm">
          取得に失敗しました（候補が無いという意味ではありません）。
        </p>
      ) : candidates.length === 0 ? (
        <p className="text-gray-400 text-sm">
          {showDismissed ? '「歌回ではない」と判断した配信はありません。' : '見直しが要る配信はありません。'}
        </p>
      ) : (
        <ul className="divide-y border rounded-lg">
          {candidates.map((c) => (
            <li key={c.id} className="flex items-center justify-between gap-3 px-4 py-2">
              <div className="min-w-0">
                <Link to={`/streams/${c.id}`} className="text-indigo-600 hover:underline truncate block">
                  {c.title}
                </Link>
                <div className="text-xs text-gray-400 flex flex-wrap items-center gap-2">
                  <span>{c.song_count} 曲</span>
                  <span>{new Date(c.stream_date).toLocaleDateString('ja-JP')}</span>
                  {c.tags.length > 0 && <span>{c.tags.join('・')}</span>}
                  {/* **旧規則のままかを出す。** 古い抽出を根拠に非表示を解くのは危ない
                      （2026-08-07 より前の結果は現行規則に通すと落ちるものが多い） */}
                  {!c.analyzed_at && (
                    <span className="text-amber-600">旧規則のままの抽出（要再分析）</span>
                  )}
                </div>
              </div>
              {showDismissed ? (
                <button
                  onClick={() => restore.mutate(c.id)}
                  disabled={restore.isPending}
                  title="判断を取り消して候補に戻す"
                  className="shrink-0 px-2 py-1 text-xs text-gray-600 border border-gray-300 rounded-full hover:text-indigo-600 hover:border-indigo-300 disabled:opacity-50"
                >
                  判断を取り消す
                </button>
              ) : (
                <button
                  onClick={() => dismiss.mutate(c.id)}
                  disabled={dismiss.isPending}
                  title="見たが歌回ではない、と記録して一覧から外す（あとで取り消せます）"
                  className="shrink-0 px-2 py-1 text-xs text-gray-600 border border-gray-300 rounded-full hover:text-indigo-600 hover:border-indigo-300 disabled:opacity-50"
                >
                  歌回ではない
                </button>
              )}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
