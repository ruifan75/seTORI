import assert from 'node:assert/strict';
import { after, test } from 'node:test';
import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { QueryClient, QueryClientProvider, onlineManager } from '@tanstack/react-query';
import axios, { AxiosError } from 'axios';
import { createServer } from 'vite';

const server=await createServer({ server:{middlewareMode:true,hmr:false,ws:false},optimizeDeps:{noDiscovery:true,include:[]},
  // 非公開の管理セクションも同じ実装を描く。テスト用に export だけ足し、関数本体は変えない。
  plugins:[{name:'test-private-sections',enforce:'pre',transform(code,id){
    if(id.endsWith('/src/pages/admin/SettingsPage.tsx')) return code+'\nexport { AIProviderSection };';
    // SSR は portal に対応しないので描画先だけを差し替える。メニューの query と分岐は実物。
    if(id.endsWith('/src/components/PlaylistPickerMenu.tsx')) return code
      .replace("import { createPortal } from 'react-dom';",'const createPortal = node => node;')
      .replace('document.body,','null,');
  }}] });
after(()=>server.close());
const { ToastProvider }=await server.ssrLoadModule('/src/components/ui/Toast.tsx');
const { useAuthStore }=await server.ssrLoadModule('/src/store/auth.ts');
const { default: ConnectionStatus }=await server.ssrLoadModule('/src/components/ConnectionStatus.tsx');
const connection=await server.ssrLoadModule('/src/api/connectivity.ts');
const pagination={page:1,limit:20,total:0,total_pages:0};
const pageCache=new Map();
function error(status=503) { const e=new AxiosError('private fixture detail','ERR_BAD_RESPONSE');e.response={status,data:{error:'private fixture detail'}};return e; }
async function render(page,route,key,state='error',seeds=[]) {
  onlineManager.setOnline(state!=='paused');
  const client=new QueryClient({ defaultOptions:{queries:{staleTime:Infinity,retry:false,retryOnMount:false}} });
  for (const [k,data] of seeds) client.setQueryData(k,data);
  if (state==='error') client.getQueryCache().build(client,{queryKey:key}).setState({status:'error',fetchStatus:'idle',error:error()});
  else if (state==='paused') client.getQueryCache().build(client,{queryKey:key}).setState({status:'pending',fetchStatus:'paused'});
  else client.setQueryData(key,state);
  try {
    if (!pageCache.has(page)) pageCache.set(page,(await server.ssrLoadModule('/src/pages/'+page+'.tsx')).default);
    return renderToStaticMarkup(createElement(QueryClientProvider,{client},createElement(ToastProvider,null,
      createElement(MemoryRouter,{initialEntries:[route.replace(':id','fixture').replace(':key','fixture').replace(':slug','fixture')]},
        createElement(Routes,null,createElement(Route,{path:route.split('?')[0].replace(/\/tags\/(stream|performance)\//,'/tags/:kind/'),element:createElement(pageCache.get(page))}))))));
  } finally { client.clear(); onlineManager.setOnline(true); }
}
const emptyPlaylists={playlists:[],pagination};
const playlist={id:'fixture',name:'fixture',visibility:'public',is_owner:false,item_count:0,owner_name:'fixture'};
const preset={key:'fixture',name:'fixture',item_count:0};
// 意図するキーと空の文言を手で固定。ページから抽出・生成しない。
const cases=[
 ['SongsPage','/songs',['songs',1,'','name','asc'],/件の楽曲|楽曲がありません/,{songs:[],pagination}],
 ['ArtistsPage','/artists',['artists',1,'','name','asc'],/件のアーティスト|アーティストがありません/,{artists:[],pagination}],
 ['ChannelsPage','/channels',['singers','grouped',false],/件のチャンネル|チャンネルがありません/,{groups:[],total:0}],
 ['ChannelsPage','/channels?view=list',['singers',1,'name','asc',false],/件のチャンネル|チャンネルがありません/,{singers:[],pagination}],
 ['SongDetailPage','/songs/:id',['song','fixture','performances',1],/楽曲が見つかりません/],
 ['ArtistDetailPage','/artists/:id',['artist','fixture',1,'performances','desc'],/アーティストが見つかりません/],
 ['ChannelDetailPage','/channels/:id',['singer','fixture',false],/チャンネルが見つかりません/],
 ['StreamDetailPage','/streams/:id',['stream','fixture',false],/配信が見つかりません/],
 ['TagPage','/tags/stream/:id',['tag-streams','fixture',1],/このタグが付いた| · 0件/,{streams:[],pagination}],
 ['TagPage','/tags/performance/:id',['tag-performances','fixture',1],/このタグが付いた| · 0件/,{performances:[],pagination}],
 ['SearchPage','/search?q=term',['stream-search','term','','','','','',1],/検索結果 0件|条件に一致する動画がありません/,{streams:[],pagination}],
 ['PlaylistDetailPage','/playlists/:id',['playlist','fixture',false],/プレイリストが見つかりません/],
 ['PlaylistDetailPage','/shared/playlists/:slug',['playlist','fixture',true],/プレイリストが見つかりません/,undefined,[],{shared:true}],
 ['PlaylistDetailPage','/playlists/:id',['playlist','fixture','items',false],/まだ曲がありません/,{performances:[]},[[['playlist','fixture',false],playlist]]],
 ['PresetPlaylistPage','/playlists/preset/:key',['presets','detail','fixture'],/プレイリストが見つかりません/],
 ['PresetPlaylistPage','/playlists/preset/:key',['preset-items','fixture','all'],/曲がありません/,{performances:[]},[[['presets','detail','fixture'],preset]]],
 ['PlaylistsPage','/playlists',['playlists','public'],/公開されているプレイリストはまだありません/,emptyPlaylists],
 ['MySuggestionsPage','/my/suggestions',['suggestions','mine','',1],/まだ提案はありません/,{suggestions:[],pagination}],
 ['MyAccountPage','/my/account',['oauth','identities'],/連携しているアカウントはありません/,[]],
 ['admin/OrganizationsPage','/admin/organizations',['organizations'],/事務所がありません/,{organizations:[]}],
 ['admin/MissingTagsPage','/admin/missing-tags',['tag-gaps'],/タグ漏れはありません|記録はありません/,{gaps:[],dismissals:[]}],
 ['admin/MergeCandidatesPage','/admin/merge-candidates',['song-merge-candidates'],/重複候補はありません/,{candidates:[],total:0}],
 ['admin/ReadingsPage','/admin/readings',['readings-stats'],/0 件が未整備/,{artists_needs_fix:0,artists_total:0,songs_needs_fix:0,songs_total:0}],
 ['admin/SuggestionsPage','/admin/suggestions',['suggestions','grouped','pending','',1],/未処理の提案はありません/,{groups:[],pagination}],
 ['admin/BackupPage','/admin/backups',['backup-status'],/バックアップがまだありません/],
 ['admin/IntegrationSettingsSection','/admin/settings',['settings','integrations'],/未設定/],
 ['HomePage','/',['random-performances','home','infinite'],/おすすめはありません/],
 ['HomePage','/',['tag-streams','singing','home'],/歌枠がありません/],
 ['HomePage','/',['songs','popular'],/人気の楽曲はありません/],
 ['HomePage','/',['presets'],/プリセットがありません/],
 ['AIProviderSection','/admin/settings',['ai-providers'],/プロバイダーがありません/],
 ['admin/SettingsPage','/admin/settings',['filter-keywords'],/キーワードがありません/],
 ['admin/SettingsPage','/admin/settings',['tag-rules'],/先に「配信タグ管理」でタグを作成/],
 ['admin/UsersPage','/admin/users',['users'],/ユーザーがありません/],
 ['admin/UsersPage','/admin/users',['roles'],/ロールがありません/],
 ['admin/UsersPage','/admin/users',['permissions'],/ロールがありません/],
 ['admin/VisibilityReviewPage','/admin/visibility-review',['visibility-review',false,0],/該当する配信はありません|>0件</],
 ['admin/ProcessedReviewPage','/admin/processed-review',['processed-review',{is_processed:true,q:'',channel_id:'',tags:[],hidden:'all',has_performances:'all',from:'',until:''},0],/該当する配信はありません|>0件</],
 ['PlaylistPickerMenu','/songs',['playlists','mine'],/まだプレイリストがありません/,emptyPlaylists],
 ['AutoApplySettingsPanel','/admin/suggestions',['suggestions','settings'],/>無効</],
];
// shared は同じ実ページを props を付けて描く。
const { default: SharedPage }=await server.ssrLoadModule('/src/pages/PlaylistDetailPage.tsx');
pageCache.set('SharedPlaylist',props=>createElement(SharedPage,{...props,shared:true}));
pageCache.set('AIProviderSection',(await server.ssrLoadModule('/src/pages/admin/SettingsPage.tsx')).AIProviderSection);
const { default: PlaylistPickerMenu }=await server.ssrLoadModule('/src/components/PlaylistPickerMenu.tsx');
pageCache.set('PlaylistPickerMenu',()=>createElement(PlaylistPickerMenu,{anchorRef:{current:null},initialPosition:{top:0,left:0},onClose:()=>{},onPick:()=>{},onCreate:()=>{}}));
pageCache.set('AutoApplySettingsPanel',(await server.ssrLoadModule('/src/components/AutoApplySettingsPanel.tsx')).default);
for (const [page,route,key,empty,success,seeds=[],props] of cases) {
  test(`${page} ${JSON.stringify(key)}: 失敗と保留を空・見つからない表示と区別する`,async()=>{
    useAuthStore.setState(page.startsWith('admin/') ? { user: { id:'fixture', permissions:['*'] }, status:'authenticated' } : {user:null,status:'anonymous'});
    const target=props?.shared?'SharedPlaylist':page;
    for (const state of (['presets','roles','permissions'].includes(key[0]) || page==='HomePage' || key[0]==='tag-rules' ? ['error'] : ['error','paused'])) {
      const html=await render(target,route,key,state,seeds);
      if (state==='error') {
        assert.match(html,/role="alert"/);assert.match(html,/取得に失敗しました/);assert.match(html,/>再取得<\/button>/);
      } else { assert.doesNotMatch(html,/取得に失敗しました/);assert.match(html,/animate-spin|読み込み中|取得に失敗した実行は履歴から/); }
      assert.doesNotMatch(html,empty);assert.doesNotMatch(html,/private fixture detail/);
    }
    if (success) {
      const html=await render(target,route,key,success,seeds);
      assert.doesNotMatch(html,/取得に失敗しました/);
    }
  });
}

test('上部は online の正常・オフライン・API障害を区別し、復帰で消える',async()=>{
  const html=()=>{const client=new QueryClient();try{return renderToStaticMarkup(createElement(QueryClientProvider,{client},createElement(ConnectionStatus)));}finally{client.clear();}};
  connection.setBrowserOnline(true);assert.equal(html(),'');
  connection.setBrowserOnline(false);assert.match(html(),/role="status"/);assert.match(html(),/オフラインです/);assert.doesNotMatch(html(),/接続を確認/);
  connection.setBrowserOnline(true);assert.equal(html(),'');
  const transport=axios.create({adapter:async config=>{ const e=new AxiosError('private error','ERR_NETWORK',config);throw e; }});
  connection.observeAPIConnection(transport);
  transport.defaults.adapter=async config=>{const e=new AxiosError('private error','ERR_BAD_RESPONSE',config);e.response={config,status:500,data:{}};throw e;};
  await assert.rejects(transport.get('/api/songs'));assert.equal(html(),'','個別の500で上部の障害表示を出さない');
  transport.defaults.adapter=async config=>{throw new AxiosError('private error','ERR_NETWORK',config);};
  await assert.rejects(transport.get('/api/version'));
  assert.match(html(),/サーバーに接続できません/);assert.match(html(),/接続を確認/);assert.doesNotMatch(html(),/private error/);
  transport.defaults.adapter=async config=>({config,data:{commit:'fixture'},status:200,statusText:'OK',headers:{}});
  await transport.get('/api/version');assert.match(html(),/サーバーに接続できません/);
  const originalAdapter=axios.defaults.adapter;
  try {
    axios.defaults.adapter=async config=>({config,data:{status:'ok'},status:200,statusText:'OK',headers:{}});
    await connection.probeAPI();assert.equal(html(),'');
  } finally {axios.defaults.adapter=originalAdapter;}
});
