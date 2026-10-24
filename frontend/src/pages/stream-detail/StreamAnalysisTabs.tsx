import ManualInputImport from '../../components/ManualInputImport';
import QueryError from '../../components/ui/QueryError';
import { playerSeekTo } from '../../components/youtubePlayerControl';
import RawCommentsPanel from '../../components/RawCommentsPanel';
import SourceSongList from '../../components/SourceSongList';
import HolodexSyncActions from '../../components/HolodexSyncActions';
import { formatTime } from './utils';

import type { StreamDetailModel } from './useStreamDetail';

export default function StreamAnalysisTabs({ model }: { model: StreamDetailModel }) {
  const {
    setEditTab,
    editTab,
    autoLoad,
    holodexAnalyzeLoading,
    commentAnalyzeLoading,
    chapterAnalyzeLoading,
    syncYouTubeCommentsMutation,
    loadFromHolodex,
    holodexTimelineSongs,
    loadFromComments,
    stream,
    runAINormalization,
    editableSongs,
    aiNormalizeMutation,
    syncVideoMutation,
    syncToHolodexMutation,
    addSuggestionSong,
    chatEndMutation,
    commentTimelineSongs,
    PERFORMANCE_TAGS,
    addCommentSongToList,
    loadFromChapters,
    chapterTimelineSongs,
    addChapterSongToList,
    addFromRawComment,
  } = model;
  return (
    <div className="flex flex-col min-h-0">
      {model.fetchError && <QueryError error={model.fetchError.error} onRetry={model.fetchError.refetch} />}
      {/* 狭い器ではタブを次の行へ送る。ラベル自体を縮めて縦に折り返さない。
          幅が足りるデスクトップでは従来と同じ一列の寸法になる。 */}
      <div className="flex flex-wrap border-b shrink-0 sticky top-0 bg-white z-10">
        {([
          { key: 'actions', label: '操作' },
          { key: 'holodex', label: 'Holodex' },
          { key: 'comment', label: 'コメント' },
          { key: 'chapter', label: 'チャプター' },
          // 会限配信はサーバーから入力源を取れないので、編集者が手元の
          // yt-dlp で取ったものを持ち込む口。コメントと live chat の
          // 両方を扱うので、どちらかのタブに寄せず独立させる。
          { key: 'import', label: '手動' },
          { key: 'raw', label: '生コメント' },
        ] as const).map((t) => (
          <button
            key={t.key}
            onClick={() => setEditTab(t.key)}
            className={`px-3.5 py-2.5 text-sm font-medium shrink-0 whitespace-nowrap border-b-2 -mb-px transition-colors ${
              editTab === t.key
                ? 'border-indigo-600 text-indigo-600'
                : 'border-transparent text-gray-500 hover:text-gray-700'
            }`}
          >
            {t.label}
          </button>
        ))}
      </div>

      <div className="p-4">
        {editTab === 'actions' && (
          <div className="space-y-4">
            <div>
              <p className="text-xs font-medium text-gray-400 mb-1.5">読み込み</p>
              <div className="flex flex-wrap gap-2">
                <button
                  onClick={autoLoad}
                  disabled={holodexAnalyzeLoading || commentAnalyzeLoading || chapterAnalyzeLoading || syncYouTubeCommentsMutation.isPending}
                  className="px-3 py-1.5 text-sm bg-indigo-600 text-white font-medium rounded-lg hover:bg-indigo-700 transition-colors disabled:opacity-50"
                  title="Holodex → コメント → チャプター の優先順で読み込み、正規化と chat 時間チェックまで実行"
                >
                  {holodexAnalyzeLoading || commentAnalyzeLoading || chapterAnalyzeLoading ? '読み込み中...' : '自動読み込み'}
                </button>
                <button
                  onClick={() => loadFromHolodex(false)}
                  disabled={holodexTimelineSongs.length === 0 || holodexAnalyzeLoading}
                  className="px-3 py-1.5 text-sm bg-indigo-50 text-indigo-700 border border-indigo-200 font-medium rounded-lg hover:bg-indigo-100 transition-colors disabled:opacity-50"
                >
                  {holodexAnalyzeLoading ? 'Holodex分析中...' : 'Holodex データ'}
                </button>
                <button
                  onClick={() => loadFromComments(false)}
                  disabled={!stream?.has_comment_raw || commentAnalyzeLoading || syncYouTubeCommentsMutation.isPending}
                  className="px-3 py-1.5 text-sm bg-indigo-50 text-indigo-700 border border-indigo-200 font-medium rounded-lg hover:bg-indigo-100 transition-colors disabled:opacity-50"
                >
                  {commentAnalyzeLoading ? 'コメント分析中...' : 'コメント データ'}
                </button>
                <button
                  onClick={runAINormalization}
                  disabled={editableSongs.length === 0 || aiNormalizeMutation.isPending}
                  className="px-3 py-1.5 text-sm bg-indigo-50 text-indigo-700 border border-indigo-200 font-medium rounded-lg hover:bg-indigo-100 transition-colors disabled:opacity-50"
                >
                  {aiNormalizeMutation.isPending ? 'AI処理中...' : 'AI正規化'}
                </button>
              </div>
            </div>
            <div>
              <p className="text-xs font-medium text-gray-400 mb-1.5">コメント同期</p>
              <div className="flex flex-wrap gap-2">
                <button
                  onClick={() => syncYouTubeCommentsMutation.mutate()}
                  disabled={syncYouTubeCommentsMutation.isPending || commentAnalyzeLoading}
                  title="YouTube Data API から公開トップレベルコメントを取得し直します"
                  className="px-3 py-1.5 text-sm bg-red-50 text-red-700 border border-red-200 font-medium rounded-lg hover:bg-red-100 transition-colors disabled:opacity-50"
                >
                  {syncYouTubeCommentsMutation.isPending ? 'YouTubeから同期中...' : 'YouTubeからコメント同期'}
                </button>
              </div>
            </div>
            <HolodexSyncActions
              onDownload={() => syncVideoMutation.mutate()}
              onUpload={() => syncToHolodexMutation.mutate()}
              downloading={syncVideoMutation.isPending}
              uploading={syncToHolodexMutation.isPending}
            />
          </div>
        )}

        {editTab === 'holodex' && (
          <div className="space-y-2">
            <div className="flex gap-2">
              <button
                onClick={() => loadFromHolodex(false)}
                disabled={holodexTimelineSongs.length === 0 || holodexAnalyzeLoading}
                className="flex-1 px-3 py-1.5 text-sm bg-indigo-600 text-white font-medium rounded-lg hover:bg-indigo-700 transition-colors disabled:opacity-50"
                title="全曲を編集リストへ読み込む（正規化＋chat 時間チェック込み）"
              >
                {holodexAnalyzeLoading ? '分析中...' : '全部読み込む'}
              </button>
              <button
                onClick={() => loadFromHolodex(true)}
                disabled={holodexTimelineSongs.length === 0 || holodexAnalyzeLoading}
                title="キャッシュを無視して AI で再分析・再正規化します"
                className="px-3 py-1.5 text-sm bg-white text-gray-600 border border-gray-300 font-medium rounded-lg hover:bg-gray-50 transition-colors disabled:opacity-50"
              >
                再分析
              </button>
            </div>
            {holodexTimelineSongs.length === 0 ? (
              <p className="text-sm text-gray-400 py-2">Holodex データがありません</p>
            ) : (
              <div className="space-y-0.5">
                {holodexTimelineSongs.map((song, i) => (
                  <div
                    key={i}
                    onClick={() => addSuggestionSong(song)}
                    className="flex items-baseline gap-2 px-2 py-1.5 rounded hover:bg-indigo-50 cursor-pointer group text-sm"
                    title="クリックで追加"
                  >
                    <span className="shrink-0 flex items-baseline gap-0.5">
                      <button
                        onClick={(e) => {
                          e.stopPropagation();
                          playerSeekTo('page', song.start_seconds);
                        }}
                        className="px-1.5 rounded bg-blue-50 text-blue-700 font-mono text-xs hover:bg-blue-100 transition-colors"
                        title="開始時間にジャンプ"
                      >
                        {formatTime(song.start_seconds)}
                      </button>
                      {song.end_seconds > 0 && (
                        <>
                          <span className="text-gray-300 text-xs">〜</span>
                          <button
                            onClick={(e) => {
                              e.stopPropagation();
                              playerSeekTo('page', song.end_seconds);
                            }}
                            className="px-1.5 rounded bg-blue-50/60 text-blue-600 font-mono text-xs hover:bg-blue-100 transition-colors"
                            title="終了時間にジャンプ"
                          >
                            {formatTime(song.end_seconds)}
                          </button>
                        </>
                      )}
                    </span>
                    <span className="min-w-0 flex-1 truncate">
                      <span className="text-gray-900 font-medium">{song.name}</span>
                      {song.original_artist && <span className="text-gray-500"> / {song.original_artist}</span>}
                    </span>
                    <span className="shrink-0 text-gray-300 group-hover:text-indigo-600 transition-colors">＋</span>
                  </div>
                ))}
              </div>
            )}
          </div>
        )}

        {editTab === 'comment' && (
          <div className="space-y-2">
            <div className="flex gap-2">
              <button
                onClick={() => loadFromComments(false)}
                disabled={!stream?.has_comment_raw || commentAnalyzeLoading || syncYouTubeCommentsMutation.isPending}
                className="flex-1 px-3 py-1.5 text-sm bg-indigo-600 text-white font-medium rounded-lg hover:bg-indigo-700 transition-colors disabled:opacity-50"
                title="分析済みの全曲を編集リストへ読み込む（正規化＋chat 時間チェック込み）"
              >
                {commentAnalyzeLoading ? '分析中...' : '全部読み込む'}
              </button>
              <button
                onClick={() => loadFromComments(true)}
                disabled={!stream?.has_comment_raw || commentAnalyzeLoading || syncYouTubeCommentsMutation.isPending}
                title="キャッシュを無視して AI で再分析・再正規化します"
                className="px-3 py-1.5 text-sm bg-white text-gray-600 border border-gray-300 font-medium rounded-lg hover:bg-gray-50 transition-colors disabled:opacity-50"
              >
                再分析
              </button>
              <button
                onClick={() => chatEndMutation.mutate()}
                disabled={chatEndMutation.isPending || commentAnalyzeLoading}
                title="live chat の拍手から終了時間だけを取り直します（AI は使いません。live chat のダウンロードで数十秒かかることがあります）"
                className="px-2 py-1.5 text-sm bg-white text-gray-600 border border-gray-300 rounded-lg hover:bg-gray-50 transition-colors disabled:opacity-50"
              >
                {chatEndMutation.isPending ? (
                  <svg className="w-4 h-4 animate-spin" fill="none" viewBox="0 0 24 24">
                    <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
                    <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
                  </svg>
                ) : (
                  /* 拍手＝手のアイコン */
                  <svg className="w-4 h-4" fill="none" stroke="currentColor" strokeWidth={1.8} viewBox="0 0 24 24">
                    <path
                      strokeLinecap="round"
                      strokeLinejoin="round"
                      d="M7 11V6a1.5 1.5 0 013 0v4m0-4.5a1.5 1.5 0 013 0V10m0-3.5a1.5 1.5 0 013 0V13m0-2.5a1.5 1.5 0 013 0V15a6 6 0 01-6 6h-2a6 6 0 01-5.2-3L4 14.5a1.5 1.5 0 012.6-1.5L7 14"
                    />
                  </svg>
                )}
              </button>
            </div>
            {/* 最後に解析した時刻。プロンプトや抽出規則を変えたあと、
                この配信がまだ古い規則のままかを判断する手がかりになる。
                stream.updated_at は Holodex 同期でも動くので使えない。 */}
            {stream.comment_songs_analyzed_at && (
              <p className="px-1 text-xs text-gray-400">
                最終解析:{' '}
                {new Date(stream.comment_songs_analyzed_at).toLocaleString('ja-JP', {
                  year: 'numeric', month: '2-digit', day: '2-digit',
                  hour: '2-digit', minute: '2-digit', hour12: false,
                })}
              </p>
            )}
            <SourceSongList
              songs={commentTimelineSongs}
              performanceTags={PERFORMANCE_TAGS}
              onAdd={addCommentSongToList}
              emptyMessage="分析済みの曲がありません（「全部読み込む」で分析を実行）"
            />
          </div>
        )}

        {editTab === 'chapter' && (
          <div className="space-y-2">
            <div className="flex gap-2">
              <button
                onClick={() => loadFromChapters(false)}
                disabled={stream?.chapter_count === 0 || chapterAnalyzeLoading}
                className="flex-1 px-3 py-1.5 text-sm bg-indigo-600 text-white font-medium rounded-lg hover:bg-indigo-700 transition-colors disabled:opacity-50"
                title="配信者が付けた目次から曲を取り出して編集リストへ読み込む（正規化＋chat 時間チェック込み）"
              >
                {chapterAnalyzeLoading ? '分析中...' : '全部読み込む'}
              </button>
              <button
                onClick={() => loadFromChapters(true)}
                disabled={chapterAnalyzeLoading}
                title="チャプターを取り直して AI で再分析します（配信者が後から目次を足した場合）"
                className="px-3 py-1.5 text-sm bg-white text-gray-600 border border-gray-300 font-medium rounded-lg hover:bg-gray-50 transition-colors disabled:opacity-50"
              >
                再取得
              </button>
            </div>
            {/* 「まだ調べていない（-1）」と「調べたが無い（0）」を書き分ける。
                同じ文言にすると、取得を試せば済む配信が諦めた配信に見える */}
            {stream.chapter_count === -1 && (
              <p className="px-1 text-xs text-gray-400">
                チャプターは未取得です（「全部読み込む」で YouTube から取得します）
              </p>
            )}
            {stream.chapter_count === 0 && (
              <p className="px-1 text-xs text-gray-400">この配信にチャプターはありません</p>
            )}
            <SourceSongList
              songs={chapterTimelineSongs}
              performanceTags={PERFORMANCE_TAGS}
              onAdd={addChapterSongToList}
              emptyMessage="分析済みの曲がありません（「全部読み込む」で分析を実行）"
            />
          </div>
        )}

        {editTab === 'import' && (
          <ManualInputImport
            // **配信ごとに作り直す。** 取り込み結果は state に持って
            // いるので、同じ画面のまま別の配信へ移ると前の配信の要約が
            // 新しい配信の長さと並んで出る ── 取り違えの確認を
            // 誤らせるうえ、削除は現在の配信に効く。
            key={stream.id}
            videoId={stream.id}
            durationSeconds={stream.duration_seconds}
          />
        )}

        {editTab === 'raw' && (
          <RawCommentsPanel
            videoId={stream.id}
            onSeek={(secs) => playerSeekTo('page', secs)}
            onAddSong={addFromRawComment}
          />
        )}
      </div>
    </div>
  );
}
