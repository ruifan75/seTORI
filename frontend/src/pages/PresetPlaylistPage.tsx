import { useQuery } from '@tanstack/react-query';
import { Link, useParams } from 'react-router-dom';
import { presetPlaylistApi } from '../api/client';
import type { Performance } from '../api/types';
import Loading from '../components/ui/Loading';
import PerformanceList from '../components/PerformanceList';
import PerformanceListToolbar from '../components/PerformanceListToolbar';
import { toPerformanceListItem } from '../utils/performanceList';
import PresetActions from '../components/PresetActions';

/**
 * プリセットプレイリストの詳細（運営が用意した歌単）。
 *
 * 中身はサーバー側で毎回計算されるので編集も並び替えもできない。
 * 自分で手を入れたい人は「コピー」で通常のプレイリストへ複製する。
 * 誰でも同じものが見えるため、共有は URL をそのまま渡すだけでよい（share_slug は無い）。
 */
export default function PresetPlaylistPage() {
  const { key } = useParams<{ key: string }>();

  const presetQuery = useQuery({
    queryKey: ['presets', 'detail', key],
    queryFn: () => presetPlaylistApi.get(key!),
    enabled: !!key,
    retry: false,
  });

  const itemsQuery = useQuery({
    queryKey: ['preset-items', key, 'all'],
    queryFn: () => presetPlaylistApi.items(key!),
    enabled: !!key,
    retry: false,
  });

  const preset = presetQuery.data;
  const items: Performance[] = itemsQuery.data?.performances ?? [];

  const listItems = items.map((perf) => toPerformanceListItem(perf));

  if (presetQuery.isLoading) return <Loading />;

  if (presetQuery.isError || !preset) {
    return (
      <div className="text-center py-16 space-y-3">
        <p className="text-gray-900 font-medium">プレイリストが見つかりません</p>
        <Link to="/playlists" className="inline-block text-indigo-600 hover:underline">
          プレイリスト一覧へ
        </Link>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <div className="space-y-3">
        <div className="flex flex-wrap items-center gap-3">
          <h1 className="text-3xl font-bold text-gray-900">{preset.name}</h1>
          <span className="px-2 py-0.5 text-xs font-medium text-indigo-700 bg-indigo-50 border border-indigo-200 rounded-full">
            プリセット
          </span>
        </div>
        {preset.description && <p className="text-gray-600">{preset.description}</p>}
        <p className="text-sm text-gray-500">
          {preset.item_count} 曲 · 条件に合う歌唱が登録されると自動で増えます
          {/* 読み込み中に「先頭 0 曲」と出さないよう、件数が確定してから注記する */}
          {items.length > 0 && items.length < preset.item_count && `（表示は先頭 ${items.length} 曲）`}
        </p>
        <div className="flex flex-wrap items-center gap-2">
          <PresetActions preset={preset} />
        </div>
      </div>

      {itemsQuery.isError ? (
        <p role="alert" className="py-8 text-center text-red-700">歌唱リストを取得できませんでした</p>
      ) : itemsQuery.isLoading ? <Loading /> : listItems.length === 0 ? (
        <p className="rounded-xl border bg-white py-12 text-center text-gray-500">条件に合う歌唱がまだありません。</p>
      ) : (
        <section className="space-y-4" aria-label="収録曲">
          <PerformanceListToolbar items={listItems} scope={items.length < preset.item_count ? 'shown' : 'all'} total={preset.item_count} />
          <PerformanceList items={listItems} numbered />
        </section>
      )}
    </div>
  );
}
