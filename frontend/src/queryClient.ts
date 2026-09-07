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
export function resetQueryCacheForAuthChange() {
  queryClient.clear();
}
