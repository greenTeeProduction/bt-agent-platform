const { test, expect } = require('@playwright/test');
const fs = require('node:fs/promises');
const path = require('node:path');

// Serve the real dashboard assets with a deterministic session API. These
// browser tests need no running daemon and cannot mutate production state.
test('sign in, restore session, handle expiry, and sign out', async ({ page }) => {
  let signedIn = false;
  let loginCalls = 0;
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  await page.route('**/*', async route => {
    const request = route.request();
    const url = new URL(request.url());
    if (url.origin !== 'http://localhost:9800') return route.abort();
    if (!url.pathname.startsWith('/api/')) {
      const file = url.pathname === '/' ? 'index.html' : url.pathname.slice('/static/'.length);
      const body = await fs.readFile(path.join(__dirname, '../../cmd/bt-dashboard/static', file));
      const contentType = file.endsWith('.js') ? 'text/javascript' : file.endsWith('.css') ? 'text/css' : 'text/html';
      return route.fulfill({ body, contentType, headers: { 'Set-Cookie': '_csrf_token=browser-test; Path=/; SameSite=Strict' } });
    }
    const json = (status, body) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) });
    if (request.method() === 'POST') {
      expect(request.headers()['x-csrf-token']).toBe('browser-test');
    }
    if (url.pathname === '/api/login') {
      loginCalls++;
      signedIn = request.postDataJSON().password === 'test-platform-key';
      return json(signedIn ? 200 : 401, signedIn ? { status: 'authenticated' } : { error: 'invalid password' });
    }
    if (url.pathname === '/api/logout') {
      signedIn = false;
      return json(200, { status: 'logged_out' });
    }
    if (!signedIn) return json(401, { error: 'unauthorized' });
    const responses = {
      '/api/session': { status: 'authenticated' },
      '/api/trees': [{ id: 'test-tree', name: 'Test tree', category: 'core' }],
      '/api/thinktank/fellows': [],
      '/api/company/default': {},
      '/api/metrics/live': { system: { disk_root: {}, disk_ssd: {}, memory: {} }, trees: { total: 1 } },
    };
    return json(200, responses[url.pathname] || {});
  });

  await page.goto('/');
  await expect(page.getByRole('heading', { name: 'Sign in to BT Studio' })).toBeVisible();
  await expect(page.locator('[data-tab="trees"]')).toBeDisabled();
  await page.getByLabel('API key', { exact: true }).fill('wrong');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('Invalid API key');
  expect(loginCalls).toBe(1);

  await page.getByLabel('API key', { exact: true }).fill('test-platform-key');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Dashboard', exact: true })).toBeVisible();
  await expect(page.locator('[data-tab="trees"]')).toBeEnabled();
  expect(await page.evaluate(() => Object.values(localStorage).concat(Object.values(sessionStorage)))).not.toContain('test-platform-key');

  await page.reload();
  await expect(page.getByRole('heading', { name: 'Dashboard', exact: true })).toBeVisible();
  signedIn = false;
  await page.evaluate(() => window.apiFetch('/trees').catch(() => {}));
  await expect(page.getByRole('alert')).toContainText('session has expired');
  await expect(page.locator('[data-tab="trees"]')).toBeDisabled();

  await page.getByLabel('API key', { exact: true }).fill('test-platform-key');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Dashboard', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Sign out', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Sign in to BT Studio' })).toBeVisible();
  expect(signedIn).toBe(false);
  expect(errors).toEqual([]);
});
