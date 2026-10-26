import { useEffect, useId, useLayoutEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import QueueAddButton from './QueueAddButton';
import ReportButton from './ReportButton';
import { menuPositionFor } from './menuPosition';
import type { PerformanceListItem } from '../utils/performanceList';

export interface PerformanceListAction {
  label: string;
  onClick: () => void;
  disabled?: boolean;
  destructive?: boolean;
}

const actionClass = 'flex min-h-11 w-full items-center gap-2 rounded-lg px-3 py-2 text-left text-sm hover:bg-gray-50 focus-visible:outline focus-visible:outline-2 focus-visible:outline-indigo-500 disabled:text-gray-300';

export default function PerformanceActions({ item, actions = [] }: {
  item: PerformanceListItem;
  actions?: PerformanceListAction[];
}) {
  const [position, setPosition] = useState<{ top: number; left: number } | null>(null);
  const anchor = useRef<HTMLButtonElement>(null);
  const menu = useRef<HTMLDivElement>(null);
  const menuId = useId();
  const open = position !== null;

  useLayoutEffect(() => {
    if (!open || !menu.current) return;
    const height = menu.current.getBoundingClientRect().height;
    menu.current.style.top = `${Math.max(8, Math.min(position.top, window.innerHeight - height - 8))}px`;
    menu.current.querySelector<HTMLElement>('button:not(:disabled), a')?.focus();
  }, [open, position]);

  useEffect(() => {
    if (!open) return;
    const dismiss = (event: Event) => {
      if (menu.current?.contains(event.target as Node) || anchor.current?.contains(event.target as Node)) return;
      setPosition(null);
    };
    const keydown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.preventDefault();
        event.stopPropagation();
        setPosition(null);
        anchor.current?.focus();
      }
    };
    const resize = () => setPosition(null);
    document.addEventListener('pointerdown', dismiss);
    document.addEventListener('focusin', dismiss);
    document.addEventListener('keydown', keydown);
    window.addEventListener('scroll', dismiss, true);
    window.addEventListener('resize', resize);
    return () => {
      document.removeEventListener('pointerdown', dismiss);
      document.removeEventListener('focusin', dismiss);
      document.removeEventListener('keydown', keydown);
      window.removeEventListener('scroll', dismiss, true);
      window.removeEventListener('resize', resize);
    };
  }, [open]);

  const close = () => {
    setPosition(null);
    anchor.current?.focus();
  };

  return (
    <div className="flex shrink-0 items-center">
      <QueueAddButton track={item.track} size="md" />
      <button
        ref={anchor}
        type="button"
        aria-label={`${item.track.songName} のその他の操作`}
        aria-expanded={open}
        aria-controls={open ? menuId : undefined}
        onClick={() => setPosition(open ? null : menuPositionFor(anchor.current, 256))}
        className="inline-flex h-10 w-10 items-center justify-center rounded-full text-gray-500 hover:bg-indigo-50 hover:text-indigo-600 focus-visible:outline focus-visible:outline-2 focus-visible:outline-indigo-500"
      >
        <svg className="h-5 w-5" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
          <circle cx="5" cy="12" r="2" /><circle cx="12" cy="12" r="2" /><circle cx="19" cy="12" r="2" />
        </svg>
      </button>
      {position && createPortal(
        <div
          id={menuId}
          ref={menu}
          role="group"
          aria-label={`${item.track.songName} の操作`}
          style={{ position: 'fixed', ...position, width: 256, maxHeight: 'calc(100dvh - 16px)' }}
          className="z-50 overflow-y-auto rounded-xl border border-gray-200 bg-white p-1.5 shadow-xl"
        >
          <ReportButton track={item.track} showLabel onActivate={close} className={`${actionClass} text-gray-700`} />
          <a href={item.youtubeUrl} target="_blank" rel="noopener noreferrer" onClick={close} className={`${actionClass} text-gray-700`}>
            YouTubeで開く <span aria-hidden="true">↗</span>
          </a>
          {actions.length > 0 && <div className="my-1 border-t border-gray-100" />}
          {actions.map((action) => (
            <button
              key={action.label}
              type="button"
              disabled={action.disabled}
              onClick={() => { close(); action.onClick(); }}
              className={`${actionClass} ${action.destructive ? 'text-red-600' : 'text-gray-700'}`}
            >
              {action.label}
            </button>
          ))}
        </div>, document.body,
      )}
    </div>
  );
}
