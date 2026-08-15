import { useRef, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { artistApi, readingApi } from '../../api/client';
import type { ImportReadingsResult } from '../../api/types';
import Loading from '../../components/ui/Loading';
import { useToast } from '../../components/ui/ToastContext';

// 読み仮名の整備画面。対象はアーティスト名と曲名の両方。
//
// 読みは一覧の五十音順の並び替えと、かな検索に使う。漢字の名前は読みが無いと
// 名前順が文字コード順になってしまうので、機械では埋まらないぶんをここで片付ける。
//
// アーティスト一覧に間借りしていたのをここへ移した。書き出し・取り込みは
// 最初からアーティストと曲名の両方を扱っていたので、片方の一覧に置くと
// 「曲名の読みをアーティスト一覧から書き出す」という導線になっていた。
//
// 埋める手段は 2 つ。手軽なのは AI 補完（登録済みの AI プロバイダーを使う）で、
// 外部の（ネット情報を参照できる）AI に頼みたいときは書き出し → 読みを埋めて → 取り込み。
// 後者は珍しい固有名詞や作品名に強い。
export default function ReadingsPage() {
  const queryClient = useQueryClient();
  const { showToast } = useToast();
  const fileRef = useRef<HTMLInputElement>(null);
  const [importResult, setImportResult] = useState<ImportReadingsResult | null>(null);

  const { data: stats, isLoading } = useQuery({
    queryKey: ['readings-stats'],
    queryFn: readingApi.stats,
  });

  // 読みが変わると一覧の並び順・かな検索の結果が変わるので、まとめてキャッシュを捨てる
  const invalidate = () => {
    queryClient.invalidateQueries({ queryKey: ['readings-stats'] });
    queryClient.invalidateQueries({ queryKey: ['artists'] });
    queryClient.invalidateQueries({ queryKey: ['songs'] });
  };

  // AI 読み仮名補完（1回で各対象最大30件ずつ、複数回押して続きを処理）
  const backfillMutation = useMutation({
    mutationFn: () => artistApi.backfillReadings(),
    onSuccess: (r) => {
      const msg = `読み補完: アーティスト${r.artists_updated}件・曲名${r.songs_updated}件`;
      showToast(r.warning ? `${msg}（${r.warning}）` : msg, r.warning ? 'error' : 'success');
      invalidate();
    },
    onError: (err: Error) => showToast(`補完エラー: ${err.message}`, 'error'),
  });

  const download = (blob: Blob, filename: string) => {
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = filename;
    document.body.appendChild(a);
    a.click();
    a.remove();
    URL.revokeObjectURL(url);
  };

  const exportMutation = useMutation({
    mutationFn: ({ filter, format }: { filter: 'all' | 'needs_fix'; format: 'json' | 'csv' }) =>
      readingApi.exportBlob(filter, format).then((blob) => ({ blob, format })),
    onSuccess: ({ blob, format }) => {
      download(blob, `readings.${format === 'csv' ? 'csv' : 'json'}`);
      showToast('読みデータをエクスポートしました', 'success');
    },
    onError: (err: Error) => showToast(`エクスポート失敗: ${err.message}`, 'error'),
  });

  const importMutation = useMutation({
    mutationFn: async (file: File) => {
      const text = await file.text();
      if (file.name.toLowerCase().endsWith('.csv')) return readingApi.importCSV(text);
      return readingApi.importJSON(JSON.parse(text));
    },
    onSuccess: (r: ImportReadingsResult) => {
      setImportResult(r);
      const parts = [`アーティスト${r.artists_updated}件`, `曲名${r.songs_updated}件`];
      if (r.skipped > 0) parts.push(`スキップ${r.skipped}件`);
      showToast(
        `読みを取り込みました: ${parts.join('・')}`,
        r.errors && r.errors.length > 0 ? 'error' : 'success'
      );
      invalidate();
    },
    onError: (err: Error) => showToast(`インポート失敗: ${err.message}`, 'error'),
  });

  const handleFile = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (file) importMutation.mutate(file);
    e.target.value = ''; // 同じファイルを連続して選べるようにリセット
  };

  const exportOptions = [
    { label: '未整備のみ（JSON）', filter: 'needs_fix', format: 'json' },
    { label: '未整備のみ（CSV）', filter: 'needs_fix', format: 'csv' },
    { label: '全件（JSON）', filter: 'all', format: 'json' },
    { label: '全件（CSV）', filter: 'all', format: 'csv' },
  ] as const;

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold text-gray-900">読み仮名</h1>
        <p className="mt-2 text-sm text-gray-600">
          アーティスト名と曲名の読み（平仮名）を整備します。読みは一覧の五十音順の並び替えと、
          かな検索に使います。
        </p>
      </div>

      {/* 残件。判定はエクスポートの「未整備のみ」と同じ条件 */}
      {isLoading ? (
        <Loading />
      ) : stats ? (
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
          {[
            { label: 'アーティスト名', needsFix: stats.artists_needs_fix, total: stats.artists_total },
            { label: '曲名', needsFix: stats.songs_needs_fix, total: stats.songs_total },
          ].map((s) => (
            <div key={s.label} className="bg-white rounded-lg shadow-sm border p-6">
              <p className="text-sm text-gray-500">{s.label}</p>
              <p className="mt-1">
                <span
                  className={`text-3xl font-bold ${s.needsFix > 0 ? 'text-amber-600' : 'text-gray-900'}`}
                >
                  {s.needsFix.toLocaleString()}
                </span>
                <span className="ml-2 text-sm text-gray-500">件が未整備 / 全 {s.total.toLocaleString()} 件</span>
              </p>
            </div>
          ))}
        </div>
      ) : null}

      <p className="text-xs text-gray-500">
        「未整備」は<strong>名前に漢字を含むのに読みが空</strong>、または<strong>読みに漢字が残っている</strong>もの。
        仮名・英字だけの名前は並び替えに困らないので対象外です。
      </p>

      {/* 手段1：登録済みの AI プロバイダーで補完 */}
      <div className="bg-white rounded-lg shadow-sm border p-6 space-y-3">
        <h2 className="text-lg font-semibold text-gray-900">AI で補完する</h2>
        <p className="text-sm text-gray-600">
          未整備のものを登録済みの AI に読ませて埋めます。1 回で各対象 30 件ずつ処理するので、
          残件が減らなくなるまで繰り返し押してください。
        </p>
        <button
          onClick={() => backfillMutation.mutate()}
          disabled={backfillMutation.isPending}
          className="px-4 py-2 text-sm bg-indigo-600 text-white font-medium rounded-lg hover:bg-indigo-700 transition-colors disabled:opacity-50"
        >
          {backfillMutation.isPending ? 'AI補完中...' : '読みをAIで補完'}
        </button>
      </div>

      {/* 手段2：外部 AI に投げるための書き出し / 取り込み */}
      <div className="bg-white rounded-lg shadow-sm border p-6 space-y-4">
        <div>
          <h2 className="text-lg font-semibold text-gray-900">ファイルで受け渡す</h2>
          <p className="mt-1 text-sm text-gray-600">
            書き出したファイルを外部の（インターネットを参照できる）AI に渡して reading 列を埋めてもらい、
            そのまま取り込みます。珍しい固有名詞や作品名はこちらのほうが当たります。
          </p>
        </div>

        <div>
          <p className="text-sm font-medium text-gray-700 mb-2">1. エクスポート</p>
          <div className="flex flex-wrap gap-2">
            {exportOptions.map((opt) => (
              <button
                key={opt.label}
                onClick={() => exportMutation.mutate({ filter: opt.filter, format: opt.format })}
                disabled={exportMutation.isPending}
                className="px-3 py-2 text-sm bg-white text-gray-700 border border-gray-300 font-medium rounded-lg hover:bg-gray-50 transition-colors disabled:opacity-50"
              >
                {opt.label}
              </button>
            ))}
          </div>
          <p className="mt-2 text-xs text-gray-500">
            CSV の列は <code className="bg-gray-100 px-1 rounded">type,id,name,reading</code>（type は
            artist / song）。取り込みは id で突き合わせるので、<strong>id と type の列は消さないでください</strong>。
          </p>
        </div>

        <div>
          <p className="text-sm font-medium text-gray-700 mb-2">2. インポート</p>
          <button
            onClick={() => fileRef.current?.click()}
            disabled={importMutation.isPending}
            className="px-3 py-2 text-sm bg-white text-gray-700 border border-gray-300 font-medium rounded-lg hover:bg-gray-50 transition-colors disabled:opacity-50"
          >
            {importMutation.isPending ? 'インポート中...' : 'JSON / CSV を選ぶ'}
          </button>
          <input
            ref={fileRef}
            type="file"
            accept=".json,.csv,application/json,text/csv"
            onChange={handleFile}
            className="hidden"
          />
          <p className="mt-2 text-xs text-gray-500">
            片仮名は平仮名に直して取り込みます。漢字が残っている読みは採用せずスキップします
            （読みとして使えないため）。reading を空にすると、その項目の読みは消えます。
          </p>
        </div>

        {importResult && (
          <div className="rounded-lg border bg-gray-50 p-4 text-sm space-y-1">
            <p className="text-gray-700">
              アーティスト {importResult.artists_updated} 件・曲名 {importResult.songs_updated} 件を更新
              {importResult.skipped > 0 && `・${importResult.skipped} 件をスキップ`}
            </p>
            {importResult.errors && importResult.errors.length > 0 && (
              <ul className="list-disc list-inside text-red-600 space-y-0.5">
                {importResult.errors.slice(0, 20).map((e, i) => (
                  <li key={i}>{e}</li>
                ))}
                {importResult.errors.length > 20 && (
                  <li className="list-none text-gray-500">ほか {importResult.errors.length - 20} 件</li>
                )}
              </ul>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
