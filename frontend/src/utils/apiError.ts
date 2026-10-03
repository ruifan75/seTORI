import { isAxiosError } from 'axios';

// apiErrorMessage はバックエンドが返した具体的な文言を取り出す（無ければ fallback）。
export function apiErrorMessage(error: unknown, fallback: string): string {
  if (isAxiosError<{ message?: string; error?: string }>(error)) {
    return error.response?.data?.message || error.response?.data?.error || fallback;
  }
  return error instanceof Error && error.message ? error.message : fallback;
}

// analysisFailureMessage は入力元の読み込み（Holodex / コメント / チャプター）の失敗を
// 利用者向けの文言にする（issue #7）。
//
// **409 は「もう一度押せば通る」と言う。** 分析中に同期がコメントを差し替えると、
// バックエンドは古い入力から作った結果を捨てて 409 を返す（`ErrCommentRawChanged`）。
// 以前は理由を捨てて「失敗しました」とだけ出していたので、壊れたのか、
// やり直せばよいのかが伝わらなかった。
//
// それ以外も**バックエンドの文言を捨てない** ── 「失敗しました」だけでは、
// 未設定・外部の障害・入力が無い、のどれなのかを利用者が切り分けられない。
export function analysisFailureMessage(label: string, error: unknown): string {
  if (isAxiosError(error) && error.response?.status === 409) {
    return `${label}：分析中に入力が更新されました。もう一度押すと最新の内容で分析し直します`;
  }
  const detail = apiErrorMessage(error, '');
  return detail ? `${label}に失敗しました：${detail}` : `${label}に失敗しました`;
}
