/* === API Helper — fetch with retry + error handling === */

const API = '/api';

/**
 * HTML-escape a value for interpolation into innerHTML markup (element bodies
 * and double-quoted attribute values). Agent and tree names/descriptions/ids
 * are partly user-influenced; interpolating them raw is an injection surface.
 * @param {any} s
 * @returns {string}
 */
function esc(s) {
  return String(s ?? '').replace(/[&<>"']/g, function (c) {
    return { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c];
  });
}

function getCookie(name) {
  const value = `; ${document.cookie}`;
  const parts = value.split(`; ${name}=`);
  if (parts.length === 2) return parts.pop().split(';').shift();
}

/**
 * Fetch JSON from API endpoint with automatic retry.
 * @param {string} path - API path (e.g. '/trees')
 * @param {object} [opts] - fetch options
 * @param {number} [retries=2] - max retries on 5xx/network errors
 * @returns {Promise<any>} parsed JSON
 */
async function apiFetch(path, opts = {}, retries = 2) {
  const url = API + path;

  // Copy headers so callers can reuse options, including Headers instances.
  const headers = new Headers(opts.headers);
  const method = (opts.method || 'GET').toUpperCase();
  if (!['GET', 'HEAD', 'OPTIONS'].includes(method)) {
    if (!headers.has('Idempotency-Key')) retries = 0;
    const csrfToken = getCookie('_csrf_token');
    if (csrfToken) headers.set('X-CSRF-Token', csrfToken);
  }

  for (let attempt = 0; attempt <= retries; attempt++) {
    let res;
    try {
      res = await fetch(url, { ...opts, headers });
    } catch (err) {
      if (err.name === 'AbortError' || opts.signal?.aborted || attempt >= retries) throw err;
      await sleep(1000 * (attempt + 1));
      continue;
    }
    if (res.status >= 500 && attempt < retries) {
      await sleep(1000 * (attempt + 1));
      continue;
    }
    if (!res.ok) {
      let message = res.statusText;
      try {
        const body = await res.json();
        message = body.error || body.message || message;
      } catch (_) { /* Non-JSON error responses still retain their HTTP status. */ }
      const error = new Error(`HTTP ${res.status}: ${message}`);
      error.status = res.status;
      if (res.status === 401 && path !== '/login' && path !== '/session') {
        window.dispatchEvent(new Event('bt:unauthorized'));
      }
      throw error;
    }
    return res.status === 204 ? null : res.json();
  }
}

/**
 * Fetch raw response (for SSE or non-JSON endpoints).
 */
async function apiFetchRaw(path, opts = {}) {
  return fetch(API + path, opts);
}

function sleep(ms) {
  return new Promise(resolve => setTimeout(resolve, ms));
}

/* Expose globally */
window.apiFetch = apiFetch;
window.apiFetchRaw = apiFetchRaw;

async function apiPost(path, body = {}) {
  const headers = {'Content-Type': 'application/json'};
  if (path === '/sprint/execute') headers['Idempotency-Key'] = crypto.randomUUID();
  return apiFetch(path, {method: 'POST', headers, body: JSON.stringify(body)});
}
window.apiPost = apiPost;
