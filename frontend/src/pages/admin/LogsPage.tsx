import { useState, useRef, useEffect } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { logsApi } from '../../api/client';
import { useToast } from '../../components/ui/Toast';

const LEVELS = ['DEBUG', 'INFO', 'WARN', 'ERROR'] as const;

export default function LogsPage() {
  const queryClient = useQueryClient();
  const { showToast } = useToast();
  const [limit, setLimit] = useState(100);
  const logsRef = useRef<HTMLDivElement>(null);

  const { data, isLoading, refetch, error } = useQuery({
    queryKey: ['logs', limit],
    queryFn: () => logsApi.list(limit),
    refetchInterval: 5000, // auto refresh every 5s
  });

  const setLevelMutation = useMutation({
    mutationFn: (level: string) => logsApi.setLevel(level),
    onSuccess: (res) => {
      showToast(`ログレベルを ${res.level} に変更しました`, 'success');
      queryClient.invalidateQueries({ queryKey: ['logs'] });
      // 明示的にリフェッチ
      refetch();
    },
    onError: (err: Error) => {
      showToast(`ログレベル変更失敗: ${err.message}`, 'error');
    },
  });

  const currentLevel = data?.level || 'INFO';

  const handleSetLevel = (level: string) => {
    if (level !== currentLevel) {
      setLevelMutation.mutate(level);
    }
  };

  const filteredLogs = data?.logs || [];

  // Auto scroll to bottom on initial load, and on updates only when user is near bottom
  useEffect(() => {
    const el = logsRef.current;
    if (!el || filteredLogs.length === 0) return;

    const threshold = 60;
    const distToBottom = el.scrollHeight - el.scrollTop - el.clientHeight;

    // Follow on first load (scrollTop near 0) or when already near bottom
    const shouldFollow = distToBottom < threshold || el.scrollTop <= 5;

    if (shouldFollow) {
      requestAnimationFrame(() => {
        const current = logsRef.current;
        if (current) {
          current.scrollTop = current.scrollHeight;
        }
      });
    }
  }, [filteredLogs]);

  return (
    <div className="space-y-6">
      <div className="flex justify-between items-center">
        <h1 className="text-3xl font-bold text-gray-900">ログ</h1>
        <button
          onClick={() => refetch()}
          className="px-4 py-2 bg-indigo-600 text-white rounded-lg hover:bg-indigo-700"
        >
          更新
        </button>
      </div>

      {/* Log Level Control */}
      <div className="bg-white rounded-lg shadow-sm border p-6">
        <h2 className="text-xl font-semibold mb-4">ログレベル設定</h2>
        <div className="flex items-center gap-4 flex-wrap">
          <span className="text-sm text-gray-600">現在のレベル:</span>
          <span className="font-mono px-3 py-1 bg-gray-100 rounded text-sm font-medium">
            {currentLevel}
          </span>
          <div className="flex gap-2">
            {LEVELS.map((lvl) => (
              <button
                key={lvl}
                onClick={() => handleSetLevel(lvl)}
                disabled={setLevelMutation.isPending || lvl === currentLevel}
                className={`px-3 py-1 text-sm rounded border transition-colors ${
                  lvl === currentLevel
                    ? 'bg-indigo-600 text-white border-indigo-600'
                    : 'bg-white hover:bg-gray-50 border-gray-300'
                } ${setLevelMutation.isPending ? 'opacity-50' : ''}`}
              >
                {lvl}
              </button>
            ))}
          </div>
          <span className="text-xs text-gray-500">
            (変更は即時反映、サーバー再起動で初期値に戻ります)
          </span>
        </div>
      </div>

      {/* Logs Display */}
      <div className="bg-white rounded-lg shadow-sm border overflow-hidden">
        <div className="px-6 py-3 border-b bg-gray-50 flex justify-between items-center">
          <h3 className="font-semibold">ログ ({filteredLogs.length} 件)</h3>
          <select
            value={limit}
            onChange={(e) => setLimit(parseInt(e.target.value))}
            className="text-sm border rounded px-2 py-1"
          >
            <option value={50}>50件</option>
            <option value={100}>100件</option>
            <option value={200}>200件</option>
            <option value={500}>500件</option>
          </select>
        </div>

        {isLoading ? (
          <div className="p-8 text-center text-gray-500">読み込み中...</div>
        ) : error ? (
          <div className="p-8 text-center text-red-500">
            ログの取得に失敗しました: {error.message || '不明なエラー'}<br />
            バックエンドが再ビルド・再起動されているか確認してください。
          </div>
        ) : filteredLogs.length === 0 ? (
          <div className="p-8 text-center text-gray-500">
            ログがありません。<br />
            ログレベルを INFO または DEBUG に設定してから、ページを操作（例: コメント分析）するとログが表示されます。
          </div>
        ) : (
          <div ref={logsRef} className="overflow-auto max-h-[600px] font-mono text-xs">
            <table className="w-full">
              <thead className="bg-gray-50 sticky top-0">
                <tr>
                  <th className="px-4 py-2 text-left w-48">時間</th>
                  <th className="px-2 py-2 text-left w-16">レベル</th>
                  <th className="px-4 py-2 text-left">メッセージ</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-100">
                {filteredLogs.map((log, idx) => {
                  const levelColor =
                    log.level === 'ERROR' ? 'text-red-600' :
                    log.level === 'WARN' ? 'text-orange-600' :
                    log.level === 'INFO' ? 'text-blue-600' : 'text-gray-600';

                  return (
                    <tr key={idx} className="hover:bg-gray-50">
                      <td className="px-4 py-1 text-gray-500 whitespace-nowrap">
                        {new Date(log.time).toLocaleTimeString()}
                      </td>
                      <td className={`px-2 py-1 font-semibold ${levelColor}`}>
                        {log.level}
                      </td>
                      <td className="px-4 py-1 break-all text-gray-800">
                        {log.message}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </div>

      <p className="text-xs text-gray-500">
        自動更新 (5秒間隔)。DEBUGレベルは詳細なAI入出力が表示されます。
      </p>
    </div>
  );
}
