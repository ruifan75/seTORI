import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { manualImportApi } from '../api/client';
import type { InfoJsonImportResult, LiveChatImportResult } from '../api/types';

// 会限（メンバー限定）配信の入力源を、編集者が手元から持ち込むための画面。
//
// **本番からは取れない。** コメントは YouTube Data API が `commentThreads/forbidden`
// で 403 を返し（API キー方式なので cookie を足しても変わらない）、live chat は
// 視聴資格が要る。Holodex にも曲は無い。一方、メンバー資格のある編集者が手元で
// yt-dlp を回せば両方取れるので、その出力を運んでもらう。
//
// cookie の役割を取り違えないこと：本番の cookie はデータセンター IP の BOT 判定を
// 抜けるためのもので、視聴資格を与えるものではない（実測：cookie 無しでも
// availability=subscriber_only は取れるが、replay の中身は取れない）。

// yt-dlp が cookie を読める主なブラウザ。**Firefox を既定にする** ──
// Chromium 系は OS によっては「ブラウザを終了していないと読めない」ので、
// 開いたまま実行できるほうを既定に置く。
const BROWSERS = ['firefox', 'chrome', 'edge', 'brave', 'safari'] as const;

function buildCommand(videoId: string, browser: string): string {
  // **1 行にする。** 行継続（\）で折るとシェルによっては貼り付けで壊れる。
  //
  // 各フラグの理由：
  //   --skip-download          映像は要らない（欲しいのは info.json と live_chat.json）
  //   --write-info-json        コメントを書き出す器。--write-comments だけでは保存されない
  //   --write-comments         コメント本文を info.json に入れる
  //   --write-subs --sub-langs live_chat   live chat replay を落とす
  //   --ignore-no-formats-error  フォーマットが 0 件でも字幕を書く前に止まらない
  //                              （本番のバックエンドが付けているのと同じ理由）
  //   -o "%(id)s.%(ext)s"      どの配信のファイルか分かる名前にする
  return [
    'yt-dlp',
    `--cookies-from-browser ${browser}`,
    '--skip-download',
    '--write-info-json --write-comments',
    '--write-subs --sub-langs live_chat',
    '--ignore-no-formats-error',
    '-o "%(id)s.%(ext)s"',
    `"https://www.youtube.com/watch?v=${videoId}"`,
  ].join(' ');
}

function formatSeconds(total: number): string {
  const s = Math.max(0, Math.round(total));
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const sec = s % 60;
  const mm = String(m).padStart(2, '0');
  const ss = String(sec).padStart(2, '0');
  return h > 0 ? `${h}:${mm}:${ss}` : `${m}:${ss}`;
}

function formatBytes(n: number): string {
  if (n >= 1 << 20) return `${(n / (1 << 20)).toFixed(1)} MB`;
  if (n >= 1 << 10) return `${Math.round(n / (1 << 10))} KB`;
  return `${n} B`;
}

export default function ManualInputImport({
  videoId,
  durationSeconds,
}: {
  videoId: string;
  durationSeconds?: number;
}) {
  const queryClient = useQueryClient();
  const [browser, setBrowser] = useState<string>('firefox');
  const [copied, setCopied] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [infoResult, setInfoResult] = useState<InfoJsonImportResult | null>(null);
  const [chatResult, setChatResult] = useState<LiveChatImportResult | null>(null);

  const command = buildCommand(videoId, browser);

  // **キーに権限を混ぜていない。** この画面は編集モードの中にしか無く、
  // 権限を失うと親ごと外れるので、ログアウト後にキャッシュが表示に残る経路が無い
  // （`['stream', id, canEdit]` のように混ぜているのは、同じ画面が権限の有無で
  // 違う応答を出すため）。
  const cached = useQuery({
    queryKey: ['import-live-chat', videoId],
    queryFn: () => manualImportApi.getLiveChat(videoId),
  });

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(command);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      // クリップボードが使えない環境（http 経由など）では手で選んでもらう。
      setError('コピーできませんでした。下のコマンドを選択してコピーしてください。');
    }
  };

  const infoMutation = useMutation({
    mutationFn: (file: File) => manualImportApi.importInfoJson(videoId, file),
    onSuccess: (res) => {
      setInfoResult(res);
      setError(null);
      // comment_raw が変わると comment_songs と hash は NULL に戻るので、
      // 配信の情報も引き直す（`has_comment_raw` が変わる）。
      // 前方一致なので ['stream', id, canEdit] も拾う。
      queryClient.invalidateQueries({ queryKey: ['stream', videoId] });
    },
    onError: (e: unknown) => setError(errorMessage(e)),
  });

  const chatMutation = useMutation({
    mutationFn: (file: File) => manualImportApi.importLiveChat(videoId, file),
    onSuccess: (res) => {
      setChatResult(res);
      setError(null);
      queryClient.invalidateQueries({ queryKey: ['import-live-chat', videoId] });
    },
    onError: (e: unknown) => setError(errorMessage(e)),
  });

  const deleteMutation = useMutation({
    mutationFn: () => manualImportApi.deleteLiveChat(videoId),
    onSuccess: () => {
      setChatResult(null);
      queryClient.invalidateQueries({ queryKey: ['import-live-chat', videoId] });
    },
    onError: (e: unknown) => setError(errorMessage(e)),
  });

  const present = cached.data?.present ?? false;
  const chat = chatResult ?? (present ? cached.data?.chat : undefined);
  // 記録が 0 件で置いてある＝読めないファイルが居座っている。
  // 本文が 1 件も無いファイルは、拍手も取れないので置いておく意味が無い
  // （検証を通ったということは replay として読めてはいる）。
  const cachedUnusable = present && !chatResult && (cached.data?.chat.messages ?? 0) === 0;

  return (
    <div className="space-y-4 text-sm">
      <div className="bg-amber-50 border border-amber-200 rounded-lg p-3 text-amber-900">
        <p className="font-medium">会限配信はサーバーから入力源を取れません。</p>
        <p className="mt-1 text-amber-800 leading-relaxed">
          コメントは YouTube API が 403 を返し（API キー方式なので cookie では変わりません）、
          live chat は視聴資格が要ります。<strong>メンバー資格のあるアカウントの端末</strong>で
          下のコマンドを実行し、できた 2 つのファイルをここへ入れてください。
        </p>
      </div>

      {/* ① コマンド */}
      <div>
        <div className="flex items-center justify-between mb-1">
          <span className="font-medium text-gray-700">① 手元で実行する</span>
          <div className="flex items-center gap-2">
            <select
              value={browser}
              onChange={(e) => setBrowser(e.target.value)}
              className="text-xs border border-gray-300 rounded px-1.5 py-1"
              title="cookie を読み出すブラウザ"
            >
              {BROWSERS.map((b) => (
                <option key={b} value={b}>{b}</option>
              ))}
            </select>
            <button
              onClick={copy}
              className="px-2 py-1 text-xs bg-indigo-600 text-white rounded hover:bg-indigo-700 transition-colors"
              title="コマンドをクリップボードへコピー"
            >
              {copied ? 'コピーしました' : 'コピー'}
            </button>
          </div>
        </div>
        <pre className="bg-gray-900 text-gray-100 text-xs rounded-lg p-3 overflow-x-auto whitespace-pre">
          {command}
        </pre>
        <p className="mt-1 text-xs text-gray-500 leading-relaxed">
          そのブラウザで YouTube にログインしている必要があります。Chromium 系は OS によって
          ブラウザを終了しないと cookie を読めないため、既定は firefox にしてあります。
        </p>
      </div>

      {/* ② 取り込み */}
      <div className="space-y-3">
        <span className="font-medium text-gray-700">② できたファイルを入れる</span>

        <UploadRow
          label="コメント"
          hint={`${videoId}.info.json`}
          accept=".json,application/json"
          pending={infoMutation.isPending}
          onPick={(f) => infoMutation.mutate(f)}
        />
        {infoResult && (
          <p className="text-xs text-green-700 bg-green-50 border border-green-200 rounded px-2 py-1.5">
            コメント {infoResult.saved} 件を取り込みました
            （うち時刻表記のあるもの {infoResult.with_times} 件）。
            {infoResult.with_times === 0 && ' 歌単らしき行は見つかりませんでした。'}
            <span className="block text-green-800/70 mt-0.5">
              「コメント」タブから読み込むと分析されます。
            </span>
          </p>
        )}

        <UploadRow
          label="live chat"
          hint={`${videoId}.live_chat.json`}
          accept=".json,application/json"
          pending={chatMutation.isPending}
          onPick={(f) => chatMutation.mutate(f)}
        />

        {chat && !cachedUnusable && (
          <div className="text-xs bg-green-50 border border-green-200 rounded px-2 py-1.5 text-green-800">
            <p>
              本文 {chat.messages.toLocaleString()} 件・拍手 {chat.applause.toLocaleString()} 件・
              {formatSeconds(chat.first_at_sec)} 〜 {formatSeconds(chat.last_at_sec)}
              （{formatBytes(chat.bytes)}）
            </p>
            {/* **ファイルに動画 ID が入っていないので、取り違えは機械では弾けない。**
                配信の長さと突き合わせて人が判断できるようにする。 */}
            {durationSeconds ? (
              <p className="mt-0.5 text-green-800/70">
                この配信の長さは {formatSeconds(durationSeconds)} です。
                大きく違う場合は別の配信のファイルかもしれません。
              </p>
            ) : null}
            {/* **消せるのはファイルだけ。** 拍手 end 検出を走らせたあとなら
                終了時間は既に入っており、しかも「end が無い曲だけ採用」なので
                入れ直して再検出しても上書きされない。そう書く。 */}
            <button
              onClick={() => deleteMutation.mutate()}
              disabled={deleteMutation.isPending}
              className="mt-1.5 text-red-700 hover:text-red-800 underline disabled:opacity-50"
              title="置いてある live chat を消す（別の配信のものを入れてしまったとき）"
            >
              取り込んだ live chat を消す
            </button>
            <p className="mt-0.5 text-green-800/70">
              消せるのはファイルだけです。すでに拍手 end を反映したあとなら、
              入った終了時間は戻りません（入れ直して再検出しても、
              終了時間のある曲は上書きされません）。編集画面で直してください。
            </p>
          </div>
        )}
        {cachedUnusable && (
          <p className="text-xs bg-red-50 border border-red-200 rounded px-2 py-1.5 text-red-800">
            置いてある live chat が読めません。取り直して入れ直してください。
            <button
              onClick={() => deleteMutation.mutate()}
              disabled={deleteMutation.isPending}
              className="ml-2 underline disabled:opacity-50"
            >
              消す
            </button>
          </p>
        )}
      </div>

      {error && (
        <p className="text-xs bg-red-50 border border-red-200 rounded px-2 py-1.5 text-red-800">
          {error}
        </p>
      )}
    </div>
  );
}

function UploadRow({
  label,
  hint,
  accept,
  pending,
  onPick,
}: {
  label: string;
  hint: string;
  accept: string;
  pending: boolean;
  onPick: (file: File) => void;
}) {
  return (
    <label className="flex items-center gap-2">
      <span className="w-20 shrink-0 text-gray-600">{label}</span>
      <input
        type="file"
        accept={accept}
        disabled={pending}
        onChange={(e) => {
          const f = e.target.files?.[0];
          // **同じファイルをもう一度選べるようにする。** value を空にしないと
          // change が発火せず、取り直したファイルを入れ直せない。
          e.target.value = '';
          if (f) onPick(f);
        }}
        className="text-xs file:mr-2 file:px-2 file:py-1 file:rounded file:border-0 file:bg-gray-100 file:text-gray-700 hover:file:bg-gray-200 disabled:opacity-50"
      />
      <span className="text-xs text-gray-400 truncate">{pending ? '取り込み中...' : hint}</span>
    </label>
  );
}

function errorMessage(e: unknown): string {
  const res = (e as { response?: { data?: { error?: string } } })?.response;
  return res?.data?.error ?? '取り込みに失敗しました';
}
