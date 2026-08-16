import { useRef, useState } from 'react';
import { PlayerScopeContext } from './playerScope';
import RangeEditor from './RangeEditor';
import SetlistStrip from './SetlistStrip';
import LoginToSuggest from './LoginToSuggest';
import { useIsCompact } from './useMediaQuery';
import { usePerformanceReport, useVideoSlot } from './usePerformanceReport';
import { ArtistField, SongField, TimeCard, TimeField, Transport, VocalPicker } from './reportFields';

// 再生中に気づいた誤りを、聴きながら直すための画面。
//
// **区間の締め切りを外した状態で開く**（store の editing）。開始/終了がずれて
// いるとき正しい位置は今の区間の外にあるので、区間に閉じ込めたまま直させる
// ことはできない ── これが小さなポップオーバー（「開始はここ」「終了はここ」）
// を畳んでこの画面にした理由。あちらは押した瞬間の再生位置しか送れず、
// その再生位置が区間の外へ出られなかった。
//
// 動画はこの画面の中に置く。ただし iframe は再マウントすると再生が切れるので、
// ここに置くのはプレースホルダだけで、実物は PlayerBar が持つ fixed 要素を
// 測った位置へ動かして重ねる。
//
// 外殻は 2 つある（横並び / タブ切り替え）。**中身は usePerformanceReport と
// reportFields で共有する** ── 送信の振り分けはこの機能で一番間違えやすい
// ところなので、2 つ書かない。

export type VideoSlotRect = { top: number; left: number; width: number; height: number };

export default function PerformanceReportDialog({
  onVideoSlot,
}: {
  // 動画を重ねる矩形を親（PlayerBar）へ伝える
  onVideoSlot: (rect: VideoSlotRect | null) => void;
}) {
  const report = usePerformanceReport();
  const compact = useIsCompact();

  if (!report.editing) return null;

  return (
    // この画面の再生位置・試聴はすべてグローバル再生バーのもの
    <PlayerScopeContext value="bar">
      {compact ? (
        <CompactShell report={report} onVideoSlot={onVideoSlot} />
      ) : (
        <WideShell report={report} onVideoSlot={onVideoSlot} />
      )}
    </PlayerScopeContext>
  );
}

type Report = ReturnType<typeof usePerformanceReport>;

// ========== 共通の小物 ==========

function Header({
  report,
  onClose,
  swipeToClose = false,
}: {
  report: Report;
  onClose: () => void;
  swipeToClose?: boolean;
}) {
  // 下スワイプで閉じる（拡大表示と同じ手つき）。追従アニメはしない ──
  // 動画は別の fixed 要素なので、両方を同じ transform で動かす必要があり、
  // 閉じるだけの導線に見合わない（✕ もある）
  const touchStart = useRef<{ x: number; y: number } | null>(null);

  return (
    <div
      className="shrink-0 flex items-center gap-3 px-4 h-12 border-b border-gray-200 bg-white"
      onTouchStart={
        swipeToClose
          ? (e) => {
              touchStart.current = { x: e.touches[0].clientX, y: e.touches[0].clientY };
            }
          : undefined
      }
      onTouchEnd={
        swipeToClose
          ? (e) => {
              const s = touchStart.current;
              touchStart.current = null;
              if (!s) return;
              const dx = e.changedTouches[0].clientX - s.x;
              const dy = e.changedTouches[0].clientY - s.y;
              if (dy > 50 && dy > Math.abs(dx)) onClose();
            }
          : undefined
      }
    >
      <span className="text-sm font-medium shrink-0 whitespace-nowrap">
        {report.missingMode ? '抜けている曲を報告' : '歌唱を報告'}
      </span>
      <span className="text-xs text-gray-500 truncate min-w-0">
        {report.missingMode ? report.stream?.title : (report.target?.song_name ?? report.stream?.title)}
      </span>
      <button
        onClick={onClose}
        className="ml-auto p-2 -mr-2 text-gray-500 hover:text-gray-900 hover:bg-gray-100 rounded-lg"
        title="閉じる（Esc）"
        aria-label="閉じる"
      >
        <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
        </svg>
      </button>
    </div>
  );
}

// 送信内容の要約と送信ボタン。**何が送られるかを押す前に見せる**
// （時間・曲・アーティスト・歌った人は宛先が違うので、まとめ方も人に見せておく）
function SubmitBar({ report, compact = false }: { report: Report; compact?: boolean }) {
  const { changed, summary, missingMode, canEdit, canSubmit, busy, close, handleSubmit } = report;

  const hint = changed
    ? `送信する内容：${summary.join('、')}`
    : missingMode
      ? '曲名を入れると送れます（管理者が確認して追加します）'
      : canEdit
        ? '直すと即座に反映されます'
        : '管理者への報告として送られます';

  if (compact) {
    return (
      <div className="shrink-0 border-t border-gray-200 bg-white px-3 py-2">
        <p className="text-[11px] text-gray-500 line-clamp-2 mb-1.5">{hint}</p>
        <div className="flex gap-2">
          <button
            type="button"
            onClick={close}
            disabled={busy}
            className="px-4 py-2.5 text-sm border border-gray-300 rounded-lg disabled:opacity-50"
          >
            キャンセル
          </button>
          <button
            type="button"
            onClick={handleSubmit}
            disabled={busy || !changed || !canSubmit}
            className="flex-1 py-2.5 text-sm bg-indigo-600 text-white font-medium rounded-lg active:bg-indigo-800 disabled:opacity-40"
          >
            {busy ? '送信中...' : missingMode || !canEdit ? '報告を送信' : '保存'}
          </button>
        </div>
      </div>
    );
  }

  return (
    <div className="flex items-center gap-2">
      <span className="text-xs text-gray-500 min-w-0 truncate">{hint}</span>
      <button
        type="button"
        onClick={close}
        disabled={busy}
        className="ml-auto px-4 py-2 text-sm border border-gray-300 rounded-lg hover:bg-gray-50 disabled:opacity-50"
      >
        キャンセル
      </button>
      <button
        type="button"
        onClick={handleSubmit}
        disabled={busy || !changed || !canSubmit}
        className="px-4 py-2 text-sm bg-indigo-600 text-white font-medium rounded-lg hover:bg-indigo-700 disabled:opacity-40"
      >
        {busy ? '送信中...' : missingMode || !canEdit ? '報告を送信' : '保存'}
      </button>
    </div>
  );
}

function MetaFields({ report }: { report: Report }) {
  const { draft, patch, songChanged, artistChanged, missingMode, stream, target, canEdit } = report;
  return (
    <>
      <SongField draft={draft} patch={patch} changed={songChanged} missingMode={missingMode} />
      <ArtistField draft={draft} patch={patch} changed={artistChanged} />
      <VocalPicker
        selected={draft.singerIds}
        participants={stream?.participants ?? []}
        channelOwner={stream?.channel_owner}
        current={target?.singers ?? []}
        canCreate={canEdit}
        onToggle={(id) =>
          patch({
            singerIds: draft.singerIds.includes(id)
              ? draft.singerIds.filter((x) => x !== id)
              : [...draft.singerIds, id],
          })
        }
      />
    </>
  );
}

function NoteField({ report }: { report: Report }) {
  const { canSubmit, canEdit, note, setNote } = report;
  if (!canSubmit) {
    return (
      <div className="rounded-lg bg-white border border-gray-200 p-3 shadow-sm">
        <LoginToSuggest message="誤りの報告にはログインが必要です。" />
      </div>
    );
  }
  return (
    <input
      type="text"
      value={note}
      onChange={(e) => setNote(e.target.value)}
      placeholder={canEdit ? 'メモ（任意）' : '報告の理由（任意）'}
      className="w-full px-3 py-2 text-sm bg-white border border-gray-300 rounded-lg text-gray-900 placeholder:text-gray-400 focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
    />
  );
}

// ========== 横並び（デスクトップ） ==========

function WideShell({ report, onVideoSlot }: { report: Report; onVideoSlot: (r: VideoSlotRect | null) => void }) {
  const {
    draft,
    patch,
    target,
    missingMode,
    currentTime,
    duration,
    neighbours,
    performances,
    performanceId,
    error,
    close,
    selectTarget,
    openMissing,
  } = report;
  const slotRef = useVideoSlot(onVideoSlot, true);

  return (
    <div className="fixed inset-0 z-[65] bg-white text-gray-900 flex flex-col pb-[env(safe-area-inset-bottom)]">
      <Header report={report} onClose={close} />

      <div className="flex-1 min-h-0 flex">
        <div className="flex-1 min-w-0 flex flex-col min-h-0">
          {/* 動画とトランスポートはスクロールさせない。**動画は fixed 要素を
              この枠へ重ねているだけ**なので、枠がスクロールで動くと追従の
              ために毎フレーム測り直すことになる（ずれる/重い） */}
          <div className="shrink-0 p-3 pb-2">
            <div
              ref={slotRef}
              className="w-full aspect-video max-h-[44vh] mx-auto bg-black rounded-lg"
              style={{ maxWidth: 'calc(44vh * 1.7778)' }}
            />
            <Transport currentTime={currentTime} />
          </div>

          {/* ここから下だけスクロールする */}
          <div className="flex-1 min-h-0 overflow-y-auto overflow-x-hidden px-3 pb-3 space-y-3">
            {target || missingMode ? (
              <div className="rounded-lg bg-white border border-gray-200 p-3 shadow-sm">
                <RangeEditor
                  start={draft.start}
                  end={draft.end}
                  duration={duration}
                  neighbours={neighbours}
                  onChange={patch}
                />
                <div className="mt-3 grid grid-cols-2 gap-3">
                  <TimeField
                    label="開始"
                    value={draft.start}
                    original={target?.start_seconds ?? draft.start}
                    currentTime={currentTime}
                    onChange={(v) => patch({ start: v })}
                  />
                  <TimeField
                    label="終了"
                    value={draft.end}
                    original={target?.end_seconds ?? 0}
                    currentTime={currentTime}
                    allowEmpty
                    onChange={(v) => patch({ end: v })}
                  />
                </div>
              </div>
            ) : (
              <p className="text-sm text-gray-500">対象の歌唱が見つかりません</p>
            )}

            {(target || missingMode) && (
              <div className="rounded-lg bg-white border border-gray-200 p-3 space-y-3 shadow-sm">
                <MetaFields report={report} />
              </div>
            )}

            <NoteField report={report} />
            {error && <p className="text-xs text-red-600">{error}</p>}
            <SubmitBar report={report} />
          </div>
        </div>

        {/* 右：この配信の曲（対象の切り替え） */}
        <div className="shrink-0 w-80 border-l border-gray-200 bg-white flex flex-col">
          <SetlistStrip
            performances={performances}
            currentId={performanceId}
            onSelect={selectTarget}
            onAddMissing={openMissing}
          />
        </div>
      </div>
    </div>
  );
}

// ========== タブ切り替え（スマホ） ==========

// スマホは縦の予算が足りない（ヘッダー＋動画＋操作でほぼ半分）。
// 全部を縦に積むと送信ボタンが画面外へ落ちるので、**一度に 1 面だけ出す**。
type Pane = 'time' | 'meta' | 'setlist';

function CompactShell({ report, onVideoSlot }: { report: Report; onVideoSlot: (r: VideoSlotRect | null) => void }) {
  const {
    draft,
    patch,
    target,
    missingMode,
    currentTime,
    duration,
    neighbours,
    performances,
    performanceId,
    error,
    close,
    selectTarget,
    openMissing,
  } = report;
  const [pane, setPane] = useState<Pane>('time');
  // 時間軸のタップがどちらのハンドルを動かすか。時刻カードの選択と兼ねる
  const [handle, setHandle] = useState<'start' | 'end'>('start');

  // **動画は「時間」タブにだけ出す。** 検索欄にキーボードが出ると測った矩形と
  // 実際のレイアウトがずれるうえ、候補を出す場所も無くなる。退避しても
  // 止めてはいないので音は鳴り続ける
  const slotRef = useVideoSlot(onVideoSlot, pane === 'time');

  const tabs: { key: Pane; label: string }[] = [
    { key: 'time', label: '時間' },
    { key: 'meta', label: '曲・歌手' },
    { key: 'setlist', label: `対象 ${performances.length}` },
  ];

  return (
    <div className="fixed inset-0 z-[65] bg-white text-gray-900 flex flex-col pb-[env(safe-area-inset-bottom)]">
      <Header report={report} onClose={close} swipeToClose />

      {pane === 'time' && (
        <div className="shrink-0 px-3 pt-2">
          <div ref={slotRef} className="w-full aspect-video max-h-[30vh] mx-auto bg-black rounded-lg" />
          <Transport currentTime={currentTime} compact />
        </div>
      )}

      {/* タブ */}
      <div className="shrink-0 mt-2 px-3">
        <div className="flex rounded-lg bg-gray-100 p-0.5">
          {tabs.map((t) => (
            <button
              key={t.key}
              type="button"
              onClick={() => setPane(t.key)}
              aria-pressed={pane === t.key}
              className={`flex-1 py-2 text-sm rounded-md transition-colors ${
                pane === t.key ? 'bg-white text-indigo-700 font-medium shadow-sm' : 'text-gray-600'
              }`}
            >
              {t.label}
            </button>
          ))}
        </div>
      </div>

      {/* 中身：1 面だけ */}
      <div className="flex-1 min-h-0 overflow-y-auto overflow-x-hidden px-3 py-3 space-y-3">
        {pane === 'time' &&
          (target || missingMode ? (
            <>
              <div className="grid grid-cols-2 gap-2">
                <TimeCard
                  label="開始"
                  value={draft.start}
                  original={target?.start_seconds ?? draft.start}
                  currentTime={currentTime}
                  active={handle === 'start'}
                  onActivate={() => setHandle('start')}
                  onChange={(v) => patch({ start: v })}
                />
                <TimeCard
                  label="終了"
                  value={draft.end}
                  original={target?.end_seconds ?? 0}
                  currentTime={currentTime}
                  active={handle === 'end'}
                  allowEmpty
                  onActivate={() => setHandle('end')}
                  onChange={(v) => patch({ end: v })}
                />
              </div>

              <div className="rounded-lg bg-white border border-gray-200 p-3 shadow-sm">
                <RangeEditor
                  start={draft.start}
                  end={draft.end}
                  duration={duration}
                  neighbours={neighbours}
                  tapTarget={handle}
                  onChange={patch}
                />
                <p className="mt-1 text-[11px] text-gray-500">
                  タップで{handle === 'start' ? '開始' : '終了'}をその位置にします。
                  区間の外へも出られます
                </p>
              </div>
            </>
          ) : (
            <p className="text-sm text-gray-500">対象の歌唱が見つかりません</p>
          ))}

        {pane === 'meta' && (target || missingMode) && (
          <div className="space-y-3">
            <MetaFields report={report} />
            <NoteField report={report} />
          </div>
        )}

        {pane === 'setlist' && (
          <SetlistStrip
            performances={performances}
            currentId={performanceId}
            onSelect={(id) => {
              selectTarget(id);
              setPane('time'); // 選んだらそのまま直しにいける
            }}
            onAddMissing={() => {
              openMissing();
              setPane('time');
            }}
          />
        )}

        {error && <p className="text-xs text-red-600">{error}</p>}
      </div>

      <SubmitBar report={report} compact />
    </div>
  );
}
