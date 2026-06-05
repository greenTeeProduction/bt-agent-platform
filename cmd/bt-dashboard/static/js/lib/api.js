/* === API Helper — fetch with retry + error handling === */

const API = '/api';

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
  
  // Auto-inject CSRF token for state-changing requests
  const method = (opts.method || 'GET').toUpperCase();
  if (method !== 'GET' && method !== 'HEAD' && method !== 'OPTIONS') {
    opts.headers = opts.headers || {};
    const csrfToken = getCookie('_csrf_token');
    if (csrfToken) {
      opts.headers['X-CSRF-Token'] = csrfToken;
    }
  }

  for (let attempt = 0; attempt <= retries; attempt++) {
    try {
      const res = await fetch(url, opts);
      if (!res.ok) {
        if (res.status >= 500 && attempt < retries) {
          await sleep(1000 * (attempt + 1));
          continue;
        }
        throw new Error(`HTTP ${res.status}: ${res.statusText}`);
      }
      return await res.json();
    } catch (err) {
      if (attempt >= retries) throw err;
      await sleep(1000 * (attempt + 1));
    }
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
