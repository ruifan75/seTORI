import { useState } from 'react';
import { useMutation } from '@tanstack/react-query';
import { holodexApi } from '../../api/client';
import { useToast } from '../../components/ui/Toast';

export default function SyncPage() {
  const [channelId, setChannelId] = useState('');
  const [videoId, setVideoId] = useState('');
  const { showToast } = useToast();

  const syncChannelMutation = useMutation({
    mutationFn: () => holodexApi.syncChannel({ channel_id: channelId }),
    onSuccess: (data) => {
      showToast(`同期完了: ${data.synced_count}件`, 'success');
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
    <div className="space-y-8">
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

          <button
            type="submit"
            disabled={syncChannelMutation.isPending || !channelId.trim()}
            className="px-4 py-2 bg-indigo-600 text-white font-medium rounded-lg hover:bg-indigo-700 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
          >
            {syncChannelMutation.isPending ? '同期中...' : '同期開始'}
          </button>
        </form>

        {/* Result */}
        {syncChannelMutation.isSuccess && (
          <div className="mt-4 p-4 bg-green-50 border border-green-200 rounded-lg">
            <h3 className="font-medium text-green-800 mb-2">同期完了</h3>
            <ul className="text-sm text-green-700 space-y-1">
              <li>同期数: {syncChannelMutation.data.synced_count}</li>
              <li>新規: {syncChannelMutation.data.new_streams.length}</li>
              <li>更新: {syncChannelMutation.data.updated.length}</li>
              <li>スキップ: {syncChannelMutation.data.skipped.length}</li>
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
    </div>
  );
}
