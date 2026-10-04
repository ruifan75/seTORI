import { useState } from 'react';
import { Link } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { nonSingingApi, visibilityReviewApi } from '../../api/client';
import { useToast } from '../../components/ui/ToastContext';

export default function VisibilityReviewPage() {
  const [dismissed, setDismissed] = useState(false);
  const [offset, setOffset] = useState(0);
  const [selected, setSelected] = useState<string[]>([]);
  const [preview, setPreview] = useState<{ run_id: string; count: number } | null>(null);
  const qc = useQueryClient();
  const { showToast } = useToast();
  const list = useQuery({ queryKey: ['visibility-review', dismissed, offset], queryFn: () => visibilityReviewApi.list(dismissed, offset) });
  const runs = useQuery({ queryKey: ['visibility-runs'], queryFn: visibilityReviewApi.runs });
  const error = (err: Error) => showToast(err.message, 'error');
  const invalidate = () => { qc.invalidateQueries({ queryKey: ['visibility-review'] }); qc.invalidateQueries({ queryKey: ['visibility-runs'] }); qc.invalidateQueries({ queryKey: ['non-singing-candidates'] }); };
  const check = useMutation({ mutationFn: () => visibilityReviewApi.preview(selected), onSuccess: (data) => { setPreview(data); invalidate(); }, onError: error });
  const apply = useMutation({ mutationFn: () => visibilityReviewApi.apply(preview!.run_id), onSuccess: (data) => { showToast(`${data.changed}件を表示に戻しました`, 'success'); setSelected([]); setPreview(null); invalidate(); }, onError: (err: Error) => { setPreview(null); invalidate(); error(err); } });
  const revert = useMutation({ mutationFn: visibilityReviewApi.revert, onSuccess: (data) => { showToast(`${data.reverted}件を非表示に戻しました。後の変更・削除で見送ったもの: ${data.skipped}件`, 'info'); invalidate(); }, onError: error });
  const judgment = useMutation({ mutationFn: (id: string) => dismissed ? nonSingingApi.restore(id) : nonSingingApi.dismiss(id), onSuccess: () => { setSelected([]); setPreview(null); invalidate(); }, onError: error });
  const busy = check.isPending || apply.isPending || judgment.isPending;
  const candidates = list.data?.candidates ?? [];
  return <div className="space-y-6">
    <h1 className="text-2xl font-bold">非表示の配信を見直す</h1>
    <p className="text-sm text-gray-600">非表示で、音楽系タグがあり、180秒を超える配信です。内容を確認してから表示に戻してください。会限の歌唱は公開可否の設定に従って引き続き伏せられます。</p>
    <div className="space-x-3">
      <button disabled={busy || !!preview} onClick={() => { setDismissed(!dismissed); setOffset(0); setSelected([]); }} className="underline">{dismissed ? '見直し候補へ' : '歌枠ではないと判断した配信'}</button>
      <span>{list.data?.total ?? 0}件</span>
      {!dismissed && <button disabled={busy || !!preview || candidates.length === 0} onClick={() => setSelected(selected.length === candidates.length ? [] : candidates.map((c) => c.id))} className="underline">{selected.length === candidates.length && candidates.length > 0 ? 'このページの選択を解除' : 'このページをすべて選択'}</button>}
    </div>
    {list.isError && <p role="alert" className="text-red-600">一覧の取得に失敗しました。</p>}
    {list.isPending ? <p>読み込み中…</p> : <div className="border rounded divide-y">
      {candidates.length === 0 && <p className="p-4">該当する配信はありません。</p>}
      {candidates.map((c) => <div key={c.id} className="flex gap-3 p-3 items-center">
        {!dismissed && <input aria-label={`${c.title}を選択`} type="checkbox" disabled={busy || !!preview} checked={selected.includes(c.id)} onChange={(e) => setSelected(e.target.checked ? [...selected, c.id] : selected.filter((id) => id !== c.id))} />}
        <div className="flex-1"><Link to={`/streams/${c.id}`} className="text-indigo-600 underline">{c.title}</Link><p className="text-xs text-gray-500">{new Date(c.stream_date).toLocaleDateString('ja-JP')} / {Math.floor(c.duration_seconds / 60)}分 / {c.tags.join(', ')}</p></div>
        <button disabled={busy || !!preview} onClick={() => judgment.mutate(c.id)} className="text-sm underline">{dismissed ? '判断を取り消す' : '歌枠ではない'}</button>
      </div>)}
    </div>}
    <div className="flex gap-4"><button disabled={offset === 0 || busy || !!preview} onClick={() => { setOffset(Math.max(0, offset - 100)); setSelected([]); }}>前へ</button><button disabled={offset + 100 >= (list.data?.total ?? 0) || busy || !!preview} onClick={() => { setOffset(offset + 100); setSelected([]); }}>次へ</button></div>
    {!dismissed && !preview && <button disabled={selected.length === 0 || busy} onClick={() => check.mutate()} className="bg-indigo-600 text-white px-4 py-2 rounded disabled:opacity-50">選んだ{selected.length}件の変更内容を確認</button>}
    {preview && <div role="dialog" aria-label="表示変更の確認" className="border border-amber-400 rounded p-4 space-y-3">
      <p>{preview.count}件の「非表示」を「表示」に変更します。歌唱・処理済み・会限の公開可否は変更しません。</p>
      <ul className="list-disc pl-5">{candidates.filter((c) => selected.includes(c.id)).map((c) => <li key={c.id}>{c.title}</li>)}</ul>
      <p className="text-sm">実行後に取り消せます。その後に編集・再同期された配信は、取り消し時に見送ります。</p>
      <button disabled={busy} onClick={() => apply.mutate()} className="bg-indigo-600 text-white px-4 py-2 rounded">表示に戻す</button>
      <button disabled={busy} onClick={() => setPreview(null)} className="ml-3">戻る</button>
    </div>}
    <div className="border rounded p-4 space-y-2"><h2 className="font-bold">変更の履歴（直近20件）</h2>
      {runs.isError && <p className="text-red-600">履歴の取得に失敗しました。</p>}
      {runs.data?.map((run) => <div key={run.id} className="flex gap-3 flex-wrap text-sm">
        <span>{new Date(run.created_at).toLocaleString('ja-JP')} / {run.item_count}件 / {{ preview: '確認のみ', applied: '表示に戻しました', reverted: '取り消しました' }[run.status]}</span>
        {run.status === 'applied' && <button disabled={revert.isPending} onClick={() => { if (window.confirm(`${run.item_count}件の表示変更を取り消します。後に変更された配信は見送ります。`)) revert.mutate(run.id); }} className="underline">取り消す</button>}
        {run.status === 'reverted' && <span>戻した件数 {run.reverted_count} / 見送り {run.item_count - run.reverted_count}</span>}
      </div>)}
    </div>
  </div>;
}
