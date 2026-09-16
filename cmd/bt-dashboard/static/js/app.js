/* === BT Dashboard — App Router & State === */

// ─── Global State ───
const state = {
  trees: [],
  fellows: [],
  company: null,
  activeTab: 'overview',
  _cachedTasks: [],
  authenticated: false,
};

function setAuthenticated(authenticated) {
  state.authenticated = authenticated;
  document.querySelectorAll('.nav-item').forEach(button => { button.disabled = !authenticated; });
  document.getElementById('chat-toggle').disabled = !authenticated;
  document.getElementById('hamburger-btn').disabled = !authenticated;
  document.getElementById('logout-btn').hidden = !authenticated;
}

function showLogin(message = '') {
  setAuthenticated(false);
  state.trees = [];
  state.fellows = [];
  state.company = null;
  state._cachedTasks = [];
  state.activeTab = 'login';
  liveData = null;
  document.getElementById('sidebar').classList.remove('open');
  document.getElementById('sidebar-overlay').classList.remove('open');
  document.getElementById('chat-panel').classList.remove('open');
  document.getElementById('chat-messages').replaceChildren();
  document.getElementById('main-content').innerHTML = `
    <section class="login-card">
      <h1>Sign in to BT Studio</h1>
      <p>Enter your platform API key to access the dashboard.</p>
      <form id="login-form">
        <label for="login-password">API key</label>
        <input id="login-password" name="password" type="password" autocomplete="current-password" required>
        <p id="login-error" role="alert">${esc(message)}</p>
        <button class="btn btn-primary" type="submit">Sign in</button>
      </form>
    </section>`;
  document.getElementById('login-form').addEventListener('submit', async event => {
    event.preventDefault();
    const form = event.currentTarget;
    const button = form.querySelector('button');
    const password = document.getElementById('login-password');
    const error = document.getElementById('login-error');
    button.disabled = true;
    error.textContent = '';
    try {
      await apiPost('/login', { password: password.value });
      password.value = '';
      await init();
    } catch (err) {
      error.textContent = err.status === 401 ? 'Invalid API key. Please try again.' : err.message;
      password.focus();
    } finally {
      button.disabled = false;
    }
  });
  document.getElementById('login-password').focus();
}

// ─── Init ───
async function init() {
  setAuthenticated(false);
  try {
    await apiFetch('/session');
    const [trees, fellows, company] = await Promise.all([
      apiFetch('/trees'),
      apiFetch('/thinktank/fellows'),
      apiFetch('/company/default'),
    ]);
    state.trees = trees;
    state.fellows = fellows;
    state.company = company;
    setAuthenticated(true);
    renderTab('overview');
  } catch (e) {
    if (e.status === 401) {
      showLogin();
      return;
    }
    document.getElementById('main-content').innerHTML = `
      <div class="empty"><div class="icon">⚠</div>Unable to load the dashboard.
        <p>${esc(e.message)}</p><button class="btn" id="retry-init">Retry</button>
      </div>`;
    document.getElementById('retry-init').addEventListener('click', init);
  }
}

// ─── Tab Routing ───
function renderTab(tab) {
  if (!state.authenticated) return;
  state.activeTab = tab;
  document.querySelectorAll('.nav-item').forEach(b =>
    b.classList.toggle('active', b.dataset.tab === tab)
  );
  const main = document.getElementById('main-content');

  switch (tab) {
    case 'overview':  main.innerHTML = renderOverview(); break;
    case 'thinktank': main.innerHTML = renderThinkTank(); setTimeout(() => { if (state.fellows.length) renderFellows(); }, 100); break;
    case 'company':   main.innerHTML = renderCompany(); break;
    case 'tasks':     main.innerHTML = renderTasks(); setTimeout(refreshTasks, 100); break;
    case 'trees':     main.innerHTML = renderTrees(); break;
    case 'mindmap':   main.innerHTML = renderMindMap(); setTimeout(loadMindMap, 200); break;
    case 'evolution': main.innerHTML = renderEvolution(); break;
    case 'agents':    main.innerHTML = renderAgents(); setTimeout(loadAgents, 200); setTimeout(populateTreeDropdown, 200); break;
    case 'workflows': main.innerHTML = renderWorkflows(); setTimeout(loadWorkflows, 200); break;
    case 'scalability': main.innerHTML = renderScalability(); break;
    case 'doormate': main.innerHTML = renderDoormate(); setTimeout(initDoormateTab, 100); break;
  }
}

// ─── Category Colors ───
function catColor(cat) {
  const cc = {
    finance: '#27a644', domain: '#3b82f6', research: '#7170ff',
    startup: '#f59e0b', thinktank: '#3b82f6', evolution: '#ec4899', core: '#62666d'
  };
  return cc[cat] || '#62666d';
}

// ─── Event Listeners ───
window.addEventListener('bt:unauthorized', () => {
  if (state.authenticated) showLogin('Your session has expired. Please sign in again.');
});

document.getElementById('logout-btn').addEventListener('click', async () => {
  try {
    await apiPost('/logout');
    showLogin();
  } catch (err) {
    toast('Unable to sign out: ' + err.message);
  }
});

document.querySelectorAll('.nav-item').forEach(b =>
  b.addEventListener('click', () => renderTab(b.dataset.tab))
);

// Hamburger menu
document.getElementById('hamburger-btn').addEventListener('click', () => {
  document.getElementById('sidebar').classList.toggle('open');
  document.getElementById('sidebar-overlay').classList.toggle('open');
});

document.getElementById('sidebar-overlay').addEventListener('click', () => {
  document.getElementById('sidebar').classList.remove('open');
  document.getElementById('sidebar-overlay').classList.remove('open');
});

// Chat toggle
document.getElementById('chat-toggle').addEventListener('click', toggleChat);

// Start
init();

// ─── Keyboard Shortcuts ───
const TAB_KEYS = ['overview', 'thinktank', 'company', 'tasks', 'trees', 'mindmap', 'evolution', 'agents', 'scalability', 'doormate'];
document.addEventListener('keydown', e => {
  if (!state.authenticated) return;
  // Don't trigger when typing in inputs
  if (e.target.tagName === 'INPUT' || e.target.tagName === 'TEXTAREA' || e.target.tagName === 'SELECT') return;
  // 1-8: switch tabs
  const num = parseInt(e.key);
  if (num >= 1 && num <= TAB_KEYS.length) {
    renderTab(TAB_KEYS[num - 1]);
    return;
  }
  // /: focus search (trees tab)
  if (e.key === '/') {
    e.preventDefault();
    renderTab('trees');
    setTimeout(() => document.getElementById('tree-search')?.focus(), 300);
    return;
  }
  // Escape: close chat/modal
  if (e.key === 'Escape') {
    document.getElementById('chat-panel').classList.remove('open');
    const modal = document.getElementById('task-modal');
    if (modal) modal.style.display = 'none';
    return;
  }
  // ?: show shortcuts
  if (e.key === '?') {
    toast('Shortcuts: 1-8 tabs, / search, Esc close, ? help', 5000);
  }
});
