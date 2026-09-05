/* === Evolution Tab — Live Gardener Data === */

function renderEvolution() {
  return `
    <div class="header"><h1>Evolution</h1><span class="badge purple">Active</span></div>
    <div class="grid-4" style="margin-bottom:24px" id="evo-stats">
      <div class="stat-card green"><div class="label">Gardener Cycles</div><div class="value">—</div></div>
      <div class="stat-card blue"><div class="label">Active Trees</div><div class="value">—</div></div>
      <div class="stat-card amber"><div class="label">Best Fitness</div><div class="value">—</div></div>
      <div class="stat-card purple"><div class="label">BT Processes</div><div class="value">—</div></div>
    </div>
    <div id="evo-system"></div>
    <div class="section-title">Algorithms Active</div>
    <div class="table-row"><div class="icon-cell" style="background:var(--accent-bg)">♟</div><div class="content"><div class="title">Stockfish Evolution</div><div class="subtitle">TT + Killer Moves + Alpha-Beta + LMR</div></div><div class="meta"><span class="badge green">running</span></div></div>
    <div class="table-row"><div class="icon-cell" style="background:var(--blue)">🧬</div><div class="content"><div class="title">Genetic Algorithm</div><div class="subtitle">Population 20 · Tournament selection · Crossover</div></div><div class="meta"><span class="badge green">running</span></div></div>
    <div class="table-row"><div class="icon-cell" style="background:var(--green)">🧠</div><div class="content"><div class="title">Q-Learning</div><div class="subtitle">Epsilon-greedy · State-action mapping</div></div><div class="meta"><span class="badge green">running</span></div></div>
    <div class="table-row"><div class="icon-cell" style="background:var(--amber)">📚</div><div class="content"><div class="title">Expert Knowledge</div><div class="subtitle">6 patterns · 5 anti-patterns · 10 heuristics</div></div><div class="meta"><span class="badge blue">active</span></div></div>
    <div class="table-row"><div class="icon-cell" style="background:#ec4899">🏭</div><div class="content"><div class="title">Tree Factory</div><div class="subtitle">Crossover breeding from ${state.trees.length} parent trees</div></div><div class="meta"><span class="badge blue">ready</span></div></div>
  `;
}

async function loadEvolutionData() {
  try {
    const data = await apiFetch('/metrics/live');
    if (state.activeTab !== 'evolution') return;

    const stats = document.getElementById('evo-stats');
    if (!stats) return;

    const cards = stats.querySelectorAll('.stat-card');
    if (data.gardener) {
      cards[0].querySelector('.value').textContent = data.gardener.cycles;
      cards[1].querySelector('.value').textContent = data.gardener.trees;
      cards[2].querySelector('.value').textContent = data.gardener.best_fitness.toFixed(1);
    } else {
      cards[0].querySelector('.value').textContent = '—';
      cards[1].querySelector('.value').textContent = '—';
      cards[2].querySelector('.value').textContent = '—';
    }
    cards[3].querySelector('.value').textContent = data.system.processes;

    // System panel
    const sysEl = document.getElementById('evo-system');
    if (sysEl && data.system) {
      sysEl.innerHTML = `<div class="section-title">System</div>
        <div class="table-row"><div class="icon-cell" style="background:var(--green)">⏱</div><div class="content"><div class="title">Uptime</div></div><div class="meta">${data.system.uptime}</div></div>
        <div class="table-row"><div class="icon-cell" style="background:var(--blue)">💾</div><div class="content"><div class="title">Root Disk</div></div><div class="meta">${data.system.disk_root.percent_use}% used</div></div>
        <div class="table-row"><div class="icon-cell" style="background:var(--amber)">🧠</div><div class="content"><div class="title">Memory</div></div><div class="meta">${data.system.memory.percent_use}% used</div></div>`;
    }
  } catch (e) {
    // silent
  }
}

// Load on tab activation via poll
setInterval(() => { if (state.activeTab === 'evolution') loadEvolutionData(); }, 30000);
setTimeout(loadEvolutionData, 800);
