import { useState } from 'react';
import { keepPreviousData, useQuery } from '@tanstack/react-query';
import { activityApi } from '../../api/client';
import { maskIP } from '../../utils/ip';

type ActivityKind = 'all' | 'anonymous' | 'authenticated';

function formatDateTime(value: string): string {
  return new Date(value).toLocaleString('ja-JP');
}

export default function ActivityPage() {
  const [days, setDays] = useState(7);
  const [kind, setKind] = useState<ActivityKind>('all');
  const [queryInput, setQueryInput] = useState('');
  const [query, setQuery] = useState('');
  const [page, setPage] = useState(1);
  const [showFullIP, setShowFullIP] = useState(false);

  const listQuery = useQuery({
    queryKey: ['activity', days, kind, query, page],
    queryFn: () => activityApi.list({ days, kind, q: query, page, limit: 50 }),
    placeholderData: keepPreviousData,
  });
  const statsQuery = useQuery({
    queryKey: ['activity', 'stats', days],
    queryFn: () => activityApi.stats(days),
  });

  const data = listQuery.data;
  const stats = statsQuery.data?.stats;
  const totalPages = Math.max(1, Math.ceil((data?.total ?? 0) / (data?.limit ?? 50)));

  const applyFilters = (nextDays = days, nextKind = kind) => {
    setDays(nextDays);
    setKind(nextKind);
    setPage(1);
  };

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="text-3xl font-bold text-gray-900">アクセス管理</h1>
          <p className="mt-2 text-sm text-gray-500">
            IP・ログイン利用者・ページ表示を日単位で集約します。ユニーク IP は訪問者数の概算です。
          </p>
        </div>
        <button
          type="button"
          onClick={() => setShowFullIP((value) => !value)}
          className="rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm text-gray-700 hover:bg-gray-50"
        >
          {showFullIP ? 'IPを隠す' : '完全なIPを表示'}
        </button>
      </div>

      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        {[
          ['ユニークIP', stats?.unique_ips ?? '—'],
          ['ログイン利用者', stats?.authenticated_users ?? '—'],
          ['ページ表示', stats?.page_views ?? '—'],
          ['匿名IP', stats?.anonymous_ips ?? '—'],
        ].map(([label, value]) => (
          <div key={label} className="rounded-lg border bg-white p-4 shadow-sm">
            <div className="text-xs text-gray-500">{label}</div>
            <div className="mt-1 text-2xl font-semibold text-gray-900">{value}</div>
          </div>
        ))}
      </div>

      <div className="rounded-lg border bg-white p-4 shadow-sm">
        <div className="flex flex-wrap items-center gap-3">
          <select
            value={days}
            onChange={(event) => applyFilters(Number(event.target.value), kind)}
            className="rounded-lg border border-gray-300 px-3 py-2 text-sm"
          >
            {[1, 7, 30, 90].map((value) => (
              <option key={value} value={value}>過去 {value} 日</option>
            ))}
          </select>
          <select
            value={kind}
            onChange={(event) => applyFilters(days, event.target.value as ActivityKind)}
            className="rounded-lg border border-gray-300 px-3 py-2 text-sm"
          >
            <option value="all">すべて</option>
            <option value="anonymous">匿名のみ</option>
            <option value="authenticated">ログイン済みのみ</option>
          </select>
          <form
            className="flex min-w-[16rem] flex-1 gap-2"
            onSubmit={(event) => {
              event.preventDefault();
              setQuery(queryInput.trim());
              setPage(1);
            }}
          >
            <input
              value={queryInput}
              onChange={(event) => setQueryInput(event.target.value)}
              placeholder="IP・ユーザー名で検索"
              className="min-w-0 flex-1 rounded-lg border border-gray-300 px-3 py-2 text-sm"
            />
            <button type="submit" className="rounded-lg bg-indigo-600 px-4 py-2 text-sm text-white hover:bg-indigo-700">
              検索
            </button>
          </form>
        </div>
        <p className="mt-3 text-xs text-gray-400">
          保存期間は {data?.retention_days ?? statsQuery.data?.retention_days ?? 30} 日です。ページの query string と referrer は保存しません。
        </p>
      </div>

      <div className="overflow-hidden rounded-lg border bg-white shadow-sm">
        {listQuery.isLoading ? (
          <p className="p-6 text-gray-400">読み込み中...</p>
        ) : listQuery.isError ? (
          <p className="p-6 text-red-600">活動記録を読み込めませんでした。</p>
        ) : !data || data.activity.length === 0 ? (
          <p className="p-6 text-gray-400">条件に一致する記録はありません。</p>
        ) : (
          <div className="overflow-x-auto">
            <table className="min-w-full divide-y divide-gray-200 text-sm">
              <thead className="bg-gray-50 text-left text-xs uppercase tracking-wide text-gray-500">
                <tr>
                  <th className="px-4 py-3 font-medium">最終アクセス</th>
                  <th className="px-4 py-3 font-medium">IP</th>
                  <th className="px-4 py-3 font-medium">利用者</th>
                  <th className="px-4 py-3 font-medium">表示数</th>
                  <th className="px-4 py-3 font-medium">最終ページ</th>
                  <th className="px-4 py-3 font-medium">User Agent</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-100">
                {data.activity.map((item) => (
                  <tr key={item.id} className="align-top hover:bg-gray-50">
                    <td className="whitespace-nowrap px-4 py-3 text-gray-700">
                      {formatDateTime(item.last_seen)}
                      <div className="text-xs text-gray-400">初回 {formatDateTime(item.first_seen)}</div>
                    </td>
                    <td className="whitespace-nowrap px-4 py-3 font-mono text-xs text-gray-700" title={showFullIP ? undefined : 'ボタンで完全なIPを表示'}>
                      {showFullIP ? item.ip_address : maskIP(item.ip_address)}
                    </td>
                    <td className="whitespace-nowrap px-4 py-3">
                      {item.user_id ? (
                        <>
                          <div className="font-medium text-gray-900">{item.display_name || item.username}</div>
                          <div className="text-xs text-gray-400">{item.username}</div>
                        </>
                      ) : (
                        <span className="text-gray-400">匿名</span>
                      )}
                    </td>
                    <td className="px-4 py-3 text-right tabular-nums text-gray-700">{item.page_views}</td>
                    <td className="max-w-[16rem] truncate px-4 py-3 font-mono text-xs text-gray-700" title={item.last_path}>
                      {item.last_path}
                    </td>
                    <td className="max-w-[18rem] truncate px-4 py-3 text-xs text-gray-500" title={item.user_agent}>
                      {item.user_agent || '—'}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}

        <div className="flex items-center justify-between border-t px-4 py-3 text-sm text-gray-600">
          <span>{data?.total ?? 0} 件</span>
          <div className="flex items-center gap-2">
            <button
              type="button"
              disabled={page <= 1}
              onClick={() => setPage((value) => Math.max(1, value - 1))}
              className="rounded border px-3 py-1.5 disabled:opacity-40"
            >
              前へ
            </button>
            <span>{page} / {totalPages}</span>
            <button
              type="button"
              disabled={page >= totalPages}
              onClick={() => setPage((value) => Math.min(totalPages, value + 1))}
              className="rounded border px-3 py-1.5 disabled:opacity-40"
            >
              次へ
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
