import SongSearchInput from './SongSearchInput';
import SingerSearchInput from './SingerSearchInput';
import FieldProvenance from './FieldProvenance';
import { youtubePlayerGetCurrentTime, youtubePlayerSeekTo } from './youtubePlayerControl';
import TimestampTweaker from './TimestampTweaker';
import { formatTimeInput, formatDuration } from '../utils/timeFormat';
import type { ArtistAliasProposal, FieldChange, Singer, Song } from '../api/types';

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
  performanceTags: PerformanceTagOption[];
  currentPlayerTime: number | null;
  participants: Singer[];
  channelOwner?: Singer | null;
  // 参加者に居ない歌手を選んだとき、配信の参加者へ足す（編集画面のみ）
  onAddParticipant?: (singer: Singer) => void;
  showToast?: (message: string, type?: 'success' | 'error' | 'info') => void;
}

export default function PerformanceFields({
  value,
  onChange,
  onSelectSong,
  onTimeChange,
  onToggleTag,
  onApplyEndSource,
  performanceTags,
  currentPlayerTime,
  participants,
  channelOwner,
  onAddParticipant,
  showToast,
}: Props) {
  return (
    <>
                    <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                      {/* Song Name with Search */}
                      <div>
                        <label className="block text-sm font-medium text-gray-700 mb-1">
                          楽曲名 <span className="text-gray-400 font-normal">(入力して検索)</span>
                        </label>
                        <SongSearchInput
                          value={value.name}
                          onChange={(value) => onChange({ name: value })}
                          onSelectSong={(selectedSong) => onSelectSong(selectedSong)}
                          placeholder="楽曲名を入力して検索"
                          showToast={showToast}
                        />
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
                        {/* 合併元顯示 */}
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
                        <input
                          type="text"
                          value={value.artist}
                          onChange={(e) => onChange({ artist: e.target.value })}
                          className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
                          placeholder="アーティスト名を入力"
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
                          作曲者と原唱の取り違えは「同じ曲だが別人」で、こちらの方が多い。
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
                              const currentTime = youtubePlayerGetCurrentTime();
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
                            onClick={() => youtubePlayerSeekTo(value.start)}
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
                          currentTime={currentPlayerTime}
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
                            placeholder={value.end === 0 ? "歌曲長度ボタンで自動設定" : "0:00"}
                          />
                          {/* 時長按鈕 */}
                          <button
                            onClick={() => {
                              if (value.trackDuration) {
                                const newEnd = value.start + value.trackDuration;
                                onChange({ end: newEnd });
                                onChange({ isEndTimeEstimated: false });
                              }
                            }}
                            disabled={!value.trackDuration}
                            className={`px-3 py-2 rounded-lg font-mono text-sm font-medium transition-colors whitespace-nowrap min-w-[5.5rem] ${
                              value.trackDuration
                                ? 'bg-indigo-100 text-indigo-700 hover:bg-indigo-200'
                                : 'bg-gray-100 text-gray-400 cursor-not-allowed'
                            }`}
                            title={value.trackDuration ? 'iTunes歌曲長度を適用' : '歌曲長度情報なし'}
                          >
                            {formatDuration(value.trackDuration ?? null)}
                          </button>
                          <button
                            onClick={() => {
                              const currentTime = youtubePlayerGetCurrentTime();
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
                                onClick={() => youtubePlayerSeekTo(Math.max(value.end - 3, 0))}
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
                                onClick={() => youtubePlayerSeekTo(value.end)}
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
                            currentTime={currentPlayerTime}
                            onChange={(v) => onChange({ end: v })}
                          />
                        )}
                        {/* 沒有結束時間的提示 */}
                        {value.end === 0 && (
                          <div className="mt-1 flex items-center gap-1 text-xs text-red-500">
                            <svg className="w-4 h-4" fill="currentColor" viewBox="0 0 20 20">
                              <path fillRule="evenodd" d="M18 10a8 8 0 11-16 0 8 8 0 0116 0zm-7-4a1 1 0 11-2 0 1 1 0 012 0zM9 9a1 1 0 000 2v3a1 1 0 001 1h1a1 1 0 100-2v-3a1 1 0 00-1-1H9z" clipRule="evenodd" />
                            </svg>
                            <span>終了時間は必須です</span>
                          </div>
                        )}
                        {/* 估計時間警告 */}
                        {value.end > 0 && value.isEndTimeEstimated && (
                          <div className="mt-1 flex items-center gap-1 text-xs text-orange-600">
                            <svg className="w-4 h-4" fill="currentColor" viewBox="0 0 20 20">
                              <path fillRule="evenodd" d="M8.257 3.099c.765-1.36 2.722-1.36 3.486 0l5.58 9.92c.75 1.334-.213 2.98-1.742 2.98H4.42c-1.53 0-2.493-1.646-1.743-2.98l5.58-9.92zM11 13a1 1 0 11-2 0 1 1 0 012 0zm-1-8a1 1 0 00-1 1v3a1 1 0 002 0V6a1 1 0 00-1-1z" clipRule="evenodd" />
                            </svg>
                            <span>推定時間 - 要確認</span>
                          </div>
                        )}
                        {/* Chat 與 Comment end 差異過大警告 + 套用按鈕 */}
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

                        {/* 還原為 Comment 原始 end 的按鈕 */}
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

                    {/* Vocalist (ボーカル) Selection */}
                    <div className="mt-4">
                      <label className="block text-sm font-medium text-gray-700 mb-2">
                        ボーカル
                      </label>
                      <div className="space-y-2">
                        {/* Display selected vocalists */}
                        {value.singerIds.length > 0 && (
                          <div className="flex flex-wrap gap-2 mb-2">
                            {value.singerIds
                              .slice()
                              .sort((a, b) => {
                                // 頻道擁有者排在最前面
                                if (channelOwner && a === channelOwner.id) return -1;
                                if (channelOwner && b === channelOwner.id) return 1;
                                return 0;
                              })
                              .map((singerId) => {
                              const singer = participants.find((p) => p.id === singerId);
                              return singer ? (
                                <div
                                  key={singerId}
                                  className="flex items-center gap-2 px-3 py-1 bg-indigo-100 text-indigo-700 rounded-full text-sm"
                                >
                                  {singer.photo_url && (
                                    <img
                                      src={singer.photo_url}
                                      alt={singer.name}
                                      className="w-5 h-5 rounded-full"
                                      onError={(e) => {
                                        e.currentTarget.onerror = null;
                                        e.currentTarget.src = `https://holodex.net/statics/channelImg/${singer.id}/50.png`;
                                      }}
                                    />
                                  )}
                                  <span>{singer.name}</span>
                                  <button
                                    onClick={() => {
                                      const newSingerIds = value.singerIds.filter((id) => id !== singerId);
                                      onChange({ singerIds: newSingerIds });
                                    }}
                                    className="ml-1 text-indigo-600 hover:text-indigo-800"
                                  >
                                    ✕
                                  </button>
                                </div>
                              ) : null;
                            })}
                          </div>
                        )}
                        {/* Vocalist search input */}
                        <SingerSearchInput
                          onSelectSinger={(singer) => {
                            if (!value.singerIds.includes(singer.id)) {
                              onChange({ singerIds: [...value.singerIds, singer.id] });
                              // 如果歌手不在participants中，加入
                              if (!participants.find(p => p.id === singer.id)) {
                                onAddParticipant?.(singer);
                              }
                            }
                          }}
                          excludeIds={value.singerIds}
                          placeholder="ボーカルを検索して追加..."
                        />
                      </div>
                    </div>
    </>
  );
}
