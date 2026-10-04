import { Link } from 'react-router-dom';
import Tag from '../../components/ui/Tag';
import { playerSeekTo } from '../../components/youtubePlayerControl';
import QueueAddButton from '../../components/QueueAddButton';
import PerformanceListRow, { RowSingerAvatars } from '../../components/PerformanceListRow';
import ReportButton from '../../components/ReportButton';
import ArtistLinks from '../../components/ArtistLinks';
import { formatTime } from './utils';

import type { StreamDetailModel } from './useStreamDetail';

export default function StreamPerformanceList({ model }: { model: StreamDetailModel }) {
  const { stream, channelOwner, toRowTrack, setVocalistPopupSingers } = model;
  return (
    stream.performances.length === 0 ? (
      <div className="text-center py-12 text-gray-500 bg-white rounded-lg border">
        {/* **「登録されていない」と「見せていない」は別の事実。** 秘匿された配信では
            歌唱が返らないので performances は 0 件になるが、無いとは限らない。
            同じ文言にすると、利用者には「誰もまだ作っていない」に見える。 */}
        {stream.is_restricted
          ? 'この配信のセットリストは公開していません'
          : 'セットリストがまだ登録されていません'}
      </div>
    ) : (
      <div className="bg-white rounded-lg shadow-sm border">
        {/* 狭い画面は表をやめて縦積みの行にする。390px に 7 列は入らず、
            実際は横スクロール＋曲名の折り返しになっていて、列が揃っている
            という表の利点は失われていた */}
        <ul className="divide-y divide-gray-100 md:hidden">
          {stream.performances.map((perf, index) => {
            const tags = [
              ...perf.tags.map((t) => ({ key: t.id, label: t.display_name, color: t.color })),
              ...(perf.custom_tags ?? []).map((t) => ({ key: t, label: t, color: '#6B7280' })),
            ];
            // チャンネル所有者を先頭に（表と同じ並び）
            const singers = [...(perf.singers ?? [])].sort((a, b) => {
              if (channelOwner && a.id === channelOwner.id) return -1;
              if (channelOwner && b.id === channelOwner.id) return 1;
              return 0;
            });
            return (
              <PerformanceListRow
                key={perf.id}
                track={toRowTrack(perf)}
                thumbnailUrl={perf.arts}
                badge={`#${index + 1}`}
                youtubeUrl={perf.youtube_url}
                playLabel={`${perf.song_name} をここから再生`}
                onPlay={() => playerSeekTo('page', perf.start_seconds)}
                meta={
                  <span className="flex items-center gap-2 overflow-hidden">
                    <span className="shrink-0 font-mono">
                      {formatTime(perf.start_seconds)}
                      {perf.end_seconds > 0 && `–${formatTime(perf.end_seconds)}`}
                    </span>
                    <RowSingerAvatars singers={singers} />
                    {/* タグは 2 つまで。多い配信で行が膨らむと一覧として眺められない */}
                    {tags.slice(0, 2).map((t) => (
                      <span key={t.key} className="shrink-0">
                        <Tag label={t.label} color={t.color} />
                      </span>
                    ))}
                    {tags.length > 2 && <span className="shrink-0">+{tags.length - 2}</span>}
                  </span>
                }
              />
            );
          })}
        </ul>

        <div className="hidden md:block overflow-x-auto min-[1300px]:max-h-[calc(100vh-14rem)] min-[1300px]:overflow-y-auto">
          <table className="min-w-full divide-y divide-gray-200">
            <thead className="bg-gray-50 sticky top-0 z-10">
            <tr>
              <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider w-24">
                #
              </th>
              <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider w-32">
                時間
              </th>
              <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
                楽曲
              </th>
              <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
                アーティスト
              </th>
              <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider w-40">
                タグ
              </th>
              <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider w-32">
                ボーカル
              </th>
              <th className="px-4 py-3 text-right text-xs font-medium text-gray-500 uppercase tracking-wider w-20">
              </th>
            </tr>
          </thead>
          <tbody className="bg-white divide-y divide-gray-200">
            {stream.performances.map((perf, index) => {
              const rowTrack = toRowTrack(perf);
              const singerCount = perf.singers?.length || 0;
              const showCount = singerCount > 3;
              // 歌手を並べ替える：チャンネル所有者を優先
              const sortedSingers = perf.singers?.sort((a, b) => {
                if (channelOwner && a.id === channelOwner.id) return -1;
                if (channelOwner && b.id === channelOwner.id) return 1;
                return 0;
              }) || [];
              const displaySingers = showCount ? sortedSingers.slice(0, 3) : sortedSingers;

              return (
                <tr key={perf.id} className="hover:bg-gray-50">
                  {/* # with Art thumbnail */}
                  <td className="px-4 py-4">
                    <div className="relative w-16 h-16">
                      {perf.arts ? (
                        <img
                          src={perf.arts}
                          alt={perf.song_name}
                          className="w-16 h-16 object-cover rounded-lg shadow-sm"
                        />
                      ) : (
                        <div className="w-16 h-16 bg-gray-100 rounded-lg flex items-center justify-center">
                          <svg className="w-8 h-8 text-gray-300" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 19V6l12-3v13M9 19c0 1.105-1.343 2-3 2s-3-.895-3-2 1.343-2 3-2 3 .895 3 2zm12-3c0 1.105-1.343 2-3 2s-3-.895-3-2 1.343-2 3-2 3 .895 3 2zM9 10l12-3" />
                          </svg>
                        </div>
                      )}
                      {/* Index badge */}
                      <div className="absolute top-0 left-0 bg-indigo-600 text-white text-xs font-bold rounded-tl-lg rounded-br-lg px-2 py-0.5">
                        #{index + 1}
                      </div>
                    </div>
                  </td>
                  {/* 開始と終了は**常に2行**にする。1行に流していたときは
                      列幅（w-32）にちょうど収まるかどうかで折り返しが決まり、
                      13:25 は1行・1:23:15 は2行と行ごとに形が変わっていた */}
                  <td className="px-4 py-4 text-sm text-gray-500 font-mono">
                    <span className="block whitespace-nowrap">
                      {formatTime(perf.start_seconds)}
                      {perf.end_seconds > 0 && ' ~'}
                    </span>
                    {perf.end_seconds > 0 && (
                      <span className="block whitespace-nowrap text-gray-400">
                        {formatTime(perf.end_seconds)}
                      </span>
                    )}
                  </td>
                  <td className="px-4 py-4">
                    <Link
                      to={`/songs/${perf.song_id}`}
                      className="text-indigo-600 hover:text-indigo-900 font-medium"
                    >
                      {perf.song_name}
                    </Link>
                  </td>
                  <td className="px-4 py-4 text-sm text-gray-500">
                    <ArtistLinks
                      artists={perf.artists}
                      fallback={perf.original_artist}
                      linkClassName="hover:text-indigo-600"
                    />
                  </td>
                  <td className="px-4 py-4">
                    <div className="flex flex-wrap gap-1">
                      {perf.tags.map((tag) => (
                        <Tag key={tag.id} label={tag.display_name} color={tag.color} />
                      ))}
                      {perf.custom_tags?.map((ct) => (
                        <Tag key={ct} label={ct} color="#6B7280" />
                      ))}
                    </div>
                  </td>
                  {/* Singer avatars */}
                  <td className="px-4 py-4">
                    {singerCount === 0 ? (
                      <span className="text-sm text-gray-400">なし</span>
                    ) : (
                      <div className="flex items-center relative h-8">
                        {displaySingers?.map((singer, singerIndex) => (
                          <Link
                            key={singer.id}
                            to={`/singers/${singer.id}`}
                            title={singer.name}
                            className="relative -ml-2 first:ml-0 hover:z-50"
                            style={{
                              zIndex: displaySingers.length - singerIndex,
                            }}
                          >
                            <img
                              src={
                                singer.photo_url ||
                                `https://holodex.net/statics/channelImg/${singer.id}/50.png`
                              }
                              alt={singer.name}
                              className="w-8 h-8 rounded-full border-2 border-white shadow-sm hover:shadow-md transition-shadow"
                              onError={(e) => {
                                e.currentTarget.onerror = null;
                                e.currentTarget.src = `https://holodex.net/statics/channelImg/${singer.id}/50.png`;
                              }}
                            />
                          </Link>
                        ))}
                        {showCount && (
                          <button
                            onClick={() => setVocalistPopupSingers(sortedSingers)}
                            title={perf.singers
                              ?.map((s) => s.name)
                              .join(', ')}
                            className="relative -ml-2 w-8 h-8 rounded-full bg-gray-300 border-2 border-white flex items-center justify-center text-xs font-bold text-gray-700 cursor-pointer hover:bg-gray-400 transition-colors"
                          >
                            +{singerCount - 3}
                          </button>
                        )}
                      </div>
                    )}
                  </td>
                  <td className="px-4 py-4 text-right">
                    <div className="inline-flex items-center gap-1.5">
                      <button
                        onClick={() => playerSeekTo('page', perf.start_seconds)}
                        className="inline-flex items-center justify-center w-8 h-8 rounded-full bg-indigo-600 text-white hover:bg-indigo-700 transition-colors"
                        title={`${perf.song_name} を再生`}
                      >
                        <svg className="w-4 h-4 ml-0.5" fill="currentColor" viewBox="0 0 24 24">
                          <path d="M8 5v14l11-7z" />
                        </svg>
                      </button>
                      <QueueAddButton track={rowTrack} />
                      {/* 時間・曲・歌った人はすべて 1 つの報告画面で直す */}
                      <ReportButton track={rowTrack} />
                      <a
                        href={perf.youtube_url}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="inline-flex items-center justify-center w-8 h-8 rounded-full text-gray-400 hover:text-red-600 hover:bg-red-50 transition-colors"
                        title="YouTubeで開く"
                      >
                        <svg className="w-4 h-4" fill="currentColor" viewBox="0 0 24 24">
                          <path d="M23.498 6.186a3.016 3.016 0 0 0-2.122-2.136C19.505 3.545 12 3.545 12 3.545s-7.505 0-9.377.505A3.017 3.017 0 0 0 .502 6.186C0 8.07 0 12 0 12s0 3.93.502 5.814a3.016 3.016 0 0 0 2.122 2.136c1.871.505 9.376.505 9.376.505s7.505 0 9.377-.505a3.015 3.015 0 0 0 2.122-2.136C24 15.93 24 12 24 12s0-3.93-.502-5.814zM9.545 15.568V8.432L15.818 12l-6.273 3.568z" />
                        </svg>
                      </a>
                    </div>
                  </td>
                </tr>
              );
            })}
          </tbody>
          </table>
        </div>
      </div>
    )
  );
}
