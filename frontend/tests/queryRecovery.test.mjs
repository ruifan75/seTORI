import assert from 'node:assert/strict';
import { after, test } from 'node:test';
import axios, { AxiosError, CanceledError } from 'axios';
import { QueryObserver, onlineManager } from '@tanstack/react-query';
import { createServer } from 'vite';

const server = await createServer({ configFile: false, server: { middlewareMode: true, ws: false }, optimizeDeps: { noDiscovery: true, include: [] } });
const { queryClient: client, applyViewerChange } = await server.ssrLoadModule('/src/queryClient.ts');
const policy = await server.ssrLoadModule('/src/queryPolicy.ts');
const connectivity = await server.ssrLoadModule('/src/api/connectivity.ts');
const { refetchConnectionFailures, monitorConnection } = await server.ssrLoadModule('/src/connectionMonitor.ts');
let respond = () => { throw new Error('unexpected HTTP request'); };
const originalAdapter = axios.defaults.adapter;
axios.defaults.adapter = (config) => respond(config);
const api = await server.ssrLoadModule('/src/api/client.ts');
after(async () => { client.clear(); onlineManager.setOnline(true); axios.defaults.adapter = originalAdapter; await server.close(); });
const reply = (config, data) => ({ config, data, status: 200, statusText: 'OK', headers: {} });
function failure(status, config, code = 'ERR_NETWORK') {
  const error = new AxiosError('private transport detail', code, config);
  if (status) error.response = { status, data: { error: 'private API detail' }, config };
  return error;
}
async function until(predicate) {
  for (let i = 0; i < 250; i++) { if (predicate()) return; await new Promise(r => setTimeout(r, 2)); }
  assert.fail('expected state did not arrive');
}
function observe(key, queryFn) {
  const observer = new QueryObserver(client, { queryKey: key, queryFn, retryDelay: () => 0 });
  const unsubscribe = observer.subscribe(() => {});
  return { observer, unsubscribe };
}

test('実際の query は4xxを再試行せず、5xx・通信障害は最大3回再試行する', async () => {
  for (const [status, expected] of [[400,1],[401,1],[403,1],[404,1],[409,1],[429,1],[500,4],[502,4],[503,4],[599,4],[null,4]]) {
    client.clear(); let calls = 0;
    respond = async config => { calls++; throw failure(status, config); };
    await assert.rejects(client.fetchQuery({ queryKey: ['songs', status], queryFn: () => api.songApi.list(), retryDelay: () => 0 }));
    assert.equal(calls, expected, String(status));
  }
  for (const code of ['ECONNABORTED', 'ETIMEDOUT', 'ERR_BAD_OPTION_VALUE', 'ERR_INVALID_URL']) {
    client.clear(); let calls = 0;
    respond = async config => { calls++; throw failure(null, config, code); };
    await assert.rejects(client.fetchQuery({ queryKey: ['songs', code], queryFn: () => api.songApi.list(), retryDelay: () => 0 }));
    assert.equal(calls, ['ECONNABORTED','ETIMEDOUT'].includes(code) ? 4 : 1, code);
  }
  for (const error of [new CanceledError(), new Error('program error')]) {
    let calls = 0;
    await assert.rejects(client.fetchQuery({ queryKey: ['exception', error.name], queryFn: async () => { calls++; throw error; }, retryDelay: () => 0 }));
    assert.equal(calls, 1);
  }
});

test('再試行間隔は1秒・2秒・4秒で、上限10秒', () => {
  assert.deepEqual([0,1,2,3,4,8].map(policy.queryRetryDelay), [1000,2000,4000,8000,10000,10000]);
  assert.equal(typeof client.getDefaultOptions().queries.retryDelay,'function');
  assert.deepEqual([0,1,2].map(i => client.getDefaultOptions().queries.retryDelay(i, failure(503))), [1000,2000,4000]);
  assert.equal(policy.retryQuery(3, failure(503)), false);
});

test('mutation は障害でも1回だけ。オフライン中も復帰まで保留しない', async () => {
  for (const online of [true, false]) {
    onlineManager.setOnline(online); let calls = 0;
    respond = async config => { calls++; throw failure(503, config); };
    const mutation = client.getMutationCache().build(client, { mutationFn: () => api.songApi.create({ name: 'fixture' }), retryDelay: () => 0 });
    await assert.rejects(Promise.race([mutation.execute(), new Promise((_,reject) => setTimeout(() => reject(new Error('mutation was queued')), 500))]), error => error instanceof AxiosError);
    assert.equal(calls, 1);
    assert.equal(mutation.state.isPaused, false);
    onlineManager.setOnline(true);
    await new Promise(r => setTimeout(r, 5));
    assert.equal(calls, 1);
  }
});

test('古い疎通失敗は新しい成功を取り消さず、4xx・キャンセルを全体障害にしない', async () => {
  respond = async config => reply(config, { status: 'ok' });
  await connectivity.probeAPI();
  let rejectOld;
  respond = config => new Promise((_,reject) => { rejectOld = () => reject(failure(null, config)); });
  const old = api.songApi.list().catch(() => {});
  await until(() => rejectOld);
  respond = async config => reply(config, { commit: 'fixture' });
  await api.versionApi.get(); rejectOld(); await old;
  assert.equal(connectivity.connectionSnapshot().apiUnavailable, false);
  for (const status of [403,404]) {
    respond = async config => { throw failure(status, config); };
    await assert.rejects(api.songApi.list());
    assert.equal(connectivity.connectionSnapshot().apiUnavailable, false);
  }
  respond = async () => { throw new CanceledError(); };
  await assert.rejects(api.songApi.list());
  assert.equal(connectivity.connectionSnapshot().apiUnavailable, false);
  respond = async config => { throw failure(503, config); };
  await assert.rejects(api.songApi.list());
  assert.equal(connectivity.connectionSnapshot().apiUnavailable, true);
});

test('500は個別の失敗。全体の障害は応答なし・502/503/504・520〜526だけ', async () => {
  for (const [status, code, unavailable] of [
    [400,'ERR_BAD_REQUEST',false], [403,'ERR_BAD_REQUEST',false],
    [500,'ERR_BAD_RESPONSE',false], [501,'ERR_BAD_RESPONSE',false], [505,'ERR_BAD_RESPONSE',false], [599,'ERR_BAD_RESPONSE',false],
    [502,'ERR_BAD_RESPONSE',true], [503,'ERR_BAD_RESPONSE',true], [504,'ERR_BAD_RESPONSE',true],
    [519,'ERR_BAD_RESPONSE',false], ...[520,521,522,523,524,525,526].map(status=>[status,'ERR_BAD_RESPONSE',true]), [527,'ERR_BAD_RESPONSE',false],
    [null,'ERR_NETWORK',true], [null,'ECONNABORTED',true], [null,'ETIMEDOUT',true], [null,'ERR_BAD_OPTION_VALUE',false],
  ]) {
    respond = async config => reply(config, { status:'ok' });
    await connectivity.probeAPI();
    respond = async config => { throw failure(status, config, code); };
    await assert.rejects(api.songApi.list());
    assert.equal(connectivity.connectionSnapshot().apiUnavailable, unavailable, `${status}/${code}`);
  }
  respond = async config => reply(config, { status:'ok' });
  await connectivity.probeAPI();
});

test('復帰は表示中の一時的な失敗だけ取り直す（成功・非表示・4xx・mutationは残す）', async () => {
  client.clear(); onlineManager.setOnline(true);
  const counts = new Map(); const cleanup = [];
  for (const [name,status,active] of [['failed',503,true],['network',null,true],['inactive',503,false],['forbidden',403,true],['healthy',200,true]]) {
    const queryFn = async () => { counts.set(name,(counts.get(name) ?? 0)+1); return name; };
    const query = client.getQueryCache().build(client, { queryKey: [name], queryFn, staleTime: Infinity });
    query.setState({ status: status===200 ? 'success' : 'error', data: status===200 ? name : undefined, error: status===200 ? null : failure(status), fetchStatus: 'idle', dataUpdateCount: status===200 ? 1 : 0, errorUpdateCount: status===200 ? 0 : 1 });
    if (active) { const observer = new QueryObserver(client, { queryKey: [name], queryFn, staleTime: Infinity, retryOnMount: false }); cleanup.push(observer.subscribe(() => {})); }
  }
  let mutationCalls=0;
  client.getMutationCache().build(client, { mutationFn: async () => { mutationCalls++; } });
  try {
    await refetchConnectionFailures(client);
    assert.deepEqual(Object.fromEntries(counts), { failed:1, network:1 });
    assert.equal(mutationCalls,0);
  } finally { cleanup.forEach(fn => fn()); client.clear(); }
});

test('オフラインの読み取りは保留し、オンライン復帰で現在の query を1回実行する', async () => {
  client.clear();client.mount();onlineManager.setOnline(false);
  let calls=0; const { observer,unsubscribe } = observe(['paused'],async () => { calls++; return 'current'; });
  try {
    assert.equal(observer.getCurrentResult().isPending,true);
    assert.equal(observer.getCurrentResult().fetchStatus,'paused');assert.equal(calls,0);
    onlineManager.setOnline(true);
    await until(() => observer.getCurrentResult().data==='current');assert.equal(calls,1);
  } finally { unsubscribe();client.unmount();client.clear(); }
});

test('復帰の確認は公開 health GET・5秒・Bearerなし。200かつstatus:okだけ成功', async () => {
  api.setAuthToken('fixture-token'); let seen;
  respond = async config => { seen=config;return reply(config,{status:'ok'}); };
  assert.equal(await connectivity.probeAPI(),true);
  assert.equal(seen.method,'get');assert.equal(seen.url,'http://localhost:8080/api/health');
  assert.equal(seen.timeout,5000);assert.equal(seen.headers.Authorization,undefined);
  for (const [status,data] of [[200,'<html>SPA</html>'],[200,{commit:'fixture'}],[200,{status:'error'}],[200,{status:'OK'}],[201,{status:'ok'}]]) {
    respond = async config => ({...reply(config,data),status});
    assert.equal(await connectivity.probeAPI(),false, JSON.stringify({status,data}));
    assert.equal(connectivity.connectionSnapshot().apiUnavailable,true);
  }
  respond = async config => reply(config,{status:'ok'});await connectivity.probeAPI();
  api.setAuthToken(null);
});

test('確認中に版APIが成功しても、遅れて返るhealthの復帰確認を妨げない',async()=>{
  respond=async config=>{throw failure(503,config);};await assert.rejects(api.songApi.list());
  let release;
  respond=config=>new Promise(resolve=>{release=()=>resolve(reply(config,{status:'ok'}));});
  const probe=connectivity.probeAPI();await until(()=>release);
  respond=async config=>reply(config,{commit:'fixture'});await api.versionApi.get();
  assert.equal(connectivity.connectionSnapshot().apiUnavailable,true);
  release();assert.equal(await probe,true);assert.equal(connectivity.connectionSnapshot().apiUnavailable,false);
});

test('監視は offline/online を反映し、後始末後のイベントでは変えない', () => {
  const browser=new EventTarget(); const oldWindow=globalThis.window; const oldNavigator=Object.getOwnPropertyDescriptor(globalThis,'navigator');
  globalThis.window=browser;Object.defineProperty(globalThis,'navigator',{value:{onLine:false},configurable:true});
  let stop;
  try {
    stop=monitorConnection(client);assert.equal(connectivity.connectionSnapshot().online,false);
    browser.dispatchEvent(new Event('online'));assert.equal(connectivity.connectionSnapshot().online,true);
    browser.dispatchEvent(new Event('offline'));assert.equal(connectivity.connectionSnapshot().online,false);
    stop();stop=null;browser.dispatchEvent(new Event('online'));assert.equal(connectivity.connectionSnapshot().online,false);
  } finally { stop?.();globalThis.window=oldWindow;Object.defineProperty(globalThis,'navigator',oldNavigator);connectivity.setBrowserOnline(true);onlineManager.setOnline(true); }
});

test('DB障害中はhealthを15秒ごとに確認し、版APIの成功では復帰・再取得しない',async(t)=>{
  client.clear();onlineManager.setOnline(true);
  let ready=false, healthCalls=0, versionCalls=0, queryCalls=0;
  respond=async config=>{
    if(config.url.endsWith('/api/health')) {
      healthCalls++;if(!ready) throw failure(503,config);return reply(config,{status:'ok'});
    }
    if(config.url.endsWith('/api/version')) {versionCalls++;return reply(config,{commit:'fixture'});}
    queryCalls++;if(!ready) throw failure(503,config);return reply(config,{songs:['current']});
  };
  client.setQueryData(['db-outage'],{songs:['old']});
  const observer=new QueryObserver(client,{queryKey:['db-outage'],queryFn:()=>api.songApi.list(),retry:false,refetchOnMount:false});
  const unsubscribe=observer.subscribe(()=>{});
  await assert.rejects(observer.refetch({throwOnError:true}));
  assert.equal(queryCalls,1);assert.equal(connectivity.connectionSnapshot().apiUnavailable,true);
  const oldWindow=globalThis.window;const oldNavigator=Object.getOwnPropertyDescriptor(globalThis,'navigator');
  globalThis.window=new EventTarget();Object.defineProperty(globalThis,'navigator',{value:{onLine:true},configurable:true});
  t.mock.timers.enable({apis:['setTimeout']});let stop;
  const flush=async()=>{for(let i=0;i<100;i++) await Promise.resolve();};
  try {
    stop=monitorConnection(client);
    t.mock.timers.tick(14999);await flush();assert.equal(healthCalls,0);
    t.mock.timers.tick(1);await flush();assert.equal(healthCalls,1);
    assert.equal(connectivity.connectionSnapshot().apiUnavailable,true);assert.equal(queryCalls,1);
    // DBを使わない成功応答だけでは、失敗した一覧を再取得してはいけない。
    await api.versionApi.get();assert.equal(versionCalls,1);
    assert.equal(connectivity.connectionSnapshot().apiUnavailable,true);assert.equal(queryCalls,1);
    t.mock.timers.tick(15000);await flush();assert.equal(healthCalls,2);
    assert.equal(connectivity.connectionSnapshot().apiUnavailable,true);assert.equal(queryCalls,1);
    ready=true;t.mock.timers.tick(15000);await flush();assert.equal(healthCalls,3);
    assert.equal(connectivity.connectionSnapshot().apiUnavailable,false);assert.equal(queryCalls,2);
    assert.deepEqual(observer.getCurrentResult().data,{songs:['current']});
    t.mock.timers.tick(30000);await flush();assert.equal(healthCalls,3);assert.equal(queryCalls,2);
    stop();stop=null;
  } finally {
    stop?.();t.mock.timers.reset();unsubscribe();client.clear();
    globalThis.window=oldWindow;Object.defineProperty(globalThis,'navigator',oldNavigator);
  }
});

test('再接続は4xxを繰り返さず、Infinityの取得でも一時的な失敗を取り直す',async()=>{
  client.clear();client.mount();
  let calls=0;const observer=new QueryObserver(client,{queryKey:['reconnect'],queryFn:async()=>{calls++;return 'current';},staleTime:Infinity,retryOnMount:false,refetchOnMount:false});
  const query=client.getQueryCache().find({queryKey:['reconnect']});
  query.setState({status:'error',data:'old',fetchStatus:'idle',error:failure(403),isInvalidated:true,errorUpdateCount:1});
  const unsubscribe=observer.subscribe(()=>{});
  try {
    onlineManager.setOnline(false);onlineManager.setOnline(true);await new Promise(r=>setTimeout(r,5));assert.equal(calls,0);
    query.setState({status:'error',data:'old',fetchStatus:'idle',error:failure(503),isInvalidated:false,errorUpdateCount:2});
    onlineManager.setOnline(false);onlineManager.setOnline(true);await until(()=>observer.getCurrentResult().data==='current');assert.equal(calls,1);
  } finally {unsubscribe();client.unmount();client.clear();onlineManager.setOnline(true);}
});

test('ブラウザ再接続はQueryClientだけが再取得し、監視と併用しても要求は1回',async()=>{
  for(const queryFirst of [false,true]) {
    client.clear();onlineManager.setOnline(true);
    // この試験はAPI障害からの復帰ではなく、ブラウザのoffline→onlineを扱う。
    respond=async config=>reply(config,{status:'ok'});await connectivity.probeAPI();
    const oldWindow=globalThis.window;const oldNavigator=Object.getOwnPropertyDescriptor(globalThis,'navigator');
    const browser=new EventTarget();globalThis.window=browser;
    Object.defineProperty(globalThis,'navigator',{value:{onLine:false},configurable:true});
    client.mount();let calls=0, release, monitorRefetches=0, stop, unsubscribe;
    const originalRefetch=client.refetchQueries;
    client.refetchQueries=function(...args){monitorRefetches++;return originalRefetch.apply(this,args);};
    respond=config=>{calls++;return new Promise(resolve=>{release=()=>resolve(reply(config,{songs:['current']}));});};
    try {
      stop=monitorConnection(client);
      client.setQueryData(['browser-reconnect'],{songs:['old']});
      const observer=new QueryObserver(client,{queryKey:['browser-reconnect'],queryFn:()=>api.songApi.list(),staleTime:Infinity,retryOnMount:false,refetchOnMount:false});
      const query=client.getQueryCache().find({queryKey:['browser-reconnect']});
      query.setState({status:'error',error:failure(503),fetchStatus:'idle',errorUpdateCount:1});
      unsubscribe=observer.subscribe(()=>{});
      if(queryFirst) {onlineManager.setOnline(true);await until(()=>release);}
      browser.dispatchEvent(new Event('online'));await until(()=>release);
      // 直接refetchQueriesを重ねず、QueryClientの再接続で一度だけ開始する。
      assert.equal(monitorRefetches,0);assert.equal(calls,1);
      browser.dispatchEvent(new Event('online'));await new Promise(r=>setTimeout(r,5));assert.equal(calls,1);
      release();await until(()=>observer.getCurrentResult().data?.songs?.[0]==='current');
      assert.equal(calls,1);assert.equal(monitorRefetches,0);
    } finally {
      stop?.();unsubscribe?.();client.refetchQueries=originalRefetch;client.unmount();client.clear();
      globalThis.window=oldWindow;Object.defineProperty(globalThis,'navigator',oldNavigator);
      connectivity.setBrowserOnline(true);onlineManager.setOnline(true);
    }
  }
});

test('再試行待ちの視点変更は旧要求を中断し、旧権限で取得し直さない', async () => {
  client.clear();applyViewerChange('admin|*');api.setAuthToken('admin-fixture');
  const sent=[];
  respond=async config => { sent.push(config.headers.Authorization ?? 'anonymous');if(config.headers.Authorization) throw failure(503,config);return reply(config,{songs:['public']}); };
  const observer=new QueryObserver(client,{ queryKey:['viewer-retry'],queryFn:() => api.songApi.list(),retryDelay:()=>40 });
  const unsubscribe=observer.subscribe(()=>{});
  try {
    await until(()=>sent.length===1);
    api.setAuthToken(null);applyViewerChange(null);
    await until(()=>observer.getCurrentResult().data?.songs?.[0]==='public');
    await new Promise(r=>setTimeout(r,90));
    assert.deepEqual(sent,['Bearer admin-fixture','anonymous']);
    assert.deepEqual(client.getQueryData(['viewer-retry']),{songs:['public']});
  } finally { unsubscribe();client.clear();api.setAuthToken(null);applyViewerChange(null); }
});

test('視点変更前に飛んでいた応答を、復帰後の cache に書き戻さない', async () => {
  client.clear();applyViewerChange('admin|*');api.setAuthToken('admin-fixture');
  let release;let calls=0;const seen=[];
  respond=config => { calls++;if(config.headers.Authorization) return new Promise(resolve=>{ release=()=>resolve(reply(config,{songs:['restricted-fixture']})); });return Promise.resolve(reply(config,{songs:['public']})); };
  const observer=new QueryObserver(client,{ queryKey:['viewer-flight'],queryFn:()=>api.songApi.list() });
  const unsubscribe=observer.subscribe(result=>seen.push(result.data));
  try {
    await until(()=>release);api.setAuthToken(null);applyViewerChange(null);
    release();await new Promise(r=>setTimeout(r,10));
    assert.equal(seen.some(data=>data?.songs?.includes('restricted-fixture')),false);
    await until(()=>client.getQueryData(['viewer-flight'])?.songs?.[0]==='public');
    assert.equal(calls,2);assert.deepEqual(client.getQueryData(['viewer-flight']),{songs:['public']});
    assert.equal(seen.some(data=>data?.songs?.includes('restricted-fixture')),false);
  } finally { unsubscribe();client.clear();api.setAuthToken(null);applyViewerChange(null); }
});
