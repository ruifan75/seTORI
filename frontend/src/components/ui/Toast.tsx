import { useState, useCallback, useRef, type ReactNode } from 'react';
import { ToastContext, type ToastMessage } from './ToastContext';

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<ToastMessage[]>([]);
  const toastIdCounter = useRef(0);

  const showToast = useCallback((message: string, type: ToastMessage['type'] = 'info', action?: ToastMessage['action']) => {
    const id = Date.now() + toastIdCounter.current++;
    setToasts((prev) => [...prev, { id, message, type, action }]);

    // 自動移除
    setTimeout(() => {
      setToasts((prev) => prev.filter((t) => t.id !== id));
    }, 5000);
  }, []);

  const removeToast = useCallback((id: number) => {
    setToasts((prev) => prev.filter((t) => t.id !== id));
  }, []);

  return (
    <ToastContext.Provider value={{ showToast, removeToast }}>
      {children}

      {/* Toast Container */}
      <div className="fixed bottom-4 right-4 z-50 flex flex-col gap-2">
        {toasts.map((toast) => (
          <div
            key={toast.id}
            className={`
              px-4 py-3 rounded-lg shadow-lg flex items-center gap-3 min-w-[300px] max-w-[400px]
              animate-slide-in
              ${toast.type === 'error' ? 'bg-red-600 text-white' : ''}
              ${toast.type === 'success' ? 'bg-green-600 text-white' : ''}
              ${toast.type === 'info' ? 'bg-gray-800 text-white' : ''}
            `}
          >
            {/* Icon */}
            <span className="text-lg flex-shrink-0">
              {toast.type === 'error' && '⚠'}
              {toast.type === 'success' && '✓'}
              {toast.type === 'info' && 'ℹ'}
            </span>

            {/* Message */}
            <p className="flex-1 text-sm">{toast.message}</p>

            {/* Action（「元に戻す」など）。押したらトーストは閉じる */}
            {toast.action && (
              <button
                onClick={() => {
                  toast.action?.onClick();
                  removeToast(toast.id);
                }}
                className="shrink-0 text-sm font-medium underline underline-offset-2 text-white/90 hover:text-white"
              >
                {toast.action.label}
              </button>
            )}

            {/* Close button */}
            <button
              onClick={() => removeToast(toast.id)}
              className="text-white/70 hover:text-white"
            >
              ✕
            </button>
          </div>
        ))}
      </div>
    </ToastContext.Provider>
  );
}
