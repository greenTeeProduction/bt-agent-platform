const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');
const vm = require('node:vm');

function loadAPI(fetch) {
  const window = new EventTarget();
  const context = vm.createContext({
    window, fetch, Headers, Event,
    document: { cookie: '_csrf_token=test-csrf' },
    setTimeout: callback => callback(),
  });
  vm.runInContext(fs.readFileSync(path.join(__dirname, '../../cmd/bt-dashboard/static/js/lib/api.js'), 'utf8'), context);
  return window;
}

test('unauthorized responses fail immediately and retain status for sign-in', async () => {
  let requests = 0;
  const api = loadAPI(async () => {
    requests++;
    return new Response(JSON.stringify({ error: 'sign in required' }), { status: 401 });
  });
  let expired = 0;
  api.addEventListener('bt:unauthorized', () => expired++);
  await assert.rejects(api.apiFetch('/trees'), error => error.status === 401 && error.message.includes('sign in required'));
  assert.equal(requests, 1);
  assert.equal(expired, 1);
});

test('login failures stay on the form without broadcasting session expiry', async () => {
  const api = loadAPI(async () => new Response(JSON.stringify({ error: 'invalid password' }), { status: 401 }));
  let expired = 0;
  api.addEventListener('bt:unauthorized', () => expired++);
  await assert.rejects(api.apiPost('/login', { password: 'wrong' }), /invalid password/);
  assert.equal(expired, 0);
});

test('mutations receive CSRF headers without changing caller options or retrying', async () => {
  let requests = 0;
  const headers = new Headers({ 'Content-Type': 'application/json' });
  const api = loadAPI(async (_, options) => {
    requests++;
    assert.equal(new Headers(options.headers).get('X-CSRF-Token'), 'test-csrf');
    throw new TypeError('network error');
  });
  await assert.rejects(api.apiFetch('/tasks/create', { method: 'POST', headers }), /network error/);
  assert.equal(requests, 1);
  assert.equal(headers.has('X-CSRF-Token'), false);
});

test('safe requests retry transient failures', async () => {
  let requests = 0;
  const api = loadAPI(async () => {
    requests++;
    if (requests === 1) throw new TypeError('network error');
    if (requests === 2) return new Response('unavailable', { status: 503 });
    return new Response('{"ok":true}');
  });
  assert.equal((await api.apiFetch('/trees')).ok, true);
  assert.equal(requests, 3);
});

test('idempotency headers are case insensitive and retained across retries', async () => {
  let requests = 0;
  const api = loadAPI(async (_, options) => {
    assert.equal(new Headers(options.headers).get('Idempotency-Key'), 'same-job');
    requests++;
    return requests === 1 ? new Response('', { status: 503 }) : new Response('{}');
  });
  await api.apiFetch('/sprint/execute', { method: 'POST', headers: { 'idempotency-key': 'same-job' } });
  assert.equal(requests, 2);
});

test('cancellation and malformed successful responses are not retried', async () => {
  for (const fetch of [
    async () => { throw new DOMException('cancelled', 'AbortError'); },
    async () => new Response('not JSON'),
  ]) {
    let requests = 0;
    const api = loadAPI(async () => { requests++; return fetch(); });
    await assert.rejects(api.apiFetch('/trees'));
    assert.equal(requests, 1);
  }
});
