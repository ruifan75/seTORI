// 秘匿の配信に載っている歌唱だと分かる印。
//
// `restricted:view` を持つ運用者には発見面（曲・歌手・タグ・検索・ランダム・
// プリセット）にも秘匿の歌唱が出るが、**見えるだけでは公開されているものと
// 見分けが付かない**。曲ページの「5 回」のうち何回が公開ぶんか分からないし、
// 画面共有に伏せているはずの内容がそのまま出る。
//
// 権限の無い人には行そのものが返らないので、この印は運用者にしか出ない
// （`is_restricted` は false のとき応答から省かれる）。
export default function RestrictedBadge() {
  return (
    <span
      className="shrink-0 px-1.5 py-0.5 text-[11px] font-medium rounded bg-amber-100 text-amber-800"
      title="この歌唱は秘匿の配信のものです。公開されている一覧には出ません"
    >
      非公開
    </span>
  );
}
