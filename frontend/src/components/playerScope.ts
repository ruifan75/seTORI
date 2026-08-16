import { createContext, useContext } from 'react';
import type { PlayerScope } from './youtubePlayerControl';

// 「この画面の再生位置・試聴はどのプレイヤーのものか」をツリーに配る。
//
// TimestampTweaker / PerformanceFields は編集ページ（埋め込みプレイヤー）からも
// 再生バーからも使われる。scope をプロパティで穿つと通り道の全部品が中継する
// ことになるので、文脈として持たせる。
//
// null は「対応するプレイヤーが無い」。別の配信を再生中のときに渡すと、
// 赤い再生位置も「再生位置を使う」も出なくなる ── **別の動画の位置を
// この曲の時刻として出さない**ための口。
//
// 既定が 'page' なのは、埋め込みプレイヤーを持つ編集ページ・審査ページが
// 何も包まずに従来どおり動くようにするため。
export const PlayerScopeContext = createContext<PlayerScope | null>('page');

export function usePlayerScope(): PlayerScope | null {
  return useContext(PlayerScopeContext);
}
