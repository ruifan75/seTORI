import { mkdtemp, readFile, writeFile, rm } from 'node:fs/promises';
import { dirname, join } from 'node:path';
import { tmpdir } from 'node:os';
import { fileURLToPath } from 'node:url';
import { build, createServer } from 'vite';
import postcss from 'postcss';
import tailwindcss from 'tailwindcss';
import config from '../../tailwind.config.js';
import { openChrome } from './chrome.mjs';

const root = dirname(dirname(dirname(fileURLToPath(import.meta.url))));

// ページ・hook・子コンポーネント・CSS は実物。API と YouTube のみ既存の合成 fixture にする。
export async function measureAnalysisTabs(cases, screenshots, validate) {
  const bundle = await build({ root, configFile: false, logLevel: 'silent',
    resolve: { alias: { '/src': join(root, 'src') } }, esbuild: { jsx: 'automatic' },
    define: { __APP_COMMIT__: '"dev"', __APP_BUILT_AT__: '""', 'process.env.NODE_ENV': '"production"' },
    build: { write: false, minify: false, lib: { entry: join(root, 'tests/fixtures/streamDetail.jsx'),
      name: 'StreamDetailFixture', formats: ['iife'] } } });
  const output = Array.isArray(bundle) ? bundle[0].output : bundle.output;
  const script = output.find((item) => item.type === 'chunk').code;
  const input = (await readFile(join(root, 'src/index.css'), 'utf8')).replace(/^@import[^\n]*\n/, '');
  const { css } = await postcss([tailwindcss({ ...config, content: [join(root, 'src/**/*.{ts,tsx}')] })])
    .process(input, { from: undefined });
  const server = await createServer({ root, configFile: false, logLevel: 'silent',
    server: { host: 'localhost', port: 15173, strictPort: true, hmr: false }, appType: 'custom' });
  server.middlewares.use((req, res) => {
    const tc = cases[Number(new URL(req.url, 'http://localhost').searchParams.get('case'))];
    if (!tc) { res.statusCode = 404; res.end(); return; }
    const doc = `<html><head><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data:; frame-src about: 'self'"><style>${css}</style></head><body><script>
      addEventListener('error',e=>parent.postMessage({error:e.message},'*'));
      window.fetch=()=>{throw new Error('fixture からの通信は禁止')};
      XMLHttpRequest.prototype.open=()=>{throw new Error('fixture からの通信は禁止')};
      ${script.replaceAll('</script', '<\\/script')}
      StreamDetailFixture.run(${JSON.stringify(tc)},{layoutOnly:true}).then(value=>parent.postMessage({value},'*'))
        .catch(error=>parent.postMessage({error:error.stack},'*'));
      </script></body></html>`;
    const escaped = doc.replaceAll('&', '&amp;').replaceAll('"', '&quot;');
    res.setHeader('Content-Type', 'text/html; charset=utf-8');
    res.end(`<html><head><meta charset="utf-8"></head><body style="margin:0"><script>window.result=null;addEventListener('message',e=>{window.result=e.data;});</script>
      <iframe style="display:block;border:0;width:${tc.w}px;height:${tc.h}px" srcdoc="${escaped}"></iframe></body></html>`);
  });
  const dir = await mkdtemp(join(tmpdir(), 'setori-analysis-tabs-'));
  let chrome;
  try {
    await server.listen();
    chrome = await openChrome(process.env.SETORI_CHROME_PATH, dir);
    await chrome.send('Page.enable');
    const rows = [];
    for (const [index, tc] of cases.entries()) {
      await chrome.send('Emulation.setDeviceMetricsOverride', { width: tc.w, height: tc.h, deviceScaleFactor: 1, mobile: false });
      await chrome.send('Page.navigate', { url: `http://localhost:15173/__analysis-tabs?case=${index}` });
      const result = await chrome.send('Runtime.evaluate', { awaitPromise: true, returnByValue: true,
        expression: `(async()=>{for(let i=0;i<300;i++){if(window.result)return window.result;await new Promise(r=>setTimeout(r,50));}throw new Error('測定が完了しません');})()` });
      if (result.exceptionDetails) throw new Error(JSON.stringify(result.exceptionDetails));
      rows.push({ tc, ...result.result.value });
      if (validate) validate(rows.at(-1));
      if (screenshots) {
        const { data } = await chrome.send('Page.captureScreenshot', { format: 'png' });
        await writeFile(join(screenshots, `${tc.w}x${tc.h}-${tc.role}.png`), Buffer.from(data, 'base64'));
      }
    }
    return rows;
  } finally {
    if (chrome) await chrome.close();
    await server.close(); await rm(dir, { recursive: true, force: true });
  }
}
