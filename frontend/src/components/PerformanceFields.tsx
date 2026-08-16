import { useEffect, useMemo, useState } from 'react';
import SongSearchInput from './SongSearchInput';
import SingerSearchInput from './SingerSearchInput';
import ArtistSearchInput from './ArtistSearchInput';
import FieldProvenance from './FieldProvenance';
import { playerGetCurrentTime, playerSeekTo } from './youtubePlayerControl';
import { usePlayerScope } from './playerScope';
import { usePlayerTime } from './usePlayerTime';
import TimestampTweaker from './TimestampTweaker';
import { formatTimeInput, formatDuration } from '../utils/timeFormat';
import type { ArtistAliasProposal, FieldChange, Singer, Song } from '../api/types';
import { itunesApi } from '../api/client';

// 歌唱 1 件の編集欄（曲・アーティスト・時間・タグ・ボーカル）。
//
// **編集画面と審査画面で同じものを使う。** 審査カードは自前で最小限の欄しか
// 持っておらず、タグを付けるには登録後に編集画面へ行き直すことになっていた。
// それでは審査だけで仕事が終わらない ── 同じ操作は同じ実装を通す。
//
// 値の形は編集画面の EditableSong の部分集合。編集画面はこれに確認状態や
// 別名義の申し送りを足した型を持っているので、そのまま value に渡せる。
export interface PerformanceFieldValues {
  id: string;
  name: string;
  nameReading: string;
  artist: string;
  artistReading: string;
  start: number;
  end: number;
  tags: string[];
  customTags: string[];
  singerIds: string[];
  matchedSongId: string | null;
  artUrl: string | null;
  itunesId: number | null;
  itunesFromDb?: boolean;
  trackDuration?: number | null;
  isEndTimeEstimated?: boolean;
  chatEnd?: number;
  commentEnd?: number;
  changes?: FieldChange[];
  mergedFrom?: string[];
  artistAlias?: ArtistAliasProposal;
  aliasChecked?: boolean;
  endDiff?: number;
  originalCommentEnd?: number;
  aiNormalizedName?: string;
  aiNormalizedArtist?: string;
}

export interface PerformanceTagOption {
  id: string;
  label: string;
  color: string;
}

interface Props {
  value: PerformanceFieldValues;
  onChange: (patch: Partial<PerformanceFieldValues>) => void;
  onSelectSong: (song: Song) => void;
  onTimeChange: (field: 'start' | 'end', timeStr: string) => void;
  onToggleTag: (tagId: string) => void;
  onApplyEndSource: (source: 'chat' | 'comment') => void;
  // iTunes ID の紐付けを外す。誤った ID が song_itunes に焼き付くのを防ぐ
  onClearItunes?: () => void;
  // 既存曲への紐付けを解除する（＝新しい曲として登録し直す）
  onClearSong?: () => void;
  performanceTags: PerformanceTagOption[];
  participants: Singer[];
  channelOwner?: Singer | null;
  // 参加者に居ない歌手を選んだとき、配信の参加者へ足す（編集画面のみ）
  onAddParticipant?: (singer: Singer) => void;
  showToast?: (message: string, type?: 'success' | 'error' | 'info') => void;
}

// MatchStatus は「今この行がどの楽曲・どの iTunes ID に結びついているか」を出す。
//
// **ここに置くのが要点。** 以前は編集画面の外殻にだけあり、審査画面には
// iTunes の状態がまったく出ていなかった ── 検索で iTunes の曲を選んでも
// 画面上は何も変わらず、紐付いたのか分からなかった。入力欄のすぐ下に置けば
// 両方の画面で同じものが出る。
//
// iTunes が 3 態あるのは、**保存時に紐付けが作られる**状態を目立たせるため。
// primary な iTunes ID は Holodex へのアップロードにも使われるので、
// 誤った紐付けは外部へ伝播する。
function MatchStatus({
  value,
  onClearItunes,
  onClearSong,
}: {
  value: PerformanceFieldValues;
  onClearItunes?: () => void;
  onClearSong?: () => void;
}) {
  return (
    <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs">
      {value.matchedSongId ? (
        <span className="text-gray-500">
          楽曲{' '}
          <a
            href={`/songs/${value.matchedSongId}`}
            target="_blank"
            rel="noopener noreferrer"
            className="font-mono text-indigo-600 hover:underline"
          >
            {value.matchedSongId.slice(0, 8)}…
          </a>{' '}
          （既存）
          {onClearSong && (
            <button
              onClick={onClearSong}
              className="ml-1 text-gray-400 hover:text-red-600"
              title="紐付けを解除し、新しい曲として登録する"
            >
              ×
            </button>
          )}
        </span>
      ) : (
        <span className="text-green-700">新規作成されます</span>
      )}

      {value.itunesId ? (
        <span className={value.itunesFromDb ? 'text-gray-500' : 'text-amber-700'}>
          iTunes <span className="font-mono">{value.itunesId}</span>
          {value.itunesFromDb ? '（紐付け済み）' : '（保存時に紐付け）'}
          {/* 外せるのは「まだ書き込まれていない紐付け」だけ。保存済みのものを
              ここで消しても保存時に何も起きない（linkItunesID は追加しかしない）ので、
              取り消せるかのように見せない。解除は楽曲ページの iTunes 管理で行う */}
          {onClearItunes && !value.itunesFromDb && (
            <button
              onClick={onClearItunes}
              className="ml-1 text-gray-400 hover:text-red-600"
              title="この iTunes ID の紐付けを外す"
            >
              ×
            </button>
          )}
        </span>
      ) : (
        <span className="text-gray-400">iTunes なし</span>
      )}
    </div>
  );
}

export default function PerformanceFields({
  value,
  onChange,
  onSelectSong,
  onTimeChange,
  onToggleTag,
  onApplyEndSource,
  onClearItunes,
  onClearSong,
  performanceTags,
  participants,
  channelOwner,
  onAddParticipant,
  showToast,
}: Props) {
  const [searchingSinger, setSearchingSinger] = useState(false);
  // 再生位置は自分で追う。親から prop で渡していた頃は、親の再レンダー頻度が
  // そのまま更新頻度になっていた（1 秒に 1 回＝±6 秒の窓では幅の 8%）
  const scope = usePlayerScope();
  const currentPlayerTime = usePlayerTime(scope);

  // iTunes の再生時間。呼び出し側が持っていなければ ID から引く。
  //
  // **ここで引くのが要点。** 編集画面は自前で引いていたが審査画面は引いておらず、
  // 曲を iTunes から選んでも終了時間の「+??:??」が押せないままだった。
  // 片側にだけ処理を置くと、共用にした意味が無い。
  const [fetchedDuration, setFetchedDuration] = useState<{ itunesId: number; seconds: number } | null>(null);
  useEffect(() => {
    const id = value.itunesId;
    if (value.trackDuration != null || id == null) return;
    let cancelled = false;
    itunesApi
      .queryById(id)
      .then((r) => {
        if (!cancelled && r?.track_time_millis) {
          setFetchedDuration({ itunesId: id, seconds: Math.round(r.track_time_millis / 1000) });
        }
      })
      .catch(() => undefined);
    return () => {
      cancelled = true;
    };
  }, [value.itunesId, value.trackDuration]);

  // 引いた値は取得元の ID と対で持つ。曲を選び直した直後に、前の曲の長さを出さないため
  const trackDuration =
    value.trackDuration ??
    (fetchedDuration && fetchedDuration.itunesId === value.itunesId ? fetchedDuration.seconds : null);

  // 並べる候補＝配信の参加者。チャンネル主を先頭に置く（歌枠では最も押される）。
  // 選択済みなのに参加者に居ない歌手も足す ── 落とすと、値は送られるのに
  // 画面には出ないという最悪の形になる（外すことも確認することもできない）
  const vocalOptions = useMemo(() => {
    const byId = new Map<string, Singer>();
    if (channelOwner) byId.set(channelOwner.id, channelOwner);
    for (const singer of participants) {
      if (!byId.has(singer.id)) byId.set(singer.id, singer);
    }
    for (const id of value.singerIds) {
      if (!byId.has(id)) byId.set(id, { id, name: id } as Singer);
    }
    return Array.from(byId.values());
  }, [participants, channelOwner, value.singerIds]);

  return (
    <>
                    <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                      {/* Song Name with Search */}
                      <div>
                        <label className="block text-sm font-medium text-gray-700 mb-1">
                          楽曲名{' '}
                          <span className="text-gray-400 font-normal">
                            (入力して検索・iTunes ID も可)
                          </span>
                        </label>
                        <SongSearchInput
                          value={value.name}
                          onChange={(value) => onChange({ name: value })}
                          onSelectSong={(selectedSong) => onSelectSong(selectedSong)}
                          placeholder="楽曲名、または iTunes ID を入力"
                          showToast={showToast}
                        />
                        <MatchStatus value={value} onClearItunes={onClearItunes} onClearSong={onClearSong} />
                        {/* 由来（元の値 → どの処理 → 今の値）。changes が無い古い経路は従来表示 */}
                        {value.changes?.length ? (
                          <FieldProvenance changes={value.changes} field="name" />
                        ) : value.aiNormalizedName ? (
                          <div className="mt-1 text-sm">
                            <span className="text-gray-500">AI修正:</span>{' '}
                            <span className="line-through text-gray-400">{value.aiNormalizedName}</span>
                            {' → '}
                            <span className="text-blue-600 font-medium">{value.name}</span>
                          </div>
                        ) : null}
                        {/* 統合元の表示 */}
                        {value.mergedFrom && value.mergedFrom.length > 0 && (
                          <div className="mt-1 text-sm">
                            <span className="text-orange-600">統合:</span>{' '}
                            <span className="text-gray-500">{value.mergedFrom.join(', ')}</span>
                          </div>
                        )}
                      </div>

                      {/* Artist */}
                      <div>
                        <label className="block text-sm font-medium text-gray-700 mb-1">
                          原曲アーティスト
                        </label>
                        <ArtistSearchInput
                          value={value.artist}
                          onChange={(artist) => onChange({ artist })}
                          onSelectArtist={(artist) =>
                            onChange({
                              artist: artist.name,
                              // 読みは DB のものを引き継ぐ。空で上書きすると、
                              // 既に入っている読みを消してしまう
                              ...(artist.name_reading ? { artistReading: artist.name_reading } : {}),
                            })
                          }
                          placeholder="アーティスト名を入力して検索"
                        />
                        {/* 由来（元の値 → どの処理 → 今の値）。changes が無い古い経路は従来表示 */}
                        {value.changes?.length ? (
                          <FieldProvenance changes={value.changes} field="artist" />
                        ) : value.aiNormalizedArtist ? (
                          <div className="mt-1 text-sm">
                            <span className="text-gray-500">AI修正:</span>{' '}
                            <span className="line-through text-gray-400">{value.aiNormalizedArtist}</span>
                            {' → '}
                            <span className="text-blue-600 font-medium">{value.artist}</span>
                          </div>
                        ) : null}

                        {/*
                          歌手名が DB と違うときの申し送り。別名義は**その人の全楽曲に効く**ので、
                          読み込んだだけでは登録せず、保存したときにだけ書く。
                          既定でチェックが入るのは AI が「同一人物」と言った場合だけ
                          ── メルト / 初音ミク に対し DB が ryo (supercell) のような
                          作曲者と原曲歌手の取り違えは「同じ曲だが別人」で、こちらの方が多い。
                        */}
                        {value.artistAlias && (
                          <label className="mt-2 flex items-start gap-2 rounded border border-amber-200 bg-amber-50 px-2 py-1.5 text-xs text-amber-900">
                            <input
                              type="checkbox"
                              checked={!!value.aliasChecked}
                              onChange={(e) => onChange({ aliasChecked: e.target.checked })}
                              className="mt-0.5"
                            />
                            <span>
                              <span className="font-medium">{value.artistAlias.alias}</span>
                              {' は '}
                              <span className="font-medium">{value.artistAlias.canonical}</span>
                              {' の別名義として登録する'}
                              <span className="ml-1 text-amber-700/70">
                                {value.artistAlias.same_artist
                                  ? '（AI：同一人物）'
                                  : '（AI：別人と判定。同じ曲だが歌った人が違う場合はチェックしない）'}
                              </span>
                            </span>
                          </label>
                        )}
                      </div>

                      {/* Start Time */}
                      <div>
                        <label className="block text-sm font-medium text-gray-700 mb-1">
                          開始時間
                        </label>
                        <div className="flex gap-2">
                          <input
                            key={`start-${value.id}-${value.start}`}
                            type="text"
                            defaultValue={formatTimeInput(value.start)}
                            onBlur={(e) => onTimeChange('start', e.target.value)}
                            onKeyDown={(e) => {
                              if (e.key === 'Enter') {
                                onTimeChange('start', e.currentTarget.value);
                                e.currentTarget.blur();
                              }
                            }}
                            className="w-32 px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent font-mono"
                            placeholder="0:00"
                          />
                          <button
                            onClick={() => {
                              const currentTime = playerGetCurrentTime(scope);
                              if (currentTime !== null) {
                                onChange({ start: Math.floor(currentTime) });
                              }
                            }}
                            className="px-3 py-2 bg-indigo-100 text-indigo-700 rounded-lg hover:bg-indigo-200 transition-colors font-mono text-sm whitespace-nowrap min-w-[5.5rem]"
                            title="現在の再生時間を設定"
                          >
                            {currentPlayerTime !== null ? formatTimeInput(Math.floor(currentPlayerTime)) : '--:--'}
                          </button>
                          <button
                            onClick={() => playerSeekTo(scope, value.start)}
                            className="px-3 py-2 bg-red-100 text-red-600 rounded-lg hover:bg-red-200 transition-colors"
                            title="この時間から再生"
                          >
                            <svg className="w-5 h-5" fill="currentColor" viewBox="0 0 24 24">
                              <path d="M8 5v14l11-7z" />
                            </svg>
                          </button>
                        </div>
                        {/* ±6秒微調整（離すと確定して試聴） */}
                        <TimestampTweaker
                          value={value.start}
                          mode="start"
                          onChange={(v) => onChange({ start: v })}
                        />
                      </div>

                      {/* End Time */}
                      <div>
                        <label className="block text-sm font-medium text-gray-700 mb-1">
                          終了時間 <span className="text-red-500">*</span>
                        </label>
                        <div className="flex gap-2">
                          <input
                            key={`end-${value.id}-${value.end}`}
                            type="text"
                            defaultValue={value.end ? formatTimeInput(value.end) : ''}
                            onBlur={(e) => onTimeChange('end', e.target.value)}
                            onKeyDown={(e) => {
                              if (e.key === 'Enter') {
                                onTimeChange('end', e.currentTarget.value);
                                e.currentTarget.blur();
                              }
                            }}
                            className={`w-32 px-3 py-2 border rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent font-mono ${
                              value.end === 0 ? 'border-red-400 bg-red-50' : value.isEndTimeEstimated ? 'border-orange-300 bg-orange-50' : 'border-gray-300'
                            }`}
                            placeholder={value.end === 0 ? "曲の長さボタンで自動設定" : "0:00"}
                          />
                          {/* 曲の長さを設定するボタン */}
                          <button
                            onClick={() => {
                              if (trackDuration) {
                                onChange({ end: value.start + trackDuration, isEndTimeEstimated: false });
                              }
                            }}
                            disabled={!trackDuration}
                            className={`px-3 py-2 rounded-lg font-mono text-sm font-medium transition-colors whitespace-nowrap min-w-[5.5rem] ${
                              trackDuration
                                ? 'bg-indigo-100 text-indigo-700 hover:bg-indigo-200'
                                : 'bg-gray-100 text-gray-400 cursor-not-allowed'
                            }`}
                            title={trackDuration ? 'iTunes の曲の長さを適用' : '曲の長さ情報なし'}
                          >
                            {formatDuration(trackDuration)}
                          </button>
                          <button
                            onClick={() => {
                              const currentTime = playerGetCurrentTime(scope);
                              if (currentTime !== null) {
                                onChange({ end: Math.floor(currentTime) });
                              }
                            }}
                            className="px-3 py-2 bg-indigo-100 text-indigo-700 rounded-lg hover:bg-indigo-200 transition-colors font-mono text-sm whitespace-nowrap min-w-[5.5rem]"
                            title="現在の再生時間を設定"
                          >
                            {currentPlayerTime !== null ? formatTimeInput(Math.floor(currentPlayerTime)) : '--:--'}
                          </button>
                          {value.end > 0 && (
                            <>
                              <button
                                onClick={() => playerSeekTo(scope, Math.max(value.end - 3, 0))}
                                className="px-3 py-2 bg-red-100 text-red-600 rounded-lg hover:bg-red-200 transition-colors"
                                title="終了時間の3秒前から再生"
                              >
                                <svg className="w-6 h-6" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                                  <path d="M6 8a7 7 0 1 1 1.2 8.8" />
                                  <polyline points="6 3 6 8 11 8" />
                                  <text x="12" y="15" textAnchor="middle" fontSize="8" fontWeight="bold" fill="currentColor" stroke="none">3</text>
                                </svg>
                                <span className="sr-only">-3s</span>
                              </button>
                              <button
                                onClick={() => playerSeekTo(scope, value.end)}
                                className="px-3 py-2 bg-red-100 text-red-600 rounded-lg hover:bg-red-200 transition-colors"
                                title="終了時間から再生"
                              >
                                <svg className="w-5 h-5" fill="currentColor" viewBox="0 0 24 24">
                                  <path d="M8 5v14l11-7z" />
                                </svg>
                              </button>
                            </>
                          )}
                        </div>
                        {/* ±6秒微調整（離すと終了3秒前から試聴して締めを確認） */}
                        {value.end > 0 && (
                          <TimestampTweaker
                            value={value.end}
                            mode="end"
                            onChange={(v) => onChange({ end: v })}
                          />
                        )}
                        {/* 終了時刻がない場合の案内 */}
                        {value.end === 0 && (
                          <div className="mt-1 flex items-center gap-1 text-xs text-red-500">
                            <svg className="w-4 h-4" fill="currentColor" viewBox="0 0 20 20">
                              <path fillRule="evenodd" d="M18 10a8 8 0 11-16 0 8 8 0 0116 0zm-7-4a1 1 0 11-2 0 1 1 0 012 0zM9 9a1 1 0 000 2v3a1 1 0 001 1h1a1 1 0 100-2v-3a1 1 0 00-1-1H9z" clipRule="evenodd" />
                            </svg>
                            <span>終了時間は必須です</span>
                          </div>
                        )}
                        {/* 推定時刻の警告 */}
                        {value.end > 0 && value.isEndTimeEstimated && (
                          <div className="mt-1 flex items-center gap-1 text-xs text-orange-600">
                            <svg className="w-4 h-4" fill="currentColor" viewBox="0 0 20 20">
                              <path fillRule="evenodd" d="M8.257 3.099c.765-1.36 2.722-1.36 3.486 0l5.58 9.92c.75 1.334-.213 2.98-1.742 2.98H4.42c-1.53 0-2.493-1.646-1.743-2.98l5.58-9.92zM11 13a1 1 0 11-2 0 1 1 0 012 0zm-1-8a1 1 0 00-1 1v3a1 1 0 002 0V6a1 1 0 00-1-1z" clipRule="evenodd" />
                            </svg>
                            <span>推定時間 - 要確認</span>
                          </div>
                        )}
                        {/* Chat とコメントの end の差が大きい場合の警告 + 適用ボタン */}
                        {value.endDiff !== undefined && value.endDiff >= 10 && (
                          <div className="mt-1 flex items-center gap-2 text-xs text-amber-700 bg-amber-50 px-2 py-1 rounded">
                            <div className="flex items-center gap-1">
                              <svg className="w-4 h-4" fill="currentColor" viewBox="0 0 20 20">
                                <path fillRule="evenodd" d="M8.257 3.099c.765-1.36 2.722-1.36 3.486 0l5.58 9.92c.75 1.334-.213 2.98-1.742 2.98H4.42c-1.53 0-2.493-1.646-1.743-2.98l5.58-9.92zM11 13a1 1 0 11-2 0 1 1 0 012 0zm-1-8a1 1 0 00-1 1v3a1 1 0 002 0V6a1 1 0 00-1-1z" clipRule="evenodd" />
                              </svg>
                              <span>
                                Chat との差 {value.endDiff} 秒
                                {value.chatEnd !== undefined && `（Chat: ${formatTimeInput(value.chatEnd)}）`}
                              </span>
                            </div>
                            <button
                              onClick={() => onApplyEndSource('chat')}
                              className="px-2 py-0.5 bg-amber-200 hover:bg-amber-300 text-amber-800 rounded text-xs font-medium"
                            >
                              Chat の値を適用
                            </button>
                          </div>
                        )}

                        {/* コメントに記載された元の終了時刻へ戻すボタン */}
                        {value.originalCommentEnd !== undefined && value.end !== value.originalCommentEnd && (
                          <button
                            onClick={() => onApplyEndSource('comment')}
                            className="mt-1 text-xs text-blue-600 hover:text-blue-800 underline"
                          >
                            元の値に戻す ({value.originalCommentEnd ? formatTimeInput(value.originalCommentEnd) : ''})
                          </button>
                        )}
                      </div>
                    </div>

                    {/* Tags */}
                    <div className="mt-4">
                      <label className="block text-sm font-medium text-gray-700 mb-2">
                        タグ
                      </label>
                      <div className="flex flex-wrap gap-2">
                        {performanceTags.map((tag) => (
                          <button
                            key={tag.id}
                            onClick={() => onToggleTag(tag.id)}
                            className={`px-3 py-1 rounded-full text-sm font-medium transition-colors ${
                              value.tags.includes(tag.id)
                                ? 'text-white'
                                : 'bg-gray-200 text-gray-700 hover:bg-gray-300'
                            }`}
                            style={value.tags.includes(tag.id) ? { backgroundColor: tag.color } : {}}
                          >
                            {tag.label}
                          </button>
                        ))}
                        {/* Custom tags */}
                        {value.customTags.map((ct) => (
                          <span
                            key={ct}
                            className="inline-flex items-center gap-1 px-3 py-1 rounded-full text-sm font-medium bg-gray-500 text-white"
                          >
                            {ct}
                            <button
                              onClick={() => {
                                onChange({ customTags: value.customTags.filter((t) => t !== ct) });
                              }}
                              className="hover:text-red-200 ml-0.5"
                            >
                              ×
                            </button>
                          </span>
                        ))}
                        {/* Custom tag input */}
                        <input
                          type="text"
                          placeholder="+ カスタムタグ"
                          className="px-3 py-1 rounded-full text-sm border border-dashed border-gray-300 bg-transparent text-gray-500 focus:border-gray-500 focus:outline-none w-32"
                          onKeyDown={(e) => {
                            if (e.key === 'Enter') {
                              const tag = e.currentTarget.value.trim();
                              if (tag && !value.customTags.includes(tag)) {
                                onChange({ customTags: [...value.customTags, tag] });
                                e.currentTarget.value = '';
                              }
                              e.preventDefault();
                            }
                          }}
                        />
                      </div>
                    </div>

                    {/* ボーカル。**参加者からの選択が主で、検索は例外の入口**。
                        以前は毎回検索して選び、外すのは ✕ を押す形だった。歌枠の
                        ボーカルはほぼ必ず配信の参加者の中に居るので、並べて押すだけにする。
                        参加者に居ない歌手（ゲスト等）だけ検索から足す */}
                    <div className="mt-4">
                      <label className="block text-sm font-medium text-gray-700 mb-2">
                        ボーカル
                      </label>
                      <div className="flex flex-wrap items-center gap-1.5">
                        {vocalOptions.map((singer) => {
                          const selected = value.singerIds.includes(singer.id);
                          return (
                            <button
                              key={singer.id}
                              type="button"
                              onClick={() =>
                                onChange({
                                  singerIds: selected
                                    ? value.singerIds.filter((id) => id !== singer.id)
                                    : [...value.singerIds, singer.id],
                                })
                              }
                              title={singer.name}
                              aria-pressed={selected}
                              className={`inline-flex items-center gap-1.5 pl-1 pr-2.5 py-1 rounded-full border text-sm transition-colors ${
                                selected
                                  ? 'bg-indigo-100 border-indigo-300 text-indigo-800'
                                  : 'bg-white border-gray-200 text-gray-500 hover:border-indigo-300 hover:bg-indigo-50'
                              }`}
                            >
                              {singer.photo_url ? (
                                <img
                                  src={singer.photo_url}
                                  alt=""
                                  className={`w-5 h-5 rounded-full ${selected ? '' : 'opacity-60'}`}
                                  onError={(e) => {
                                    e.currentTarget.onerror = null;
                                    e.currentTarget.src = `https://holodex.net/statics/channelImg/${singer.id}/50.png`;
                                  }}
                                />
                              ) : (
                                <span className="w-5 h-5 rounded-full bg-gray-200" />
                              )}
                              <span className="max-w-[11rem] truncate">{singer.name}</span>
                              {/* 色だけに頼らない。選択済みかどうかを記号でも出す */}
                              {selected && <span aria-hidden="true">✓</span>}
                            </button>
                          );
                        })}
                        <button
                          type="button"
                          onClick={() => setSearchingSinger((v) => !v)}
                          className="px-2.5 py-1 rounded-full border border-dashed border-gray-300 text-sm text-gray-500 hover:border-indigo-300 hover:text-indigo-600"
                        >
                          {searchingSinger ? '閉じる' : '＋ 参加者以外'}
                        </button>
                      </div>
                      {(searchingSinger || vocalOptions.length === 0) && (
                        <div className="mt-2">
                          <SingerSearchInput
                            onSelectSinger={(singer) => {
                              if (!value.singerIds.includes(singer.id)) {
                                onChange({ singerIds: [...value.singerIds, singer.id] });
                              }
                              // 参加者に足しておかないと、選んだ本人が候補に並ばず
                              // 次に外すことも選び直すこともできなくなる
                              if (!participants.find((p) => p.id === singer.id)) {
                                onAddParticipant?.(singer);
                              }
                              setSearchingSinger(false);
                            }}
                            excludeIds={value.singerIds}
                            placeholder="参加者以外のボーカルを検索..."
                          />
                        </div>
                      )}
                    </div>
    </>
  );
}
