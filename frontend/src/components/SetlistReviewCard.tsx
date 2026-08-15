import { useState } from 'react';
import type { MissingSongPayload, Singer, Song, Suggestion } from '../api/types';
import PerformanceFields, {
  type PerformanceFieldValues,
  type PerformanceTagOption,
} from './PerformanceFields';
import { formatTimeInput, parseTime } from '../utils/timeFormat';
import { youtubePlayerSeekTo } from './youtubePlayerControl';

// 審査へ回した理由。バックエンドの reviewNoEnd … と対応させること。
const REASON_LABELS: Record<string, string> = {
  no_end: '終了時間が無い',
  no_artist: '歌手が未記入',
  unmatched: '曲が決まらない',
  multi_singer: '歌手が複数',
  conflict: '既存と食い違う',
  low_conf: 'AI の確信度が低い',
  comment_only: 'コメントにのみ存在',
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
// 編集欄は編集画面と同じ PerformanceFields を使う ── 以前は曲・artist・時間しか
// 触れず、タグを付けるには登録後に編集画面へ行き直す必要があった。それでは
// 審査だけで仕事が終わらない。
//
// 一度に 1 枚だけ開く（どれを開くかは親が持つ）。開いた曲にプレイヤーが飛ぶので、
// セットリスト編集と同じ操作感になる。
export default function SetlistReviewCard({
  suggestion,
  participants,
  channelOwner,
  performanceTags,
  currentPlayerTime,
  expanded,
  draft,
  onDraftChange,
  onExpand,
  busy,
  onApprove,
  onReject,
}: {
  suggestion: Suggestion;
  participants: Singer[];
  channelOwner?: Singer | null;
  performanceTags: PerformanceTagOption[];
  currentPlayerTime: number | null;
  expanded: boolean;
  // 編集中の値は親が持つ。生コメントの ＋ から直接書き換えられるようにするため
  // ── カードの内部 state にしていた頃は effect で流し込むしかなく、
  // React が勧めない形（setState in effect）になっていた。
  draft: PerformanceFieldValues;
  onDraftChange: (patch: Partial<PerformanceFieldValues>) => void;
  onExpand: () => void;
  busy: boolean;
  onApprove: (id: string, payload: MissingSongPayload) => void;
  onReject: (id: string, note: string, notThisSong: boolean) => void;
}) {
  const base = suggestion.payload;
  const [error, setError] = useState('');

  if (!base) return null;

  const patch = onDraftChange;

  // 検索結果から曲を選ぶ。id が空文字なら「iTunes にはあるが DB に無い曲」で、
  // 照合先は無いまま曲名・歌手・iTunes ID・封面だけを受け取る。
  const selectSong = (song: Song) =>
    patch({
      matchedSongId: song.id || null,
      name: song.name,
      artist: song.original_artist,
      itunesId: song.itunes_ids?.[0]?.itunes_id ?? null,
      artUrl: song.arts || null,
    });

  const approve = () => {
    if (draft.end !== 0 && draft.end <= draft.start) {
      setError('終了は開始より後にしてください');
      return;
    }
    if (!draft.matchedSongId && draft.name.trim() === '') {
      setError('曲を選ぶか、曲名を入力してください');
      return;
    }
    setError('');
    onApprove(suggestion.id, {
      ...base,
      song_id: draft.matchedSongId ?? '',
      song_name: draft.name.trim(),
      original_artist: draft.artist.trim(),
      name_reading: draft.nameReading,
      original_artist_reading: draft.artistReading,
      start_seconds: draft.start,
      end_seconds: draft.end,
      tags: draft.tags,
      custom_tags: draft.customTags,
      singer_ids: draft.singerIds,
      itunes_id: draft.itunesId ?? undefined,
      art_url: draft.artUrl ?? undefined,
    });
  };

  const reasons = base.review_reasons ?? [];

  // 畳んでいるとき：1 行だけ。押すと開き、プレイヤーがその位置へ飛ぶ
  if (!expanded) {
    return (
      <button
        onClick={onExpand}
        className="w-full flex flex-wrap items-center gap-x-3 gap-y-1 py-2 px-2 text-left rounded hover:bg-gray-50"
      >
        <span className="font-mono text-xs text-indigo-600 shrink-0">
          {formatTimeInput(draft.start)}
        </span>
        <span className="text-sm text-gray-800">{draft.name || '（曲名なし）'}</span>
        {draft.artist && <span className="text-xs text-gray-400">/ {draft.artist}</span>}
        <span className="ml-auto flex flex-wrap gap-1">
          {reasons.map((r) => (
            <span
              key={r}
              className="px-1.5 py-0.5 rounded text-xs bg-amber-50 text-amber-800 border border-amber-200"
            >
              {REASON_LABELS[r] ?? r}
            </span>
          ))}
        </span>
      </button>
    );
  }

  return (
    <div className="border border-indigo-300 rounded-lg p-3 bg-white space-y-3">
      {/* 理由と由来。なぜこの行が来たのかを最初に出す */}
      <div className="flex flex-wrap items-center gap-1.5 text-xs">
        {reasons.map((r) => (
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
        <button onClick={onExpand} className="ml-auto text-gray-400 hover:text-gray-700" title="畳む">
          ▲
        </button>
      </div>

      {/* 食い違いの中身 */}
      {base.conflict_kind && (
        <div className="rounded border border-amber-200 bg-amber-50/60 px-2 py-1.5 text-xs">
          <div className="text-amber-900">{CONFLICT_LABELS[base.conflict_kind] ?? base.conflict_kind}</div>
          {base.existing && (
            <div className="mt-1 text-gray-600">
              <span className="text-gray-400 mr-1">既存</span>
              <button
                onClick={() => youtubePlayerSeekTo(base.existing!.start_seconds)}
                className="font-mono text-indigo-600 hover:text-indigo-900"
                title="ここから再生"
              >
                {formatTimeInput(base.existing.start_seconds)}
                {base.existing.end_seconds > 0 && `–${formatTimeInput(base.existing.end_seconds)}`}
              </button>
              <span className="ml-1">{base.existing.song_name}</span>
              {base.existing.original_artist && (
                <span className="text-gray-400"> / {base.existing.original_artist}</span>
              )}
            </div>
          )}
        </div>
      )}

      {/* 源の表記と AI の理由（照合で書き換わっているときの手がかり） */}
      {(base.raw_name || base.ai_reason) && (
        <div className="text-xs text-gray-500 space-y-0.5">
          {base.raw_name && (base.raw_name !== draft.name || base.raw_artist !== draft.artist) && (
            <div>
              源の表記: {base.raw_name}
              {base.raw_artist ? ` / ${base.raw_artist}` : ''}
            </div>
          )}
          {base.ai_reason && <div>AI: {base.ai_reason}</div>}
        </div>
      )}

      {/* 候補。押すだけで確定できる（実測で最有力候補はほぼ正解） */}
      {(base.candidates?.length ?? 0) > 0 && (
        <div className="flex flex-wrap gap-1.5">
          {base.candidates!.map((c) => (
            <button
              key={c.song_id}
              onClick={() =>
                patch({
                  matchedSongId: c.song_id,
                  name: c.name,
                  artist: c.artist,
                  itunesId: null,
                  artUrl: null,
                })
              }
              className={`px-2 py-1 text-xs rounded border ${
                c.song_id === draft.matchedSongId
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

      {/* 編集欄は編集画面と同じもの */}
      <PerformanceFields
        value={draft}
        onChange={patch}
        onSelectSong={selectSong}
        onTimeChange={(field, timeStr) => patch({ [field]: parseTime(timeStr) })}
        onToggleTag={(tagId) =>
          patch({
            tags: draft.tags.includes(tagId)
              ? draft.tags.filter((t) => t !== tagId)
              : [...draft.tags, tagId],
          })
        }
        onApplyEndSource={() => undefined}
        onClearItunes={() => patch({ itunesId: null })}
        onClearSong={() => patch({ matchedSongId: null })}
        performanceTags={performanceTags}
        currentPlayerTime={currentPlayerTime}
        participants={participants}
        channelOwner={channelOwner}
      />

      {suggestion.note && <p className="text-xs text-gray-500">💬 {suggestion.note}</p>}
      {(suggestion.overlaps?.length ?? 0) > 0 && (
        <p className="text-xs text-amber-700">
          ⚠ 時間が重なる既存の歌唱:{' '}
          {suggestion.overlaps!.map((o) => `${o.song_name}（${formatTimeInput(o.start_seconds)}）`).join('、')}
        </p>
      )}
      {error && <p className="text-xs text-red-600">{error}</p>}

      <div className="flex flex-wrap justify-end gap-2">
        {/* 否決は**提案時点の曲**について記録するので、画面で選び直した曲ではなく
            payload の内容で押せるかを決める */}
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
