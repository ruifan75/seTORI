import assert from 'node:assert/strict';
import { test } from 'node:test';
import { mkdtemp, readFile, writeFile, rm } from 'node:fs/promises';
import { dirname, join } from 'node:path';
import { tmpdir } from 'node:os';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { build } from 'vite';
import postcss from 'postcss';
import tailwindcss from 'tailwindcss';
import config from '../tailwind.config.js';
import { openChrome } from './fixtures/chrome.mjs';

const root = dirname(dirname(fileURLToPath(import.meta.url)));
const chrome = process.env.SETORI_CHROME_PATH;

test('歌唱リスト：実ページの表示・再生・報告・並び替えを画面幅ごとに確かめる', { skip: !chrome }, async (t) => {
  const dir = await mkdtemp(join(tmpdir(), 'setori-performance-lists-'));
  let browser;
  try {
    const bundle = await build({ root, configFile: false, logLevel: 'silent',
      resolve: { alias: { '/src': join(root, 'src') } }, esbuild: { jsx: 'automatic' },
      define: { __APP_COMMIT__: '"dev"', __APP_BUILT_AT__: '""', 'process.env.NODE_ENV': '"production"' },
      build: { write: false, minify: false, lib: { entry: join(root, 'tests/fixtures/performanceLists.jsx'), name: 'ListsFixture', formats: ['iife'] } } });
    const output = Array.isArray(bundle) ? bundle[0].output : bundle.output;
    const script = output.find((item) => item.type === 'chunk').code;
    const input = (await readFile(join(root, 'src/index.css'), 'utf8')).replace(/^@import[^\n]*\n/, '');
    const { css } = await postcss([tailwindcss({ ...config, content: [join(root, 'src/**/*.{ts,tsx}')] })]).process(input, { from: undefined });
    const file = join(dir, 'fixture.html');
    await writeFile(file, `<html><head><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data:"><style>${css}</style></head><body><script>
      window.fetch=()=>{throw new Error('fixture からの通信は禁止')};
      XMLHttpRequest.prototype.open=()=>{throw new Error('fixture からの通信は禁止')};
      ${script.replaceAll('</script', '<\\/script')}
      window.fixtureReady=true;</script></body></html>`);
    browser = await openChrome(chrome, dir);
    await browser.send('Page.enable');
    const cases = [];
    for (const page of ['channel', 'song', 'playlist', 'shared', 'preset', 'tag']) {
      for (const role of ['anonymous', 'editor']) {
        for (const [width, height] of [[360, 800], [390, 844], [430, 932], [844, 390], [1440, 1000]]) cases.push({ page, role, width, height });
      }
    }
    for (const tc of cases) await t.test(JSON.stringify(tc), async () => {
      await browser.send('Emulation.setDeviceMetricsOverride', { width: tc.width, height: tc.height, deviceScaleFactor: 1, mobile: false });
      await browser.send('Page.navigate', { url: pathToFileURL(file).href });
      const result = await browser.send('Runtime.evaluate', { awaitPromise: true, returnByValue: true,
        expression: `(async()=>{for(let i=0;i<100&&!window.fixtureReady;i++)await new Promise(r=>setTimeout(r,20));return ListsFixture.run(${JSON.stringify(tc)});})()` });
      if (result.exceptionDetails) throw new Error(JSON.stringify(result.exceptionDetails));
      const v = result.result.value;
      assert.equal(v.rows.length, 3);
      assert.equal(v.pageOverflow, 0, 'ページが横にはみ出す');
      for (const row of v.rows) {
        assert.equal(row.overflow, 0, '歌唱行が横にはみ出す');
        for (const control of row.controls) {
          assert.ok(control.width >= 40 && control.height >= 40, '主要操作のタッチ面積');
          assert.ok(control.x >= row.rect.x && control.right <= row.rect.right + 1, '操作が行からはみ出す');
        }
      }
      assert.match(v.rows[0].text, /4:00/, '歌唱の長さは終了と開始の差');
      assert.match(v.rows[1].text, /4:32/, '歌唱の長さは終了と開始の差');
      assert.match(v.rows[2].text, /—/);
      assert.match(v.rows[1].text, /ゲストシンガー/);
      assert.match(v.rows[1].text, /Acoustic.*コラボ/);
      assert.equal(v.rows[1].text.includes('非公開'), tc.role === 'editor');
      if (tc.page === 'channel') assert.ok(!v.rows[0].text.includes('デモチャンネル'), '本人だけなら省略する');
      assert.ok(v.rows[1].links.includes('/channels/guest'));
      const paginated = ['channel', 'song', 'tag'].includes(tc.page);
      assert.ok(v.toolbar.includes(paginated ? 'このページを再生' : tc.page === 'preset' ? '表示中を再生' : 'すべてを再生'));
      assert.deepEqual(v.played.ids, ['p1', 'p2', 'p3']);
      assert.equal(v.played.index, 1, '選んだ行から再生する');
      assert.deepEqual(v.currentRows, [null, 'true', null], '同じ曲の別演唱は選択しない');
      assert.equal(v.paused.playing, false);
      assert.deepEqual(v.paused.ids, v.played.ids, '一時停止でキューを作り直さない');
      assert.equal(v.resumed.playing, true);
      assert.ok(v.closedWithFocus, 'Esc で閉じて起点へフォーカスを戻す');
      assert.ok(v.menu.rect.x >= 0 && v.menu.rect.right <= tc.width);
      assert.ok(v.menu.rect.y >= 0 && v.menu.rect.bottom <= tc.height, '下端のメニューを画面内に収める');
      assert.equal(v.menu.youtube, 'https://www.youtube.com/watch?v=stream-two&t=3800');
      assert.ok(v.menu.text.includes(tc.role === 'editor' ? 'この曲の情報を直す' : 'この曲の情報の誤りを報告'));
      assert.equal(v.menu.text.includes('プレイリストから削除'), tc.page === 'playlist' && tc.role === 'editor', '管理操作はプレイリスト所有者だけ');
      assert.deepEqual(v.report, { streamId: 'stream-two', performanceId: 'p2' });
      assert.equal(v.reportTrack.songId, 'song', '曲詳細の親から曲IDを補完する');
      assert.equal(v.reportTrack.songName, '夜明けのメロディー');
      assert.deepEqual(v.added.ids, ['p1', 'p2', 'p3', 'p1']);
      assert.equal(v.added.index, 1);
      assert.deepEqual(v.playAll.ids, ['p1', 'p2', 'p3']);
      assert.equal(v.playAll.index, 0);
      assert.deepEqual([...v.shuffled.ids].sort(), ['p1', 'p2', 'p3']);
      if (tc.page === 'channel') {
        assert.ok(v.sortVisible);
        assert.equal(v.sortLocation, '/channels/channel?tab=performances&psort=song');
        assert.deepEqual(v.calls, [['sort', 'channel', 1, 20, 'song', 'asc']]);
      }
      if (tc.page === 'playlist' && tc.role === 'editor') {
        assert.ok(v.firstMoveDisabled);
        assert.deepEqual(v.calls, [['reorder', 'playlist', ['p2', 'p1', 'p3']], ['remove', 'playlist', 'p1']]);
      }
      assert.ok(v.linkDidNotPlay);
      assert.match(v.linkLocation, /^\/streams\//);
    });
    if (process.env.SETORI_LIST_PREVIEW_DIR) {
      const previewDir = process.env.SETORI_LIST_PREVIEW_DIR;
      for (const page of ['channel', 'song', 'playlist']) {
        for (const [width, height] of [[390, 1000], [1440, 1000]]) {
          await browser.send('Emulation.setDeviceMetricsOverride', { width, height, deviceScaleFactor: 1, mobile: false });
          await browser.send('Page.navigate', { url: pathToFileURL(file).href });
          await browser.send('Runtime.evaluate', { awaitPromise: true,
            expression: `(async()=>{for(let i=0;i<100&&!window.fixtureReady;i++)await new Promise(r=>setTimeout(r,20));await ListsFixture.run(${JSON.stringify({ page, role: 'anonymous', preview: true })});})()` });
          const screenshot = await browser.send('Page.captureScreenshot', { format: 'png', captureBeyondViewport: true });
          await writeFile(join(previewDir, `${page}-${width}.png`), Buffer.from(screenshot.data, 'base64'));
        }
      }
    }
  } finally { await browser?.close(); await rm(dir, { recursive: true, force: true }); }
});
