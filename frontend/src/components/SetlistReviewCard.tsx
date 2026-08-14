import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { songApi } from '../api/client';
import type { MissingSongPayload, Singer, Song, Suggestion } from '../api/types';
import { formatSeconds, parseSeconds } from './usePerformanceTiming';
import { youtubePlayerGetCurrentTime, youtubePlayerSeekTo } from './youtubePlayerControl';

// 審査へ回した理由。バックエンドの reviewNoEnd … と対応させること。
const REASON_LABELS: Record<string, string> = {
  no_end: '終了時間が無い',
  no_artist: '歌手が未記入',
  unmatched: '曲が決まらない',
  multi_singer: '歌手が複数',
  conflict: '既存と食い違う',
  low_conf: 'AI の確信度が低い',
  source_gap: '源の取りこぼしの疑い',
  duplicate: '同じ曲が重複',
};

const SOURCE_LABELS: Record<string, string> = { holodex: 'Holodex', comment: 'コメント' };
const VIA_LABELS: Record<string, string> = { rule: '規則で照合', ai: 'AI が照合' };

// 審査 1 件ぶんのカード。
//
// 一括が積んだ審査待ちは「そのまま登録する」ものではなく「人が直して登録する」もの。
// 再生して時間を確かめ、曲を選び直し、歌った人を選んでから承認する ── その全部を
// このカードの中で完結させ、承認は 1 往復（payload を添えた approve）で済ませる。
export default function SetlistReviewCard({
  suggestion,
  participants,
  busy,
  onApprove,
  onReject,
}: {
  suggestion: Suggestion;
  participants: Singer[];
  busy: boolean;
  onApprove: (id: string, payload: MissingSongPayload) => void;
  onReject: (id: string, note: string, notThisSong: boolean) => void;
}) {
  const base = suggestion.payload;
  const [songId, setSongId] = useState(base?.song_id ?? '');
  const [songName, setSongName] = useState(base?.song_name ?? '');
  const [artist, setArtist] = useState(base?.original_artist ?? '');
  const [startText, setStartText] = useState(formatSeconds(base?.start_seconds ?? 0));
  const [endText, setEndText] = useState(base?.end_seconds ? formatSeconds(base.end_seconds) : '');
  const [singerIds, setSingerIds] = useState<string[]>(base?.singer_ids ?? []);
  const [query, setQuery] = useState('');
  const [error, setError] = useState('');

  const { data: searchResult, isFetching } = useQuery({
    queryKey: ['songs', 'review-search', query],
    queryFn: () => songApi.list(1, 8, query),
    enabled: query.trim().length >= 2,
  });

  if (!base) return null;

  const start = parseSeconds(startText);
  const end = endText.trim() === '' ? 0 : parseSeconds(endText);

  const pickSong = (song: { id: string; name: string; original_artist: string }) => {
    setSongId(song.id);
    setSongName(song.name);
    setArtist(song.original_artist);
    setQuery('');
  };

  // 照合を解除する。曲名はそのまま残し、承認時に findOrCreateSong で作り直す。
  const clearSong = () => setSongId('');

  // 押した瞬間の再生位置を取り込む。ダイアログで問い直さない（PlaybackFeedback と同じ考え方）。
  const grabTime = (field: 'start' | 'end') => {
    const now = youtubePlayerGetCurrentTime();
    if (now === null) return;
    const text = formatSeconds(now);
    if (field === 'start') setStartText(text);
    else setEndText(text);
  };

  const nudge = (field: 'start' | 'end', delta: number) => {
    const cur = field === 'start' ? start : end;
    if (cur === null) return;
    const next = formatSeconds(Math.max(0, cur + delta));
    if (field === 'start') setStartText(next);
    else setEndText(next);
  };

  const approve = () => {
    if (start === null || end === null) {
      setError('時間は 1:23:45 / 2:30 / 150 の形式で入力してください');
      return;
    }
    if (end !== 0 && end <= start) {
      setError('終了は開始より後にしてください');
      return;
    }
    if (songId === '' && songName.trim() === '') {
      setError('曲を選ぶか、曲名を入力してください');
      return;
    }
    setError('');
    onApprove(suggestion.id, {
      ...base,
      song_id: songId,
      song_name: songName.trim(),
      original_artist: artist.trim(),
      start_seconds: start,
      end_seconds: end,
      singer_ids: singerIds,
    });
  };

  const toggleSinger = (id: string) =>
    setSingerIds((prev) => (prev.includes(id) ? prev.filter((s) => s !== id) : [...prev, id]));

  const matched = songId !== '';
  const changedFromRaw =
    (base.raw_name && base.raw_name !== songName) || (base.raw_artist && base.raw_artist !== artist);

  return (
    <div className="border rounded-lg p-3 bg-white space-y-3">
      {/* 理由と由来。なぜこの行が来たのかを最初に出す */}
      <div className="flex flex-wrap items-center gap-1.5 text-xs">
        {(base.review_reasons ?? []).map((r) => (
          <span key={r} className="px-1.5 py-0.5 rounded bg-amber-50 text-amber-800 border border-amber-200">
            {REASON_LABELS[r] ?? r}
          </span>
        ))}
        {base.source && <span className="text-gray-400">源: {SOURCE_LABELS[base.source] ?? base.source}</span>}
        {base.via && (
          <span className="text-gray-400">
            {VIA_LABELS[base.via] ?? base.via}
            {base.confidence ? ` ${Math.round(base.confidence * 100)}%` : ''}
          </span>
        )}
        {suggestion.created_by_name && (
          <span className="text-gray-400">提案: {suggestion.created_by_name}</span>
        )}
      </div>

      {/* 曲 */}
      <div>
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-xs text-gray-500 w-10 shrink-0">曲</span>
          <input
            type="text"
            value={songName}
            onChange={(e) => {
              setSongName(e.target.value);
              // 曲名を手で書き換えたら、それはもう選んだ曲ではない
              setSongId('');
            }}
            className="flex-1 min-w-40 px-2 py-1 text-sm border border-gray-300 rounded"
            placeholder="曲名"
          />
          <input
            type="text"
            value={artist}
            onChange={(e) => setArtist(e.target.value)}
            className="flex-1 min-w-32 px-2 py-1 text-sm border border-gray-300 rounded"
            placeholder="原曲アーティスト"
          />
        </div>

        <div className="mt-1 flex flex-wrap items-center gap-2 text-xs pl-12">
          {matched ? (
            <>
              <span className="text-green-700">✓ 登録済みの曲に紐づけ済み</span>
              <button onClick={clearSong} className="text-gray-500 hover:text-gray-800 underline">
                解除して新しい曲として登録
              </button>
            </>
          ) : (
            <span className="text-amber-700">未紐づけ（承認すると新しい曲が作られます）</span>
          )}
          {changedFromRaw && (
            <span className="text-gray-400">
              源の表記: {base.raw_name || '—'}
              {base.raw_artist ? ` / ${base.raw_artist}` : ''}
            </span>
          )}
        </div>

        {base.ai_reason && (
          <p className="mt-1 pl-12 text-xs text-gray-500">AI: {base.ai_reason}</p>
        )}

        {/* 候補。押すだけで確定できるようにする（実測で最有力候補はほぼ正解） */}
        {(base.candidates?.length ?? 0) > 0 && (
          <div className="mt-1.5 pl-12 flex flex-wrap gap-1.5">
            {base.candidates!.map((c) => (
              <button
                key={c.song_id}
                onClick={() => pickSong({ id: c.song_id, name: c.name, original_artist: c.artist })}
                className={`px-2 py-1 text-xs rounded border ${
                  c.song_id === songId
                    ? 'bg-indigo-600 text-white border-indigo-600'
                    : 'bg-white border-gray-300 hover:bg-indigo-50'
                }`}
                title={`${c.reason} ${Math.round(c.score * 100)}%`}
              >
                {c.name}
                {c.artist && <span className="opacity-70"> / {c.artist}</span>}
                <span className="ml-1 opacity-60">{Math.round(c.score * 100)}%</span>
              </button>
            ))}
          </div>
        )}

        {/* 候補に無いときの検索 */}
        <div className="mt-1.5 pl-12">
          <input
            type="text"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="登録済みの曲を検索して選ぶ（2文字以上）"
            className="w-full px-2 py-1 text-xs border border-gray-200 rounded"
          />
          {query.trim().length >= 2 && (
            <div className="mt-1 flex flex-wrap gap-1.5">
              {isFetching && <span className="text-xs text-gray-400">検索中…</span>}
              {searchResult?.songs.map((s: Song) => (
                <button
                  key={s.id}
                  onClick={() => pickSong(s)}
                  className="px-2 py-1 text-xs rounded border border-gray-300 bg-white hover:bg-indigo-50"
                >
                  {s.name}
                  {s.original_artist && <span className="opacity-70"> / {s.original_artist}</span>}
                </button>
              ))}
              {searchResult && searchResult.songs.length === 0 && !isFetching && (
                <span className="text-xs text-gray-400">見つかりません</span>
              )}
            </div>
          )}
        </div>
      </div>

      {/* 時間 */}
      <div className="flex flex-wrap items-center gap-2 text-xs">
        <span className="text-gray-500 w-10 shrink-0">時間</span>
        <TimeField
          label="開始"
          value={startText}
          onChange={setStartText}
          onSeek={() => start !== null && youtubePlayerSeekTo(start)}
          onGrab={() => grabTime('start')}
          onNudge={(d) => nudge('start', d)}
        />
        <TimeField
          label="終了"
          value={endText}
          placeholder="空=最後まで"
          onChange={setEndText}
          onSeek={() => end !== null && end > 0 && youtubePlayerSeekTo(end)}
          onGrab={() => grabTime('end')}
          onNudge={(d) => nudge('end', d)}
        />
        {base.end_source && (
          <span className="text-gray-400" title="終了時間の由来">
            由来: {base.end_source}
          </span>
        )}
      </div>

      {/* 歌手。複数人の配信では機械が決められないので、参加者を並べて選ばせる */}
      <div className="flex flex-wrap items-center gap-2 text-xs">
        <span className="text-gray-500 w-10 shrink-0">歌手</span>
        {participants.length === 0 ? (
          <span className="text-gray-400">この配信の参加者が登録されていません</span>
        ) : (
          participants.map((p) => (
            <label
              key={p.id}
              className={`px-2 py-1 rounded border cursor-pointer ${
                singerIds.includes(p.id)
                  ? 'bg-indigo-50 border-indigo-300 text-indigo-800'
                  : 'bg-white border-gray-200 text-gray-600'
              }`}
            >
              <input
                type="checkbox"
                checked={singerIds.includes(p.id)}
                onChange={() => toggleSinger(p.id)}
                className="mr-1 accent-indigo-600"
              />
              {p.name}
            </label>
          ))
        )}
      </div>

      {suggestion.note && <p className="text-xs text-gray-500">💬 {suggestion.note}</p>}
      {(suggestion.overlaps?.length ?? 0) > 0 && (
        <p className="text-xs text-amber-700">
          ⚠ 時間が重なる既存の歌唱:{' '}
          {suggestion.overlaps!.map((o) => `${o.song_name}（${formatSeconds(o.start_seconds)}）`).join('、')}
        </p>
      )}
      {error && <p className="text-xs text-red-600">{error}</p>}

      <div className="flex flex-wrap justify-end gap-2">
        {/* 否決は**提案時点の曲**（照合が出した答え）について記録するので、
            画面で選び直した曲ではなく payload の内容で押せるかを決める */}
        <button
          onClick={() => onReject(suggestion.id, 'この表記はこの曲ではない', true)}
          disabled={busy || (!base.song_id && (base.candidates?.length ?? 0) === 0)}
          title="次回の一括作成で同じ組を提案しないよう、否決を記録します"
          className="px-3 py-1.5 text-xs bg-white border border-gray-300 text-gray-600 rounded-lg hover:bg-gray-50 disabled:opacity-40"
        >
          この曲ではない
        </button>
        <button
          onClick={() => onReject(suggestion.id, '', false)}
          disabled={busy}
          className="px-3 py-1.5 text-xs bg-white border border-gray-300 text-gray-600 rounded-lg hover:bg-gray-50 disabled:opacity-50"
        >
          却下
        </button>
        <button
          onClick={approve}
          disabled={busy}
          className="px-3 py-1.5 text-xs bg-indigo-600 text-white font-medium rounded-lg hover:bg-indigo-700 disabled:opacity-50"
        >
          この内容で登録
        </button>
      </div>
    </div>
  );
}

// 時間 1 つぶんの入力。再生位置へ飛ぶ／今の再生位置を取り込む／±の微調整。
function TimeField({
  label,
  value,
  placeholder,
  onChange,
  onSeek,
  onGrab,
  onNudge,
}: {
  label: string;
  value: string;
  placeholder?: string;
  onChange: (v: string) => void;
  onSeek: () => void;
  onGrab: () => void;
  onNudge: (delta: number) => void;
}) {
  return (
    <span className="inline-flex items-center gap-1">
      <span className="text-gray-500">{label}</span>
      <button onClick={() => onNudge(-1)} className="px-1 text-gray-400 hover:text-gray-700" title="1秒戻す">
        −
      </button>
      <input
        type="text"
        value={value}
        placeholder={placeholder}
        onChange={(e) => onChange(e.target.value)}
        className="w-20 px-2 py-1 font-mono text-xs border border-gray-300 rounded text-center"
      />
      <button onClick={() => onNudge(1)} className="px-1 text-gray-400 hover:text-gray-700" title="1秒進める">
        ＋
      </button>
      <button onClick={onSeek} className="px-1.5 py-0.5 rounded hover:bg-gray-100" title="ここから再生">
        ▶
      </button>
      <button
        onClick={onGrab}
        className="px-1.5 py-0.5 rounded hover:bg-gray-100 text-gray-500"
        title="今の再生位置を取り込む"
      >
        ⤓
      </button>
    </span>
  );
}
