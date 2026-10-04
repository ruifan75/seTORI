import { createHash } from 'node:crypto';
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { spawn } from 'node:child_process';
import { mkdtemp, readFile, writeFile, rm } from 'node:fs/promises';
import { dirname, join } from 'node:path';
import { tmpdir } from 'node:os';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { build } from 'vite';
import postcss from 'postcss';
import tailwindcss from 'tailwindcss';
import config from '../tailwind.config.js';

const chrome = process.env.SETORI_CHROME_PATH;
const root = dirname(dirname(fileURLToPath(import.meta.url)));

async function fixture(dir) {
  const bundle = await build({ root, configFile: false, logLevel: 'silent',
    resolve: { alias: { '/src': join(root, 'src') } }, esbuild: { jsx: 'automatic' },
    define: { __APP_COMMIT__: '"dev"', __APP_BUILT_AT__: '""', 'process.env.NODE_ENV': '"production"' },
    build: { write: false, minify: false, lib: { entry: join(root, 'tests/fixtures/streamDetail.jsx'),
      name: 'StreamDetailFixture', formats: ['iife'] } } });
  const output = Array.isArray(bundle) ? bundle[0].output : bundle.output;
  const script = output.find((item) => item.type === 'chunk').code;
  // フォントはローカルの既定値。Google Fonts・YouTube・バックエンドには接続しない。
  const input = (await readFile(join(root, 'src/index.css'), 'utf8')).replace(/^@import[^\n]*\n/, '');
  const { css } = await postcss([tailwindcss({ ...config, content: [join(root, 'src/**/*.{ts,tsx}')] })])
    .process(input, { from: undefined });
  const cases = [];
  for (const w of [390, 1024, 1440]) for (const role of ['anonymous', 'restricted', 'editor', 'both']) {
    for (const restricted of [false, true]) cases.push({ w, h: 900, role, restricted });
  }
  const docs = cases.map((tc) => {
    const style = postcss.parse(css);
    // 未対応ブラウザが無視する宣言を取り除く。inset は実機の代わりに 20px を注入する。
    style.walkDecls((decl) => {
      if ((tc.fallback && decl.value.includes('dvh')) || (tc.noEnv && decl.value.includes('env('))) decl.remove();
      else if (tc.inset) decl.value = decl.value.replace(/env\(safe-area-inset-bottom(?:,\s*0px)?\)/g, `${tc.inset}px`);
      if (tc.staticVh && decl.parent?.selector === '.max-h-player-queue-dynamic') {
        decl.value = decl.value.replaceAll('100vh', `${tc.staticVh}px`);
      }
    });
    return { tc, doc: `<html><head><meta http-equiv="Content-Security-Policy" content="default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data:; frame-src about: 'self'"><style>${style}</style></head><body><script>
    addEventListener('error',e=>parent.postMessage({tc:${JSON.stringify(tc)},error:e.message},'*'));
    window.fetch=()=>{throw new Error('fixture からの通信は禁止')};
    XMLHttpRequest.prototype.open=()=>{throw new Error('fixture からの通信は禁止')};
    ${script.replaceAll('</script', '<\\/script')}
    StreamDetailFixture.run(${JSON.stringify(tc)}).then(value=>parent.postMessage({tc:${JSON.stringify(tc)},value},'*'))
      .catch(error=>parent.postMessage({tc:${JSON.stringify(tc)},error:error.stack},'*'));
    </script></body></html>` };
  });
  const html = `<html><body><pre id="result"></pre><script>
    const docs=${JSON.stringify(docs).replaceAll('<', '\\u003c')};const results=[];
    addEventListener('message',e=>{if(!e.data.tc)return;results.push(e.data);if(results.length===docs.length){
      document.getElementById('result').textContent=JSON.stringify(results);
      for(const f of document.querySelectorAll('iframe'))f.remove();document.querySelector('script').remove();}});
    for(const {tc,doc} of docs){const f=document.createElement('iframe');
      f.style.cssText='position:absolute;left:0;top:0;border:0;width:'+tc.w+'px;height:'+tc.h+'px';
      f.srcdoc=doc;document.body.append(f);}
    </script></body></html>`;
  const file = join(dir, 'fixture.html');
  await writeFile(file, html);
  return { file, count: cases.length };
}

async function measure(file, dir) {
  const child = spawn(chrome, ['--headless=new', '--disable-gpu', '--disable-background-networking',
    '--disable-component-update', '--disable-sync', '--no-first-run', '--no-default-browser-check',
    '--password-store=basic', '--use-mock-keychain', `--user-data-dir=${join(dir, 'chrome')}`,
    '--remote-debugging-pipe'], { stdio: ['ignore', 'ignore', 'pipe', 'pipe', 'pipe'] });
  let sequence = 0, buffer = '', errors = '';
  const pending = new Map();
  const fail = (error) => {
    for (const { reject, timer } of pending.values()) { clearTimeout(timer); reject(error); }
    pending.clear();
  };
  child.on('error', fail);
  child.on('exit', (code, signal) => fail(new Error(`Chrome 終了: ${code}/${signal} ${errors.slice(-500)}`)));
  child.stderr.on('data', (data) => { errors = (errors + data).slice(-2000); });
  child.stdio[4].on('data', (data) => {
    buffer += data;
    let end;
    while ((end = buffer.indexOf('\0')) >= 0) {
      const raw = buffer.slice(0, end); buffer = buffer.slice(end + 1);
      if (!raw) continue;
      const message = JSON.parse(raw);
      const request = pending.get(message.id);
      if (!request) continue;
      pending.delete(message.id); clearTimeout(request.timer);
      if (message.error) request.reject(new Error(JSON.stringify(message.error)));
      else request.resolve(message.result);
    }
  });
  function send(method, params = {}, sessionId) {
    return new Promise((resolve, reject) => {
      const id = ++sequence;
      const timer = setTimeout(() => { pending.delete(id); reject(new Error(`Chrome timeout: ${method} ${errors.slice(-500)}`)); }, 25000);
      pending.set(id, { resolve, reject, timer });
      child.stdio[3].write(JSON.stringify({ id, method, params, ...(sessionId ? { sessionId } : {}) }) + '\0');
    });
  }
  try {
    const { targetId } = await send('Target.createTarget', { url: 'about:blank' });
    const { sessionId } = await send('Target.attachToTarget', { targetId, flatten: true });
    await send('Page.enable', {}, sessionId);
    await send('Page.navigate', { url: pathToFileURL(file).href }, sessionId);
    // 仮想時間は matchMedia / ResizeObserver の通知を飛ばし得るため、実時間で描画を待つ。
    const result = await send('Runtime.evaluate', { awaitPromise: true, returnByValue: true,
      expression: `(async()=>{for(let i=0;i<300;i++){
        const text=document.getElementById('result')?.textContent;
        if(text)return JSON.parse(text);await new Promise(r=>setTimeout(r,50));}
        throw new Error('測定が完了しません');})()` }, sessionId);
    if (result.exceptionDetails) throw new Error(JSON.stringify(result.exceptionDetails));
    return result.result.value;
  } finally {
    const closed = new Promise((resolve) => child.once('close', resolve));
    if (child.exitCode === null && child.signalCode === null) { child.kill('SIGTERM'); await closed; }
  }
}


// この固定値は分割前の 7232ad6 の実画面から記録したもの。
// 更新するときは、比較対象の旧版を別の展開先で測る（現在の実装から期待値を作らない）。
test('配信詳細の権限別 DOM・iframe・キューが分割前と一致する', { skip: !chrome }, async (t) => {
  const dir = await mkdtemp(join(tmpdir(), 'setori-stream-detail-'));
  try {
    const { file, count } = await fixture(dir);
    const rows = await measure(file, dir);
    assert.equal(rows.length, count);
    for (const row of rows) {
      assert.equal(row.error, undefined, JSON.stringify(row.tc));
      row.value.snapshots = Object.fromEntries(Object.entries(row.value.snapshots).map(([name, dom]) => {
        assert.ok(!dom.includes('["href","/singers/'), '旧チャンネル URL が残っている');
        if (name === 'view') assert.ok(dom.includes('["href","/channels/channel"]'), '配信主への新リンクが無い');
        // 分割前の固定 DOM は書き換えず、意図したリンク先変更だけを比較から除く。
        const comparable = dom.replaceAll('["href","/channels/', '["href","/singers/');
        return [name, createHash('sha256').update(comparable).digest('hex')];
      }));
    }
    const baseline = JSON.parse(await readFile(join(root, 'tests/fixtures/streamDetailDom.json'), 'utf8'));
    for (const row of rows) await t.test(JSON.stringify(row.tc), () => {
      const old = baseline.find((entry) => JSON.stringify(entry.tc) === JSON.stringify(row.tc));
      assert.ok(old, '比較対象がありません');
      assert.equal(row.value.iframeSame, true, 'タブ・編集・幅変更で iframe が再マウントされた');
      assert.equal(row.value.queueSame, true, 'キュー・再生位置が変わった');
      assert.deepEqual(row.value, old.value, 'DOM のテキスト・構造・属性、再生操作が変わった');
    });
  } finally { await rm(dir, { recursive: true, force: true }); }
});
