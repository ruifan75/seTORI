import React from 'react';
import { flushSync } from 'react-dom';
import { createRoot } from 'react-dom/client';
import { QueryClientProvider } from '@tanstack/react-query';
import axios from 'axios';

async function run() {
  const act = async (fn) => { flushSync(fn); await new Promise((resolve) => setTimeout(resolve, 10)); };
  globalThis.__xss = 0;
  const attack = '<img id="xss-probe" src=x onerror="globalThis.__xss++"><script>globalThis.__xss++</script>';
  const integration = { encryption_enabled: true, secrets: {}, plain: {}, plain_from_env: {} };
  axios.defaults.adapter = async (config) => ({ config, data: config.url.includes('comments') ? { comments: [`1:23 ${attack}`] } : integration,
    status: 200, statusText: 'OK', headers: {} });
  const [{ queryClient, applyViewerChange }, { useAuthStore }, { useViewerState }, { useToast }, { ToastProvider },
    { default: Integration }, { default: RawComments }, { default: Row }, { BrowserRouter }] = await Promise.all([
    import('../../src/queryClient'), import('../../src/store/auth'), import('../../src/hooks/useViewerState'),
    import('../../src/components/ui/ToastContext'), import('../../src/components/ui/Toast'),
    import('../../src/pages/admin/IntegrationSettingsSection'), import('../../src/components/RawCommentsPanel'),
    import('../../src/components/PerformanceListRow'), import('react-router-dom'),
  ]);
  let setCopy, oldSetter, oldToast;
  function Probe() {
    const [copy, setter] = useViewerState('');
    setCopy = setter;
    const toast = useToast();
    if (!oldToast) oldToast = toast.showToast;
    return <div id="copy">{copy}</div>;
  }
  applyViewerChange('owner|content:edit,restricted:view');
  useAuthStore.setState({ user: { id: 'owner', permissions: ['content:edit', 'restricted:view'] }, status: 'authenticated' });
  queryClient.setQueryData(['settings', 'integrations'], integration);
  queryClient.setQueryData(['raw-comments', 'fixture'], { comments: [`1:23 ${attack}`] });
  const mount = document.getElementById('root');
  const root = createRoot(mount);
  const results = {};
  const badURL = 'javascript:globalThis.__xss++';
  await act(async () => root.render(<QueryClientProvider client={queryClient}><BrowserRouter><ToastProvider>
    <Probe /><Integration /><RawComments videoId="fixture" onSeek={() => {}} onAddSong={() => {}} />
    <ul><Row track={{ videoId: 'fixture', songId: 'song', songName: attack, artist: attack, singers: [{ id: 'channel', name: attack }] }}
      meta={attack} playLabel={attack} youtubeUrl={badURL} thumbnailUrl={badURL} onPlay={() => {}} /></ul>
  </ToastProvider></BrowserRouter></QueryClientProvider>));
  results.textEscaped = mount.textContent.includes(attack) && !mount.querySelector('#xss-probe') && globalThis.__xss === 0;
  const link = [...mount.querySelectorAll('a')].find((a) => a.target === '_blank');
  results.javascriptHrefBlocked = !!link && link.getAttribute('href') !== badURL;
  // img の URL は React に残るが、画像コンテキストで JavaScript は実行されない。
  results.javascriptImageDidNotExecute = globalThis.__xss === 0;
  results.blankRel = [...mount.querySelectorAll('a[target="_blank"]')].every((a) => a.rel.split(' ').includes('noreferrer') || a.rel.split(' ').includes('noopener'));
  await act(async () => { setCopy('private draft'); oldToast('private notification'); });
  oldSetter = setCopy;
  results.positiveCopy = document.getElementById('copy').textContent === 'private draft' && mount.textContent.includes('private notification');
  await act(async () => {
    useAuthStore.setState({ user: { id: 'owner', permissions: ['content:edit'] }, status: 'authenticated' });
    applyViewerChange('owner|content:edit');
  });
  results.sameUserPermissionLoss = document.getElementById('copy').textContent === '' && !mount.textContent.includes('private notification');
  await act(async () => { oldSetter('late private'); oldToast('late private'); });
  results.lateCallbacksBlocked = document.getElementById('copy').textContent === '' && !mount.textContent.includes('late private');
  await act(async () => setCopy('public draft'));
  results.newSetterWorks = document.getElementById('copy').textContent === 'public draft';
  await act(async () => root.unmount()); queryClient.clear();
  document.getElementById('result').textContent = JSON.stringify(results);
}
run().catch((error) => { document.getElementById('result').textContent = JSON.stringify({ error: error.message, stack: error.stack }); });
