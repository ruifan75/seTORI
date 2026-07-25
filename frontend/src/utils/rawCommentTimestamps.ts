const RAW_COMMENT_TIMESTAMP_RE = /(\d{1,2}):(\d{2})(?::(\d{2}))?/g;
const HAS_RAW_COMMENT_TIMESTAMP_RE = /\d{1,2}:\d{2}/;

export interface ParsedRawCommentTimestamp {
  text: string;
  start: number;
  index: number;
}

export interface RawCommentTimestamp {
  id: string;
  start: number;
  label: string;
}

export function findRawCommentTimestamps(line: string): ParsedRawCommentTimestamp[] {
  return [...line.matchAll(RAW_COMMENT_TIMESTAMP_RE)].map((match) => {
    const [, a, b, c] = match;
    return {
      text: match[0],
      start: c !== undefined
        ? parseInt(a) * 3600 + parseInt(b) * 60 + parseInt(c)
        : parseInt(a) * 60 + parseInt(b),
      index: match.index ?? 0,
    };
  });
}

export function hasRawCommentTimestamp(comment: string): boolean {
  return HAS_RAW_COMMENT_TIMESTAMP_RE.test(comment);
}

// 生コメントに書かれたタイムスタンプを、そのままタイムライン表示用の点へ変換する。
// 曲の抽出・正規化・終了時間の推定は行わない。
export function extractRawCommentTimestamps(comments: string[]): RawCommentTimestamp[] {
  return comments.flatMap((comment, commentIndex) =>
    comment.split('\n').flatMap((line, lineIndex) =>
      findRawCommentTimestamps(line).map((timestamp, timestampIndex) => ({
        id: `raw-comment-${commentIndex}-${lineIndex}-${timestampIndex}`,
        start: timestamp.start,
        label: line.trim(),
      })),
    ),
  );
}
