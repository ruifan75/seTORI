import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { holodexApi, batchAnalyzeApi } from '../../api/client';
import { useToast } from '../../components/ui/ToastContext';

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
] as const;

export default function SyncPage() {
  const [channelId, setChannelId] = useState('');
  const [videoId, setVideoId] = useState('');
  const [syncMode, setSyncMode] = useState<'new' | 'all'>('new');
  const [batchMode, setBatchMode] = useState<string>('unanalyzed');
  const { showToast } = useToast();
  const queryClient = useQueryClient();

  // 一括分析：実行中は 3 秒ごとに進捗をポーリング
  const { data: batchStatus } = useQuery({
    queryKey: ['batch-analyze-status'],
    queryFn: batchAnalyzeApi.status,
    refetchInterval: (query) => (query.state.data?.running ? 3000 : false),
  });

  const startBatchMutation = useMutation({
    mutationFn: () => batchAnalyzeApi.start(batchMode),
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
              同步模式
            </label>
            <select
              id="syncMode"
              value={syncMode}
              onChange={(e) => setSyncMode(e.target.value as 'new' | 'all')}
              className="w-full px-4 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
            >
              <option value="new">只同步新的影片</option>
              <option value="all">同步所有影片（包含更新）</option>
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

        {/* Progress - 由於 API 是同步的，無法顯示即時進度 */}
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

        {batchStatus?.running ? (
          <div className="rounded-lg border border-indigo-200 bg-indigo-50 p-3 text-sm flex flex-wrap items-center gap-3">
            <svg className="animate-spin h-4 w-4 text-indigo-500 shrink-0" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
              <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
              <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
            </svg>
            <span className="font-medium text-indigo-700">
              一括分析中（{BATCH_MODES.find((m) => m.value === batchStatus.mode)?.label ?? batchStatus.mode}）{' '}
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
    </div>
  );
}
