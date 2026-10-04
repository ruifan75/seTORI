import assert from 'node:assert/strict';
import { test } from 'node:test';
import { spawn } from 'node:child_process';
import { mkdtemp, writeFile, rm } from 'node:fs/promises';
import { dirname, join } from 'node:path';
import { tmpdir } from 'node:os';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { build } from 'vite';

const chrome = process.env.SETORI_CHROME_PATH;
const root = dirname(dirname(fileURLToPath(import.meta.url)));
test('Chrome: actual React state/notification cleanup and raw comment/name/URL rendering', { skip: !chrome }, async () => {
  const dir = await mkdtemp(join(tmpdir(), 'setori-fesec-'));
  try {
    const bundle = await build({ configFile: false, root, logLevel: 'silent', esbuild: { jsx: 'automatic' },
      define: { 'process.env.NODE_ENV': JSON.stringify('test') },
      build: { write: false, minify: false, lib: { entry: join(root, 'tests/fixtures/frontendSecurity.jsx'), name: 'SecurityFixture', formats: ['iife'] },
        rollupOptions: { output: { inlineDynamicImports: true } } } });
    const script = (Array.isArray(bundle) ? bundle[0] : bundle).output.find((item) => item.type === 'chunk').code;
    await writeFile(join(dir, 'fixture.js'), script);
    const file = join(dir, 'fixture.html');
    await writeFile(file, '<!doctype html><div id="root"></div><pre id="result"></pre><script src="fixture.js"></script>');
    const child = spawn(chrome, ['--headless=new', '--disable-gpu', '--disable-background-networking', '--disable-component-update',
      '--disable-sync', '--no-first-run', '--no-default-browser-check', '--password-store=basic', '--use-mock-keychain',
      '--remote-debugging-port=0', `--user-data-dir=${join(dir, 'profile')}`, 'about:blank']);
    let stderr = '', socket, result;
    child.stderr.on('data', (chunk) => { stderr += chunk; });
    try {
      for (let i = 0; i < 100 && !stderr.includes('DevTools listening'); i++) await new Promise((r) => setTimeout(r, 50));
      const endpoint = stderr.match(/DevTools listening on (ws:\/\/\S+)/)?.[1];
      assert.ok(endpoint, stderr.slice(-2000));
      socket = new WebSocket(endpoint);
      await new Promise((resolve, reject) => { socket.addEventListener('open', resolve, { once: true }); socket.addEventListener('error', reject, { once: true }); });
      let sequence = 0, session;
      const pending = new Map();
      socket.addEventListener('message', (event) => {
        const message = JSON.parse(event.data), waiter = pending.get(message.id);
        if (waiter) { pending.delete(message.id); clearTimeout(waiter.timer); waiter.resolve(message); }
      });
      function call(method, params = {}, sessionId = session) {
        return new Promise((resolve, reject) => {
          const id = ++sequence;
          pending.set(id, { resolve, timer: setTimeout(() => { pending.delete(id); reject(new Error(`Chrome: ${method} timed out`)); }, 5000) });
          socket.send(JSON.stringify({ id, method, params, ...(sessionId ? { sessionId } : {}) }));
        });
      }
      const target = await call('Target.createTarget', { url: 'about:blank' }, null);
      const attached = await call('Target.attachToTarget', { targetId: target.result.targetId, flatten: true }, null);
      session = attached.result.sessionId;
      await call('Network.enable');
      await call('Network.setBlockedURLs', { urls: ['http://*', 'https://*'] });
      await call('Page.navigate', { url: pathToFileURL(file).href });
      for (let i = 0; i < 100; i++) {
        const response = await call('Runtime.evaluate', { expression: 'document.getElementById("result")?.textContent', returnByValue: true });
        const value = response.result?.result?.value;
        if (value) { result = JSON.parse(value); break; }
        await new Promise((r) => setTimeout(r, 50));
      }
      assert.ok(result, 'Chrome fixture did not finish');
    } finally {
      socket?.close(); child.kill('SIGTERM');
      await new Promise((resolve) => { if (child.exitCode !== null) resolve(); else child.once('exit', resolve); });
    }
    assert.equal(result.error, undefined, JSON.stringify(result));
    assert.deepEqual(result, { textEscaped: true, javascriptHrefBlocked: true, javascriptImageDidNotExecute: true, blankRel: true,
      positiveCopy: true, sameUserPermissionLoss: true, lateCallbacksBlocked: true, newSetterWorks: true });
  } finally { await rm(dir, { recursive: true, force: true }); }
});
