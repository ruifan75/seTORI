import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { test } from 'node:test';
import { measureAnalysisTabs } from './fixtures/streamAnalysisLayout.mjs';

const chrome = process.env.SETORI_CHROME_PATH;
const labels = ['操作', 'Holodex', 'コメント', 'チャプター', '手動', '生コメント'];

test('編集タブは狭い幅でも一行で読めて押せ、デスクトップの寸法と再生を保つ', { skip: !chrome }, async () => {
  const cases = [];
  for (const [w, h] of [[360, 844], [390, 844], [430, 844], [844, 390], [640, 300], [1024, 900], [1299, 900], [1300, 900], [1440, 900], [1920, 900]]) {
    for (const role of ['editor', 'admin']) cases.push({ w, h, role, restricted: false });
  }
  // origin/main 296850d の変更前の画面を同じ Chrome で計測した固定値。現実装から生成しない。
  const desktop = JSON.parse(await readFile(new URL('./fixtures/streamAnalysisDesktop.json', import.meta.url), 'utf8'));
  const rows = await measureAnalysisTabs(cases, undefined, (row) => {
    const name = JSON.stringify(row.tc);
    assert.equal(row.error, undefined, name);
    assert.equal(row.value.iframeSame, true, `iframe の再マウント: ${name}`);
    assert.equal(row.value.queueSame, true, `再生キューの変更: ${name}`);
    assert.equal(row.value.measurements.length, 7, name);
    for (const [index, m] of row.value.measurements.entries()) {
      assert.deepEqual(m.tabs.map((tab) => tab.label), labels, name);
      assert.ok(m.scrollWidth <= row.tc.w, `ページの横はみ出し: ${name}`);
      const expectedActive = index === 0 ? '操作' : ['Holodex', 'コメント', 'チャプター', '手動', '生コメント', '操作'][index - 1];
      assert.deepEqual(m.tabs.filter((tab) => tab.active).map((tab) => tab.label), [expectedActive], `タブ切替: ${name}`);
      for (const tab of m.tabs) {
        assert.equal(tab.lines, 1, `ラベルが折り返す: ${name} ${tab.label}`);
        assert.ok(tab.textLeft >= tab.x - 1 && tab.textRight <= tab.x + tab.w + 1, `ラベルがボタンから溢れる: ${name} ${tab.label}`);
        assert.ok(tab.hit, `押せない: ${name} ${tab.label}`);
        assert.ok(tab.x >= m.bar.x - 1 && tab.x + tab.w <= m.bar.x + m.bar.w + 1, `タブが切れる: ${name} ${tab.label}`);
      }
    }
    if (row.tc.w >= 1300) {
      const before = desktop.find((old) => old.w === row.tc.w && old.h === row.tc.h);
      assert.ok(before, `変更前の比較対象が無い: ${name}`);
      for (const m of row.value.measurements) {
        const { x, y, h } = m.bar;
        assert.deepEqual({ x, y, h }, before.bar, `デスクトップのタブ領域の寸法が変わった: ${name}`);
        // 変更前から長い内容ではスクロールバー分だけ内幅が減る。タブ自体の寸法は同じ。
        assert.ok(before.widths.includes(m.bar.w), `デスクトップの内幅が変わった: ${name}`);
        assert.deepEqual(m.tabs.map(({ label, x, y, w, h }) => ({ label, x, y, w, h })), before.tabs,
          `デスクトップのタブの寸法が変わった: ${name}`);
      }
    }
  });
  assert.equal(rows.length, cases.length);
});
