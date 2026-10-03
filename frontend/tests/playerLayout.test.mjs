import assert from 'node:assert/strict';
import { test } from 'node:test';
import { spawn } from 'node:child_process';
import { mkdtemp, readFile, writeFile, rm } from 'node:fs/promises';
import { dirname, join } from 'node:path';
import { tmpdir } from 'node:os';
import { fileURLToPath, pathToFileURL } from 'node:url';
import ts from 'typescript';
import postcss from 'postcss';
import tailwindcss from 'tailwindcss';
import { createServer } from 'vite';
import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import config from '../tailwind.config.js';

const chrome = process.env.SETORI_CHROME_PATH;
const root = dirname(dirname(fileURLToPath(import.meta.url)));

// 実際のクラスから CSS を生成し、YouTube iframe の器と実際の案内を Chrome で測る。
// 情報カードの本文・外部 YouTube API・セットリストの行は再現対象に含めない。
async function layoutFixture(dir) {
  const page = join(root, 'src/pages/StreamDetailPage.tsx');
  const noticeFile = join(root, 'src/components/UnplayableNotice.tsx');
  const source = await readFile(page, 'utf8');
  const tree = ts.createSourceFile(page, source, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
  let youtube;
  function visit(node, fn) { fn(node); ts.forEachChild(node, (child) => visit(child, fn)); }
  visit(tree, (node) => {
    if (ts.isJsxSelfClosingElement(node) && node.tagName.getText(tree) === 'YoutubePlayer') youtube = node;
  });
  function classes(node) {
    return node.openingElement.attributes.properties.find((p) => p.name?.getText(tree) === 'className')?.initializer?.text;
  }
  const ancestors = [];
  for (let node = youtube.parent; node; node = node.parent) {
    if (ts.isJsxElement(node) && classes(node)) ancestors.push(node);
  }
  assert.equal(ancestors.length, 6, 'プレイヤーの構造が変わったら fixture も見直す');
  const [video, widthBox, sizeBox, card, left, outer] = ancestors;
  let timeline, rows;
  const bars = [];
  visit(card, (node) => {
    if (!ts.isJsxElement(node)) return;
    const c = classes(node);
    if (c?.startsWith('border-t py-3')) timeline = node;
    if (c?.startsWith('space-y-1 px-3')) rows = node;
    if (c?.startsWith('relative h-')) bars.push(node);
  });
  assert.equal(bars.length, 3);
  const server = await createServer({ root, server: { middlewareMode: true, hmr: false, ws: false },
    optimizeDeps: { noDiscovery: true, include: [] }, appType: 'custom' });
  let notice, youtubeHTML;
  try {
    const { default: Notice } = await server.ssrLoadModule('/src/components/UnplayableNotice.tsx');
    notice = renderToStaticMarkup(createElement(Notice, { kind: 'members_only', videoId: 'fixture' }));
    const { default: Player } = await server.ssrLoadModule('/src/components/YoutubePlayer.tsx');
    youtubeHTML = renderToStaticMarkup(createElement(Player, { videoId: 'fixture' }));
  } finally { await server.close(); }
  const { css } = await postcss([tailwindcss({ ...config, content: [page, noticeFile, join(root, 'src/components/YoutubePlayer.tsx')] })])
    .process('@tailwind base;@tailwind components;@tailwind utilities;body{margin:0;min-width:320px}', { from: undefined });
  function div(node, content, id = '') {
    return `<div ${id ? `id="${id}"` : ''} class="${classes(node)}">${content}</div>`;
  }
  const cases = [];
  for (const w of [414, 639, 640, 800, 1024, 1299, 1300, 1920]) {
    for (const h of [300, 800]) for (const editor of [false, true]) for (const noticeMode of [false, true]) {
      cases.push({ w, h, editor, noticeMode });
    }
  }
  const docs = cases.map((tc) => {
    const player = tc.noticeMode ? notice : div(video, youtubeHTML, 'player');
    const timelineHTML = div(timeline, div(rows, bars.slice(0, tc.editor ? 3 : 1).map((bar) => div(bar, '')).join('')), 'timeline');
    const content = div(outer, div(left, '<div class="flex-1 min-w-0 min-h-0 overflow-y-auto">情報</div>' +
      div(card, div(sizeBox, div(widthBox, player), 'sizebox') + timelineHTML, 'card')));
    const doc = `<html><head><style>${css}</style></head><body><main style="padding:24px;height:calc(100vh - 160px)">${content}</main><script>
      // YT.Player の置換先だけをローカル iframe にする。属性は実装と同じ 100%。
      const mount=document.querySelector('#player > div > div');
      if(mount){const iframe=document.createElement('iframe');iframe.width='100%';iframe.height='100%';iframe.srcdoc='';mount.append(iframe);}
      function rect(el) { const r=el.getBoundingClientRect();return {x:r.x,y:r.y,w:r.width,h:r.height,b:r.bottom}; }
      setTimeout(()=>{ const p=document.getElementById('player')||document.querySelector('.aspect-video');
        const inner=p.firstElementChild;const link=p.querySelector('a');
        const topOK=!link||inner.getBoundingClientRect().top>=p.getBoundingClientRect().top-1;
        if(link) p.scrollTop=p.scrollHeight;
        const endOK=!link||link.getBoundingClientRect().bottom<=p.getBoundingClientRect().bottom+1;
        parent.postMessage({tc:${JSON.stringify(tc)},player:rect(p),size:rect(document.getElementById('sizebox')),
          timeline:rect(document.getElementById('timeline')),topOK,endOK},'*'); },50);
      </script></body></html>`;
    return { tc, doc };
  });
  // srcdoc の </script> が親の script を閉じないよう、JSON 内の < を escape する。
  const encoded = JSON.stringify(docs).replaceAll('<', '\\u003c');
  const html = `<html><body><pre id="result"></pre><script>
    const docs=${encoded};const results=[];
    addEventListener('message',e=>{results.push(e.data);if(results.length===docs.length){
      document.getElementById('result').textContent=JSON.stringify(results);
      for(const f of document.querySelectorAll('iframe'))f.remove();document.querySelector('script').remove();}});
    for(const {tc,doc} of docs){const f=document.createElement('iframe');
      f.style.cssText='position:absolute;left:-10000px;border:0;width:'+tc.w+'px;height:'+tc.h+'px';
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
    '--dump-dom', '--virtual-time-budget=1500', pathToFileURL(file).href]);
  let stdout = '', stderr = '', rows;
  try {
    await new Promise((resolve, reject) => {
      const timer = setTimeout(() => reject(new Error(`Chrome の計測が完了しません: ${stderr.slice(-500)}`)), 20000);
      function finish(error) { clearTimeout(timer); error ? reject(error) : resolve(); }
      child.on('error', finish);
      child.stderr.on('data', (data) => { stderr += data; });
      child.stdout.on('data', (data) => {
        stdout += data;
        const match = stdout.match(/<pre id="result">(.*?)<\/pre>/s);
        if (match?.[1]) { rows = JSON.parse(match[1]); finish(); }
      });
      child.on('close', (code) => { if (!rows) finish(new Error(`Chrome が計測前に終了: ${code} ${stderr.slice(-500)}`)); });
    });
    return rows;
  } finally {
    // 計測 DOM を受け取ったら隔離した Chrome だけを終了する。
    const closed = new Promise((resolve) => child.once('close', resolve));
    if (child.exitCode === null && child.signalCode === null) { child.kill('SIGTERM'); await closed; }
  }
}

test('幅・高さ・閲覧権限・案内表示が変わっても 16:9 を保ち、案内の末尾まで読める',
  { skip: !chrome }, async (t) => {
    const dir = await mkdtemp(join(tmpdir(), 'setori-player-'));
    try {
      const { file, count } = await layoutFixture(dir);
      const rows = await measure(file, dir);
      assert.equal(rows.length, count);
      const sample = rows.find((r) => r.tc.w === 640 && r.tc.h === 300 && r.tc.editor && r.tc.noticeMode);
      t.diagnostic(`${count} 通りを計測。640×300 編集者の案内: ${sample.player.w.toFixed(2)}×${sample.player.h.toFixed(2)}px`);
      for (const row of rows) {
        const label = JSON.stringify(row.tc);
        assert.ok(row.player.h > 0, `プレイヤーが消えた: ${label}`);
        assert.ok(Math.abs(row.player.w / row.player.h - 16 / 9) < 0.015, `16:9 が崩れた: ${label} ${JSON.stringify(row.player)}`);
        if (row.tc.w >= 640 && row.tc.w < 1300) {
          assert.ok(row.player.x >= row.size.x - 1 && row.player.x + row.player.w <= row.size.x + row.size.w + 1, `横へ溢れた: ${label}`);
          assert.ok(row.player.y >= row.size.y - 1, `上へ溢れた: ${label}`);
          assert.ok(row.player.b <= row.timeline.y + 1, `時間帯バーと重なる: ${label}`);
        }
        assert.ok(row.topOK && row.endOK, `案内の先頭・リンクまで辿れない: ${label}`);
      }
    } finally { await rm(dir, { recursive: true, force: true }); }
  });
