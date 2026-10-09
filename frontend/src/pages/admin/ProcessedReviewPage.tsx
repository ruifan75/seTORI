import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { channelApi, processedReviewApi, tagApi, type ProcessedPreview, type ProcessedReviewFilters } from '../../api/client';
import { useToast } from '../../components/ui/ToastContext';
import { onViewerChange, sameViewer, viewerID } from '../../queryClient';
import { invalidateProcessedReviewQueries } from '../../utils/processedReviewCache';
import { reviewDateBoundary } from '../../utils/processedReviewDate';

const initialFilters: ProcessedReviewFilters = {
  is_processed: true, q: '', channel_id: '', tags: [], hidden: 'all', has_performances: 'all', from: '', until: '',
};
type Operation = { id: string; startedAs: string | null };

export default function ProcessedReviewPage() {
  const [filters, setFilters] = useState(initialFilters);
  const [offset, setOffset] = useState(0);
  const [selected, setSelected] = useState<string[]>([]);
  const [preview, setPreview] = useState<ProcessedPreview | null>(null);
  const qc = useQueryClient();
  const { showToast } = useToast();
  useEffect(() => onViewerChange(() => { setSelected([]); setPreview(null); }), []);
  const list = useQuery({ refetchOnMount: 'always', queryKey: ['processed-review', filters, offset], queryFn: () => processedReviewApi.list(filters, offset) });
  const runs = useQuery({ queryKey: ['processed-runs'], queryFn: processedReviewApi.runs });
  const channels = useQuery({ queryKey: ['singers', 'processed-options'], queryFn: () => channelApi.listGrouped(true) });
  const tags = useQuery({ queryKey: ['stream-tags'], queryFn: tagApi.listStreamTags });
  const invalidate = () => invalidateProcessedReviewQueries(qc);
  const check = useMutation({
    mutationFn: (args: { ids: string[]; after: boolean; startedAs: string | null }) => processedReviewApi.preview(args.ids, args.after),
    onSuccess: (data, args) => { if (!sameViewer(args.startedAs)) return; setPreview(data); void qc.invalidateQueries({ queryKey: ['processed-runs'] }); },
    onError: (err: Error, args) => { if (sameViewer(args.startedAs)) showToast(err.message, 'error'); },
  });
  const apply = useMutation({
    mutationFn: (args: Operation) => processedReviewApi.apply(args.id),
    onSuccess: (data, args) => { if (!sameViewer(args.startedAs)) return; showToast(`${data.changed}件の処理済み状態を変更しました`, 'success'); setSelected([]); setPreview(null); invalidate(); },
    onError: (err: Error, args) => { if (!sameViewer(args.startedAs)) return; setPreview(null); invalidate(); showToast(err.message, 'error'); },
  });
  const revert = useMutation({
    mutationFn: (args: Operation) => processedReviewApi.revert(args.id),
    onSuccess: (data, args) => { if (!sameViewer(args.startedAs)) return; showToast(`${data.reverted}件を戻しました。後の処理済み状態の変更・削除で見送ったもの: ${data.skipped}件`, 'info'); setSelected([]); setPreview(null); invalidate(); },
    onError: (err: Error, args) => { if (sameViewer(args.startedAs)) showToast(err.message, 'error'); },
  });
  const busy = check.isPending || apply.isPending || revert.isPending;
  const locked = busy || !!preview;
  const candidates = list.data?.candidates ?? [];
  const changeFilters = (next: Partial<ProcessedReviewFilters>) => { setFilters({ ...filters, ...next }); setOffset(0); setSelected([]); setPreview(null); };
  const channelOptions = [...(channels.data?.groups.flatMap((g) => g.singers) ?? []), ...(channels.data?.hidden ?? [])];
  const inputClass = 'border rounded px-2 py-1 w-full min-w-0';
  return <div className="space-y-6">
    <h1 className="text-2xl font-bold">処理済みを一括変更</h1>
    <p className="text-sm text-gray-600">処理済みは、人が内容を確認した印であり、自動処理の停止条件です。付けると今後の自動処理の対象から外れ、外すと再び対象になり得ます。実行中の処理は停止しません。歌唱・表示状態・会限の公開可否は変えません。</p>
    <p className="text-sm text-gray-600">歌唱の有無は、現在の権限で閲覧できる歌唱だけを基にします。</p>
    <fieldset disabled={locked} className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3">
      <legend className="font-medium mb-2">対象を絞り込む</legend>
      <label>変更先<select className={inputClass} value={String(filters.is_processed)} onChange={(e) => changeFilters({ is_processed: e.target.value === 'true' })}><option value="true">処理済みを付ける（未処理のみ）</option><option value="false">処理済みを外す（処理済みのみ）</option></select></label>
      <label>チャンネル（参加者を含む）<select className={inputClass} value={filters.channel_id} onChange={(e) => changeFilters({ channel_id: e.target.value })}><option value="">すべて</option>{channelOptions.map((c) => <option key={c.id} value={c.id}>{c.name}{c.is_hidden ? '（非表示）' : ''}</option>)}</select></label>
      <label>配信名<input className={inputClass} value={filters.q} onChange={(e) => changeFilters({ q: e.target.value })} /></label>
      <label>表示状態<select className={inputClass} value={filters.hidden} onChange={(e) => changeFilters({ hidden: e.target.value as ProcessedReviewFilters['hidden'] })}><option value="all">すべて</option><option value="false">表示</option><option value="true">非表示</option></select></label>
      <label>閲覧できる歌唱<select className={inputClass} value={filters.has_performances} onChange={(e) => changeFilters({ has_performances: e.target.value as ProcessedReviewFilters['has_performances'] })}><option value="all">有無を問わない</option><option value="true">あり</option><option value="false">なし</option></select></label>
      <label>配信タグ（複数はすべて一致・20個まで）<select multiple className={inputClass} value={filters.tags} onChange={(e) => changeFilters({ tags: Array.from(e.target.selectedOptions, (o) => o.value) })}>{tags.data?.map((tag) => <option key={tag.id} value={tag.id}>{tag.display_name}</option>)}</select></label>
      <label>配信日・開始<input type="date" className={inputClass} onChange={(e) => changeFilters({ from: reviewDateBoundary(e.target.value, false) })} /></label>
      <label>配信日・終了（含む）<input type="date" className={inputClass} onChange={(e) => changeFilters({ until: reviewDateBoundary(e.target.value, true) })} /></label>
    </fieldset>
    {(channels.isError || tags.isError) && <p role="alert" className="text-red-600">絞り込みの選択肢を取得できませんでした。</p>}
    <div className="flex flex-wrap gap-3"><span>{list.data?.total ?? 0}件</span><button disabled={locked || list.isFetching || !candidates.length} onClick={() => setSelected(selected.length === candidates.length ? [] : candidates.map((c) => c.id))} className="underline">{selected.length === candidates.length && candidates.length > 0 ? 'このページの選択を解除' : 'このページをすべて選択'}</button></div>
    {list.isError && <p role="alert" className="text-red-600">一覧の取得に失敗しました。</p>}
    {list.isPending ? <p>読み込み中…</p> : <div className="border rounded divide-y">
      {candidates.length === 0 && <p className="p-4">該当する配信はありません。</p>}
      {candidates.map((c) => <div key={c.id} className="flex gap-3 p-3 items-center">
        <input aria-label={`${c.title}を選択`} type="checkbox" disabled={locked || list.isFetching} checked={selected.includes(c.id)} onChange={(e) => setSelected(e.target.checked ? [...selected, c.id] : selected.filter((id) => id !== c.id))} />
        <div className="min-w-0 flex-1"><Link to={`/streams/${c.id}`} className="text-indigo-600 underline break-words">{c.title}</Link><p className="text-xs text-gray-500">{new Date(c.stream_date).toLocaleDateString('ja-JP')} / {c.is_processed ? '処理済み' : '未処理'} / {c.is_hidden ? '非表示' : '表示'}</p></div>
      </div>)}
    </div>}
    <div className="flex gap-4"><button disabled={offset === 0 || locked || list.isFetching} onClick={() => { setOffset(Math.max(0, offset - 100)); setSelected([]); }}>前へ</button><button disabled={offset + 100 >= (list.data?.total ?? 0) || locked || list.isFetching} onClick={() => { setOffset(offset + 100); setSelected([]); }}>次へ</button></div>
    {!preview && <button disabled={!selected.length || busy || list.isFetching} onClick={() => check.mutate({ ids: selected, after: filters.is_processed, startedAs: viewerID() })} className="bg-indigo-600 text-white px-4 py-2 rounded disabled:opacity-50">選んだ{selected.length}件の変更内容を確認</button>}
    {preview && <div role="dialog" aria-label="処理済み変更の確認" className="border border-amber-400 rounded p-4 space-y-3">
      <p>{preview.count}件の「{preview.is_processed ? '未処理' : '処理済み'}」を「{preview.is_processed ? '処理済み' : '未処理'}」に変更します。</p>
      <ul className="list-disc pl-5 break-words">{candidates.filter((c) => selected.includes(c.id)).map((c) => <li key={c.id}>{c.title}</li>)}</ul>
      <p>{preview.is_processed ? '今後の自動処理の対象から外れます。' : '自動処理の対象に戻り、解析・歌唱作成が再び行われる可能性があります。'}</p>
      <p className="text-sm">確認後に1件でも処理済み状態が確認時と違ったり、削除されたりしていたら全体を拒否します。実行後は、処理済み状態がこの実行の変更後の値と同じ配信だけを取り消せます。同期・解析による他の項目の更新は妨げになりません。自動処理がすでに作った歌唱なども戻しません。</p>
      <button disabled={busy} onClick={() => apply.mutate({ id: preview.run_id, startedAs: viewerID() })} className="bg-indigo-600 text-white px-4 py-2 rounded">{preview.is_processed ? '処理済みを付ける' : '処理済みを外す'}</button><button disabled={busy} onClick={() => setPreview(null)} className="ml-3">戻る</button>
    </div>}
    <div className="border rounded p-4 space-y-2"><h2 className="font-bold">変更の履歴（直近20件）</h2>
      {runs.isError && <p role="alert" className="text-red-600">履歴の取得に失敗しました。</p>}
      {runs.data?.map((run) => <div key={run.id} className="flex gap-3 flex-wrap text-sm">
        <span>{new Date(run.created_at).toLocaleString('ja-JP')} / {run.item_count}件 / {run.after_processed ? '処理済みを付ける' : '処理済みを外す'} / {{ preview: '確認のみ', applied: '実行済み', reverted: '取り消しました' }[run.status]}</span>
        {run.status === 'applied' && <button disabled={locked} onClick={() => { if (window.confirm(`${run.item_count}件の処理済み変更を取り消します。処理済み状態が変更後の値と違う配信や、削除された配信は見送ります。自動処理の結果は戻しません。`)) revert.mutate({ id: run.id, startedAs: viewerID() }); }} className="underline">取り消す</button>}
        {run.status === 'reverted' && <span>戻した件数 {run.reverted_count} / 見送り {run.item_count - run.reverted_count}</span>}
      </div>)}
    </div>
  </div>;
}
