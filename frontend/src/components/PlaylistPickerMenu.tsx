import { useCallback, useEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { useQuery } from '@tanstack/react-query';
import { playlistApi } from '../api/client';
import { menuPositionFor, type MenuPosition } from './menuPosition';

interface Props {
  /** 位置合わせの基準になるボタン */
  anchorRef: React.RefObject<HTMLElement | null>;
  /** 開く直前に呼び出し元が測った表示位置（menuPositionFor で作る） */
  initialPosition: MenuPosition;
  onClose: () => void;
  /** プレイリスト一覧の前に置く操作（再生キューに追加など） */
  leadingAction?: { label: string; onClick: () => void };
  /** 未ログインなどでプレイリストを取得できない場合に代わりに出す操作 */
  playlistUnavailableAction?: { label: string; onClick: () => void };
  /** プレイリスト一覧の見出し */
  heading?: string;
  onPick: (playlistId: string, playlistName: string) => void;
  onCreate: (name: string) => void;
  /** 追加処理の実行中は選べないようにする */
  busy?: boolean;
  /** 新規作成欄の初期値（プリセット名など） */
  defaultName?: string;
}

export const PLAYLIST_MENU_WIDTH = 256;
const MENU_WIDTH = PLAYLIST_MENU_WIDTH;

/**
 * 「どのプレイリストへ入れるか」を選ばせるメニュー（既存から選ぶ / 新しく作る）。
 *
 * 1曲・おすすめの追加（QueueAddButton）とプリセットの追加（PresetActions）で共通に使う。
 * 選択の仕組みは同じなので、片方だけ挙動が変わらないようにここへ寄せてある。
 *
 * メニューはビューポート基準（fixed）で body 直下に描く。呼び出し元がホームの
 * 横スクロール列（overflow-x-auto）の中にも置かれるため、通常の absolute だと
 * 祖先に切り取られて見えなくなる。
 */
export default function PlaylistPickerMenu({
  anchorRef,
  initialPosition,
  onClose,
  leadingAction,
  playlistUnavailableAction,
  heading = 'プレイリストに追加',
  onPick,
  onCreate,
  busy = false,
  defaultName = '',
}: Props) {
  const menuRef = useRef<HTMLDivElement>(null);
  const [name, setName] = useState(defaultName);
  // 以後はスクロールやリサイズのたびに測り直す（イベントの中なので ref を読んでよい）。
  const [pos, setPos] = useState(initialPosition);

  const updatePosition = useCallback(() => {
    setPos(menuPositionFor(anchorRef.current, MENU_WIDTH));
  }, [anchorRef]);

  // メニュー外クリック・Esc で閉じる。スクロールやリサイズでは位置を追従させる。
  useEffect(() => {
    const onPointerDown = (e: MouseEvent) => {
      const target = e.target as Node;
      if (anchorRef.current?.contains(target) || menuRef.current?.contains(target)) return;
      onClose();
    };
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    document.addEventListener('mousedown', onPointerDown);
    document.addEventListener('keydown', onKeyDown);
    window.addEventListener('resize', updatePosition);
    window.addEventListener('scroll', updatePosition, true); // 祖先のスクロールも拾う
    return () => {
      document.removeEventListener('mousedown', onPointerDown);
      document.removeEventListener('keydown', onKeyDown);
      window.removeEventListener('resize', updatePosition);
      window.removeEventListener('scroll', updatePosition, true);
    };
  }, [anchorRef, onClose, updatePosition]);

  // 開いているときだけ自分のプレイリストを取る（このコンポーネントは開くと同時に生える）
  const playlists = useQuery({
    queryKey: ['playlists', 'mine'],
    queryFn: playlistApi.listMine,
    enabled: !playlistUnavailableAction,
  });

  return createPortal(
    <div
      ref={menuRef}
      style={{ position: 'fixed', top: pos.top, left: pos.left, width: MENU_WIDTH }}
      className="z-50 bg-white border border-gray-200 rounded-lg shadow-lg py-1 text-left"
    >
      {leadingAction && (
        <>
          <button
            onClick={leadingAction.onClick}
            disabled={busy}
            className="w-full px-3 py-2 text-left text-sm text-gray-700 hover:bg-gray-50 transition-colors"
          >
            {leadingAction.label}
          </button>
          <div className="border-t border-gray-100 my-1" />
        </>
      )}

      <p className="px-3 py-1 text-xs font-medium text-gray-400">{heading}</p>

      {playlistUnavailableAction ? (
        <button
          onClick={playlistUnavailableAction.onClick}
          className="w-full px-3 py-2 text-left text-sm text-indigo-600 hover:bg-indigo-50 transition-colors"
        >
          {playlistUnavailableAction.label}
        </button>
      ) : playlists.isLoading ? (
        <p className="px-3 py-2 text-sm text-gray-400">読み込み中...</p>
      ) : (playlists.data?.playlists.length ?? 0) === 0 ? (
        <p className="px-3 py-2 text-sm text-gray-400">まだプレイリストがありません</p>
      ) : (
        <ul className="max-h-48 overflow-y-auto">
          {playlists.data!.playlists.map((pl) => (
            <li key={pl.id}>
              <button
                onClick={() => onPick(pl.id, pl.name)}
                disabled={busy}
                className="w-full px-3 py-2 text-left text-sm text-gray-700 hover:bg-gray-50 disabled:text-gray-300 transition-colors flex items-center justify-between gap-2"
              >
                <span className="truncate">{pl.name}</span>
                <span className="text-xs text-gray-400 shrink-0">{pl.item_count}</span>
              </button>
            </li>
          ))}
        </ul>
      )}

      {!playlistUnavailableAction && (
        <>
          <div className="border-t border-gray-100 my-1" />
          <form
            onSubmit={(e) => {
              e.preventDefault();
              if (!busy && name.trim()) onCreate(name.trim());
            }}
            className="px-2 py-1"
          >
            <input
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="新しいプレイリスト名"
              disabled={busy}
              className="w-full px-2 py-1 text-sm border border-gray-300 rounded focus:ring-2 focus:ring-indigo-500 focus:border-transparent disabled:bg-gray-50"
            />
          </form>
        </>
      )}
    </div>,
    document.body,
  );
}
