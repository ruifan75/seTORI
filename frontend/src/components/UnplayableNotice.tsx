// 埋め込み再生できない配信のために、プレイヤーの代わりに置く案内。
//
// 会限（メンバー限定）の動画は YouTube が埋め込みを塞いでいる。メンバー資格の
// あるアカウントでログインしていても再生できないので、「入れば見られる」とは書かない。
// 削除・非公開はそもそも YouTube 側にも無いが、リンクは残す ── 復活したり
// 別の場所へ移っていたりするので、行き止まりにしないため。
// NoticeKind は**実際に案内へ切り替える理由**だけを持つ。`Playability` から
// 導いた型にはしない ── 保存済みの判定は全部が案内の理由になるわけではない。
//
// **再生できるかは見る人の所在地で変わる。** yt-dlp は東京の VPS で走るので、
// その結果は「東京から見て再生できるか」でしかない。日本で権利上ブロックされて
// いても他の地域では再生できることがあり、先に案内へ倒すとその人から再生を
// 奪う ── しかもプレイヤーを描かないので `onError` は永遠に鳴らず、
// **取り返す機会が無い**（片道の判断）。
//
// 逆向き（東京で再生できるが他の地域では不可）は `onError` が拾うので、
// 誤りは「先に判定する」側にしか出ない。だから**先に判定してよいのは
// 所在地に依存しないと実測できているものだけ**＝会限。
//
// **エラーコードからは会限と埋め込み無効を区別できない**（どちらも 101/150）ので、
// 推測して片方の文言を出さず、両方の可能性を書く。
export type NoticeKind = 'members_only' | 'unavailable' | 'playback_failed';

const MESSAGES: Record<NoticeKind, { title: string; detail: string }> = {
  playback_failed: {
    title: 'この配信は再生できませんでした',
    detail: 'YouTube が埋め込み再生を許可していません。メンバー限定配信か、配信者が外部サイトでの再生を無効にしている可能性があります。',
  },
  members_only: {
    title: 'メンバー限定の配信です',
    detail: 'YouTube がメンバー限定配信の埋め込み再生を許可していないため、ここでは再生できません。メンバーシップに加入していても同じです。',
  },
  unavailable: {
    title: 'この動画は YouTube 上で見られなくなっています',
    detail: '削除・非公開化・権利上の理由で取り下げられた可能性があります。登録済みの歌唱記録はそのまま残ります。',
  },
};

export default function UnplayableNotice({
  kind,
  videoId,
}: {
  kind: NoticeKind;
  videoId: string;
}) {
  const { title, detail } = MESSAGES[kind];
  return (
    <div className="w-full aspect-video bg-gray-900 text-gray-100 flex items-center justify-center p-6">
      <div className="max-w-md text-center space-y-2">
        <p className="text-base font-medium">{title}</p>
        <p className="text-sm text-gray-400 leading-relaxed">{detail}</p>
        <a
          href={`https://www.youtube.com/watch?v=${videoId}`}
          target="_blank"
          rel="noopener noreferrer"
          className="inline-block mt-2 text-sm text-blue-300 hover:text-blue-200 underline"
        >
          YouTube で開く
        </a>
      </div>
    </div>
  );
}
