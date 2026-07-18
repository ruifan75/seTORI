import { useRef, useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { readingApi } from '../api/client';
import type { ImportReadingsResult } from '../api/types';
import { useToast } from './ui/Toast';

// 読みデータのエクスポート/インポート UI。
// 外部の（ネット情報を参照できる）AI で読みを作成してもらう運用を支える。
// エクスポート → AI に渡して reading 列を埋める → インポートで取り込む。
export default function ReadingsIO() {
  const { showToast } = useToast();
  const queryClient = useQueryClient();
  const [menuOpen, setMenuOpen] = useState(false);
  const fileRef = useRef<HTMLInputElement>(null);

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
      setMenuOpen(false);
    },
    onError: (err: Error) => showToast(`エクスポート失敗: ${err.message}`, 'error'),
  });

  const importMutation = useMutation({
    mutationFn: async (file: File) => {
      const text = await file.text();
      const isCSV = file.name.toLowerCase().endsWith('.csv');
      if (isCSV) return readingApi.importCSV(text);
      const payload = JSON.parse(text);
      return readingApi.importJSON(payload);
    },
    onSuccess: (r: ImportReadingsResult) => {
      const parts = [`アーティスト${r.artists_updated}件`, `曲名${r.songs_updated}件`];
      if (r.skipped > 0) parts.push(`スキップ${r.skipped}件`);
      showToast(`読みを取り込みました: ${parts.join('・')}`, r.errors && r.errors.length > 0 ? 'error' : 'success');
      queryClient.invalidateQueries({ queryKey: ['artists'] });
      queryClient.invalidateQueries({ queryKey: ['songs'] });
    },
    onError: (err: Error) => showToast(`インポート失敗: ${err.message}`, 'error'),
  });

  const handleFile = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (file) importMutation.mutate(file);
    e.target.value = ''; // 同じファイルを連続選択できるようにリセット
  };

  return (
    <div className="relative">
      <div className="flex gap-2 items-center">
        <button
          onClick={() => setMenuOpen((v) => !v)}
          disabled={exportMutation.isPending}
          title="アーティスト・曲名の読みをファイルに書き出します（外部 AI で読みを作成する用）"
          className="px-3 py-2 text-sm bg-white text-gray-700 border border-gray-300 font-medium rounded-lg hover:bg-gray-50 transition-colors disabled:opacity-50 shrink-0"
        >
          {exportMutation.isPending ? 'エクスポート中...' : '読みエクスポート'}
        </button>
        <button
          onClick={() => fileRef.current?.click()}
          disabled={importMutation.isPending}
          title="読みを埋めた JSON / CSV を取り込みます（片仮名は自動で平仮名に変換）"
          className="px-3 py-2 text-sm bg-white text-gray-700 border border-gray-300 font-medium rounded-lg hover:bg-gray-50 transition-colors disabled:opacity-50 shrink-0"
        >
          {importMutation.isPending ? 'インポート中...' : '読みインポート'}
        </button>
        <input
          ref={fileRef}
          type="file"
          accept=".json,.csv,application/json,text/csv"
          onChange={handleFile}
          className="hidden"
        />
      </div>

      {menuOpen && (
        <div className="absolute right-0 mt-1 w-56 bg-white rounded-lg shadow-lg border z-50 p-2 space-y-1">
          <p className="px-2 py-1 text-xs text-gray-400">エクスポート対象と形式</p>
          {([
            { label: '未整備のみ（JSON）', filter: 'needs_fix', format: 'json' },
            { label: '未整備のみ（CSV）', filter: 'needs_fix', format: 'csv' },
            { label: '全件（JSON）', filter: 'all', format: 'json' },
            { label: '全件（CSV）', filter: 'all', format: 'csv' },
          ] as const).map((opt) => (
            <button
              key={opt.label}
              onClick={() => exportMutation.mutate({ filter: opt.filter, format: opt.format })}
              className="w-full text-left px-2 py-1.5 text-sm text-gray-700 rounded hover:bg-indigo-50"
            >
              {opt.label}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
