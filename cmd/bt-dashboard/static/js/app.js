/* === BT Dashboard — App Router & State === */

// ─── Global State ───
const state = {
  trees: [],
  fellows: [],
  company: null,
  activeTab: 'overview',
  _cachedTasks: [],
};

// ─── Init ───
async function init() {
  try {
    const [trees, fellows, company] = await Promise.all([
      apiFetch('/trees'),
      apiFetch('/thinktank/fellows'),
      apiFetch('/company/default'),
    ]);
    state.trees = trees;
    state.fellows = fellows;
    state.company = company;
    renderTab('overview');
  } catch (e) {
    document.getElementById('main-content').innerHTML =
      '<div class="empty"><div class="icon">⚠</div>Failed to load dashboard. Is the server running?</div>';
  }
}

// ─── Tab Routing ───
function renderTab(tab) {
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
    case 'agents':    main.innerHTML = renderAgents(); setTimeout(loadAgents, 200); break;
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
const TAB_KEYS = ['overview', 'thinktank', 'company', 'tasks', 'trees', 'mindmap', 'evolution', 'agents'];
document.addEventListener('keydown', e => {
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
