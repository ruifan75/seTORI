import { useMemo, useState } from 'react';
import { usePlayerStore } from '../store/player';
import { playerSeekTo } from './youtubePlayerControl';
import { parseSeconds } from './usePerformanceTiming';
import { formatTimeInput } from '../utils/timeFormat';
import SongSearchInput from './SongSearchInput';
import ArtistSearchInput from './ArtistSearchInput';
import SingerSearchInput from './SingerSearchInput';
import type { Draft } from './usePerformanceReport';
import type { Singer } from '../api/types';

// 報告画面の入力部品。**デスクトップとスマホで同じものを使う。**
// 外殻（横並び / タブ切り替え）は別でも、ここが割れると片方だけ挙動が変わる。

// 再生の操作。**ダイアログが再生バーを覆うので、ここに無いと聴きながら
// 直せない**（区間の外を聴きにいくのがこの画面の目的なので、±5 秒も置く）。
export function Transport({ currentTime, compact = false }: { currentTime: number | null; compact?: boolean }) {
  const playing = usePlayerStore((s) => s.playing);
  const setPlaying = usePlayerStore((s) => s.setPlaying);

  const nudge = (delta: number) => {
    if (currentTime == null) return;
    playerSeekTo('bar', Math.max(0, currentTime + delta));
  };

  return (
    <div className={`mt-2 flex items-center ${compact ? 'gap-3 justify-center' : 'gap-2'}`}>
      <button
        type="button"
        onClick={() => nudge(-5)}
        className={`rounded-lg text-gray-600 hover:bg-gray-100 ${compact ? 'px-3 py-2 text-sm' : 'px-2 py-1 text-xs'}`}
        title="5秒戻す"
      >
        -5s
      </button>
      <button
        type="button"
        onClick={() => setPlaying(!playing)}
        className={`bg-indigo-600 text-white rounded-full hover:bg-indigo-700 ${compact ? 'p-3' : 'p-2'}`}
        title={playing ? '一時停止' : '再生'}
        aria-label={playing ? '一時停止' : '再生'}
      >
        {playing ? (
          <svg className={compact ? 'w-5 h-5' : 'w-4 h-4'} fill="currentColor" viewBox="0 0 24 24">
            <path d="M6 19h4V5H6v14zm8-14v14h4V5h-4z" />
          </svg>
        ) : (
          <svg className={compact ? 'w-5 h-5' : 'w-4 h-4'} fill="currentColor" viewBox="0 0 24 24">
            <path d="M8 5v14l11-7z" />
          </svg>
        )}
      </button>
      <button
        type="button"
        onClick={() => nudge(5)}
        className={`rounded-lg text-gray-600 hover:bg-gray-100 ${compact ? 'px-3 py-2 text-sm' : 'px-2 py-1 text-xs'}`}
        title="5秒進める"
      >
        +5s
      </button>
      <span className={`font-mono text-gray-800 ${compact ? 'text-base' : 'text-sm'}`}>
        {currentTime != null ? formatTimeInput(Math.floor(currentTime)) : '--:--'}
      </span>
      {!compact && (
        <span className="text-xs text-gray-500 truncate min-w-0">
          区間の終わりで止まりません。前後どこでも聴けます
        </span>
      )}
    </div>
  );
}

// 時刻 1 つの入力欄（手入力 ＋ 今の再生位置の取り込み ＋ そこから試聴）。
// デスクトップ用の 1 行版。
export function TimeField({
  label,
  value,
  original,
  currentTime,
  allowEmpty = false,
  onChange,
}: {
  label: string;
  value: number;
  original: number;
  currentTime: number | null;
  allowEmpty?: boolean;
  onChange: (v: number) => void;
}) {
  const { text, setText, commit } = useTimeText(value, allowEmpty, onChange);

  return (
    <div>
      <div className="flex items-center gap-1.5">
        <span className="text-xs text-gray-500 w-7 shrink-0">{label}</span>
        <input
          type="text"
          value={text}
          onChange={(e) => setText(e.target.value)}
          onBlur={commit}
          onKeyDown={(e) => {
            if (e.key === 'Enter') {
              e.preventDefault();
              commit();
            }
          }}
          placeholder={allowEmpty ? '空欄=最後まで' : '0:00'}
          className="w-24 px-2 py-1 text-sm font-mono border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
        />
        <button
          type="button"
          onClick={() => currentTime != null && onChange(Math.round(currentTime))}
          disabled={currentTime == null}
          className="px-2 py-1 text-xs bg-indigo-100 text-indigo-700 rounded-lg hover:bg-indigo-200 disabled:opacity-40"
          title={`今の再生位置を${label}にする`}
        >
          今ここ
        </button>
        <button
          type="button"
          onClick={() => playerSeekTo('bar', label === '終了' ? Math.max(0, value - 3) : value)}
          disabled={value === 0}
          className="px-1.5 py-1 text-red-600 bg-red-50 rounded-lg hover:bg-red-100 disabled:opacity-40"
          title={label === '終了' ? '終了の3秒前から再生' : 'ここから再生'}
        >
          <svg className="w-4 h-4" fill="currentColor" viewBox="0 0 24 24">
            <path d="M8 5v14l11-7z" />
          </svg>
        </button>
      </div>
      {value !== original && (
        <p className="mt-0.5 ml-8 text-[11px] font-mono text-indigo-600">
          {original === 0 ? '最後まで' : formatTimeInput(original)} → {value === 0 ? '最後まで' : formatTimeInput(value)}
        </p>
      )}
    </div>
  );
}

// スマホ用の時刻カード。**「今ここ」を主役にする。**
//
// 細いハンドルを指でつまむのは無理があるので、主な操作は
// 「聴く → その瞬間にボタンを押す」に戻す。ポップオーバー時代の 1 タップは
// 速さが問題だったのではなく、区間の外へ出られないことが問題だった
// ── 締め切りを外した今、この操作は正しい。
//
// カード自体が「今どちらを編集しているか」の選択も兼ねる（時間軸のタップ先）。
export function TimeCard({
  label,
  value,
  original,
  currentTime,
  active,
  allowEmpty = false,
  onActivate,
  onChange,
}: {
  label: string;
  value: number;
  original: number;
  currentTime: number | null;
  active: boolean;
  allowEmpty?: boolean;
  onActivate: () => void;
  onChange: (v: number) => void;
}) {
  const { text, setText, commit } = useTimeText(value, allowEmpty, onChange);
  const empty = value === 0 && allowEmpty;

  // 微調整は押すたびに試聴する。開始はそこから、終了は 3 秒前から
  const nudge = (delta: number) => {
    const next = Math.max(0, value + delta);
    onChange(next);
    playerSeekTo('bar', label === '終了' ? Math.max(0, next - 3) : next);
  };

  return (
    <div
      onFocus={onActivate}
      onClick={onActivate}
      className={`rounded-xl border p-2.5 transition-colors ${
        active ? 'border-indigo-400 bg-indigo-50/60' : 'border-gray-200 bg-white'
      }`}
    >
      <div className="flex items-center justify-between gap-2">
        <span className={`text-xs font-medium ${active ? 'text-indigo-700' : 'text-gray-500'}`}>{label}</span>
        <button
          type="button"
          onClick={() => playerSeekTo('bar', label === '終了' ? Math.max(0, value - 3) : value)}
          disabled={empty}
          className="px-2 py-1 text-red-600 bg-red-50 rounded-lg disabled:opacity-40"
          title={label === '終了' ? '終了の3秒前から再生' : 'ここから再生'}
          aria-label={label === '終了' ? '終了の3秒前から再生' : 'ここから再生'}
        >
          <svg className="w-4 h-4" fill="currentColor" viewBox="0 0 24 24">
            <path d="M8 5v14l11-7z" />
          </svg>
        </button>
      </div>

      <input
        type="text"
        value={text}
        onChange={(e) => setText(e.target.value)}
        onBlur={commit}
        onKeyDown={(e) => {
          if (e.key === 'Enter') {
            e.preventDefault();
            e.currentTarget.blur();
          }
        }}
        placeholder={allowEmpty ? '最後まで' : '0:00'}
        className="mt-1 w-full px-2 py-1.5 text-lg font-mono text-center border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
      />

      <button
        type="button"
        onClick={() => currentTime != null && onChange(Math.round(currentTime))}
        disabled={currentTime == null}
        className="mt-1.5 w-full py-2.5 text-sm font-medium bg-indigo-600 text-white rounded-lg active:bg-indigo-800 disabled:opacity-40"
      >
        ここが{label}
      </button>

      <div className="mt-1.5 flex gap-1.5">
        <button
          type="button"
          onClick={() => nudge(-1)}
          disabled={empty}
          className="flex-1 py-2 text-sm font-mono bg-gray-100 text-gray-700 rounded-lg active:bg-gray-300 disabled:opacity-40"
        >
          −1s
        </button>
        <button
          type="button"
          onClick={() => nudge(1)}
          disabled={empty}
          className="flex-1 py-2 text-sm font-mono bg-gray-100 text-gray-700 rounded-lg active:bg-gray-300 disabled:opacity-40"
        >
          +1s
        </button>
      </div>

      {value !== original && (
        <p className="mt-1 text-[11px] font-mono text-indigo-600 text-center">
          {original === 0 ? '最後まで' : formatTimeInput(original)} → {empty ? '最後まで' : formatTimeInput(value)}
        </p>
      )}
    </div>
  );
}

// 入力欄の文字列と秒数の同期。時間軸のドラッグで値が動いたら欄も追従する
function useTimeText(value: number, allowEmpty: boolean, onChange: (v: number) => void) {
  const show = (v: number) => (v === 0 && allowEmpty ? '' : formatTimeInput(v));
  const [text, setText] = useState(() => show(value));
  const [shownFor, setShownFor] = useState(value);
  if (shownFor !== value) {
    setShownFor(value);
    setText(show(value));
  }

  const commit = () => {
    if (allowEmpty && text.trim() === '') {
      onChange(0);
      return;
    }
    const parsed = parseSeconds(text);
    if (parsed === null) {
      setText(show(value)); // 不正なら戻す
      return;
    }
    onChange(parsed);
  };

  return { text, setText, commit };
}

// 曲の欄。検索から選ばずに打ち替えたら「別の曲（未登録）」の指摘として扱う
export function SongField({
  draft,
  patch,
  changed,
  missingMode,
}: {
  draft: Draft;
  patch: (p: Partial<Draft>) => void;
  changed: boolean;
  missingMode: boolean;
}) {
  return (
    <div>
      <label className="block text-xs font-medium text-gray-600 mb-1">
        曲 <span className="font-normal text-gray-400">（登録済み / iTunes から選ぶ・無ければそのまま入力）</span>
      </label>
      <SongSearchInput
        value={draft.songName}
        onChange={(name) =>
          // 曲名の表記そのものを直したい場合は曲ページから直す（全歌唱に効く別の操作）
          patch({ songName: name, songId: '', itunesId: null, artUrl: null })
        }
        onSelectSong={(song) =>
          patch({
            songId: song.id,
            songName: song.name,
            artist: song.original_artist,
            itunesId: song.itunes_ids?.[0]?.itunes_id ?? null,
            artUrl: song.arts ?? null,
          })
        }
        placeholder="曲名で検索"
      />
      {(changed || (missingMode && draft.songName.trim() !== '')) && (
        <p className="mt-1 text-[11px] text-amber-700">
          {draft.songId
            ? missingMode
              ? '登録済みの曲として追加します'
              : 'この歌唱を別の曲へ繋ぎ替えます'
            : 'この曲名は未登録です。承認時に新しい曲として登録されます'}
        </p>
      )}
    </div>
  );
}

// 原曲アーティストの欄。曲の属性なので、直すとその曲の全歌唱に効く
export function ArtistField({
  draft,
  patch,
  changed,
}: {
  draft: Draft;
  patch: (p: Partial<Draft>) => void;
  changed: boolean;
}) {
  return (
    <div>
      <label className="block text-xs font-medium text-gray-600 mb-1">原曲アーティスト</label>
      <ArtistSearchInput
        value={draft.artist}
        onChange={(artist) => patch({ artist })}
        onSelectArtist={(a) => patch({ artist: a.name })}
        placeholder="アーティスト名で検索"
      />
      {changed && (
        <p className="mt-1 text-[11px] text-amber-700">
          アーティストは曲の属性です。直すとこの曲の歌唱すべてに反映されます
        </p>
      )}
    </div>
  );
}

// ボーカル。**並べて押すのが主で、検索は例外**（歌枠のボーカルはほぼ必ず
// 配信の参加者の中に居る）。選択済みなのに参加者に居ない歌手も並べる ──
// 落とすと、値は送られるのに画面に出ないという最悪の形になる。
export function VocalPicker({
  selected,
  participants,
  channelOwner,
  current,
  canCreate,
  onToggle,
}: {
  selected: string[];
  participants: Singer[];
  channelOwner?: Singer;
  current: Singer[];
  canCreate: boolean;
  onToggle: (id: string) => void;
}) {
  const [searching, setSearching] = useState(false);
  const [extraSingers, setExtraSingers] = useState<Singer[]>([]);
  const options = useMemo(() => {
    const byId = new Map<string, Singer>();
    if (channelOwner) byId.set(channelOwner.id, channelOwner);
    for (const s of participants) if (!byId.has(s.id)) byId.set(s.id, s);
    for (const s of current) if (!byId.has(s.id)) byId.set(s.id, s);
    for (const s of extraSingers) if (!byId.has(s.id)) byId.set(s.id, s);
    return [...byId.values()];
  }, [participants, channelOwner, current, extraSingers]);

  const selectSinger = (singer: Singer) => {
    setExtraSingers((items) => (items.some((item) => item.id === singer.id) ? items : [...items, singer]));
    if (!selected.includes(singer.id)) onToggle(singer.id);
    setSearching(false);
  };

  return (
    <div>
      <div className="mb-1 flex items-center justify-between gap-2">
        <label className="block text-xs font-medium text-gray-600">歌った人</label>
        <button
          type="button"
          onClick={() => setSearching((open) => !open)}
          className="text-xs text-indigo-600 hover:text-indigo-800"
        >
          {searching ? '閉じる' : canCreate ? '＋ チャンネルを検索・追加' : '＋ チャンネルを検索'}
        </button>
      </div>
      {options.length > 0 && (
        <div className="flex flex-wrap gap-1.5">
          {options.map((singer) => {
            const on = selected.includes(singer.id);
            return (
              <button
                key={singer.id}
                type="button"
                onClick={() => onToggle(singer.id)}
                aria-pressed={on}
                title={singer.name}
                className={`inline-flex items-center gap-1.5 pl-1 pr-2.5 py-1 rounded-full border text-sm transition-colors ${
                  on
                    ? 'bg-indigo-100 border-indigo-300 text-indigo-800'
                    : 'bg-white border-gray-200 text-gray-500 hover:border-indigo-300 hover:bg-indigo-50'
                }`}
              >
                {singer.photo_url ? (
                  <img src={singer.photo_url} alt="" className={`w-5 h-5 rounded-full ${on ? '' : 'opacity-60'}`} />
                ) : (
                  <span className="w-5 h-5 rounded-full bg-gray-200" />
                )}
                <span className="max-w-[9rem] truncate">{singer.name}</span>
                {/* 色だけに頼らない */}
                {on && <span aria-hidden="true">✓</span>}
              </button>
            );
          })}
        </div>
      )}
      {(searching || options.length === 0) && (
        <div className={options.length > 0 ? 'mt-2' : ''}>
          <SingerSearchInput
            onSelectSinger={selectSinger}
            excludeIds={selected}
            allowCreate={canCreate}
            placeholder={canCreate ? '名前で検索／@handle・Channel ID・URLで新規追加' : 'チャンネル名で検索'}
          />
        </div>
      )}
    </div>
  );
}
