import { useState } from 'react';

// 「可修改的地方」に置くインライン編集フィールド。
// 表示中はホバーで鉛筆アイコンが現れ、クリックでその場編集。
// - 編集権限あり（canEdit）: onSave で即時保存
// - 閲覧のみ: onSuggest で管理者への修正提案として送信
// テキストボタンではなくアイコンで編集導線を示すための共通コンポーネント。
interface EditableFieldProps {
  label: string;
  value: string;
  canEdit: boolean;
  onSave: (val: string) => Promise<unknown> | void;
  onSuggest: (val: string, note: string) => Promise<unknown> | void;
  as?: React.ElementType;
  className?: string;
  placeholder?: string;
  emptyText?: string;
  editHint?: string;
  required?: boolean;
  display?: React.ReactNode;
}

function PencilIcon() {
  return (
    <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
      <path
        strokeLinecap="round"
        strokeLinejoin="round"
        strokeWidth={2}
        d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z"
      />
    </svg>
  );
}

export default function EditableField({
  label,
  value,
  canEdit,
  onSave,
  onSuggest,
  as: Wrapper = 'span',
  className = '',
  placeholder,
  emptyText = '未設定',
  editHint,
  required = false,
  display,
}: EditableFieldProps) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(value);
  const [note, setNote] = useState('');
  const [busy, setBusy] = useState(false);

  const start = () => {
    setDraft(value);
    setNote('');
    setEditing(true);
  };

  const cancel = () => {
    setEditing(false);
    setNote('');
  };

  const changed = draft.trim() !== value.trim();
  const invalid = required && draft.trim() === '';

  const submit = async () => {
    if (!changed || invalid) return;
    setBusy(true);
    try {
      if (canEdit) await onSave(draft.trim());
      else await onSuggest(draft.trim(), note.trim());
      setEditing(false);
      setNote('');
    } finally {
      setBusy(false);
    }
  };

  if (editing) {
    return (
      <div className="space-y-1.5 max-w-md">
        <input
          type="text"
          value={draft}
          autoFocus
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') submit();
            if (e.key === 'Escape') cancel();
          }}
          placeholder={placeholder}
          className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
        />
        {!canEdit && (
          <input
            type="text"
            value={note}
            onChange={(e) => setNote(e.target.value)}
            placeholder="提案の理由（任意）"
            className="w-full px-3 py-1.5 text-sm border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
          />
        )}
        {editHint && <p className="text-xs text-gray-400">{editHint}</p>}
        <div className="flex items-center gap-2 pt-0.5">
          <button
            type="button"
            onClick={submit}
            disabled={busy || !changed || invalid}
            className="px-3 py-1.5 text-sm bg-indigo-600 text-white font-medium rounded-lg hover:bg-indigo-700 disabled:opacity-50"
          >
            {busy ? '送信中...' : canEdit ? '保存' : '提案を送信'}
          </button>
          <button
            type="button"
            onClick={cancel}
            disabled={busy}
            className="px-3 py-1.5 text-sm bg-white border border-gray-300 text-gray-700 rounded-lg hover:bg-gray-50"
          >
            キャンセル
          </button>
        </div>
      </div>
    );
  }

  return (
    <Wrapper className={`group flex w-fit max-w-full items-center gap-1.5 ${className}`}>
      {value ? <span>{display ?? value}</span> : <span className="text-gray-300">{emptyText}</span>}
      <button
        type="button"
        onClick={start}
        title={canEdit ? `${label}を編集` : `${label}の修正を提案`}
        aria-label={canEdit ? `${label}を編集` : `${label}の修正を提案`}
        className="opacity-0 group-hover:opacity-100 focus:opacity-100 transition-opacity text-gray-400 hover:text-indigo-600 shrink-0"
      >
        <PencilIcon />
      </button>
    </Wrapper>
  );
}
