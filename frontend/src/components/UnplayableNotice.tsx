import type { Playability } from '../api/types';

// 埋め込み再生できない配信のために、プレイヤーの代わりに置く案内。
//
// 会限（メンバー限定）の動画は YouTube が埋め込みを塞いでいる。メンバー資格の
// あるアカウントでログインしていても再生できないので、「入れば見られる」とは書かない。
// 削除・非公開はそもそも YouTube 側にも無いが、リンクは残す ── 復活したり
// 別の場所へ移っていたりするので、行き止まりにしないため。
// NoticeKind は保存済みの判定に加えて、再生時に実際に失敗した場合を持つ。
// **エラーコードからは会限と埋め込み無効を区別できない**（どちらも 101/150）ので、
// 推測して片方の文言を出さず、両方の可能性を書く。
export type NoticeKind = Exclude<Playability, 'unknown' | 'playable'> | 'playback_failed';

const MESSAGES: Record<NoticeKind, { title: string; detail: string }> = {
  playback_failed: {
    title: 'この配信は再生できませんでした',
    detail: 'YouTube が埋め込み再生を許可していません。メンバー限定配信か、配信者が外部サイトでの再生を無効にしている可能性があります。',
  },
  members_only: {
    title: 'メンバー限定の配信です',
    detail: 'YouTube がメンバー限定配信の埋め込み再生を許可していないため、ここでは再生できません。メンバーシップに加入していても同じです。',
  },
  embed_disabled: {
    title: '埋め込み再生が許可されていません',
    detail: 'この動画は配信者が外部サイトでの再生を無効にしています。',
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
