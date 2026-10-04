import assert from 'node:assert/strict';
import { after, test } from 'node:test';
import { mkdtemp, readFile, writeFile, rm } from 'node:fs/promises';
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import ts from 'typescript';
import { AxiosError } from 'axios';

// 実際の TS モジュールをコンパイルして実行する（ソースの字面は検査しない）。
const dir = await mkdtemp(join(dirname(fileURLToPath(import.meta.url)), '.api-error-'));
after(() => rm(dir, { recursive: true, force: true }));
const source = await readFile(new URL('../src/utils/apiError.ts', import.meta.url), 'utf8');
await writeFile(join(dir, 'apiError.mjs'), ts.transpileModule(source, {
  compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 },
}).outputText);
const { analysisFailureMessage, apiErrorMessage } = await import(pathToFileURL(join(dir, 'apiError.mjs')).href);
function httpError(status, data) {
  const error = new AxiosError('request failed');
  error.response = { status, data };
  return error;
}

test('409 は最新の入力での再実行を案内する', () => {
  assert.equal(analysisFailureMessage('コメント分析', httpError(409, { error: 'changed' })),
    'コメント分析：分析中に入力が更新されました。もう一度押すと最新の内容で分析し直します');
});
test('409 以外は具体的なエラーを残す', () => {
  assert.equal(analysisFailureMessage('Holodex分析', httpError(500, { error: 'API 未設定' })),
    'Holodex分析に失敗しました：API 未設定');
  assert.equal(apiErrorMessage(httpError(400, { message: '入力が無い' }), 'fallback'), '入力が無い');
});
test('応答の無いネットワークエラーも理由を残す', () => {
  assert.equal(analysisFailureMessage('チャプター分析', new AxiosError('サーバーに接続できません')),
    'チャプター分析に失敗しました：サーバーに接続できません');
  assert.equal(apiErrorMessage(new Error('timeout'), 'fallback'), 'timeout');
  assert.equal(analysisFailureMessage('コメント分析', null), 'コメント分析に失敗しました');
});
