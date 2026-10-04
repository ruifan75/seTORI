import type { ArtistAliasProposal, FieldChange, EndSource } from '../../api/types';

// 編集可能な配信情報
export interface EditableSong {
  id: string;
  name: string;
  nameReading: string;
  artist: string;
  artistReading: string;
  start: number;
  end: number;
  tags: string[];
  singerIds: string[];
  // 追加フィールド
  matchedSongId: string | null; // マッチした楽曲ID、null は新規作成を示す
  artUrl: string | null; // アートワークURL
  itunesId: number | null; // Holodex が提供する iTunes ID
  // itunesId が既に DB（song_itunes）に紐付いているか。false は保存時に新規紐付けが作られることを示す
  // （primary な iTunes ID は Holodex へのアップロードにも使われるため、誤りは外部に伝播する）
  itunesFromDb?: boolean;
  trackDuration: number | null; // iTunes の曲の長さ（秒）
  originalName: string; // 元の名称を追跡（変更判定用）
  originalArtist: string; // 元のアーティストを追跡
  // AI 正規化追跡
  aiNormalizedName?: string; // AI 変更前の名称（変更された場合）
  aiNormalizedArtist?: string; // AI 変更前のアーティスト（変更された場合）
  // AI が照合したときの「この 2 つは同じ人か」の申し送りと、人のチェック状態。
  // 別名義はその人の全楽曲に効くので、**保存したときにだけ**登録する。
  artistAlias?: ArtistAliasProposal;
  aliasChecked?: boolean;
  // 「抽出したままの値が、どの処理でどう変わったか」。AI 正規化と DB 照合を区別して出す。
  // aiNormalized* は 1 段しか表せず、どちらの仕業かも分からないので、表示はこちらを使う。
  changes?: FieldChange[];
  // 時間推定マーク
  isEndTimeEstimated?: boolean; // 終了時間が推定値かどうか
  // Chat の拍手検出による参考値（コメントに明記された終了時刻との差が大きい場合に警告）
  chatEnd?: number;
  endDiff?: number;
  // 由来の追跡と復元
  originalCommentEnd?: number; // コメント分析で明記されていた元の終了時刻（復元用）
  endSource?: EndSource;
  // マージ追跡
  mergedFrom?: string[]; // AI 正規化後にマージされた元の曲名
  // 自由文本タグ
  customTags: string[];
  // 単曲編集フロー：確認済みフラグ（ローカルのみ、保存は最後に一括）
  confirmed?: boolean;
}
