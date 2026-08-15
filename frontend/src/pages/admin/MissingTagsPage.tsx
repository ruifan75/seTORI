import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Link } from 'react-router-dom';
import { tagGapApi, tagApi, performanceApi } from '../../api/client';
import type { TagGap, TagGapDismissal, PerformanceTag } from '../../api/types';
import { formatTimeInput } from '../../utils/timeFormat';
import Loading from '../../components/ui/Loading';
import Tag from '../../components/ui/Tag';
import { useToast } from '../../components/ui/ToastContext';

// タグ漏れのレビュー。
//
// コメント / Holodex の解析はバージョンタグ（short / piano …）を付けるが、
// そのタグが歌唱に付くのは「編集フォームへ取り込んで保存した」ときだけ。
// 取り込む前に手で作った歌唱、規則を足す前に保存した歌唱、タグ ID の語彙が
// ずれていた時期の歌唱では、キャッシュにタグがあるのに歌唱に無い、という差が残る。
//
// 差分は毎回計算する派生値なので、付ければ次から消える。**記録するのは否定だけ**
// （「このタグは付けない」）── 人が意図的に付けなかったものを計算では区別できず、
// 残り続けると作業一覧として使えなくなるうえ、別の担当が「漏れ」と読んで付けてしまう。

export default function MissingTagsPage() {
  const queryClient = useQueryClient();
  const { showToast } = useToast();
  const [showDismissed, setShowDismissed] = useState(false);

  const { data, isLoading } = useQuery({
    queryKey: ['tag-gaps'],
    queryFn: () => tagGapApi.list(300),
  });

  const { data: perfTags = [] } = useQuery({
    queryKey: ['performance-tags'],
    queryFn: tagApi.listPerformanceTags,
  });
  const tagMeta = (id: string): PerformanceTag | undefined => perfTags.find((t) => t.id === id);

  const invalidate = () => {
    queryClient.invalidateQueries({ queryKey: ['tag-gaps'] });
    queryClient.invalidateQueries({ queryKey: ['stream'] });
  };

  // 付ける：タグは総入れ替えなので、今のタグに足したものを送る。
  const applyMutation = useMutation({
    mutationFn: ({ gap, tagId }: { gap: TagGap; tagId: string }) =>
      performanceApi.update(gap.performance_id, { tags: [...gap.current_tags, tagId] }),
    onSuccess: () => {
      showToast('タグを付けました', 'success');
      invalidate();
    },
    onError: (e: Error) => showToast(`付けられません: ${e.message}`, 'error'),
  });

  const dismissMutation = useMutation({
    mutationFn: ({ performanceId, tagId }: { performanceId: string; tagId: string }) =>
      tagGapApi.dismiss(performanceId, tagId),
    onSuccess: (r) => {
      showToast(r.message, 'success');
      invalidate();
    },
    onError: (e: Error) => showToast(`記録できません: ${e.message}`, 'error'),
  });

  const undismissMutation = useMutation({
    mutationFn: ({ performanceId, tagId }: { performanceId: string; tagId: string }) =>
      tagGapApi.undismiss(performanceId, tagId),
    onSuccess: (r) => {
      showToast(r.message, 'success');
      invalidate();
    },
    onError: (e: Error) => showToast(`取り消せません: ${e.message}`, 'error'),
  });

  if (isLoading) return <Loading />;

  const gaps = data?.gaps ?? [];
  const dismissed = data?.dismissed ?? [];
  const pending = applyMutation.isPending || dismissMutation.isPending;

  return (
    <div className="mx-auto max-w-5xl p-4">
      <h1 className="mb-1 text-2xl font-bold">タグ漏れ</h1>
      <p className="mb-4 text-sm text-gray-600">
        コメント / Holodex の解析が付けた演奏バージョンのタグのうち、歌唱に付いていないものです。
        付ければ一覧から消えます。意図的に付けないものは「無視」してください（次回から出ません）。
      </p>

      {gaps.length === 0 ? (
        <p className="rounded border border-gray-200 bg-gray-50 p-6 text-center text-sm text-gray-500">
          タグ漏れはありません。
        </p>
      ) : (
        <ul className="divide-y rounded border border-gray-200">
          {gaps.map((g) => (
            <li key={g.performance_id} className="p-3">
              <div className="flex flex-wrap items-baseline gap-x-2 gap-y-1">
                <Link
                  to={`/streams/${g.stream_id}`}
                  className="shrink-0 rounded bg-orange-50 px-1.5 font-mono text-xs text-orange-700 hover:bg-orange-100"
                  title="配信を開く"
                >
                  {formatTimeInput(g.start_seconds)}
                </Link>
                <Link to={`/songs/${g.song_id}`} className="font-medium text-gray-900 hover:text-indigo-600">
                  {g.song_name}
                </Link>
                {g.song_artist && <span className="text-sm text-gray-500">/ {g.song_artist}</span>}

                {/* 現在付いているタグ（何に足すのかが分かるように） */}
                {g.current_tags.map((id) => (
                  <Tag key={id} label={tagMeta(id)?.display_name || id} color={tagMeta(id)?.color} />
                ))}

                <span className="ml-auto shrink-0 text-xs text-gray-400">
                  {g.sources.join(' / ')}
                </span>
              </div>

              <div className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-gray-500">
                <span className="truncate" title={g.stream_title}>
                  {g.stream_title}
                </span>
              </div>

              {/*
                解析側の曲名。バージョン表記（"幾億光年 piano ver."）が残っているので、
                タグの根拠がそのまま読める。曲が食い違っていれば警告を出す ──
                同じ時刻に別の曲が登録されている＝そのタグがこの歌唱のものとは限らない。
                機械では裁けないので落とさず、人に見せて判断してもらう。
              */}
              {(!g.name_matches || g.cached_name !== g.song_name) && (
                <div className="mt-1 text-xs">
                  <span className="text-gray-400">解析側の表記: </span>
                  <span className={g.name_matches ? 'text-gray-600' : 'text-amber-800'}>{g.cached_name}</span>
                  {!g.name_matches && (
                    <span
                      className="ml-1 rounded bg-amber-50 px-1.5 py-0.5 text-amber-800"
                      title="同じ時刻に別の曲が登録されています。タグがこの歌唱のものとは限りません"
                    >
                      曲が一致しません
                    </span>
                  )}
                </div>
              )}

              <div className="mt-2 flex flex-wrap items-center gap-2">
                {g.missing_tags.map((tagId) => {
                  const meta = tagMeta(tagId);
                  return (
                    <span key={tagId} className="flex items-center gap-1 rounded border border-gray-200 py-0.5 pl-1 pr-0.5">
                      <Tag label={meta?.display_name || tagId} color={meta?.color || '#6B7280'} />
                      <button
                        onClick={() => applyMutation.mutate({ gap: g, tagId })}
                        disabled={pending || !meta}
                        className="rounded bg-indigo-600 px-2 py-0.5 text-xs text-white hover:bg-indigo-700 disabled:opacity-40"
                        title={
                          meta
                            ? 'この歌唱にこのタグを付ける'
                            : 'performance_tags に無い ID なので付けられません（語彙のずれ）'
                        }
                      >
                        付ける
                      </button>
                      <button
                        onClick={() => dismissMutation.mutate({ performanceId: g.performance_id, tagId })}
                        disabled={pending}
                        className="rounded border border-gray-300 px-2 py-0.5 text-xs text-gray-600 hover:bg-gray-50 disabled:opacity-40"
                        title="このタグは付けない（次回から一覧に出ません）"
                      >
                        無視
                      </button>
                    </span>
                  );
                })}
              </div>
            </li>
          ))}
        </ul>
      )}

      {/*
        無視の一覧。無視はその組を一覧から消し続けるので、見えないと誤って
        無視したものを戻せない（「この曲ではない」の判定を見直せるようにしてあるのと同じ理由）。
      */}
      <div className="mt-8 border-t pt-6">
        <button
          onClick={() => setShowDismissed(!showDismissed)}
          className="text-sm font-medium text-gray-700 hover:text-gray-900"
        >
          {showDismissed ? '▾' : '▸'} 「付けない」と判断した組（{dismissed.length}）
        </button>
        <p className="mt-1 text-xs text-gray-500">
          一覧から外し続けている記録です。取り消すと次回からまた出ます。
        </p>

        {showDismissed &&
          (dismissed.length === 0 ? (
            <p className="mt-3 text-sm text-gray-500">記録はありません。</p>
          ) : (
            <ul className="mt-3 divide-y text-xs">
              {dismissed.map((d: TagGapDismissal) => (
                <li key={`${d.performance_id}-${d.tag_id}`} className="flex flex-wrap items-center gap-x-2 gap-y-1 py-2">
                  <Link
                    to={`/streams/${d.stream_id}`}
                    className="rounded bg-orange-50 px-1.5 font-mono text-orange-700 hover:bg-orange-100"
                  >
                    {formatTimeInput(d.start_seconds)}
                  </Link>
                  <span className="text-gray-700">{d.song_name}</span>
                  <Tag
                    label={tagMeta(d.tag_id)?.display_name || d.tag_id}
                    color={tagMeta(d.tag_id)?.color || '#6B7280'}
                  />
                  {d.checked_by && <span className="text-gray-400">{d.checked_by}</span>}
                  <span className="text-gray-400">
                    {new Date(d.checked_at).toLocaleDateString('ja-JP')}
                  </span>
                  <button
                    onClick={() =>
                      undismissMutation.mutate({ performanceId: d.performance_id, tagId: d.tag_id })
                    }
                    disabled={undismissMutation.isPending}
                    className="ml-auto rounded border border-gray-300 px-2 py-0.5 text-gray-600 hover:bg-gray-50 disabled:opacity-50"
                    title="無視を取り消す"
                  >
                    取り消す
                  </button>
                </li>
              ))}
            </ul>
          ))}
      </div>
    </div>
  );
}
