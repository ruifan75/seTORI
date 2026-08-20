import { Fragment, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link } from 'react-router-dom';
import { holodexApi, batchAnalyzeApi, batchFillApi, singerApi } from '../../api/client';
import { useToast } from '../../components/ui/ToastContext';
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
              {batchStatus.done + batchStatus.failed}/{batchStatus.total}
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
                {batchStatus.failed > 0 && `・失敗 ${batchStatus.failed} 件`}）
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
