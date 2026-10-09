import { spawn } from 'node:child_process';
import { join } from 'node:path';

// 専用プロファイルと pipe を使い、利用者のブラウザには接続しない。
export async function openChrome(executable, dir) {
  const child = spawn(executable, ['--headless=new', '--disable-gpu', '--disable-background-networking',
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
      const message = JSON.parse(raw), request = pending.get(message.id);
      if (!request) continue;
      pending.delete(message.id); clearTimeout(request.timer);
      if (message.error) request.reject(new Error(JSON.stringify(message.error)));
      else request.resolve(message.result);
    }
  });
  function send(method, params = {}, sessionId) {
    return new Promise((resolve, reject) => {
      const id = ++sequence;
      const timer = setTimeout(() => { pending.delete(id); reject(new Error(`Chrome timeout: ${method}`)); }, 30000);
      pending.set(id, { resolve, reject, timer });
      child.stdio[3].write(JSON.stringify({ id, method, params, ...(sessionId ? { sessionId } : {}) }) + '\0');
    });
  }
  const close = async () => {
    if (child.exitCode !== null || child.signalCode !== null || !child.pid) return;
    const closed = new Promise((resolve) => child.once('close', resolve));
    child.kill('SIGTERM'); await closed;
  };
  try {
    const { targetId } = await send('Target.createTarget', { url: 'about:blank' });
    const { sessionId } = await send('Target.attachToTarget', { targetId, flatten: true });
    return { send: (method, params) => send(method, params, sessionId), close };
  } catch (error) { await close(); throw error; }
}
