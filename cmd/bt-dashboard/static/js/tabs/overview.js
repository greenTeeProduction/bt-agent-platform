/* === Overview Tab — Live Data === */

let liveData = null;

function renderOverview() {
  // Initial render with loading state — real data fills in via poll
  return `
    <div class="header">
      <h1>Dashboard</h1>
      <div class="header-stats"><span class="status-dot"></span>Live · <span id="ov-tree-count">${state.trees.length}</span> trees</div>
    </div>
    <div class="grid-4" id="ov-stats">
      <div class="stat-card green"><div class="label">Behavior Trees</div><div class="value">${state.trees.length}</div><div class="trend">7 categories</div></div>
      <div class="stat-card blue"><div class="label">System Health</div><div class="value" id="ov-disk">—</div><div class="trend" id="ov-mem">loading...</div></div>
      <div class="stat-card amber"><div class="label">BT Processes</div><div class="value" id="ov-procs">—</div><div class="trend">running</div></div>
      <div class="stat-card purple"><div class="label">Uptime</div><div class="value" id="ov-uptime">—</div><div class="trend" id="ov-gardener">gardener —</div></div>
    </div>
    <div class="grid-2">
      <div>
        <div class="section-title">📊 Categories</div>
        <div id="ov-categories">${renderCategories(state.trees)}</div>
      </div>
      <div>
        <div class="section-title">⚡ System Status</div>
        <div id="ov-system"></div>
      </div>
    </div>
  `;
}

function renderCategories(trees) {
  const cats = {};
  trees.forEach(t => { if (!cats[t.category]) cats[t.category] = []; cats[t.category].push(t); });
  return Object.entries(cats).map(([k, v]) => `
    <div class="table-row">
      <div class="icon-cell" style="background:${catColor(k)}">${v.length}</div>
      <div class="content"><div class="title">${k}</div><div class="subtitle">${v.length} trees</div></div>
      <div class="meta"><span class="badge ${k === 'evolution' ? 'purple' : 'blue'}">active</span></div>
    </div>
  `).join('');
}

function renderSystemStatus(data) {
  if (!data) return '<div class="loading">Loading system data...</div>';
  const s = data.system;
  const g = data.gardener;
  return `
    <div class="table-row"><div class="icon-cell" style="background:${s.disk_root.ok ? 'var(--green)' : 'var(--red)'}">💾</div>
      <div class="content"><div class="title">Disk /</div><div class="subtitle">${s.disk_root.used_gb}G / ${s.disk_root.total_gb}G (${s.disk_root.percent_use}%)</div></div>
      <div class="meta"><span class="badge ${s.disk_root.ok ? 'green' : 'red'}">${s.disk_root.ok ? 'OK' : 'FULL'}</span></div></div>
    <div class="table-row"><div class="icon-cell" style="background:${s.disk_ssd.ok ? 'var(--green)' : 'var(--red)'}">💾</div>
      <div class="content"><div class="title">Disk /mnt/ssd</div><div class="subtitle">${s.disk_ssd.used_gb}G / ${s.disk_ssd.total_gb}G (${s.disk_ssd.percent_use}%)</div></div>
      <div class="meta"><span class="badge ${s.disk_ssd.ok ? 'green' : 'red'}">${s.disk_ssd.ok ? 'OK' : 'LOW'}</span></div></div>
    <div class="table-row"><div class="icon-cell" style="background:var(--blue)">🧠</div>
      <div class="content"><div class="title">Memory</div><div class="subtitle">${s.memory.used_gb}G / ${s.memory.total_gb}G (${s.memory.percent_use}%)</div></div>
      <div class="meta">${s.memory.available_gb}G free</div></div>
    <div class="table-row"><div class="icon-cell" style="background:var(--accent-bg)">⚙</div>
      <div class="content"><div class="title">BT Processes</div><div class="subtitle">${s.processes} bt-* processes running</div></div>
      <div class="meta">uptime ${s.uptime}</div></div>
    ${g ? `
    <div class="table-row"><div class="icon-cell" style="background:#ec4899">🌱</div>
      <div class="content"><div class="title">Gardener</div><div class="subtitle">${g.cycles} cycles · ${g.trees} trees · fitness ${g.best_fitness.toFixed(1)}</div></div>
      <div class="meta"><span class="badge purple">active</span></div></div>
    ` : ''}
  `;
}

async function pollLiveData() {
  try {
    liveData = await apiFetch('/metrics/live');
    updateOverviewStats(liveData);
  } catch (e) {
    // Silent — retry next poll
  }
}

function updateOverviewStats(data) {
  if (!data || state.activeTab !== 'overview') return;
  const s = data.system;

  // Stat cards
  const diskEl = document.getElementById('ov-disk');
  const memEl = document.getElementById('ov-mem');
  const procsEl = document.getElementById('ov-procs');
  const uptimeEl = document.getElementById('ov-uptime');
  const gardenerEl = document.getElementById('ov-gardener');

  if (diskEl) diskEl.textContent = s.disk_root.percent_use + '%';
  if (memEl) memEl.textContent = s.memory.used_gb + 'G / ' + s.memory.total_gb + 'G';
  if (procsEl) procsEl.textContent = s.processes;
  if (uptimeEl) uptimeEl.textContent = s.uptime;
  if (gardenerEl && data.gardener) gardenerEl.textContent = 'gardener ' + data.gardener.cycles + ' cycles';

  // System status panel
  const sysEl = document.getElementById('ov-system');
  if (sysEl) sysEl.innerHTML = renderSystemStatus(data);

  // Tree count
  const treeEl = document.getElementById('ov-tree-count');
  if (treeEl) treeEl.textContent = data.trees.total;
}

// Poll every 30 seconds
setInterval(pollLiveData, 30000);
// Initial poll
setTimeout(pollLiveData, 500);
