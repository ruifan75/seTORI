// ボタンにぶら下げるメニューの表示位置。
//
// 呼び出し元が「開く」ハンドラの中で測って、メニューへ渡す。描画中に ref は読めないので、
// 最初の位置は開く側の責任にしてある（測る前に描くと一瞬左上に出る）。

export interface MenuPosition {
  top: number;
  left: number;
}

/** 既定はボタン右端に右揃え。画面外へはみ出す場合は内側へ寄せる。 */
export function menuPositionFor(anchor: HTMLElement | null, width: number): MenuPosition {
  const rect = anchor?.getBoundingClientRect();
  if (!rect) return { top: 0, left: 0 };
  return {
    top: rect.bottom + 4,
    left: Math.min(Math.max(8, rect.right - width), window.innerWidth - width - 8),
  };
}
