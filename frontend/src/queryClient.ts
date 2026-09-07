import { QueryClient } from '@tanstack/react-query';

// **QueryClient をモジュールとして持つ。** 認証状態が変わったときに
// キャッシュを捨てる必要があり、その判断は React の木の外（Zustand の
// store）にあるため。
export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 1000 * 60 * 5, // 5 minutes
      retry: 1,
    },
  },
});

// resetQueryCacheForAuthChange はログイン・ログアウト・利用者の切り替えで呼ぶ。
//
// **応答の中身が権限で変わるので、権限が変わったキャッシュは捨てるしかない。**
// 個々の queryKey に権限を混ぜる方法もあるが、この codebase では
// **同じ間違いを 4 回している** ── 混ぜ忘れた 1 つが残るたびに
// 「ログアウトしても admin の結果が見える」が復活する。
// ここで丸ごと捨てれば、新しく足した query も自動的に守られる。
//
// **`clear()` では足りない。** キャッシュは空になるが、**購読中の
// QueryObserver は直前の結果を持ったままで再取得もしない**ので、秘匿を
// 表示している画面はそのまま残る（レビューで実測して確認された）。
//
// `resetQueries()` は初期状態へ戻したうえで**表示中の query を取り直す**。
// データが先に消えるので、取り直しの間に古い値が描かれることもない
// （`invalidateQueries()` は古い値を見せたまま取り直すので、ここでは使えない）。
//
// **画面ごと作り直す方法は採らなかった。** `BrowserRouter` に key を付ける形を
// 試したが、ログイン後に URL だけ変わってログイン画面が残る・OAuth の
// コードを 3 回交換する・再生位置と一時停止が失われる、と別の壊れ方をした。
// 直すのは「表示されているデータ」であって、画面の同一性ではない。
export function resetQueryCacheForAuthChange() {
  // 表示中も含めて初期状態へ戻し、購読中のものは取り直させる。
  void queryClient.resetQueries();
}

// ========== 権限が変わったときに捨てるもの ==========

// currentViewerID は「いま誰として見ているか」。**キャッシュの外へコピーした
// データを捨てる判断に使う。**
//
// query cache は `resetQueries()` で取り直せるが、**コピーされたものには届かない**
// ── 編集フォーム、再生キュー、ポップアップ、通知。これらは自分で捨てるしかない。
//
// 非同期処理は「始めたときの利用者」を覚えておき、**完了時に照合する**こと。
// 応答が返るまでの間にログアウトすると、捨てたはずのデータが復活する
// （実測：おすすめ全曲再生の補充リクエストで再現）。
let currentViewerID: string | null = null;

// ViewerChangeListener は利用者が変わったときに呼ばれる。
type ViewerChangeListener = () => void;

const viewerChangeListeners = new Set<ViewerChangeListener>();

// onViewerChange は破棄処理を登録する（戻り値で解除）。
export function onViewerChange(fn: ViewerChangeListener): () => void {
  viewerChangeListeners.add(fn);
  return () => viewerChangeListeners.delete(fn);
}

// viewerID は現在の利用者。非同期処理の前後で比べる。
export function viewerID(): string | null {
  return currentViewerID;
}

// sameViewer は「始めたときと同じ利用者か」。**非同期の完了時に必ず確かめる。**
export function sameViewer(startedAs: string | null): boolean {
  return currentViewerID === startedAs;
}

// applyViewerChange は利用者が変わったときの後始末を 1 か所で行う。
//
// **散らばらせないこと。** 以前はログアウト時にしか捨てていなかったので、
// ログイン（利用者の切り替え）・非同期の完了・ポップアップ・通知が全部漏れた。
export function applyViewerChange(nextViewerID: string | null) {
  if (currentViewerID === nextViewerID) return;
  currentViewerID = nextViewerID;
  // 表示中も含めて初期状態へ戻し、購読中のものは取り直させる。
  void queryClient.resetQueries();
  for (const fn of viewerChangeListeners) fn();
}
