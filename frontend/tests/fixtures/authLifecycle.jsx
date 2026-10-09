import React, { StrictMode, useEffect } from 'react';
import { flushSync } from 'react-dom';
import { createRoot } from 'react-dom/client';
import { QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import axios from 'axios';

async function run() {
  const requests = [];
  axios.defaults.adapter = (config) => {
    if (config.url === '/api/auth/logout') return Promise.resolve({ config, data: {}, status: 200, headers: {} });
    if (config.url === '/api/auth/oauth/providers') return Promise.resolve({ config, data: { providers: ['google'] }, status: 200, headers: {} });
    return new Promise((resolve) => requests.push({ config, resolve }));
  };
  const [{ useAuthStore }, { queryClient }, { default: OAuthCallbackPage }, { default: LoginPage }] = await Promise.all([
    import('../../src/store/auth'), import('../../src/queryClient'), import('../../src/pages/OAuthCallbackPage'), import('../../src/pages/LoginPage'),
  ]);
  const tick = () => new Promise(resolve => setTimeout(resolve, 10));
  const user = id => ({ id, username: id, permissions: ['content:edit'] });
  const respond = (request, data) => request.resolve({ config: request.config, data, status: 200, statusText: 'OK', headers: {} });
  // App.tsx has this parent startup effect, while the real callback's child effect
  // redeems the single-use code. Test React's actual ordering and StrictMode replay.
  function Startup({ entry = '/login/oauth?code=synthetic-code' }) {
    const init = useAuthStore(s => s.init);
    useEffect(() => { void init(); }, [init]);
    return <QueryClientProvider client={queryClient}><MemoryRouter initialEntries={[entry]}><Routes>
      <Route path="/login/oauth" element={<OAuthCallbackPage />} />
      <Route path="/login" element={<LoginPage />} />
      <Route path="/" element={<p id="home">signed in</p>} />
    </Routes></MemoryRouter></QueryClientProvider>;
  }
  const results = {};
  for (const strict of [false, true]) for (const saved of [false, true]) {
    await useAuthStore.getState().logout();
    requests.length = 0;
    if (saved) localStorage.setItem('setori_token', 'synthetic-old-token');
    const root = createRoot(document.getElementById('root'));
    flushSync(() => root.render(strict ? <StrictMode><Startup /></StrictMode> : <Startup />));
    await tick();
    const exchange = requests.find(request => request.config.url === '/api/auth/oauth/exchange');
    if (!exchange) throw new Error('OAuth exchange did not start');
    respond(exchange, { token: 'synthetic-new-token', user: user('next') });
    for (const request of requests.filter(request => request.config.url === '/api/auth/me')) respond(request, user('old'));
    await tick(); await tick();
    results[`oauth-${strict ? 'strict' : 'normal'}-${saved ? 'saved' : 'cold'}`] =
      requests.filter(request => request.config.url === '/api/auth/oauth/exchange').length === 1 &&
      useAuthStore.getState().user?.id === 'next' &&
      localStorage.getItem('setori_token') === 'synthetic-new-token' && !!document.getElementById('home');
    flushSync(() => root.unmount()); queryClient.clear();
  }
  for (const strict of [false, true]) {
    await useAuthStore.getState().logout();
    requests.length = 0;
    const mount = document.getElementById('root');
    const root = createRoot(mount);
    flushSync(() => root.render(strict ? <StrictMode><Startup entry="/login" /></StrictMode> : <Startup entry="/login" />));
    const button = text => [...mount.querySelectorAll('button')].find(node => node.textContent.trim() === text);
    // Provider lookup is a real Query, including startup reset and its retry.
    for (let i = 0; i < 200 && !button('Google でログイン'); i++) await tick();
    results[`providers-${strict ? 'strict' : 'normal'}`] = !!button('Google でログイン');
    flushSync(() => button('パスワードでログイン')?.click());
    for (const [selector, value] of [['input[autocomplete="username"]', 'next'], ['input[type="password"]', 'synthetic-password']]) {
      const input = mount.querySelector(selector);
      if (!input) throw new Error(`Login field missing: ${selector}`);
      flushSync(() => {
        Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set.call(input, value);
        input.dispatchEvent(new Event('input', { bubbles: true }));
      });
    }
    flushSync(() => mount.querySelector('form').requestSubmit()); await tick();
    const login = requests.find(request => request.config.url === '/api/auth/login');
    if (!login || JSON.parse(login.config.data).username !== 'next') throw new Error('Password login did not submit entered credentials');
    respond(login, { token: 'synthetic-password-token', user: user('next') }); await tick(); await tick();
    results[`password-${strict ? 'strict' : 'normal'}`] = !!document.getElementById('home') &&
      useAuthStore.getState().user?.id === 'next' && localStorage.getItem('setori_token') === 'synthetic-password-token';
    flushSync(() => root.unmount()); queryClient.clear();
  }
  document.getElementById('result').textContent = JSON.stringify(results);
}
run().catch(error => { document.getElementById('result').textContent = JSON.stringify({ error: error.message, stack: error.stack }); });
