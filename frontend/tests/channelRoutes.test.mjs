import assert from 'node:assert/strict';
import { test } from 'node:test';
import { readFileSync, readdirSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { createRequire } from 'node:module';
import vm from 'node:vm';
import { spawn } from 'node:child_process';
import { mkdtemp, writeFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { pathToFileURL } from 'node:url';
import { build } from 'vite';
import ts from 'typescript';

const root = dirname(dirname(fileURLToPath(import.meta.url)));
const require = createRequire(import.meta.url);
function load(relative, overrides = {}) {
  const filename = join(root, relative);
  const source = readFileSync(filename, 'utf8').replaceAll('import.meta.env', '({})');
  const code = ts.transpileModule(source, { compilerOptions: {
    module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022,
    jsx: ts.JsxEmit.ReactJSX, esModuleInterop: true,
  }, fileName: filename }).outputText;
  const module = { exports: {} };
  vm.runInNewContext(code, { module, exports: module.exports, URLSearchParams,
    require: (id) => overrides[id] ?? require(id),
  }, { filename });
  return module.exports;
}

test('singerApi の全操作は新 API を使い、query/body と JSON のキーは保つ', async () => {
  const axios = require('axios');
  const requests = [];
  const response = { singers: [{ id: 'channel' }], singer: { id: 'channel' }, singer_ids: ['vocalist'] };
  const instance = axios.create({ adapter: async (config) => {
    requests.push({ method: config.method, url: config.url,
      body: config.data === undefined ? null : JSON.parse(config.data) });
    return { data: response, status: 200, statusText: 'OK', headers: {}, config };
  } });
  const { singerApi } = load('src/api/client.ts', { axios: { ...axios, create: () => instance } });
  const cases = [
    ['list', [2, 30, 'name', 'desc', true], 'get', '/api/channels?page=2&limit=30&sort=name&dir=desc&include_hidden=true', null],
    ['listGrouped', [true], 'get', '/api/channels?group=organization&include_hidden=true', null],
    ['get', ['channel'], 'get', '/api/channels/channel', null],
    ['search', ['名前', 7], 'get', '/api/channels/search?q=%E5%90%8D%E5%89%8D&limit=7', null],
    ['getStreams', ['channel', 2, 30, 'true', 'all'], 'get', '/api/channels/channel/streams?page=2&limit=30&processed=true&hidden=all', null],
    ['getPerformances', ['channel', 2, 30, 'date', 'desc'], 'get', '/api/channels/channel/performances?page=2&limit=30&sort=date&dir=desc', null],
    ['create', ['@channel'], 'post', '/api/channels', { id: '@channel' }],
    ['update', ['channel', { name: '名前', english_name: 'Name', photo_url: 'photo' }], 'put', '/api/channels/channel', { name: '名前', english_name: 'Name', photo_url: 'photo' }],
    ['setOrganization', ['channel', 'org'], 'put', '/api/channels/channel/organization', { organization: 'org' }],
    ['setHidden', ['channel', true], 'put', '/api/channels/channel/visibility', { is_hidden: true }],
    ['setAutoFill', ['channel', false], 'put', '/api/channels/channel/auto-fill', { auto_fill_enabled: false }],
    ['listAutoFill', [], 'get', '/api/channels/auto-fill', null],
    ['setMembersPolicy', ['channel', 'allow'], 'put', '/api/channels/channel/members-policy', { members_only_policy: 'allow' }],
  ];
  assert.deepEqual(Object.keys(singerApi).sort(), cases.map(([name]) => name).sort(), '新しい操作も期待する URL を決める');
  for (const [name, args, method, url, body] of cases) {
    assert.equal(await singerApi[name](...args), response, `${name}: JSON のキーを変換しない`);
    assert.deepEqual(requests.at(-1), { method, url, body }, name);
  }
  assert.equal(requests.length, cases.length);
});

export const redirectCases = [
  ['/singers', '/channels', '', ''],
  ['/singers?view=list&sort=name#list', '/channels', '?view=list&sort=name', '#list'],
  ['/singers/?include_hidden=true#hidden', '/channels/', '?include_hidden=true', '#hidden'],
  ['/singers/channel?tab=streams#song-2', '/channels/channel', '?tab=streams', '#song-2'],
  ['/singers/channel?tab=streams&tab=performances&q=%E6%AD%8C#song%202', '/channels/channel', '?tab=streams&tab=performances&q=%E6%AD%8C', '#song%202'],
  ['/singers/.%2Fchannel?x=%2F#%2F', '/channels/.%2Fchannel', '?x=%2F', '#%2F'],
  ['/singers/%E5%90%8D%E5%89%8D?x=1#song', '/channels/%E5%90%8D%E5%89%8D', '?x=1', '#song'],
  ['/SINGERS/channel?x=1#song', '/channels/channel', '?x=1', '#song'],
  ['/%73ingers?view=list#top', '/channels', '?view=list', '#top'],
  ['/%73ingers/channel?x=1#song', '/channels/channel', '?x=1', '#song'],
  ['/%73ingers/.%2Fchannel?x=%2F#%2F', '/channels/.%2Fchannel', '?x=%2F', '#%2F'],
  ['/singers/channel/?tab=streams#song', '/channels/channel/', '?tab=streams', '#song'],
];

test('旧画面 URL は query/hash・符号化 ID を保ち、履歴を replace する', () => {
  const router = require('react-router-dom');
  for (const [old, pathname, search, hash] of redirectCases) {
    const url = new URL(old, 'https://setori.example');
    const { default: LegacyChannelRedirect } = load('src/components/LegacyChannelRedirect.tsx', {
      'react-router-dom': { ...router, useLocation: () => ({ pathname: url.pathname, search: url.search, hash: url.hash }) },
    });
    const element = LegacyChannelRedirect();
    assert.equal(element.type, router.Navigate);
    assert.equal(element.props.replace, true, old);
    assert.deepEqual(JSON.parse(JSON.stringify(element.props.to)), { pathname, search, hash }, old);
  }
});

// App の実登録を棚卸しする。新旧とも同じ Layout の子なので PlayerBar を置換しない。
export function channelRouteInventory() {
  const file = ts.createSourceFile('App.tsx', readFileSync(join(root, 'src/App.tsx'), 'utf8'), ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
  const routes = [];
  function attr(node, name) { return node.attributes.properties.find((p) => ts.isJsxAttribute(p) && p.name.getText(file) === name)?.initializer; }
  function visit(node, ancestors = []) {
    if (ts.isJsxElement(node)) {
      const opening = node.openingElement;
      if (opening.tagName.getText(file) === 'Route') {
        const element = attr(opening, 'element');
        ancestors = [...ancestors, element?.expression?.tagName?.getText(file)];
      }
    }
    const opening = ts.isJsxSelfClosingElement(node) ? node : ts.isJsxElement(node) ? node.openingElement : null;
    if (opening?.tagName.getText(file) === 'Route') {
      const path = attr(opening, 'path');
      if (path && ts.isStringLiteral(path) && /^(singers|channels)(\/|$)/.test(path.text)) {
        const element = attr(opening, 'element');
        routes.push({ path: path.text, element: element?.expression?.tagName?.getText(file), layouts: ancestors.filter(Boolean) });
      }
    }
    ts.forEachChild(node, (child) => visit(child, ancestors));
  }
  visit(file);
  return routes;
}

test('新旧チャンネル画面を同じ Layout に登録する', () => {
  assert.deepEqual(channelRouteInventory(), [
    { path: 'channels', element: 'SingersPage', layouts: ['Layout'] },
    { path: 'channels/:id', element: 'SingerDetailPage', layouts: ['Layout'] },
    { path: 'singers', element: 'LegacyChannelRedirect', layouts: ['Layout'] },
    { path: 'singers/:id', element: 'LegacyChannelRedirect', layouts: ['Layout'] },
  ]);
});

// 実ソースの URL リテラルを固定し、導線の片側だけ旧名へ戻す改変も検出する。
test('既存のアプリ内チャンネルリンクをすべて新 URL にする', () => {
  const expected = {
    'src/components/Layout.tsx': ['/channels'],
    'src/components/SingerAvatars.tsx': ['/channels/${singer.id}', '/channels/${singer.id}'],
    'src/components/PlayerBar.tsx': ['/channels/${s.id}', '/channels/${s.id}'],
    'src/pages/SearchPage.tsx': ['/channels/${singer.id}'],
    'src/pages/SingersPage.tsx': ['/channels/${singer.id}'],
    'src/pages/admin/SyncPage.tsx': ['/channels/${sg.id}'],
    'src/pages/stream-detail/StreamInfoCard.tsx': ['/channels/${singer.id}'],
    'src/pages/stream-detail/StreamPerformanceList.tsx': ['/channels/${singer.id}'],
    'src/pages/stream-detail/StreamVocalistPopup.tsx': ['/channels/${singer.id}'],
  };
  function files(dir) { return readdirSync(dir, { withFileTypes: true }).flatMap((e) => e.isDirectory() ? files(join(dir, e.name)) : /\.tsx?$/.test(e.name) ? [join(dir, e.name)] : []); }
  for (const path of files(join(root, 'src'))) {
    const relative = path.slice(root.length + 1);
    const file = ts.createSourceFile(path, readFileSync(path, 'utf8'), ts.ScriptTarget.Latest, true, path.endsWith('.tsx') ? ts.ScriptKind.TSX : ts.ScriptKind.TS);
    const urls = [];
    function visit(node) {
      const value = ts.isStringLiteralLike(node) ? node.text : ts.isTemplateExpression(node) ? node.getText(file).slice(1, -1) : null;
      if (value && /^\/(singers|channels)(\/|$)/.test(value)) urls.push(value);
      ts.forEachChild(node, visit);
    }
    visit(file);
    if (relative === 'src/components/LegacyChannelRedirect.tsx') assert.deepEqual(urls, ['/channels']);
    else assert.deepEqual(urls, expected[relative] ?? [], relative);
  }
});


const chrome = process.env.SETORI_CHROME_PATH;
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

test('Chrome: 旧 URL の replace と Layout/PlayerBar の継続（スマホ・PC）', { skip: !chrome }, async (t) => {
  const dir = await mkdtemp(join(tmpdir(), 'setori-channel-routes-'));
  try {
    const bundle = await build({ root, configFile: false, logLevel: 'silent',
      resolve: { alias: { '/src': join(root, 'src') } }, esbuild: { jsx: 'automatic' },
      define: { __APP_COMMIT__: '"dev"', __APP_BUILT_AT__: '""', 'process.env.NODE_ENV': '"production"' },
      build: { write: false, minify: false, lib: { entry: join(root, 'tests/fixtures/channelRoutes.jsx'),
        name: 'ChannelFixture', formats: ['iife'] } } });
    const output = Array.isArray(bundle) ? bundle[0].output : bundle.output;
    const script = output.find((item) => item.type === 'chunk').code;
    const routes = channelRouteInventory();
    const cases = [390, 1440].flatMap((w) => redirectCases.map(([old, pathname, search, hash]) => ({
      w, old, pathname, search, hash, routes,
    })));
    const docs = cases.map((tc) => ({ tc, doc: `<html><head><meta http-equiv="Content-Security-Policy" content="default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data:; frame-src about: 'self'"></head><body><script>
      window.fetch=()=>{throw new Error('外部通信は禁止')};
      XMLHttpRequest.prototype.open=()=>{throw new Error('外部通信は禁止')};
      ${script.replaceAll('</script', '<\\/script')}
      ChannelFixture.run(${JSON.stringify(tc)}).then(value=>parent.postMessage({tc:${JSON.stringify(tc)},value},'*'))
        .catch(error=>parent.postMessage({tc:${JSON.stringify(tc)},error:error.stack},'*'));
      </script></body></html>` }));
    const file = join(dir, 'fixture.html');
    await writeFile(file, `<html><body><pre id="result"></pre><script>
      const docs=${JSON.stringify(docs).replaceAll('<', '\\u003c')};const results=[];
      addEventListener('message',e=>{if(!e.data.tc)return;results.push(e.data);if(results.length===docs.length){
        document.getElementById('result').textContent=JSON.stringify(results);
        for(const f of document.querySelectorAll('iframe'))f.remove();}});
      for(const {tc,doc} of docs){const f=document.createElement('iframe');
        f.style.cssText='width:'+tc.w+'px;height:900px';f.srcdoc=doc;document.body.append(f);}
      </script></body></html>`);
    const rows = await measure(file, dir);
    assert.equal(rows.length, cases.length);
    for (const { tc, value, error } of rows) await t.test(`${tc.w} ${tc.old}`, () => {
      assert.equal(error, undefined);
      assert.deepEqual(value.location, { pathname: tc.pathname, search: tc.search, hash: tc.hash, type: 'REPLACE' });
      assert.equal(value.back, '/entry', '戻る操作で旧 URL を再度開かない');
      assert.equal(value.iframeSame, true, 'PlayerBar の iframe を再マウントしない');
      assert.equal(value.queueSame, true, 'キュー・再生状態を変えない');
      assert.equal(value.playerCallsSame, true, '再生操作・プレイヤー再生成を起こさない');
    });
  } finally { await rm(dir, { recursive: true, force: true }); }
});
