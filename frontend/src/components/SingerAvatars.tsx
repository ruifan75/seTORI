import { useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import type { Singer } from '../api/types';

/** チャンネルのアイコン。画像が取れなければ頭文字にする。 */
export function SingerImage({ singer }: { singer: Singer }) {
  const [imageFailed, setImageFailed] = useState(false);
  const imageUrl = singer.photo_url || `https://holodex.net/statics/channelImg/${singer.id}/50.png`;

  if (imageFailed) {
    return <span className="flex h-full w-full items-center justify-center">{singer.name.trim().charAt(0) || '?'}</span>;
  }

  return (
    <img
      src={imageUrl}
      alt=""
      loading="lazy"
      className="h-full w-full object-cover"
      onError={() => setImageFailed(true)}
    />
  );
}

function SingerAvatar({ singer }: { singer: Singer }) {
  return (
    <Link
      to={`/singers/${singer.id}`}
      aria-label={singer.name}
      title={singer.name}
      className="relative inline-flex h-7 w-7 shrink-0 items-center justify-center overflow-hidden rounded-full border-2 border-white bg-indigo-100 text-[10px] font-semibold text-indigo-700 shadow-sm transition-transform hover:z-10 hover:-translate-y-0.5 focus-visible:z-10 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-indigo-500"
    >
      <SingerImage singer={singer} />
    </Link>
  );
}

function SingerOverflowMenu({ singers }: { singers: Singer[] }) {
  const buttonRef = useRef<HTMLButtonElement>(null);
  const popoverRef = useRef<HTMLDivElement>(null);
  const [open, setOpen] = useState(false);

  const toggle = () => {
    const button = buttonRef.current;
    const popover = popoverRef.current;
    if (!button || !popover) return;
    if (popover.matches(':popover-open')) {
      popover.hidePopover();
      return;
    }

    const rect = button.getBoundingClientRect();
    const menuWidth = 224;
    popover.style.left = `${Math.min(Math.max(8, rect.right - menuWidth), window.innerWidth - menuWidth - 8)}px`;
    popover.style.top = `${rect.bottom + 6}px`;
    popover.showPopover();

    window.requestAnimationFrame(() => {
      if (rect.bottom + 6 + popover.offsetHeight > window.innerHeight - 8) {
        popover.style.top = `${Math.max(8, rect.top - popover.offsetHeight - 6)}px`;
      }
    });
  };

  return (
    <>
      <button
        ref={buttonRef}
        type="button"
        onClick={toggle}
        aria-label={`ほか${singers.length}チャンネルを表示`}
        aria-expanded={open}
        title={`ほか${singers.length}チャンネル`}
        className="relative inline-flex h-7 min-w-7 shrink-0 items-center justify-center rounded-full border-2 border-white bg-gray-700 px-1 text-[9px] font-semibold text-white shadow-sm transition-colors hover:z-10 hover:bg-gray-800 focus-visible:z-10 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-indigo-500"
      >
        +{singers.length}
      </button>
      <div
        ref={popoverRef}
        popover="auto"
        onToggle={(event) => setOpen(event.currentTarget.matches(':popover-open'))}
        className="fixed inset-auto z-[100] m-0 w-56 rounded-lg border border-gray-200 bg-white p-1 shadow-xl"
      >
        <div className="px-2 py-1.5 text-xs font-medium text-gray-400">その他のチャンネル</div>
        {singers.map((singer) => (
          <Link
            key={singer.id}
            to={`/singers/${singer.id}`}
            onClick={() => popoverRef.current?.hidePopover()}
            className="flex items-center gap-2 rounded-md px-2 py-2 text-sm text-gray-700 hover:bg-indigo-50 hover:text-indigo-700 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-indigo-500"
          >
            <span className="inline-flex h-8 w-8 shrink-0 overflow-hidden rounded-full bg-indigo-100 text-[10px] font-semibold text-indigo-700">
              <SingerImage singer={singer} />
            </span>
            <span className="min-w-0 truncate">{singer.name}</span>
          </Link>
        ))}
      </div>
    </>
  );
}

/** 重なったチャンネルアイコン。5人以上は「+N」にまとめ、押すと一覧を出す。 */
export default function SingerAvatars({ singers }: { singers: Singer[] }) {
  const uniqueSingers = singers.filter(
    (singer, index) => singers.findIndex((candidate) => candidate.id === singer.id) === index,
  );
  if (uniqueSingers.length === 0) return null;

  const visibleSingers = uniqueSingers.slice(0, 4);
  const remainingSingers = uniqueSingers.slice(4);

  return (
    <div className="flex shrink-0 -space-x-2 pl-2" aria-label="チャンネル">
      {visibleSingers.map((singer) => <SingerAvatar key={singer.id} singer={singer} />)}
      {remainingSingers.length > 0 && (
        <SingerOverflowMenu singers={remainingSingers} />
      )}
    </div>
  );
}
