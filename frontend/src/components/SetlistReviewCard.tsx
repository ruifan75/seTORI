import { useState } from 'react';
import type { MissingSongPayload, Singer, Song, Suggestion } from '../api/types';
import SongSearchInput from './SongSearchInput';
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
  addition: '既存歌単への追加',
  duplicate: '同じ曲が重複',
};

// 食い違いの中身。バックエンドの conflictSong … と対応させること。
const CONFLICT_LABELS: Record<string, string> = {
  song: '同じ時間帯に別の曲が登録されています',
  start: '開始時間がずれています',
  end: '終了時間がずれています',
  addition: 'この配信には既にセットリストがあるため、追加は自動では行わず審査に回っています',
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
  // iTunes ID は検索で選んだときだけ入る。承認時に新曲を作るならそれに紐づく
  const [itunesID, setItunesID] = useState<number | undefined>(base?.itunes_id);
  const [error, setError] = useState('');

  if (!base) return null;

  const start = parseSeconds(startText);
  const end = endText.trim() === '' ? 0 : parseSeconds(endText);

  // 検索結果から曲を選ぶ。
  //
  // id が空文字なら「iTunes にはあるが DB に無い曲」── 照合先は無いので song_id は空のまま、
  // 曲名・歌手と **iTunes ID** だけ受け取る。承認時に findOrCreateSong が新曲を作り、
  // その iTunes ID が紐づく（iTunes ID は照合で最も強い証拠なので、ここで拾えるかどうかが
  // 次回以降の当たりやすさに効く）。
  const pickSong = (song: Song) => {
    setSongId(song.id);
    setSongName(song.name);
    setArtist(song.original_artist);
    setItunesID(song.itunes_ids?.[0]?.itunes_id);
  };

  // 候補ボタンから確定する。候補は DB の曲なので iTunes ID は持たない
  // （紐付いていれば楽曲側に既にあるので、ここで送る必要がない）。
  const pickCandidate = (songID: string, name: string, songArtist: string) => {
    setSongId(songID);
    setSongName(name);
    setArtist(songArtist);
    setItunesID(undefined);
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
      itunes_id: itunesID,
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

      {/* 食い違いの中身。「既存と食い違う」とだけ出していた頃は、
          人が何を見ればいいか分からず既存歌単を自分で探すことになっていた。 */}
      {base.conflict_kind && (
        <div className="rounded border border-amber-200 bg-amber-50/60 px-2 py-1.5 text-xs">
          <div className="text-amber-900">{CONFLICT_LABELS[base.conflict_kind] ?? base.conflict_kind}</div>
          {base.existing && (
            <div className="mt-1 space-y-0.5 text-gray-600">
              <div>
                <span className="text-gray-400 mr-1">既存</span>
                <button
                  onClick={() => youtubePlayerSeekTo(base.existing!.start_seconds)}
                  className="font-mono text-indigo-600 hover:text-indigo-900"
                  title="ここから再生"
                >
                  {formatSeconds(base.existing.start_seconds)}
                  {base.existing.end_seconds > 0 && `–${formatSeconds(base.existing.end_seconds)}`}
                </button>
                <span className="ml-1">{base.existing.song_name}</span>
                {base.existing.original_artist && (
                  <span className="text-gray-400"> / {base.existing.original_artist}</span>
                )}
              </div>
              <div>
                <span className="text-gray-400 mr-1">提案</span>
                <span className="font-mono">
                  {formatSeconds(base.start_seconds)}
                  {base.end_seconds > 0 && `–${formatSeconds(base.end_seconds)}`}
                </span>
                <span className="ml-1">{base.song_name}</span>
                {base.original_artist && <span className="text-gray-400"> / {base.original_artist}</span>}
              </div>
            </div>
          )}
        </div>
      )}

      {/* 曲 */}
      <div>
        <div className="flex flex-wrap items-start gap-2">
          <span className="text-xs text-gray-500 w-10 shrink-0 pt-2">曲</span>
          {/* 編集画面と同じ検索を使う（DB と iTunes を同時に引く）。
              別実装にしていた頃、ここだけ DB のみ・2 文字以上で、
              `糸` `恋` のような 1 文字の曲が引けず iTunes からの登録もできなかった。 */}
          <div className="flex-1 min-w-40">
            <SongSearchInput
              value={songName}
              onChange={(v) => {
                setSongName(v);
                // 曲名を手で書き換えたら、それはもう選んだ曲ではない
                setSongId('');
                setItunesID(undefined);
              }}
              onSelectSong={pickSong}
              placeholder="曲名を入力して検索（DB と iTunes）"
            />
          </div>
          <input
            type="text"
            value={artist}
            onChange={(e) => setArtist(e.target.value)}
            className="flex-1 min-w-32 px-2 py-2 text-sm border border-gray-300 rounded-lg"
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
                onClick={() => pickCandidate(c.song_id, c.name, c.artist)}
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
