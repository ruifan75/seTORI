import Tag from './ui/Tag';
import { playerSeekTo } from './youtubePlayerControl';
import { usePlayerScope } from './playerScope';
import { matchReasonLabel } from '../utils/matchReason';
import type { CommentSong } from '../api/types';

interface TagMeta {
  id: string;
  label: string;
  color: string;
}

interface Props {
  songs: CommentSong[];
  performanceTags: TagMeta[];
  onAdd: (song: CommentSong) => void;
  emptyMessage: string;
}

function formatTime(seconds: number): string {
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  const s = seconds % 60;
  if (h > 0) {
    return `${h}:${m.toString().padStart(2, '0')}:${s.toString().padStart(2, '0')}`;
  }
  return `${m}:${s.toString().padStart(2, '0')}`;
}

/**
 * SourceSongList は入力元から抽出済みの曲を並べ、押すと編集リストへ足す。
 *
 * **コメント経路とチャプター経路で共有する。** どちらも `CommentSong` を返し、
 * 出すもの（時刻・曲名・照合先・タグ・決めきれなかった候補）が同じなので、
 * 分けて書くと片方にしか出ない情報が生まれる（バックエンドで `ExtractSongs` を
 * 1 つに保っているのと同じ理由）。
 */
export default function SourceSongList({ songs, performanceTags, onAdd, emptyMessage }: Props) {
  const scope = usePlayerScope();

  if (songs.length === 0) {
    return <p className="text-sm text-gray-400 py-2">{emptyMessage}</p>;
  }

  return (
    <div className="space-y-0.5">
      {songs.map((song, i) => (
        <div key={i}>
          <div
            onClick={() => onAdd(song)}
            className="flex items-baseline gap-2 px-2 py-1.5 rounded hover:bg-indigo-50 cursor-pointer group text-sm"
            title="クリックで追加"
          >
            <span className="shrink-0 flex items-baseline gap-0.5">
              <button
                onClick={(e) => {
                  e.stopPropagation();
                  playerSeekTo(scope, song.start);
                }}
                className="px-1.5 rounded bg-orange-50 text-orange-700 font-mono text-xs hover:bg-orange-100 transition-colors"
                title="開始時間にジャンプ"
              >
                {formatTime(song.start)}
              </button>
              {song.end > 0 && (
                <>
                  <span className="text-gray-300 text-xs">〜</span>
                  <button
                    onClick={(e) => {
                      e.stopPropagation();
                      playerSeekTo(scope, song.end);
                    }}
                    className="px-1.5 rounded bg-orange-50/60 text-orange-600 font-mono text-xs hover:bg-orange-100 transition-colors"
                    title={song.is_end_time_estimated ? '終了時間にジャンプ（推定値）' : '終了時間にジャンプ'}
                  >
                    {formatTime(song.end)}
                    {/* 推定値であることを見えるようにする。チャプター由来の end は
                        次の章節の開始そのものなので、曲のあとの MC を含んでいる */}
                    {song.is_end_time_estimated && <span className="ml-0.5 text-orange-400">?</span>}
                  </button>
                </>
              )}
            </span>
            <span className="min-w-0 flex-1 truncate">
              <span className="text-gray-900 font-medium">{song.name}</span>
              {song.original_artist && <span className="text-gray-500"> / {song.original_artist}</span>}
              {song.matched_song_id && song.matched_song_name && (
                <span className="ml-1 text-xs text-emerald-600" title="DB の楽曲に照合済み">
                  → {song.matched_song_name}
                </span>
              )}
            </span>
            {/*
              正規化で付いた演奏バージョンのタグ（short / piano …）。
              追加すると EditableSong.tags にそのまま入るので、
              取り込む前にここで見えるようにしてある。
              PERFORMANCE_TAGS に無い ID は語彙のずれなので、
              そのまま灰色で出して気付けるようにする（黙って隠さない）。
            */}
            {(song.tags?.length ?? 0) > 0 && (
              <span className="shrink-0 flex items-center gap-1">
                {song.tags!.map((tagId) => {
                  const meta = performanceTags.find((t) => t.id === tagId);
                  return <Tag key={tagId} label={meta?.label || tagId} color={meta?.color || '#6B7280'} />;
                })}
              </span>
            )}
            <span className="shrink-0 text-gray-300 group-hover:text-indigo-600 transition-colors">＋</span>
          </div>

          {/*
            照合が決めきれなかった候補。songmatch が 0.50〜0.85 で返したもので、
            「アーティストが書かれていないので曲名だけでは決めきれない」
            「feat. や CV 名の表記が違う」といった、文字列では決まらない組が来る。
            確定は行わない ── 照合を決めるのは「読み込む」で
            編集フォームへ取り込むときの仕事。
          */}
          {!song.matched_song_id && (song.match_candidates?.length ?? 0) > 0 && (
            <div className="ml-2 mb-1 flex flex-wrap items-center gap-1 pl-14">
              <span className="shrink-0 text-[11px] text-gray-400">候補</span>
              {song.match_candidates!.map((c) => (
                <span
                  key={c.song_id}
                  title={`${matchReasonLabel(c.reason)}（確信度 ${Math.round(c.score * 100)}%）`}
                  className="max-w-full truncate rounded-full border border-amber-200 bg-amber-50 px-2 py-0.5 text-xs text-amber-900"
                >
                  {c.name}
                  {c.artist && <span className="text-amber-700/70"> / {c.artist}</span>}
                  <span className="ml-1 text-amber-600/60">{Math.round(c.score * 100)}%</span>
                </span>
              ))}
            </div>
          )}
        </div>
      ))}
    </div>
  );
}
