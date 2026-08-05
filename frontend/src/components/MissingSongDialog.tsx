import { useEffect, useState } from 'react';
import { suggestionApi } from '../api/client';
import { useToast } from './ui/ToastContext';
import { formatSeconds, parseSeconds } from './usePerformanceTiming';

// 「この配信のここに、登録されていない曲がある」と報告するダイアログ。
//
// 時間の修正と違って曲名が要るので、ここだけは入力を求める。
// 開始時間は押した瞬間の再生位置を初期値に入れておき、そのままでも送れるようにする。
//
// 編集権限の有無にかかわらず提案として送る（曲の新規登録は影響が大きく、
// 承認時に曲マスタも作られるため必ず人の目を通す）。
export default function MissingSongDialog({
  streamId,
  streamTitle,
  startSeconds,
  onClose,
}: {
  streamId: string;
  streamTitle?: string;
  startSeconds: number;
  onClose: () => void;
}) {
  const { showToast } = useToast();
  const [songName, setSongName] = useState('');
  const [artist, setArtist] = useState('');
  const [startText, setStartText] = useState(formatSeconds(startSeconds));
  const [endText, setEndText] = useState('');
  const [note, setNote] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [onClose]);

  const submit = async () => {
    const start = parseSeconds(startText);
    if (start === null) {
      setError('開始時間は 1:23:45 / 2:30 / 150 の形式で入力してください');
      return;
    }
    // 空欄は「動画の最後まで」を意味する 0
    const end = endText.trim() === '' ? 0 : parseSeconds(endText);
    if (end === null) {
      setError('終了時間は 1:23:45 / 2:30 / 150 の形式で入力してください');
      return;
    }
    if (end !== 0 && end <= start) {
      setError(`終了（${formatSeconds(end)}）は開始（${formatSeconds(start)}）より後にしてください`);
      return;
    }
    setError('');

    setBusy(true);
    try {
      await suggestionApi.create({
        kind: 'perf.missing',
        payload: {
          stream_id: streamId,
          song_name: songName.trim(),
          original_artist: artist.trim(),
          start_seconds: start,
          end_seconds: end,
        },
        note: note.trim(),
      });
      showToast('抜けている曲として報告しました。管理者の確認をお待ちください', 'success');
      onClose();
    } catch (e) {
      showToast(`送信できませんでした: ${(e as Error).message}`, 'error');
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="fixed inset-0 z-[60] flex items-center justify-center bg-black/40 p-4" onClick={onClose}>
      <div
        className="bg-white rounded-xl shadow-xl w-full max-w-md p-5 max-h-[90vh] overflow-y-auto"
        onClick={(e) => e.stopPropagation()}
      >
        <h2 className="text-lg font-bold text-gray-900">抜けている曲を報告</h2>
        {streamTitle && <p className="text-xs text-gray-500 mt-0.5 line-clamp-2">{streamTitle}</p>}
        <p className="text-xs text-gray-400 mt-1">管理者が確認して歌唱記録を追加します</p>

        <label className="block mt-4">
          <span className="text-sm text-gray-600">曲名</span>
          <input
            type="text"
            value={songName}
            autoFocus
            onChange={(e) => setSongName(e.target.value)}
            placeholder="例：ロキ"
            className="mt-1 w-full px-3 py-2 text-sm border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
          />
        </label>

        <label className="block mt-3">
          <span className="text-sm text-gray-600">原曲アーティスト（任意）</span>
          <input
            type="text"
            value={artist}
            onChange={(e) => setArtist(e.target.value)}
            placeholder="例：みきとP"
            className="mt-1 w-full px-3 py-2 text-sm border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
          />
        </label>

        <div className="flex gap-3 mt-3">
          <label className="block flex-1">
            <span className="text-sm text-gray-600">開始</span>
            <input
              type="text"
              value={startText}
              onChange={(e) => setStartText(e.target.value)}
              className="mt-1 w-full px-3 py-2 text-sm font-mono border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
            />
          </label>
          <label className="block flex-1">
            <span className="text-sm text-gray-600">終了（任意）</span>
            <input
              type="text"
              value={endText}
              onChange={(e) => setEndText(e.target.value)}
              placeholder="空欄=最後まで"
              className="mt-1 w-full px-3 py-2 text-sm font-mono border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
            />
          </label>
        </div>

        <input
          type="text"
          value={note}
          onChange={(e) => setNote(e.target.value)}
          placeholder="補足（任意）"
          className="mt-3 w-full px-3 py-1.5 text-sm border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
        />

        {error && <p className="text-xs text-red-600 mt-2">{error}</p>}

        <div className="flex justify-end gap-2 mt-5">
          <button
            type="button"
            onClick={onClose}
            disabled={busy}
            className="px-4 py-2 text-sm bg-white border border-gray-300 text-gray-700 rounded-lg hover:bg-gray-50 disabled:opacity-50"
          >
            キャンセル
          </button>
          <button
            type="button"
            onClick={submit}
            disabled={busy || songName.trim() === ''}
            className="px-4 py-2 text-sm bg-indigo-600 text-white font-medium rounded-lg hover:bg-indigo-700 disabled:opacity-50"
          >
            {busy ? '送信中...' : '報告を送信'}
          </button>
        </div>
      </div>
    </div>
  );
}
