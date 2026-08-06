import { useEffect, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { performanceApi, songApi, suggestionApi } from '../api/client';
import type { Song } from '../api/types';
import { useAuthStore, hasPermission, PERM } from '../store/auth';
import LoginToSuggest from './LoginToSuggest';
import { useToast } from './ui/ToastContext';
import { withdrawSuggestion } from './usePerformanceTiming';

// 「この歌唱は別の曲だ」と直す／提案するダイアログ。
//
// 時間のズレと違い、曲の同一性は文字列の差分では表せない（別の曲マスタへ繋ぎ替える操作）。
// 既存の曲を検索して選ぶのが基本で、まだ登録されていない曲なら名前を直接入力できる
// （承認時に曲マスタも作られる）。
//
// 歌唱の ID は変わらないので、この歌唱を参照しているプレイリストはそのまま残る。
export default function SongSwapDialog({
  performanceId,
  currentSongName,
  subtitle,
  onClose,
  onDone,
}: {
  performanceId: string;
  currentSongName: string;
  subtitle?: string; // 配信タイトルなど
  onClose: () => void;
  onDone?: () => void;
}) {
  const { showToast } = useToast();
  const user = useAuthStore((s) => s.user);
  const canEdit = hasPermission(user, PERM.CONTENT_EDIT);
  const canSubmit = user !== null;

  const [query, setQuery] = useState('');
  const [picked, setPicked] = useState<Song | null>(null);
  // 検索で見つからないとき用の直接入力
  const [manual, setManual] = useState(false);
  const [manualName, setManualName] = useState('');
  const [manualArtist, setManualArtist] = useState('');
  const [note, setNote] = useState('');
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [onClose]);

  // 2文字以上で検索（1文字だと候補が多すぎて選べない）
  const { data, isFetching } = useQuery({
    queryKey: ['songs', 'swap-search', query],
    queryFn: () => songApi.list(1, 8, query),
    enabled: !manual && query.trim().length >= 2,
  });

  const target = manual
    ? { song_id: '', song_name: manualName.trim(), original_artist: manualArtist.trim() }
    : picked
      ? { song_id: picked.id, song_name: picked.name, original_artist: picked.original_artist }
      : null;

  const submit = async () => {
    if (!target || target.song_name === '') return;
    setBusy(true);
    try {
      if (canEdit && target.song_id !== '') {
        // 既存の曲へ繋ぎ替えるだけなら単件更新で足りる。
        // 提案を作って自分で承認すると、承認済み一覧が自己承認で埋まってしまう。
        await performanceApi.update(performanceId, { song_id: target.song_id });
        showToast(`${currentSongName} を ${target.song_name} に差し替えました`, 'success');
      } else if (canEdit) {
        // 未登録の曲名へ差し替える場合は曲の作成を伴うので、承認経路
        // （findOrCreateSong）を通す。曲を作る API が単独では無いため。
        const created = await suggestionApi.create({
          kind: 'perf.meta',
          target_id: performanceId,
          song_swap: target,
          note: note.trim(),
        });
        await suggestionApi.approve(created.id);
        showToast(`${currentSongName} を ${target.song_name} に差し替えました`, 'success');
      } else {
        const created = await suggestionApi.create({
          kind: 'perf.meta',
          target_id: performanceId,
          song_swap: target,
          note: note.trim(),
        });
        showToast('曲の差し替えを提案しました。管理者の確認をお待ちください', 'success', {
          label: '取り消す',
          onClick: () => withdrawSuggestion(created.id, showToast),
        });
      }
      onDone?.();
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
        <h2 className="text-lg font-bold text-gray-900">別の曲に差し替え</h2>
        {subtitle && <p className="text-xs text-gray-500 mt-0.5 line-clamp-2">{subtitle}</p>}
        <p className="text-xs text-gray-400 mt-1">
          現在：<span className="text-gray-600">{currentSongName}</span>
          {' · '}
          {canEdit ? '選ぶと即座に反映されます' : '管理者への提案として送られます'}
        </p>

        {!canSubmit && (
          <div className="mt-3 rounded-lg bg-gray-50 border p-3">
            <LoginToSuggest message="曲の差し替えの提案にはログインが必要です。" />
          </div>
        )}

        {!manual ? (
          <>
            <input
              type="text"
              value={query}
              autoFocus
              onChange={(e) => {
                setQuery(e.target.value);
                setPicked(null);
              }}
              placeholder="正しい曲名で検索"
              className="mt-4 w-full px-3 py-2 text-sm border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
            />

            <div className="mt-2 max-h-56 overflow-y-auto divide-y">
              {query.trim().length < 2 ? (
                <p className="text-xs text-gray-400 py-2">2文字以上で検索します</p>
              ) : isFetching ? (
                <p className="text-xs text-gray-400 py-2">検索中...</p>
              ) : !data || data.songs.length === 0 ? (
                <p className="text-xs text-gray-400 py-2">見つかりませんでした</p>
              ) : (
                data.songs.map((song) => {
                  const selected = picked?.id === song.id;
                  return (
                    <button
                      key={song.id}
                      type="button"
                      onClick={() => setPicked(song)}
                      className={`w-full text-left px-2 py-1.5 text-sm rounded ${
                        selected ? 'bg-indigo-50 text-indigo-900' : 'hover:bg-gray-50'
                      }`}
                    >
                      <span className="font-medium">{song.name}</span>
                      {song.original_artist && (
                        <span className="text-xs text-gray-500 ml-2">{song.original_artist}</span>
                      )}
                      {selected && <span className="text-indigo-600 ml-2">✓</span>}
                    </button>
                  );
                })
              )}
            </div>

            <button
              type="button"
              onClick={() => setManual(true)}
              className="mt-2 text-xs text-indigo-600 hover:underline"
            >
              一覧にない曲を入力する
            </button>
          </>
        ) : (
          <>
            <label className="block mt-4">
              <span className="text-sm text-gray-600">曲名</span>
              <input
                type="text"
                value={manualName}
                autoFocus
                onChange={(e) => setManualName(e.target.value)}
                className="mt-1 w-full px-3 py-2 text-sm border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
              />
            </label>
            <label className="block mt-3">
              <span className="text-sm text-gray-600">原曲アーティスト（任意）</span>
              <input
                type="text"
                value={manualArtist}
                onChange={(e) => setManualArtist(e.target.value)}
                className="mt-1 w-full px-3 py-2 text-sm border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
              />
            </label>
            <p className="text-xs text-gray-400 mt-1">承認時に曲として登録されます</p>
            <button
              type="button"
              onClick={() => setManual(false)}
              className="mt-2 text-xs text-indigo-600 hover:underline"
            >
              登録済みの曲から選ぶ
            </button>
          </>
        )}

        <input
          type="text"
          value={note}
          onChange={(e) => setNote(e.target.value)}
          placeholder={canEdit ? 'メモ（任意）' : '提案の理由（任意）'}
          className="mt-3 w-full px-3 py-1.5 text-sm border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
        />

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
            disabled={busy || !canSubmit || !target || target.song_name === ''}
            className="px-4 py-2 text-sm bg-indigo-600 text-white font-medium rounded-lg hover:bg-indigo-700 disabled:opacity-50"
          >
            {busy ? '送信中...' : canEdit ? 'この曲に差し替え' : '提案を送信'}
          </button>
        </div>
      </div>
    </div>
  );
}
