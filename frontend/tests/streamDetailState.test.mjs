import assert from 'node:assert/strict';
import { test } from 'node:test';
import { readFileSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { createRequire } from 'node:module';
import vm from 'node:vm';
import ts from 'typescript';

const root = dirname(dirname(fileURLToPath(import.meta.url)));
const require = createRequire(import.meta.url);
const secret = '合成の秘匿曲';
const id = 'abcdefghijk';
const song = { name: secret, original_artist: '合成アーティスト', start: 60, end: 240,
  start_seconds: 60, end_seconds: 240, singer_ids: [], tags: [], matched_song_itunes_id: 123 };
const editable = { id: 'editable', name: secret, nameReading: '', artist: '合成アーティスト', artistReading: '',
  start: 60, end: 240, singerIds: [], tags: [], customTags: [], matchedSongId: null,
  artUrl: null, itunesId: null, trackDuration: null, originalName: secret, originalArtist: '合成アーティスト' };
const deferred = () => {
  let resolve;
  const promise = new Promise((done) => { resolve = done; });
  return { promise, resolve };
};
const tick = () => new Promise((done) => setImmediate(done));

// 本物の hook・操作関数を TS から読み込む。React の state/Effect と API の境界を記録し、
// viewerID / sameViewer / applyViewerChange / resetQueries は実際の共通モジュールを使う。
// 記録するのは最終 state だけではなく、応答後の各 setter と通知（書いてから消す漏れも検出）。
function harness({ canEdit = true, authStatus = 'authenticated', songs = [] } = {}) {
  const writes = [], values = [], effects = [], queries = [], mutations = [], calls = [], toasts = [];
  const apis = Object.fromEntries(['streamApi', 'performanceApi', 'aiApi', 'itunesApi', 'holodexApi', 'commentApi', 'chapterApi', 'tagApi', 'artistApi']
    .map((name) => [name, new Proxy({}, { get(target, key) {
      if (!(key in target)) target[key] = (...args) => { throw new Error(`API fixture がありません: ${name}.${String(key)} ${JSON.stringify(args)}`); };
      return target[key];
    } })]));
  const stream = { id, title: '合成の配信', performances: [], participants: [], tags: [],
    holodex_timeline_songs: [song], has_comment_raw: true };
  const modules = new Map();
  let stateIndex = 0;
  const react = {
    useState(initial) {
      const index = stateIndex++;
      values[index] = index === 5 ? songs : initial;
      return [values[index], (next) => {
        values[index] = typeof next === 'function' ? next(values[index]) : next;
        writes.push({ index, value: values[index] });
      }];
    },
    useEffect(fn) { effects.push(fn); }, useRef(value) { return { current: value }; }, useCallback(fn) { return fn; },
  };
  const queryPackage = {
    ...require('@tanstack/react-query'),
    useQueryClient() { return viewer.queryClient; },
    useQuery(options) {
      queries.push(options);
      switch (options.queryKey[0]) {
        case 'stream': return { data: stream, isLoading: false };
        case 'raw-comments': return { data: { comments: [] } };
        case 'stream-tags': case 'performance-tags': return { data: [] };
        default: throw new Error(`未定義の queryKey: ${JSON.stringify(options.queryKey)}`);
      }
    },
    useMutation(options) {
      mutations.push(options);
      const mutateAsync = async (params) => {
        const data = await options.mutationFn(params);
        await options.onSuccess?.(data);
        return data;
      };
      return { mutate: mutateAsync, mutateAsync, isPending: false };
    },
  };
  function load(file) {
    file = resolve(file);
    if (modules.has(file)) return modules.get(file).exports;
    const module = { exports: {} }; modules.set(file, module);
    const source = readFileSync(file, 'utf8');
    const js = ts.transpileModule(source, { compilerOptions: {
      module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022, jsx: ts.JsxEmit.ReactJSX,
    } }).outputText;
    function dependency(name) {
      if (name === 'react') return react;
      if (name === '@tanstack/react-query') return queryPackage;
      if (name === 'react-router-dom') return { useParams: () => ({ id }) };
      if (name.endsWith('/api/client')) return apis;
      if (name.endsWith('/store/auth')) return {
        useAuthStore: (select) => select({ user: { permissions: canEdit ? ['content:edit'] : [] }, status: authStatus }),
        hasPermission: (user, permission) => user.permissions.includes(permission), PERM: { CONTENT_EDIT: 'content:edit' },
      };
      if (name.endsWith('/store/player')) return { usePlayerStore: { getState: () => ({ setPlaying: (playing) => calls.push(['playing', playing]) }) } };
      if (name.endsWith('/ToastContext')) return { useToast: () => ({ showToast: (...args) => toasts.push(args) }) };
      if (name.endsWith('/youtubePlayerControl')) return { playerSeekTo: (...args) => calls.push(['seek', ...args]) };
      if (name.startsWith('.')) return load(resolve(dirname(file), name + '.ts'));
      return require(name);
    }
    vm.runInNewContext(js, { module, exports: module.exports, require: dependency, console,
      setTimeout: () => 0, clearTimeout() {}, document: { body: { style: {} }, documentElement: { style: {} } },
    }, { filename: file });
    return module.exports;
  }
  const viewer = load(join(root, 'src/queryClient.ts'));
  viewer.applyViewerChange('viewer|content:edit,restricted:view');
  const invalidate = viewer.queryClient.invalidateQueries.bind(viewer.queryClient);
  viewer.queryClient.invalidateQueries = (options) => { calls.push(['invalidate', [...options.queryKey]]); return invalidate(options); };
  const { useStreamDetail } = load(join(root, 'src/pages/stream-detail/useStreamDetail.ts'));
  const model = useStreamDetail();
  const cleanups = effects.map((fn) => fn()).filter((fn) => typeof fn === 'function');
  return { model, apis, viewer, writes, values, queries, mutations, calls, toasts,
    close() { cleanups.forEach((fn) => fn()); viewer.queryClient.clear(); } };
}
function assertNoPrivateWrites(h, after = 0) {
  assert.equal(JSON.stringify(h.writes.slice(after)).includes(secret), false, '権限変更後の setter に秘匿曲が戻った');
  assert.equal(JSON.stringify(h.toasts).includes(secret), false, '権限変更後の通知に秘匿曲が戻った');
}

for (const canEdit of [false, true]) for (const authStatus of ['loading', 'authenticated']) {
  test(`固定の queryKey と取得条件: ${canEdit}/${authStatus}`, async () => {
    const h = harness({ canEdit, authStatus });
    try {
      assert.deepEqual(h.queries.map((q) => JSON.parse(JSON.stringify(q.queryKey))), [
        ['stream', id, canEdit], ['raw-comments', id], ['stream-tags'], ['performance-tags'],
      ]);
      assert.equal(h.queries[0].enabled, authStatus !== 'loading');
      assert.equal(h.queries[0].staleTime, 0);
      assert.equal(h.queries[1].enabled, canEdit);
      assert.equal(h.queries[1].staleTime, Infinity);
      const fetched = [];
      h.apis.streamApi.get = async (...args) => { fetched.push(['stream', ...args]); return h.model.stream; };
      h.apis.commentApi.getComments = async (...args) => { fetched.push(['comments', ...args]); return { comments: [] }; };
      await h.queries[0].queryFn(); await h.queries[1].queryFn();
      assert.deepEqual(fetched, [['stream', id], ['comments', id]]);
    } finally { h.close(); }
  });
}

test('利用者・同じ人の権限が変わると編集コピー・ポップアップ・query cache を破棄する', () => {
  for (const next of [null, 'another|content:edit', 'viewer|content:edit']) {
    const h = harness({ songs: [editable] });
    try {
      h.model.toggleEditing(); h.model.setVocalistPopupSingers([{ id: 'secret', name: secret }]);
      h.viewer.queryClient.setQueryData(['private-copy'], { name: secret });
      h.viewer.applyViewerChange(next);
      assert.equal(h.values[0], false);
      assert.deepEqual(JSON.parse(JSON.stringify(h.values[5])), []);
      assert.equal(h.values[13], null);
      assert.equal(h.viewer.queryClient.getQueryData(['private-copy']), undefined);
    } finally { h.close(); }
  }
});

for (const [method, api, key, response] of [
  ['loadFromHolodex', 'holodexApi', 'analyzeSongs', [song]],
  ['loadFromComments', 'commentApi', 'analyze', { songs: [song] }],
  ['loadFromChapters', 'chapterApi', 'analyze', { songs: [song] }],
]) {
  for (const stage of ['same viewer', 'analysis', 'itunes']) test(`${method}: ${stage} 待ちの照合`, async () => {
    const h = harness();
    const analysis = deferred(), duration = deferred();
    h.apis[api][key] = () => analysis.promise;
    h.apis.itunesApi.queryById = () => duration.promise;
    try {
      const pending = h.model[method]();
      if (stage === 'analysis') h.viewer.applyViewerChange(null);
      const afterAnalysisChange = h.writes.length;
      analysis.resolve(response); await tick();
      if (stage === 'itunes') h.viewer.applyViewerChange('viewer|content:edit');
      const after = stage === 'analysis' ? afterAnalysisChange : h.writes.length;
      duration.resolve({ track_time_millis: 180000 }); await pending;
      if (stage === 'same viewer') {
        assert.equal(h.values[5][0].name, secret, '陽性対照で曲が読み込まれない');
        assert.equal(h.values[5][0].trackDuration, 180);
      } else assertNoPrivateWrites(h, after);
    } finally { h.close(); }
  });
}

for (const method of ['addSuggestionSong', 'addCommentSongToList', 'addChapterSongToList', 'addFromRawComment']) {
  test(`${method}: 単曲追加待ちに権限が変わっても書き戻さない`, async () => {
    const h = harness(), duration = deferred();
    h.apis.itunesApi.queryById = () => duration.promise;
    h.apis.commentApi.estimateChatEnds = () => duration.promise;
    try {
      const pending = h.model[method](method === 'addFromRawComment' ? { start: 60, name: secret, artist: '合成アーティスト' } : song);
      h.viewer.applyViewerChange(null); const after = h.writes.length;
      duration.resolve({ track_time_millis: 180000, ends: { 60: 240 } }); await pending;
      assertNoPrivateWrites(h, after);
    } finally { h.close(); }
  });
}

test('既存曲を選択中に権限が変わっても編集リストへ戻さない', async () => {
  const h = harness({ songs: [editable] }), duration = deferred();
  h.apis.itunesApi.queryById = () => duration.promise;
  try {
    const pending = h.model.handleSelectExistingSong(0, { id: 'song', name: secret,
      original_artist: '合成アーティスト', itunes_ids: [{ itunes_id: '123' }] });
    h.viewer.applyViewerChange(null); const after = h.writes.length;
    duration.resolve({ track_time_millis: 180000 }); await pending;
    assertNoPrivateWrites(h, after);
  } finally { h.close(); }
});

for (const stage of ['normalize', 'itunes']) test(`AI 正規化: ${stage} 待ちの照合`, async () => {
  const h = harness({ songs: [editable] }), analysis = deferred(), duration = deferred();
  h.apis.aiApi.normalize = () => analysis.promise;
  let lookups = 0;
  h.apis.itunesApi.queryById = () => { lookups++; return duration.promise; };
  try {
    const pending = h.model.aiNormalizeMutation.mutateAsync([]);
    if (stage === 'normalize') h.viewer.applyViewerChange(null);
    analysis.resolve({ suggestions: [{ index: 0, normalized_name: secret, original_artist: '合成アーティスト',
      normalized_name_reading: '', original_artist_reading: '', tags: [], matched_song_itunes_id: 123 }] });
    await tick();
    if (stage === 'itunes') h.viewer.applyViewerChange(null);
    const after = h.writes.length;
    duration.resolve({ track_time_millis: 180000 }); await pending;
    assertNoPrivateWrites(h, after);
    assert.equal(lookups, stage === 'normalize' ? 0 : 1, '応答直後の照合を過ぎて API を呼んだ');
  } finally { h.close(); }
});

test('保存時は別名義の開始→セットリスト保存。権限変更後は次の別名義を送らない', async () => {
  const h = harness({ songs: [1, 2].map((i) => ({ ...editable, artistAlias: { canonical: '合成', alias: `${secret}${i}` }, aliasChecked: true })) });
  const first = deferred(), writes = [];
  h.apis.artistApi.proposeAlias = (canonical, alias) => { writes.push(['alias', canonical, alias]); return first.promise; };
  h.apis.performanceApi.create = async (target, body) => { writes.push(['create', target, JSON.parse(JSON.stringify(body))]); return { created_count: 2 }; };
  try {
    await h.model.handleConfirm(); await tick();
    assert.deepEqual(writes.map((w) => w[0]), ['alias', 'create']);
    assert.equal(writes[1][1], id);
    assert.deepEqual(writes[1][2].performances.map((p) => [p.name, p.start_seconds, p.end_seconds]), [[secret, 60, 240], [secret, 60, 240]]);
    h.viewer.applyViewerChange(null);
    first.resolve({ applied: true }); await tick();
    assert.equal(writes.length, 2, '次の利用者で別名義を送った');
  } finally { h.close(); }
});

test('配信更新と YouTube 同期は固定の対象を invalidate する', async () => {
  const h = harness();
  h.apis.streamApi.update = async () => ({});
  h.apis.commentApi.syncYouTube = async () => ({ comment_count: 0 });
  try {
    await h.model.quickSaveStream({ is_hidden: true });
    assert.deepEqual(h.calls.filter((c) => c[0] === 'invalidate'), [
      ['invalidate', ['stream', id]], ['invalidate', ['restriction-review']],
    ]);
    h.calls.length = 0;
    await h.model.syncYouTubeCommentsMutation.mutateAsync();
    assert.deepEqual(h.calls.filter((c) => c[0] === 'invalidate'), [
      ['invalidate', ['raw-comments', id]], ['invalidate', ['stream', id]],
    ]);
  } finally { h.close(); }
});
