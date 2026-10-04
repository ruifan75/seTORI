import { Link } from 'react-router-dom';
import SingerSearchInput from '../../components/SingerSearchInput';
import Tag from '../../components/ui/Tag';

import type { StreamDetailModel } from './useStreamDetail';

export default function StreamInfoCard({ model }: { model: StreamDetailModel }) {
  const {
    stream,
    youtubeUrl,
    canEdit,
    setTagPickerOpen,
    tagPickerOpen,
    STREAM_TAGS,
    quickSaveStream,
    participantAddOpen,
    setParticipantAddOpen,
  } = model;
  return (
    <div className="p-6">
      <>
        <h1 className="text-2xl font-bold text-gray-900">{stream.title}</h1>
        {/* 日時 + YouTube リンク */}
        <p className="text-gray-500 mt-2 flex items-center gap-2">
          {new Date(stream.stream_date).toLocaleString('ja-JP', {
            year: 'numeric',
            month: '2-digit',
            day: '2-digit',
            hour: '2-digit',
            minute: '2-digit',
            second: '2-digit',
            hour12: false
          })}
          <a
            href={youtubeUrl}
            target="_blank"
            rel="noopener noreferrer"
            className="inline-flex items-center justify-center w-7 h-7 rounded-full text-gray-400 hover:text-red-600 hover:bg-red-50 transition-colors"
            title="YouTubeで見る"
          >
            <svg className="w-4 h-4" fill="currentColor" viewBox="0 0 24 24">
              <path d="M23.498 6.186a3.016 3.016 0 0 0-2.122-2.136C19.505 3.545 12 3.545 12 3.545s-7.505 0-9.377.505A3.017 3.017 0 0 0 .502 6.186C0 8.07 0 12 0 12s0 3.93.502 5.814a3.016 3.016 0 0 0 2.122 2.136c1.871.505 9.376.505 9.376.505s7.505 0 9.377-.505a3.015 3.015 0 0 0 2.122-2.136C24 15.93 24 12 24 12s0-3.93-.502-5.814zM9.545 15.568V8.432L15.818 12l-6.273 3.568z" />
            </svg>
          </a>
        </p>

        {/* Tags + 編集アイコン */}
        <div className="flex flex-wrap items-center gap-2 mt-4">
          {stream.tags.map((tag) => (
            <Tag key={tag.id} label={tag.display_name} color={tag.color} size="md" />
          ))}
          {canEdit && (
            <button
              onClick={() => setTagPickerOpen(!tagPickerOpen)}
              className={`inline-flex items-center justify-center w-7 h-7 rounded-full transition-colors ${
                tagPickerOpen ? 'text-indigo-600 bg-indigo-50' : 'text-gray-400 hover:text-indigo-600 hover:bg-indigo-50'
              }`}
              title="タグを編集"
            >
              <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z" />
              </svg>
            </button>
          )}
        </div>
        {canEdit && tagPickerOpen && (
          <div className="flex flex-wrap gap-2 mt-2 p-3 bg-gray-50 rounded-lg border">
            {STREAM_TAGS.map((tag) => {
              const ids = stream.tags.map((t) => t.id);
              const active = ids.includes(tag.id);
              return (
                <button
                  key={tag.id}
                  onClick={() =>
                    quickSaveStream({
                      tag_ids: active ? ids.filter((i) => i !== tag.id) : [...ids, tag.id],
                    })
                  }
                  className={`px-3 py-1 rounded-full text-sm font-medium transition-colors ${
                    active ? 'text-white' : 'bg-gray-200 text-gray-700 hover:bg-gray-300'
                  }`}
                  style={active ? { backgroundColor: tag.color } : {}}
                >
                  {tag.label}
                </button>
              );
            })}
          </div>
        )}

        {/* Participants + 編集アイコン */}
        <div className="flex flex-wrap items-center gap-2 mt-4">
          {stream.participants?.map((singer) => (
            <div
              key={singer.id}
              className="flex items-center gap-2 px-3 py-1 bg-gray-100 rounded-full text-sm"
            >
              {/* リンクは名前と画像だけに掛ける。チップ全体を包むと、
                  「参加チャンネルから外す」の ✕ を押したときにチャンネルページへ飛ぶ */}
              <Link
                to={`/singers/${singer.id}`}
                className="flex items-center gap-2 text-gray-700 hover:text-indigo-600 transition-colors"
                title={`${singer.name} のチャンネルページを開く`}
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
              </Link>
              {canEdit && participantAddOpen && (
                <button
                  onClick={() =>
                    quickSaveStream({
                      participant_ids: (stream.participants ?? [])
                        .map((p) => p.id)
                        .filter((pid) => pid !== singer.id),
                    })
                  }
                  className="text-gray-400 hover:text-red-600 transition-colors"
                  title="参加チャンネルから外す"
                >
                  <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                  </svg>
                </button>
              )}
            </div>
          ))}
          {canEdit && (
            <button
              onClick={() => setParticipantAddOpen(!participantAddOpen)}
              className={`inline-flex items-center justify-center w-7 h-7 rounded-full transition-colors ${
                participantAddOpen ? 'text-indigo-600 bg-indigo-50' : 'text-gray-400 hover:text-indigo-600 hover:bg-indigo-50'
              }`}
              title="参加チャンネルを編集"
              aria-label="参加チャンネルを編集"
            >
              <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z" />
              </svg>
            </button>
          )}
        </div>
        {canEdit && participantAddOpen && (
          <div className="mt-2">
            <SingerSearchInput
              excludeIds={stream.participants?.map((p) => p.id) ?? []}
              onSelectSinger={(singer) =>
                quickSaveStream({
                  participant_ids: [...(stream.participants?.map((p) => p.id) ?? []), singer.id],
                })
              }
              placeholder="参加チャンネル名を入力して追加..."
            />
          </div>
        )}

        {/* 非表示・セットリスト非公開。**別の軸**なので並べて出す */}
        {canEdit && (
          <div className="mt-4 space-y-2">
            <label className="flex items-center gap-2 cursor-pointer w-fit">
              <input
                type="checkbox"
                checked={stream.is_hidden}
                onChange={(e) => quickSaveStream({ is_hidden: e.target.checked })}
                className="w-4 h-4 text-red-600 border-gray-300 rounded focus:ring-red-500"
              />
              <span className="text-sm font-medium text-gray-700">非表示</span>
              <span className="text-xs text-gray-500">一覧・発見面から外す（中身は誰でも読める）</span>
            </label>
            <label className="flex items-center gap-2 cursor-pointer w-fit">
              <input
                type="checkbox"
                checked={stream.is_restricted}
                onChange={(e) => quickSaveStream({ is_restricted: e.target.checked })}
                className="w-4 h-4 text-red-600 border-gray-300 rounded focus:ring-red-500"
              />
              <span className="text-sm font-medium text-gray-700">セットリスト非公開</span>
              <span className="text-xs text-gray-500">歌唱を編集者だけに見せる（会限など、公開可否が未確認のもの）</span>
            </label>
            {/* **公開の裁定と現在の会限判定が食い違う**（issue #26）。控えが無い旧裁定も
                含むため、検出と裁定の前後関係は断定しない。
                上書きはしない（裁定の意味が無くなる）── 食い違いを見せて人に決めさせる。
                どちらを押しても裁定を書き直すので、その時点の判定が控えられて消える。 */}
            {stream.restriction_needs_review ? (
              <div className="text-xs text-red-800 bg-red-50 border border-red-200 rounded px-2 py-1.5 space-y-1.5">
                <p>
                  この配信は<strong>会限として検出されています</strong>が、セットリストを
                  「公開してよい」という裁定が残っています。裁定時点の判定が不明な場合もあるため、公開可否を確認してください。
                </p>
                <div className="flex flex-wrap gap-2">
                  <button
                    type="button"
                    onClick={() => quickSaveStream({ is_restricted: true })}
                    className="px-2 py-0.5 rounded bg-red-600 text-white hover:bg-red-700"
                  >
                    非公開にする
                  </button>
                  <button
                    type="button"
                    onClick={() => quickSaveStream({ is_restricted: false })}
                    className="px-2 py-0.5 rounded border border-red-300 text-red-800 hover:bg-red-100"
                  >
                    公開のまま（確認済み）
                  </button>
                </div>
              </div>
            ) : (
              stream.restriction_override !== undefined && (
                // 実効値だけでは「チャンネルの方針で公開」と「この配信だけ公開と裁定」が
                // 見分けられない。後者は方針を変えても動かないので、そう言っておく。
                <p className="text-xs text-gray-500">
                  この配信だけ個別に「{stream.restriction_override ? '非公開' : '公開してよい'}」と裁定済みです（チャンネルの方針より優先）。
                </p>
              )
            )}
            {/* **seTORI を伏せても Holodex のコピーは残りうる。** 向こうへは運用者の名義で
                書き込んでおり、seTORI からは取り消せない。

                材料は 3 つ。台帳（holodex_uploaded_at）は migration 054 以降の送信しか
                持たず、holodex_data の曲も PUT の結果を書き戻していないので旧行では空。
                **どちらも無い＝送っていない、とは言えない**ので、
                「追跡開始より前から在る」(holodex_upload_unknown) を 3 つ目に置く。
                これが無いと、警告が出ないことを安全の根拠にできない。

                文言は断定しない。台帳は PUT の**前**に書くので「試みた」までしか言えず、
                通信に失敗していれば向こうには無い。 */}
            {stream.is_restricted &&
              (stream.holodex_uploaded_at ||
                (stream.holodex_timeline_songs?.length ?? 0) > 0 ||
                stream.holodex_upload_unknown) && (
                <p className="text-xs text-amber-800 bg-amber-50 border border-amber-200 rounded px-2 py-1.5">
                  {stream.holodex_uploaded_at
                    ? `${new Date(stream.holodex_uploaded_at).toLocaleDateString('ja-JP')} に Holodex への送信を試みています。`
                    : (stream.holodex_timeline_songs?.length ?? 0) > 0
                      ? 'Holodex 側にこの配信の曲が登録されています。'
                      : 'この配信は送信記録を取り始める前からあるため、過去に Holodex へ送ったかどうか分かりません。'}
                  seTORI で非公開にしても Holodex 側のデータは残っている可能性があります
                  （seTORI からは取り消せません）。Holodex 上で確認してください。
                </p>
              )}
          </div>
        )}
      </>
    </div>
  );
}
