import assert from 'node:assert/strict';
import { test } from 'node:test';
import { existsSync, readFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { createRequire } from 'node:module';
import vm from 'node:vm';
import ts from 'typescript';
import axios, { AxiosError, isAxiosError, isCancel } from 'axios';
import { QueryObserver } from '@tanstack/react-query';

const require = createRequire(import.meta.url);
const root = resolve(dirname(fileURLToPath(import.meta.url)), '../src');
const deferred = () => {
  let resolve, reject;
  const promise = new Promise((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
};
function runtime(dependencies = {}, globals = {}) {
  const modules = new Map();
  function load(file) {
    file = resolve(root, file);
    if (!/\.tsx?$/.test(file)) file += existsSync(file + '.ts') ? '.ts' : '.tsx';
    if (modules.has(file)) return modules.get(file).exports;
    const module = { exports: {} }; modules.set(file, module);
    const source = readFileSync(file, 'utf8').replaceAll('import.meta.env', '({ VITE_API_TOKEN: "synthetic-static-secret" })');
    const code = ts.transpileModule(source, { compilerOptions: {
      module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022, jsx: ts.JsxEmit.ReactJSX,
    } }).outputText;
    function dependency(name) {
      if (Object.hasOwn(dependencies, name)) return dependencies[name];
      if (name.startsWith('.')) return load(resolve(dirname(file), name));
      return require(name);
    }
    vm.runInNewContext(code, { module, exports: module.exports, require: dependency, console, URL, ...globals }, { filename: file });
    return module.exports;
  }
  return { load };
}
const user = (id = 'owner', permissions = ['content:edit', 'restricted:view']) => ({ id, username: id, permissions });
function authFixture(sharedStorage) {
  const requests = [], events = new Map(), storage = new Map();
  const localStorage = sharedStorage ?? {
    getItem: (key) => storage.get(key) ?? null,
    setItem: (key, value) => storage.set(key, value),
    removeItem: (key) => storage.delete(key),
  };
  // 実 Axios のインターセプタを通す。通信だけを保留できる adapter に替える。
  const axiosModule = { ...require('axios'), default: {
    create: (options) => axios.create({ ...options, adapter: (config) => {
      const pending = deferred(); requests.push({ config, ...pending }); return pending.promise;
    } }),
  } };
  const rt = runtime({ axios: axiosModule }, {
    localStorage, window: { location: { protocol: 'http:', hostname: 'localhost' },
      addEventListener: (name, fn) => events.set(name, fn) },
  });
  const viewer = rt.load('queryClient.ts'), api = rt.load('api/client.ts'), store = rt.load('store/auth.ts').useAuthStore;
  function respond(request, data, status = 200) {
    const response = { config: request.config, data, status, statusText: 'fixture', headers: {} };
    if (status >= 400) request.reject(new AxiosError('request failed', 'ERR_BAD_RESPONSE', request.config, { body: request.config.data }, response));
    else request.resolve(response);
  }
  async function login(id = 'owner', permissions) {
    const promise = store.getState().login(id, 'synthetic-password');
    await new Promise((r) => setImmediate(r));
    respond(requests.at(-1), { token: `token-${id}`, user: user(id, permissions) }); await promise;
  }
  return { requests, events, localStorage, viewer, api, store, respond, login,
    async lastRequest() { await new Promise((r) => setImmediate(r)); return requests.at(-1); },
    close() { viewer.queryClient.clear(); } };
}

// Each fixture loads its own auth/API/query modules: stores and pending responses
// are tab-local; only storage is shared. Events can be delivered after another login.
function authTabs() {
  const values = new Map(), events = [], tabs = [];
  function add() {
    let tab;
    const storage = {
      getItem: (key) => values.get(key) ?? null,
      setItem(key, value) {
        const oldValue = values.get(key) ?? null;
        if (oldValue === value) return;
        values.set(key, value);
        for (const other of tabs) if (other !== tab) events.push({ tab: other, key, oldValue, newValue: value });
      },
      removeItem(key) {
        const oldValue = values.get(key) ?? null;
        if (oldValue === null) return;
        values.delete(key);
        for (const other of tabs) if (other !== tab) events.push({ tab: other, key, oldValue, newValue: null });
      },
    };
    tab = authFixture(storage); tabs.push(tab); return tab;
  }
  return { add, events,
    deliver() {
      const event = events.shift();
      if (event) event.tab.events.get('storage')({ ...event, storageArea: event.tab.localStorage });
      return event;
    },
    close() { for (const tab of tabs) tab.close(); },
  };
}

for (const method of ['login', 'loginWithOAuthCode']) for (const savedToken of [null, 'saved-token']) {
  test(`${method}: startup init cannot supersede an already started interactive login (${savedToken})`, async () => {
    const f = authFixture();
    try {
      if (savedToken) f.localStorage.setItem('setori_token', savedToken);
      const pending = (method === 'login' ? f.store.getState().login('next', 'pw') :
        f.store.getState().loginWithOAuthCode('one-time-code')).then(() => null, error => error);
      const exchange = await f.lastRequest();
      const init = f.store.getState().init();
      assert.equal(f.requests.length, 1, 'bootstrap must not start a competing /me');
      await init;
      f.respond(exchange, { token: 'token-next', user: user('next') });
      assert.equal(await pending, null);
      assert.equal(f.store.getState().user?.id, 'next');
    } finally { f.close(); }
  });
}

for (const method of ['login', 'loginWithOAuthCode']) test(`${method}: expiry while a new login is pending clears old copies without cancelling the new credentials`, async () => {
  const f = authFixture();
  try {
    await f.login();
    f.viewer.queryClient.setQueryData(['stream', 'private'], ['private']);
    const old = f.api.songApi.get('one').catch(error => error), request = await f.lastRequest();
    const login = (method === 'login' ? f.store.getState().login('next', 'pw') :
      f.store.getState().loginWithOAuthCode('new-code')).then(() => null, error => error);
    const exchange = await f.lastRequest();
    f.respond(request, { error: 'expired' }, 401); await old;
    assert.equal(f.store.getState().status, 'anonymous');
    assert.equal(f.viewer.queryClient.getQueryData(['stream', 'private']), undefined);
    f.respond(exchange, { token: 'token-next', user: user('next') });
    assert.equal(await login, null, 'old Bearer expiry is not a rejection of the new credentials');
    assert.equal(f.store.getState().token, 'token-next');
  } finally { f.close(); }
});

for (const method of ['login', 'loginWithOAuthCode']) test(`${method}: failed credentials do not revoke an existing valid session`, async () => {
  const f = authFixture();
  try {
    await f.login();
    const epoch = f.viewer.viewerID();
    const pending = (method === 'login' ? f.store.getState().login('bad', 'pw') :
      f.store.getState().loginWithOAuthCode('expired-code')).catch(error => error);
    f.respond(await f.lastRequest(), { error: 'invalid credentials' }, 401);
    assert.equal((await pending).response.status, 401);
    assert.equal(f.store.getState().token, 'token-owner');
    assert.equal(f.localStorage.getItem('setori_token'), 'token-owner');
    assert.equal(f.viewer.viewerID(), epoch);
  } finally { f.close(); }
});

for (const method of ['login', 'loginWithOAuthCode']) test(`${method}: credential rejection after expiry still returns its safe 401 reason`, async () => {
  const f = authFixture();
  try {
    await f.login();
    const old = f.api.songApi.get('one').catch(error => error), request = await f.lastRequest();
    const pending = (method === 'login' ? f.store.getState().login('bad', 'pw') :
      f.store.getState().loginWithOAuthCode('bad-code')).catch(error => error);
    const exchange = await f.lastRequest();
    f.respond(request, { error: 'expired' }, 401); await old;
    f.respond(exchange, { error: 'invalid new credentials' }, 401);
    const error = await pending;
    assert.equal(error.message, 'invalid new credentials');
    assert.equal(error.response?.status, 401);
    assert.equal(error.config, undefined);
    assert.equal(f.store.getState().status, 'anonymous');
  } finally { f.close(); }
});

for (const status of [200, 401, 500]) test(`/me ${status} from a replaced token cannot commit before the storage event arrives`, async () => {
  const f = authFixture();
  try {
    f.localStorage.setItem('setori_token', 'old-token');
    const init = f.store.getState().init(), old = await f.lastRequest();
    f.localStorage.setItem('setori_token', 'new-token');
    f.respond(old, status === 200 ? user('old') : { error: 'old failure' }, status);
    await init;
    assert.equal(f.localStorage.getItem('setori_token'), 'new-token');
    assert.equal(f.store.getState().user, null, 'unvalidated old user cannot be restored');
    assert.equal(f.store.getState().status, 'loading');
    const validation = await f.lastRequest();
    assert.equal(validation.config.headers.get('Authorization'), 'Bearer new-token');
    f.respond(validation, user('next')); await new Promise(r => setImmediate(r));
    assert.equal(f.store.getState().user?.id, 'next');
    const epoch = f.viewer.viewerID(), count = f.requests.length;
    f.events.get('storage')({ key: 'setori_token', newValue: 'new-token', storageArea: f.localStorage });
    assert.equal(f.viewer.viewerID(), epoch, 'already adopted token does not trigger another reset');
    assert.equal(f.requests.length, count);
  } finally { f.close(); }
});

test('an old tab 401 cannot remove the new tab token or cause cascading logouts', async () => {
  const tabs = authTabs(), a = tabs.add(), b = tabs.add();
  try {
    await a.login(); tabs.deliver();
    b.respond(await b.lastRequest(), user()); await new Promise(r => setImmediate(r));
    const old = a.api.songApi.get('one').catch(error => error), request = await a.lastRequest();
    await b.login('next'); // storage event is deliberately still queued
    a.respond(request, { error: 'expired' }, 401); await old;
    assert.equal(a.store.getState().user, null);
    assert.equal(b.localStorage.getItem('setori_token'), 'token-next');
    tabs.deliver(); a.respond(await a.lastRequest(), user('next')); await new Promise(r => setImmediate(r));
    assert.equal(a.store.getState().user?.id, 'next');
    assert.equal(b.store.getState().user?.id, 'next');
    assert.equal(tabs.events.length, 0, 'adoption must not write another token event');
  } finally { tabs.close(); }
});

for (const failure of ['network', 500]) test(`transient /me ${failure} in one tab does not log out the valid other tab`, async () => {
  const tabs = authTabs(), a = tabs.add(), b = tabs.add();
  try {
    await a.login(); tabs.deliver();
    const validation = await b.lastRequest();
    if (failure === 'network') validation.reject(new AxiosError('offline', 'ERR_NETWORK', validation.config));
    else b.respond(validation, { error: 'temporary failure' }, failure);
    await new Promise(r => setImmediate(r));
    assert.equal(b.store.getState().status, 'anonymous', 'this tab discards unvalidated private copies');
    assert.equal(a.localStorage.getItem('setori_token'), 'token-owner');
    assert.equal(tabs.events.length, 0, 'a temporary validation failure must not broadcast logout');
    assert.equal(a.store.getState().user?.id, 'owner');
    // Retry bootstrap is allowed, including after a transient failure.
    const retry = b.store.getState().init();
    b.respond(await b.lastRequest(), user()); await retry;
    assert.equal(b.store.getState().user?.id, 'owner');
  } finally { tabs.close(); }
});

for (const loginStarted of [false, true]) test(`queued logout event reads latest storage without cancelling a later local login (pending=${loginStarted})`, async () => {
  const tabs = authTabs(), a = tabs.add(), b = tabs.add();
  try {
    await a.login(); tabs.deliver();
    b.respond(await b.lastRequest(), user()); await new Promise(r => setImmediate(r));
    void a.store.getState().logout(); // queues a logout event for B
    void b.store.getState().logout(); // B sees the logout too, before delivery
    const login = b.store.getState().login('next', 'pw').then(() => null, error => error);
    const request = await b.lastRequest();
    if (!loginStarted) { b.respond(request, { token: 'token-next', user: user('next') }); assert.equal(await login, null); }
    const epoch = b.viewer.viewerID();
    tabs.deliver(); // old null event, latest storage is null (pending) or B's own new token
    assert.equal(b.viewer.viewerID(), epoch, 'duplicate notification must not reset the newer operation');
    if (loginStarted) { b.respond(request, { token: 'token-next', user: user('next') }); assert.equal(await login, null); }
    assert.equal(b.store.getState().user?.id, 'next');
  } finally { tabs.close(); }
});

test('interactive login superseding startup can fail without leaving status loading', async () => {
  const f = authFixture();
  try {
    f.localStorage.setItem('setori_token', 'saved-token');
    const init = f.store.getState().init(), me = await f.lastRequest();
    const login = f.store.getState().login('bad', 'pw').catch(error => error);
    f.respond(await f.lastRequest(), { error: 'bad credentials' }, 401); await login;
    f.respond(me, user()); await init;
    assert.equal(f.store.getState().status, 'anonymous');
    assert.equal(f.store.getState().user, null);
    const publicRequest = f.api.songApi.get('public');
    const request = await f.lastRequest();
    assert.equal(request.config.headers.has('Authorization'), false);
    f.respond(request, { id: 'public' }); await publicRequest;
  } finally { f.close(); }
});

for (const method of ['login', 'loginWithOAuthCode']) for (const change of ['logout', 'replacement']) {
  test(`${method}: an undelivered ${change} from another tab supersedes pending authentication`, async () => {
    const tabs = authTabs(), a = tabs.add(), b = tabs.add();
    try {
      await a.login(); tabs.deliver();
      b.respond(await b.lastRequest(), user()); await new Promise(r => setImmediate(r));
      const pending = (method === 'login' ? b.store.getState().login('next', 'pw') :
        b.store.getState().loginWithOAuthCode('code')).catch(error => error);
      const exchange = await b.lastRequest();
      if (change === 'logout') void a.store.getState().logout();
      else await a.login('other');
      b.respond(exchange, { token: 'token-next', user: user('next') });
      assert.equal((await pending)?.message, 'ログイン処理は取り消されました');
      assert.equal(b.store.getState().user, null);
      assert.equal(b.localStorage.getItem('setori_token'), change === 'logout' ? null : 'token-other');
      if (change === 'replacement') {
        b.respond(await b.lastRequest(), user('other')); await new Promise(r => setImmediate(r));
        assert.equal(b.store.getState().user?.id, 'other');
      }
      const epoch = b.viewer.viewerID();
      tabs.deliver();
      assert.equal(b.viewer.viewerID(), epoch, 'the queued event has already been handled');
      assert.equal(tabs.events.length, 0);
    } finally { tabs.close(); }
  });
}

for (const status of [200, 401, 500]) test(`auth identities ${status} remains protected by the viewer epoch`, async () => {
  const f = authFixture();
  try {
    await f.login();
    const response = f.api.authApi.oauthIdentities().then(data => ({ data }), error => ({ error }));
    const request = await f.lastRequest();
    void f.store.getState().logout(); await f.login('next');
    f.respond(request, { identities: [{ email: 'synthetic-private@example.invalid' }], error: 'private email' }, status);
    const result = await response;
    assert.equal(result.data, undefined);
    assert.equal(isCancel(result.error), true);
    assert.equal(result.error.response, undefined);
    assert.equal(f.store.getState().user?.id, 'next');
  } finally { f.close(); }
});

for (const status of [401, 500]) for (const verified of [false, true]) test(`late failed /me ${status} for the same shared token cannot erase a newer successful validation (verified=${verified})`, async () => {
  const f = authFixture();
  try {
    if (verified) await f.login();
    else f.localStorage.setItem('setori_token', 'saved-token');
    const token = f.localStorage.getItem('setori_token');
    const old = f.store.getState().init(), first = await f.lastRequest();
    const current = f.store.getState().init(), second = await f.lastRequest();
    f.respond(second, user()); await current;
    f.respond(first, { error: 'old validation failure' }, status); await old;
    assert.equal(f.store.getState().user?.id, 'owner');
    assert.equal(f.localStorage.getItem('setori_token'), token);
  } finally { f.close(); }
});

for (const method of ['login', 'loginWithOAuthCode']) test(`${method}: an old /me 401 clears the expired current viewer but lets pending new credentials finish`, async () => {
  const f = authFixture();
  try {
    await f.login();
    f.viewer.queryClient.setQueryData(['private'], ['synthetic-private']);
    const validation = f.store.getState().init(), me = await f.lastRequest();
    const login = (method === 'login' ? f.store.getState().login('next', 'pw') :
      f.store.getState().loginWithOAuthCode('new-code')).then(() => null, error => error);
    const exchange = await f.lastRequest();
    f.respond(me, { error: 'expired' }, 401); await validation;
    assert.equal(f.store.getState().user, null);
    assert.equal(f.viewer.queryClient.getQueryData(['private']), undefined);
    f.respond(exchange, { token: 'token-next', user: user('next') });
    assert.equal(await login, null);
    assert.equal(f.store.getState().user?.id, 'next');
  } finally { f.close(); }
});

test('duplicate same-account notifications keep both tabs authenticated and never revalidate in a loop', async () => {
  const tabs = authTabs(), a = tabs.add(), b = tabs.add();
  try {
    await a.login(); tabs.deliver();
    b.respond(await b.lastRequest(), user()); await new Promise(r => setImmediate(r));
    const epoch = b.viewer.viewerID(), count = b.requests.length;
    for (let i = 0; i < 3; i++) b.events.get('storage')({ key: 'setori_token', newValue: 'token-owner', storageArea: b.localStorage });
    assert.equal(b.store.getState().user?.id, 'owner');
    assert.equal(b.viewer.viewerID(), epoch);
    assert.equal(b.requests.length, count);
    assert.equal(a.store.getState().user?.id, 'owner');
    assert.equal(tabs.events.length, 0);
    // A genuine logout still clears B immediately, before the server responds.
    b.viewer.queryClient.setQueryData(['private'], ['synthetic-private']);
    void a.store.getState().logout(); tabs.deliver();
    assert.equal(b.store.getState().status, 'anonymous');
    assert.equal(b.viewer.queryClient.getQueryData(['private']), undefined);
    assert.equal(tabs.events.length, 0);
  } finally { tabs.close(); }
});

for (const status of [200, 401, 500]) test(`/me ${status} cannot revive an undelivered other-tab logout`, async () => {
  const f = authFixture();
  try {
    await f.login();
    f.viewer.queryClient.setQueryData(['private'], ['synthetic-private']);
    const init = f.store.getState().init(), request = await f.lastRequest();
    f.localStorage.removeItem('setori_token');
    f.respond(request, status === 200 ? user() : { error: 'old failure' }, status); await init;
    assert.equal(f.store.getState().user, null);
    assert.equal(f.localStorage.getItem('setori_token'), null);
    assert.equal(f.viewer.queryClient.getQueryData(['private']), undefined);
  } finally { f.close(); }
});

test('cold startup 401 deletes only the invalid saved token and rejects old private responses', async () => {
  const f = authFixture();
  try {
    f.localStorage.setItem('setori_token', 'invalid-token');
    const init = f.store.getState().init(), me = await f.lastRequest();
    const pending = f.api.streamApi.get('private').then(data => ({ data }), error => ({ error }));
    const request = await f.lastRequest();
    f.respond(me, { error: 'expired' }, 401); await init;
    assert.equal(f.localStorage.getItem('setori_token'), null);
    assert.equal(f.store.getState().status, 'anonymous');
    f.respond(request, { performances: ['synthetic-private'] });
    const result = await pending;
    assert.equal(result.data, undefined);
    assert.equal(isCancel(result.error), true);
  } finally { f.close(); }
});

test('storage ignores unrelated keys; clear() erases this tab without writing back', async () => {
  const f = authFixture();
  try {
    await f.login();
    const epoch = f.viewer.viewerID(), count = f.requests.length;
    for (const event of [{ key: 'theme', storageArea: f.localStorage }, { key: 'setori_token', storageArea: {} }]) {
      f.events.get('storage')(event);
    }
    assert.equal(f.viewer.viewerID(), epoch);
    assert.equal(f.requests.length, count);
    assert.equal(f.store.getState().user?.id, 'owner');
    f.localStorage.removeItem('setori_token');
    f.events.get('storage')({ key: null, storageArea: f.localStorage });
    assert.equal(f.store.getState().status, 'anonymous');
    assert.equal(f.requests.length, count, 'remote logout need not call the logout API again');
  } finally { f.close(); }
});

test('logout clears local Bearer, observed cache, and player immediately without a server answer', async () => {
  const f = authFixture(); let unsubscribe;
  try {
    await f.login();
    const player = runtime({ '../queryClient': f.viewer }).load('store/player.ts').usePlayerStore;
    player.getState().playTracks([{ videoId: 'private', songName: 'synthetic-private-song' }]);
    f.viewer.queryClient.setQueryData(['stream', 'private'], { name: 'synthetic-private-song' });
    const observer = new QueryObserver(f.viewer.queryClient, { queryKey: ['stream', 'private'], queryFn: () => new Promise(() => {}) });
    unsubscribe = observer.subscribe(() => {});
    const loggingOut = f.store.getState().logout();
    assert.equal(f.store.getState().status, 'anonymous');
    assert.equal(f.localStorage.getItem('setori_token'), null);
    assert.equal(observer.getCurrentResult().data, undefined);
    assert.equal(player.getState().queue.length, 0);
    const logout = (await f.lastRequest());
    assert.equal(logout.config.headers.get('Authorization'), 'Bearer token-owner');
    const publicRequest = f.api.songApi.get('public');
    assert.equal((await f.lastRequest()).config.headers.has('Authorization'), false, '静的 token へ戻らない');
    f.respond((await f.lastRequest()), { name: 'public' }); await publicRequest;
    await f.login('next'); f.respond(logout, {}); await loggingOut;
    assert.equal(f.store.getState().user?.id, 'next', '古い logout は新しいログインを消さない');
  } finally { unsubscribe?.(); f.close(); }
});

for (const method of ['login', 'loginWithOAuthCode', 'init']) for (const failure of [false, true]) {
  test(`${method}: an older ${failure ? 'failure' : 'success'} cannot restore or erase a later session`, async () => {
    const f = authFixture();
    try {
      if (method === 'init') f.localStorage.setItem('setori_token', 'old-token');
      const old = method === 'login' ? f.store.getState().login('old', 'pw') :
        method === 'init' ? f.store.getState().init() : f.store.getState().loginWithOAuthCode('one-time-code');
      const settled = old.catch(() => {}), request = (await f.lastRequest());
      void f.store.getState().logout(); await f.login('next');
      f.respond(request, failure ? { error: 'old failure' } : method === 'init' ? user('old') : { token: 'old-token', user: user('old') }, failure ? 500 : 200);
      await settled;
      assert.equal(f.store.getState().user?.id, 'next');
      assert.equal(f.localStorage.getItem('setori_token'), 'token-next');
    } finally { f.close(); }
  });
}

test('logout cancels an unfinished login before /me establishes any viewer', async () => {
  const f = authFixture();
  try {
    const login = f.store.getState().login('old', 'pw').catch(() => {}), request = (await f.lastRequest());
    await f.store.getState().logout();
    f.respond(request, { token: 'old-token', user: user('old') }); await login;
    assert.equal(f.store.getState().user, null);
    assert.equal(f.localStorage.getItem('setori_token'), null);
  } finally { f.close(); }
});

for (const status of [200, 403, 500, 401]) test(`old ${status} response cannot reveal private data, even when the same viewer returns`, async () => {
  const f = authFixture();
  try {
    await f.login();
    const pending = f.api.streamApi.get('private').then((data) => ({ data }), (error) => ({ error }));
    const old = (await f.lastRequest()), before = f.viewer.viewerID();
    f.viewer.applyViewerChange(null); f.viewer.applyViewerChange('owner|content:edit,restricted:view');
    assert.equal(f.viewer.sameViewer(before), false);
    f.respond(old, { error: 'synthetic-private-reason', performances: [{ song_name: 'synthetic-private-song' }] }, status);
    const result = await pending;
    assert.equal(result.data, undefined);
    assert.equal(isCancel(result.error), true);
    assert.equal(result.error.response, undefined);
    assert.equal(result.error.config, undefined);
    assert.equal(f.store.getState().user?.id, 'owner');
  } finally { f.close(); }
});

test('401 from the current session logs out; 401 from a superseded token does not', async () => {
  const f = authFixture();
  try {
    await f.login();
    const old = f.api.songApi.get('one').catch(() => {}), request = (await f.lastRequest());
    const login = f.store.getState().login('owner', 'pw');
    f.respond((await f.lastRequest()), { token: 'new-token', user: user() }); await login;
    f.respond(request, { error: 'expired' }, 401); await old;
    assert.equal(f.store.getState().token, 'new-token');
    const current = f.api.songApi.get('one').catch(() => {});
    f.respond((await f.lastRequest()), { error: 'expired' }, 401); await current;
    assert.equal(f.store.getState().status, 'anonymous');
  } finally { f.close(); }
});

for (const nextToken of [null, 'other-tab-token']) test(`storage event (${nextToken}) erases copies before validating the other session`, async () => {
  const f = authFixture();
  try {
    await f.login(); f.viewer.queryClient.setQueryData(['suggestions', 'mine'], ['private']);
    let copies = ['private']; const remove = f.viewer.onViewerChange(() => { copies = []; });
    if (nextToken) f.localStorage.setItem('setori_token', nextToken); else f.localStorage.removeItem('setori_token');
    assert.ok(f.events.has('storage'), '別タブの失効を購読する');
    f.events.get('storage')({ key: 'setori_token', storageArea: f.localStorage });
    assert.equal(f.store.getState().user, null);
    assert.equal(f.viewer.queryClient.getQueryData(['suggestions', 'mine']), undefined);
    assert.deepEqual(copies, []);
    if (nextToken) {
      assert.equal(f.store.getState().status, 'loading');
      f.respond((await f.lastRequest()), user('other')); await new Promise((r) => setImmediate(r));
      assert.equal(f.store.getState().user?.id, 'other');
    }
    remove();
  } finally { f.close(); }
});

for (const endpoint of ['login', 'secrets']) test(`failed ${endpoint} exposes useful status/data but no request credentials in logs`, async () => {
  const f = authFixture();
  try {
    await f.login();
    const promise = endpoint === 'login' ? f.api.authApi.login('name', 'synthetic-password') :
      f.api.integrationSettingsApi.update({ secrets: { ytdlp_cookies: 'synthetic-cookie-secret' } });
    const settled = promise.catch((error) => error), request = (await f.lastRequest());
    assert.equal(request.config.headers.get('Authorization'), 'Bearer token-owner');
    assert.ok(request.config.data.includes(endpoint === 'login' ? 'synthetic-password' : 'synthetic-cookie-secret'));
    f.respond(request, { error: '設定を保存できません', detail: 'useful' }, 409);
    const error = await settled;
    assert.equal(isAxiosError(error), true);
    assert.equal(error.response.status, 409);
    assert.equal(error.response.data.detail, 'useful');
    assert.equal(error.message, '設定を保存できません');
    assert.equal(error.config, undefined); assert.equal(error.request, undefined);
    assert.equal(error.response.config.headers.get('Authorization'), undefined);
    assert.equal(error.response.request, undefined);
    assert.equal(JSON.stringify(error).includes('synthetic-'), false);
    assert.equal(JSON.stringify(error).includes('token-owner'), false);
  } finally { f.close(); }
});

test('resetQueries erases active, inactive and pending private data, then refetches public data', async () => {
  const { load } = runtime(), v = load('queryClient.ts'), client = v.queryClient;
  const old = deferred(), fresh = deferred(); let count = 0, unsubscribe;
  try {
    v.applyViewerChange('owner|restricted:view');
    client.setQueryData(['stream', 'private'], { song: 'private' });
    client.setQueryData(['suggestions', 'mine'], { title: 'private' });
    const observer = new QueryObserver(client, { queryKey: ['stream', 'private'], staleTime: 0,
      queryFn: () => ++count === 1 ? old.promise : fresh.promise });
    unsubscribe = observer.subscribe(() => {}); v.applyViewerChange(null);
    assert.equal(observer.getCurrentResult().data, undefined);
    assert.equal(client.getQueryData(['suggestions', 'mine']), undefined);
    old.resolve({ song: 'late-private' }); await new Promise((r) => setImmediate(r));
    assert.equal(client.getQueryData(['stream', 'private']), undefined);
    assert.equal(count, 2);
    fresh.resolve({ song: 'public' }); await new Promise((r) => setImmediate(r));
    assert.equal(observer.getCurrentResult().data.song, 'public');
  } finally { unsubscribe?.(); client.clear(); }
});

// hooks の保存域だけを用意し、実 hook/component のハンドラと再レンダーを動かす。
function hookFixture(api = {}, extra = {}, globals = {}) {
  const values = [], refs = [], effects = [], mutations = [], toasts = [], calls = [], timers = [], intervals = [], cleared = new Set();
  let stateIndex = 0, refIndex = 0, effectIndex = 0, dirty = false;
  const react = {
    ...require('react'),
    useState: (initial) => {
      const i = stateIndex++;
      if (!(i in values)) values[i] = typeof initial === 'function' ? initial() : initial;
      return [values[i], (next) => {
        const value = typeof next === 'function' ? next(values[i]) : next;
        if (!Object.is(values[i], value)) { values[i] = value; dirty = true; }
      }];
    },
    useSyncExternalStore: (_subscribe, get) => get(),
    useCallback: (fn) => fn, useMemo: (fn) => fn(),
    useRef: (value) => { const i = refIndex++; return refs[i] ??= { current: value }; },
    useEffect: (fn, deps) => {
      const i = effectIndex++, before = effects[i];
      if (!before || deps.some((dep, j) => !Object.is(dep, before.deps[j]))) {
        before?.cleanup?.(); effects[i] = { deps, pending: fn };
      }
    },
  };
  const player = { editing: null, queue: [], index: 0, setEditing: (next) => { player.editing = next; }, jumpTo() {}, updateTrackTiming: (...args) => calls.push(['timing', ...args]) };
  const client = { invalidateQueries: (...args) => calls.push(['invalidate', ...args]) };
  let queryData, props = {};
  const toast = { useToast: () => ({ showToast: (...args) => toasts.push(args) }) };
  const deps = {
    react,
    '@tanstack/react-query': { ...require('@tanstack/react-query'), useQueryClient: () => client, useQuery: () => ({ data: queryData }),
      useMutation: (options) => { mutations.push(options); return { isPending: false, mutate() {} }; } },
    '../api/client': api, '../../api/client': api,
    '../store/player': { usePlayerStore: Object.assign((select) => select(player), { getState: () => player }), performancesToTracks: (items) => items },
    '../store/auth': { useAuthStore: (select) => select({ user: user('owner', props.permissions) }),
      hasPermission: (u, perm) => u.permissions.includes(perm), PERM: { CONTENT_EDIT: 'content:edit' } },
    './usePlayerTime': { usePlayerTime: () => 0 },
    './youtubePlayerControl': { playerGetCurrentTime: () => 0, playerGetDuration: () => 0, playerPause() {}, playerSeekTo() {} },
    './ui/ToastContext': toast, '../components/ui/ToastContext': toast, '../../components/ui/ToastContext': toast,
    'react-router-dom': { useParams: () => ({ id: 'playlist' }), useLocation: () => ({ pathname: '/' }), useSearchParams: () => [new URLSearchParams()], useNavigate: () => () => {}, Link: () => null },
    ...extra,
  };
  const rt = runtime(deps, { setInterval: () => 0, clearInterval() {}, setTimeout: (fn) => { timers.push(fn); return timers.length; }, clearTimeout() {},
    window: { setInterval: (fn) => { intervals.push(fn); return intervals.length; }, clearInterval: (id) => cleared.add(id), location: { origin: 'http://localhost' }, open: (...args) => api.windowOpen?.(...args), addEventListener() {}, removeEventListener() {} },
    document: { addEventListener() {}, removeEventListener() {} },
    navigator: { clipboard: { writeText: () => api.clipboard?.() ?? Promise.resolve() } },
    ...globals,
  });
  const viewer = rt.load('queryClient.ts'); viewer.applyViewerChange('owner|content:edit,restricted:view');
  let component;
  return { ...rt, viewer, values, player, mutations, toasts, calls, timers, intervals, cleared,
    setQuery(value) { queryData = value; },
    mount(file, name = 'default', nextProps = {}) { component = rt.load(file)[name]; props = nextProps; return this.render(); },
    mountFunction(fn, nextProps = {}) { component = fn; props = nextProps; return this.render(); },
    render() {
      let result;
      for (let i = 0; i < 10; i++) {
        dirty = false; stateIndex = refIndex = effectIndex = 0; mutations.length = 0;
        result = component(props);
        if (!dirty) break;
        assert.ok(i < 9, 'state adjustment must converge');
      }
      for (const effect of effects) if (effect.pending) {
        const fn = effect.pending; effect.pending = null; effect.cleanup = fn();
      }
      return result;
    },
    close() { for (const effect of effects) effect.cleanup?.(); viewer.queryClient.clear(); },
  };
}
function elements(tree, predicate) {
  const found = [];
  function visit(node) {
    if (Array.isArray(node)) { node.forEach(visit); return; }
    if (!node || typeof node !== 'object' || !node.props) return;
    if (predicate(node)) found.push(node);
    visit(node.props.children);
  }
  visit(tree); return found;
}
const performance = { id: 'perf', song_id: 'song', song_name: 'synthetic-private-song', original_artist: 'artist', start_seconds: 10, end_seconds: 20, singers: [{ id: 'singer' }] };

for (const change of [null, 'owner|content:edit']) test(`missing report draft is erased on viewer change (${change}) and does not migrate to another stream`, () => {
  const f = hookFixture({ streamApi: {}, performanceApi: {}, songApi: {}, suggestionApi: {} });
  try {
    f.setQuery({ performances: [] }); f.player.editing = { streamId: 'private', performanceId: null };
    const report = f.mount('components/usePerformanceReport.ts', 'usePerformanceReport');
    report.patch({ songName: 'synthetic-private-song', artist: 'private artist' }); report.setNote('private note');
    assert.equal(f.render().draft.songName, 'synthetic-private-song');
    f.viewer.applyViewerChange(change); f.player.editing = { streamId: 'public', performanceId: null };
    const next = f.render();
    assert.equal(next.draft.songName, ''); assert.equal(next.note, '');
    const before = JSON.stringify(f.values);
    report.patch({ songName: 'late-private-song' }); report.setNote('late-private-note');
    assert.equal(JSON.stringify(f.values), before, '古い setter は保存域にも書き込まない');
    assert.equal(f.render().draft.songName, ''); assert.equal(f.render().note, '');
  } finally { f.close(); }
});

for (const canEdit of [true, false]) for (const phase of ['missing', 'singers', 'song', 'artist']) {
  test(`report ${phase} (${canEdit ? 'editor' : 'contributor'}) stops before the next write or closing a new viewer's form`, async () => {
    const pending = deferred(), requests = [];
    const request = (method) => (...args) => { requests.push([method, ...args]); return pending.promise; };
    const f = hookFixture({ streamApi: {}, performanceApi: { update: request('performance') },
      songApi: { update: request('song') }, suggestionApi: { create: request('create'), approve: request('approve') } });
    try {
      f.setQuery({ performances: [performance] });
      f.player.editing = { streamId: 'private', performanceId: phase === 'missing' ? null : 'perf' };
      let report = f.mount('components/usePerformanceReport.ts', 'usePerformanceReport', { permissions: canEdit ? ['content:edit'] : [] });
      report.patch(phase === 'singers' ? { singerIds: ['other'], artist: 'other artist' } :
        phase === 'song' ? { songId: '', songName: 'unregistered' } : phase === 'artist' ? { artist: 'other artist' } : { songName: 'missing' });
      report = f.render(); const submitting = report.handleSubmit();
      assert.equal(requests.length, 1, '陽性：最初の保存は実行される');
      f.viewer.applyViewerChange(null); f.player.editing = { streamId: 'public', performanceId: null }; f.render();
      pending.resolve({ id: 'created' }); await submitting;
      assert.equal(requests.length, 1, '次の保存・自己承認を新しい資格情報で送らない');
      assert.equal(f.player.editing?.streamId, 'public');
      assert.equal(f.toasts.length, 0);
    } finally { f.close(); }
  });
}

test('same-viewer report still completes all writes and refreshes results', async () => {
  const requests = [];
  const f = hookFixture({ streamApi: {}, performanceApi: { update: async (...args) => requests.push(['performance', ...args]) },
    songApi: { update: async (...args) => requests.push(['song', ...args]) }, suggestionApi: {} });
  try {
    f.setQuery({ performances: [performance] }); f.player.editing = { streamId: 'private', performanceId: 'perf' };
    const initial = f.mount('components/usePerformanceReport.ts', 'usePerformanceReport');
    initial.patch({ singerIds: ['other'], artist: 'other artist' }); await f.render().handleSubmit();
    assert.deepEqual(requests.map(([method]) => method), ['performance', 'song']);
    assert.equal(f.player.editing, null); assert.equal(f.toasts[0][1], 'success'); assert.ok(f.calls.length > 0);
  } finally { f.close(); }
});

for (const result of ['resolve', 'reject']) test(`late clipboard ${result} cannot redisplay a private playlist share link`, async () => {
  const pending = deferred(), f = hookFixture({ playlistApi: {}, clipboard: () => pending.promise });
  try {
    f.setQuery({ id: 'playlist', name: 'public playlist', share_slug: 'synthetic-private-slug', is_owner: true, visibility: 'unlisted' });
    const tree = f.mount('pages/PlaylistDetailPage.tsx');
    const button = elements(tree, (n) => n.type === 'button' && n.props.onClick?.name === 'copyShareLink')[0];
    assert.ok(button); const copying = button.props.onClick();
    f.viewer.applyViewerChange(null);
    if (result === 'resolve') pending.resolve(); else pending.reject(new Error('clipboard denied'));
    await copying; assert.equal(f.toasts.length, 0);
  } finally { f.close(); }
});

test('playlist editing draft and edit mode disappear across viewer changes', () => {
  const f = hookFixture({ playlistApi: {} });
  try {
    f.setQuery({ id: 'playlist', name: 'public playlist', description: '', is_owner: true });
    let tree = f.mount('pages/PlaylistDetailPage.tsx');
    const edit = elements(tree, (n) => n.type === 'button' && n.props.title === '名前と説明を編集')[0];
    assert.ok(edit); edit.props.onClick(); tree = f.render();
    const input = elements(tree, (n) => n.type === 'input')[0]; assert.ok(input);
    input.props.onChange({ target: { value: 'private unsaved name' } });
    assert.equal(elements(f.render(), (n) => n.type === 'input')[0].props.value, 'private unsaved name');
    f.viewer.applyViewerChange('other|content:edit');
    assert.equal(elements(f.render(), (n) => n.type === 'form').length, 0);
  } finally { f.close(); }
});

test('unsaved integration keys and cookies are erased before the next viewer sees the form', () => {
  const f = hookFixture({ integrationSettingsApi: {} });
  try {
    f.setQuery({ encryption_enabled: true, secrets: {}, plain: {}, plain_from_env: {} });
    const tree = f.mount('pages/admin/IntegrationSettingsSection.tsx');
    const input = elements(tree, (n) => n.type === 'input' && n.props.type === 'password')[0];
    const textarea = elements(tree, (n) => n.type === 'textarea')[0];
    input.props.onChange({ target: { value: 'synthetic-api-key' } }); textarea.props.onChange({ target: { value: 'synthetic-cookie' } });
    assert.equal(elements(f.render(), (n) => n.type === 'textarea')[0].props.value, 'synthetic-cookie');
    f.viewer.applyViewerChange('other|*');
    const next = f.render();
    assert.equal(elements(next, (n) => n.type === 'textarea')[0].props.value, '');
    assert.equal(elements(next, (n) => n.type === 'input' && n.props.type === 'password')[0].props.value, '');
    textarea.props.onChange({ target: { value: 'late-cookie' } });
    assert.equal(elements(f.render(), (n) => n.type === 'textarea')[0].props.value, '');
  } finally { f.close(); }
});

for (const method of ['login', 'loginWithOAuthCode']) test(`${method}: only the latest pending authentication can establish a session`, async () => {
  const f = authFixture();
  try {
    const call = (id) => method === 'login' ? f.store.getState().login(id, 'pw') : f.store.getState().loginWithOAuthCode(id);
    const old = call('old').catch(() => {}), first = (await f.lastRequest());
    const next = call('next'), second = (await f.lastRequest());
    f.respond(first, { token: 'old-token', user: user('old') }); await old;
    assert.equal(f.store.getState().user, null, 'まだ最新の認証が返っていない');
    f.respond(second, { token: 'next-token', user: user('next') }); await next;
    assert.equal(f.store.getState().user?.id, 'next');
  } finally { f.close(); }
});


test('anonymous API requests never attach a credential embedded in VITE_API_TOKEN', async () => {
  const f = authFixture();
  try {
    const promise = f.api.songApi.get('public'), request = await f.lastRequest();
    assert.equal(request.config.headers.has('Authorization'), false);
    f.respond(request, { id: 'public' }); await promise;
  } finally { f.close(); }
});

for (const value of ['javascript://music.apple.com/%0Aalert(1)', 'javascript:alert(1)', 'JaVa\nScript:alert(1)', 'data:text/html,<script>bad</script>', '//evil.test/path',
  'https://music.apple.com.evil.test/song/1', 'https://music.apple.com@evil.test/', 'https://attacker@music.apple.com/song/1']) {
  test(`iTunes native navigation rejects ${value}`, () => {
    const { itunesTrackURL } = runtime().load('utils/itunesURL.ts');
    assert.equal(itunesTrackURL(value, 123), 'https://music.apple.com/song/123');
  });
}
for (const value of ['https://music.apple.com/jp/album/example/123?i=456', 'https://itunes.apple.com/jp/album/example/id123?i=456']) {
  test(`iTunes navigation retains legitimate catalog URL ${value}`, () => {
    const { itunesTrackURL } = runtime().load('utils/itunesURL.ts'); assert.equal(itunesTrackURL(value, 123), value);
  });
}

test('adopting a suggestion stops before rejecting siblings under a different viewer', async () => {
  const pending = deferred(), requests = [];
  const f = hookFixture({ suggestionApi: {
    approve: (...args) => { requests.push(['approve', ...args]); return pending.promise; },
    batchReview: async (...args) => { requests.push(['batch', ...args]); },
  }, tagApi: {}, commentApi: {}, streamApi: {} }, { '../components/QueueAddButton': { default: () => null } });
  try {
    f.setQuery({ groups: [], pagination: { page: 1, total_pages: 1, total: 0 }, suggestions: [] });
    f.mount('pages/admin/SuggestionsPage.tsx');
    const action = f.mutations[0], promise = action.mutationFn({ pick: { id: 'one' }, siblings: [{ id: 'two' }] });
    assert.equal(requests.length, 1); f.viewer.applyViewerChange(null); pending.resolve({});
    action.onSuccess(await promise);
    assert.equal(requests.length, 1); assert.equal(f.toasts.length, 0);
  } finally { f.close(); }
});

for (const canEdit of [false, true]) test(`timing ${canEdit ? 'update' : 'suggestion'} completion cannot notify or alter the new viewer`, async () => {
  const pending = deferred();
  const f = hookFixture({ performanceApi: { update: () => pending.promise }, suggestionApi: { create: () => pending.promise } });
  try {
    const timing = f.mount('components/usePerformanceTiming.ts', 'usePerformanceTiming', { permissions: canEdit ? ['content:edit'] : [] });
    const promise = timing.submit({ performanceId: 'perf', songName: 'synthetic-private-song', start: 10, end: 20 }, { end: 25 });
    f.viewer.applyViewerChange(null); pending.resolve({ id: 'suggestion' });
    assert.equal(await promise, false); assert.equal(f.toasts.length, 0); assert.equal(f.calls.length, 0);
  } finally { f.close(); }
});

test('timing undo cannot send a stale action or change a queue after a viewer switch', async () => {
  const pending = deferred(), requests = [];
  const f = hookFixture({ performanceApi: { update: (...args) => { requests.push(args); return requests.length === 1 ? Promise.resolve({}) : pending.promise; } }, suggestionApi: {} });
  try {
    const timing = f.mount('components/usePerformanceTiming.ts', 'usePerformanceTiming');
    assert.equal(await timing.submit({ performanceId: 'perf', songName: 'private', start: 10, end: 20 }, { end: 25 }), true);
    const undo = f.toasts[0][2].onClick, running = undo(), before = f.calls.length;
    f.viewer.applyViewerChange(null); pending.resolve({}); await running;
    assert.equal(f.calls.length, before); assert.equal(f.toasts.length, 1);
    await undo(); assert.equal(requests.length, 2, '旧トーストの action を直接呼んでも送信しない');
  } finally { f.close(); }
});

test('a saved showToast callback cannot recreate a private notification after reset', () => {
  const messages = [];
  const rt = runtime({ react: { ...require('react'), useContext: () => ({ showToast: (...args) => messages.push(args), removeToast() {} }),
    useSyncExternalStore: (_subscribe, get) => get(), useCallback: (fn) => fn } });
  const v = rt.load('queryClient.ts');
  try {
    v.applyViewerChange('owner|restricted:view');
    const toast = rt.load('components/ui/ToastContext.ts').useToast();
    toast.showToast('private'); assert.equal(messages.length, 1);
    v.applyViewerChange(null); toast.showToast('late private');
    assert.equal(messages.length, 1);
    const publicToast = rt.load('components/ui/ToastContext.ts').useToast(); publicToast.showToast('public');
    assert.equal(messages.at(-1)[0], 'public');
  } finally { v.queryClient.clear(); }
});

for (const cached of [true, false]) test(`SongDetailPage validates the URL at actual window.open (cached=${cached})`, async () => {
  const opened = [];
  const f = hookFixture({ songApi: {}, suggestionApi: {}, itunesApi: { queryById: async () => ({ track_view_url: 'javascript:alert(1)' }) },
    windowOpen: (...args) => opened.push(args) });
  try {
    f.setQuery({ song: { id: 'song', name: 'test', original_artist: 'artist', itunes_ids: [{ itunes_id: 123, is_primary: true }] },
      performances: [], pagination: { page: 1, total: 0, total_pages: 0 } });
    let tree = f.mount('pages/SongDetailPage.tsx');
    if (cached) { await new Promise((r) => setImmediate(r)); tree = f.render(); }
    const link = elements(tree, (n) => n.type === 'a' && n.props.title === 'Apple Musicで開く')[0];
    assert.ok(link); link.props.onClick({ preventDefault() {} }); await new Promise((r) => setImmediate(r));
    assert.deepEqual(opened, [['https://music.apple.com/song/123', '_blank', 'noopener,noreferrer']]);
  } finally { f.close(); }
});

for (const changed of [true, false]) test(`QueueAddButton does not attach old selections to a newly authenticated playlist (changed=${changed})`, async () => {
  const pending = deferred(), requests = [];
  const f = hookFixture({ playlistApi: {
    create: (...args) => { requests.push(['create', ...args]); return pending.promise; },
    addItems: async (...args) => { requests.push(['addItems', ...args]); return { added: 1, skipped: 0 }; },
  } });
  try {
    f.mount('components/QueueAddButton.tsx', 'default', { track: { songName: 'synthetic-private-song', performanceId: 'private-perf' } });
    const mutation = f.mutations[0], promise = mutation.mutationFn({ name: 'new playlist' });
    if (changed) f.viewer.applyViewerChange('other|content:edit');
    pending.resolve({ id: 'playlist', name: 'new playlist' });
    mutation.onSuccess(await promise);
    assert.equal(requests.length, changed ? 1 : 2);
    assert.equal(f.toasts.length, changed ? 0 : 1);
  } finally { f.close(); }
});


test('only the latest pending /me may establish permissions', async () => {
  const f = authFixture();
  try {
    f.localStorage.setItem('setori_token', 'saved-token');
    const old = f.store.getState().init(), first = await f.lastRequest();
    const next = f.store.getState().init(), second = await f.lastRequest();
    f.respond(first, user('owner')); await old;
    assert.equal(f.store.getState().user, null);
    f.respond(second, user('owner', ['content:edit'])); await next;
    assert.deepEqual(JSON.parse(JSON.stringify(f.store.getState().user?.permissions ?? null)), ['content:edit']);
  } finally { f.close(); }
});

test('failed startup validation discards private results requested while /me was pending', async () => {
  const f = authFixture();
  try {
    f.localStorage.setItem('setori_token', 'saved-token');
    const init = f.store.getState().init(), me = await f.lastRequest();
    const stream = f.api.streamApi.get('private').then(data => ({ data }), error => ({ error })), request = await f.lastRequest();
    f.respond(me, { error: 'temporary failure' }, 500); await init;
    f.respond(request, { performances: ['private'] });
    const result = await stream;
    assert.equal(result.data, undefined); assert.equal(isCancel(result.error), true);
  } finally { f.close(); }
});

test('a request binds its Bearer and viewer at invocation, before another synchronous session change', async () => {
  const f = authFixture();
  try {
    await f.login();
    const request = f.api.songApi.update('song', { name: 'private' }).catch(error => error);
    f.api.setAuthToken('other-token'); f.viewer.applyViewerChange('other|content:edit');
    const sent = await f.lastRequest();
    assert.equal(sent.config.headers.get('Authorization'), 'Bearer token-owner');
    f.respond(sent, { name: 'private' }); assert.equal(isCancel(await request), true);
  } finally { f.close(); }
});

for (const file of ['SongSearchInput', 'SingerSearchInput', 'ArtistSearchInput']) test(`${file}: an already queued debounce cannot send the previous viewer's input`, async () => {
  const requests = [];
  const record = (...args) => { requests.push(args); return Promise.resolve({ songs: [], artists: [], results: [] }); };
  const f = hookFixture({ songApi: { list: record }, singerApi: { search: (...args) => { requests.push(args); return Promise.resolve([]); } }, artistApi: { list: record }, itunesApi: { search: record } });
  try {
    const tree = f.mount(`components/${file}.tsx`, 'default', { value: '', onChange() {}, onSelectSong() {}, onSelectArtist() {}, onSelectSinger() {} });
    elements(tree, (n) => n.type === 'input')[0].props.onChange({ target: { value: 'synthetic-private-query' } });
    f.render(); const timer = f.timers.at(-1); assert.equal(typeof timer, 'function');
    f.viewer.applyViewerChange(null); await timer();
    assert.equal(requests.length, 0);
  } finally { f.close(); }
});

for (const pendingPoll of [false, true]) test(`Google Drive device authorization stops and cannot disclose an old email (pending=${pendingPoll})`, async () => {
  const pending = deferred(), requests = [];
  const f = hookFixture({ backupApi: { gdriveAuthPoll: (...args) => { requests.push(args); return pending.promise; } } });
  try {
    f.setQuery({ gdrive: { configured: true, connected: false }, backups: [] });
    const tree = f.mount('pages/admin/BackupPage.tsx');
    const section = elements(tree, (n) => typeof n.type === 'function' && n.type.name === 'GoogleDriveSection')[0];
    assert.ok(section); f.mountFunction(section.type, { onRestoreRequest() {} });
    f.mutations[0].onSuccess({ device_code: 'synthetic-device-secret', user_code: 'code', verification_url: 'https://google.com/device', expires_in: 300, interval: 5 });
    assert.equal(f.intervals.length, 1); const poll = f.intervals[0];
    const polling = pendingPoll ? poll() : null;
    f.viewer.applyViewerChange(null);
    assert.equal(f.cleared.has(1), true, '権限変更で interval を即座に解除する');
    if (pendingPoll) { pending.resolve({ connected: true, gdrive: { email: 'old@example.test' } }); await polling; }
    else { pending.resolve({ connected: false }); await poll(); } // 既にイベントキューへ入った callback も送らない
    assert.equal(requests.length, pendingPoll ? 1 : 0); assert.equal(f.toasts.length, 0);
  } finally { f.close(); }
});


for (const changed of [false, true]) test(`reading file contents are never imported as a different viewer (changed=${changed})`, async () => {
  const pending = deferred(), requests = [];
  const result = { artists_updated: 1, songs_updated: 1, skipped: 0 };
  const f = hookFixture({ readingApi: { importJSON: async (...args) => { requests.push(args); return result; } }, artistApi: {} },
    { '../../hooks/useTaskProgress': { useTaskProgress: () => ({}) } });
  try {
    f.mount('pages/admin/ReadingsPage.tsx');
    const action = f.mutations[2], running = action.mutationFn({ name: 'readings.json', text: () => pending.promise });
    if (changed) f.viewer.applyViewerChange('other|content:edit');
    pending.resolve(JSON.stringify({ songs: [{ name: 'synthetic-private-input' }] }));
    action.onSuccess(await running);
    assert.equal(requests.length, changed ? 0 : 1);
    assert.equal(f.toasts.length, changed ? 0 : 1);
  } finally { f.close(); }
});

test('reading export cannot download a previously completed blob for a new viewer', async () => {
  let downloads = 0;
  const f = hookFixture({ readingApi: { exportBlob: async () => 'synthetic-private-blob' }, artistApi: {} },
    { '../../hooks/useTaskProgress': { useTaskProgress: () => ({}) } }, {
      URL: class extends URL { static createObjectURL() { downloads++; return 'blob:fixture'; } static revokeObjectURL() {} },
      document: { createElement: () => ({ click() {}, remove() {} }), body: { appendChild() {} } },
    });
  try {
    f.mount('pages/admin/ReadingsPage.tsx');
    const action = f.mutations[1], result = await action.mutationFn({ filter: 'all', format: 'json' });
    f.viewer.applyViewerChange(null); action.onSuccess(result);
    assert.equal(f.toasts.length, 0); assert.equal(downloads, 0);
  } finally { f.close(); }
});

for (const changed of [false, true]) test(`backup blob cannot download after a viewer switch (changed=${changed})`, async () => {
  const pending = deferred(), saves = [];
  const f = hookFixture({ backupApi: { downloadBlob: () => pending.promise } }, {}, {
    URL: class extends URL { static createObjectURL(blob) { saves.push(blob); return 'blob:fixture'; } static revokeObjectURL() {} },
    document: { createElement: () => ({ click() {} }) },
  });
  try {
    f.setQuery({ backups: [{ name: 'fixture.sql', size: 1024, created_at: '2026-01-01T00:00:00Z' }], gdrive: {}, settings: {} });
    const tree = f.mount('pages/admin/BackupPage.tsx');
    const link = elements(tree, n => n.type === 'button' && n.props.title === 'ダウンロード')[0];
    assert.ok(link); const running = link.props.onClick();
    if (changed) f.viewer.applyViewerChange(null);
    pending.resolve('synthetic-private-blob'); await running;
    assert.equal(saves.length, changed ? 0 : 1);
  } finally { f.close(); }
});


test('reading import completion cannot update the next viewer', async () => {
  const pending = deferred();
  const f = hookFixture({ readingApi: { importJSON: () => pending.promise }, artistApi: {} },
    { '../../hooks/useTaskProgress': { useTaskProgress: () => ({}) } });
  try {
    f.mount('pages/admin/ReadingsPage.tsx');
    const action = f.mutations[2], running = action.mutationFn({ name: 'readings.json', text: async () => '{}' });
    await new Promise(r => setImmediate(r));
    f.viewer.applyViewerChange(null); pending.resolve({ artists_updated: 1, songs_updated: 1, skipped: 0 });
    action.onSuccess(await running);
    assert.equal(f.toasts.length, 0); assert.equal(f.calls.length, 0);
  } finally { f.close(); }
});

// 起動時は store.token がまだ null でも、client は保存済み Bearer を送る。
// 私有 API でその Bearer の失効が判明したあとに、先に処理された /me の
// 成功応答が遅れて届いても、その認証結果を復活させない。
for (const verified of [false, true]) test(`a private API 401 during bootstrap invalidates its Bearer before a late /me success (verified=${verified})`, async () => {
  const f = authFixture();
  try {
    if (verified) await f.login();
    f.localStorage.setItem('setori_token', 'saved-token');
    const validation = f.store.getState().init(), me = await f.lastRequest();
    const identities = f.api.authApi.oauthIdentities().catch(error => error), request = await f.lastRequest();
    assert.equal(f.store.getState().token, verified ? 'token-owner' : null, 'store still contains the previous bootstrap state');
    assert.equal(request.config.headers.get('Authorization'), 'Bearer saved-token');
    const player = runtime({ '../queryClient': f.viewer }).load('store/player.ts').usePlayerStore;
    player.getState().playTracks([{ videoId: 'private', songName: 'synthetic-private' }]);
    f.viewer.queryClient.setQueryData(['private-bootstrap'], ['synthetic-private']);
    f.respond(request, { error: 'expired' }, 401);
    assert.equal((await identities).response.status, 401);
    assert.equal(f.store.getState().status, 'anonymous', 'a current Bearer 401 invalidates unverified bootstrap too');
    assert.equal(f.localStorage.getItem('setori_token'), null);
    assert.equal(f.viewer.queryClient.getQueryData(['private-bootstrap']), undefined);
    assert.equal(player.getState().queue.length, 0);
    f.respond(me, user()); await validation;
    assert.equal(f.store.getState().user, null, 'a delayed old /me cannot restore the expired session');
    const publicRead = f.api.songApi.get('public'), anonymous = await f.lastRequest();
    assert.equal(anonymous.config.headers.has('Authorization'), false);
    f.respond(anonymous, { id: 'public' }); await publicRead;
  } finally { f.close(); }
});
