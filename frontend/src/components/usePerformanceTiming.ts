import { useQueryClient } from '@tanstack/react-query';
import { performanceApi, suggestionApi } from '../api/client';
import { useAuthStore, hasPermission, PERM } from '../store/auth';
import { usePlayerStore } from '../store/player';
import { useToast } from './ui/ToastContext';

// 歌唱の開始/終了時間を「直す」か「提案する」かを1か所にまとめたフック。
// EditableField と同じ方針：編集権限があれば即時保存、無ければ管理者への修正提案になる。
//
// 呼び出し側（プレイヤーの通報パネル・曲詳細の歌唱一覧）は、どちらの経路かを
// 意識せずに submit を呼べばよい。

export interface TimingTarget {
  performanceId: string;
  songName: string;
  start: number;
  end: number; // 0 = 動画の最後まで
}

export interface TimingChange {
  start?: number;
  end?: number;
}

// 秒 → M:SS / H:MM:SS（表示・トースト用）
export function formatSeconds(sec: number): string {
  const s = Math.max(0, Math.round(sec));
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const r = s % 60;
  return h > 0
    ? `${h}:${String(m).padStart(2, '0')}:${String(r).padStart(2, '0')}`
    : `${m}:${String(r).padStart(2, '0')}`;
}

// "1:23:45" / "2:30" / "150" のいずれも秒に変換する。不正なら null。
export function parseSeconds(input: string): number | null {
  const text = input.trim();
  if (!text) return null;
  const parts = text.split(':');
  if (parts.length > 3) return null;
  let total = 0;
  for (const part of parts) {
    if (!/^\d+$/.test(part.trim())) return null;
    total = total * 60 + Number(part);
  }
  return total;
}

export function usePerformanceTiming() {
  const user = useAuthStore((s) => s.user);
  const canEdit = hasPermission(user, PERM.CONTENT_EDIT);
  // 提案の投稿はログイン必須（誰の指摘かを追えないと信頼度も濫用対策も成り立たないため）。
  // 未ログインでも導線自体は見せ、押した時点でログインへ促す。
  const canSubmit = user !== null;
  const { showToast } = useToast();
  const queryClient = useQueryClient();
  const updateTrackTiming = usePlayerStore((s) => s.updateTrackTiming);

  const invalidate = () => {
    queryClient.invalidateQueries({ queryKey: ['song'] });
    queryClient.invalidateQueries({ queryKey: ['songs'] });
    queryClient.invalidateQueries({ queryKey: ['stream'] });
    queryClient.invalidateQueries({ queryKey: ['streams'] });
    queryClient.invalidateQueries({ queryKey: ['performances'] });
    queryClient.invalidateQueries({ queryKey: ['singer'] });
    queryClient.invalidateQueries({ queryKey: ['suggestions'] });
  };

  // 変更が妥当かを送信前に判定する。end = 0 は「動画の最後まで」なので範囲チェックから外す。
  const validate = (target: TimingTarget, change: TimingChange): string | null => {
    const start = change.start ?? target.start;
    const end = change.end ?? target.end;
    if (start < 0 || end < 0) return '時間が不正です';
    if (end !== 0 && end <= start) {
      return `終了（${formatSeconds(end)}）は開始（${formatSeconds(start)}）より後にしてください`;
    }
    if (change.start === target.start && change.end === target.end) return '変更がありません';
    return null;
  };

  // 直した内容を適用する（権限があれば即時保存、無ければ提案）。
  // 成功したら true。失敗・却下時は false（呼び出し側はパネルを閉じないなどの判断に使う）。
  const submit = async (
    target: TimingTarget,
    change: TimingChange,
    note = ''
  ): Promise<boolean> => {
    if (!canSubmit) {
      // 401 を踏ませてセッション失効扱いにしないよう、送る前に止める
      showToast('修正の提案にはログインが必要です', 'info');
      return false;
    }
    const problem = validate(target, change);
    if (problem) {
      showToast(problem, 'error');
      return false;
    }

    // TimingChange（start/end）→ API の形（start_seconds/end_seconds）
    const toRequest = (c: TimingChange) => ({
      ...(c.start !== undefined ? { start_seconds: c.start } : {}),
      ...(c.end !== undefined ? { end_seconds: c.end } : {}),
    });

    try {
      if (canEdit) {
        await performanceApi.update(target.performanceId, toRequest(change));
        // 再生中のキューにも反映しないと、古い end のまま次の曲へ送られてしまう
        updateTrackTiming(target.performanceId, change);
        invalidate();

        // 直前の値へ戻せるようにする（誤タップの取り消し）
        const undo = async () => {
          const revert: TimingChange = {};
          if (change.start !== undefined) revert.start = target.start;
          if (change.end !== undefined) revert.end = target.end;
          try {
            await performanceApi.update(target.performanceId, toRequest(revert));
            updateTrackTiming(target.performanceId, revert);
            invalidate();
            showToast('元に戻しました', 'info');
          } catch (e) {
            showToast(`元に戻せませんでした: ${(e as Error).message}`, 'error');
          }
        };
        showToast(describeChange(target, change), 'success', { label: '元に戻す', onClick: undo });
      } else {
        const fields: Record<string, string> = {};
        if (change.start !== undefined) fields.start_seconds = String(change.start);
        if (change.end !== undefined) fields.end_seconds = String(change.end);
        await suggestionApi.create({
          target_type: 'performance',
          target_id: target.performanceId,
          fields,
          note,
        });
        showToast('修正を提案しました。管理者の確認をお待ちください', 'success');
      }
      return true;
    } catch (e) {
      showToast(`送信できませんでした: ${(e as Error).message}`, 'error');
      return false;
    }
  };

  return { canEdit, canSubmit, submit };
}

function describeChange(target: TimingTarget, change: TimingChange): string {
  const parts: string[] = [];
  if (change.start !== undefined) {
    parts.push(`開始 ${formatSeconds(target.start)} → ${formatSeconds(change.start)}`);
  }
  if (change.end !== undefined) {
    parts.push(
      `終了 ${target.end === 0 ? '最後まで' : formatSeconds(target.end)} → ${
        change.end === 0 ? '最後まで' : formatSeconds(change.end)
      }`
    );
  }
  return `${target.songName}：${parts.join('、')}`;
}
