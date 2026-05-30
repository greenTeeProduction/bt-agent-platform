/* === Agents Tab === */

function renderAgents() {
  return `
    <div class="header"><h1>BT Agents</h1><span class="badge green" id="agent-count">—</span></div>
    <div id="agents-container"><div class="loading">Loading agents...</div></div>
  `;
}

async function loadAgents() {
  try {
    const agents = await apiFetch('/agents');
    if (state.activeTab !== 'agents') return;

    const countEl = document.getElementById('agent-count');
    const container = document.getElementById('agents-container');
    if (countEl) countEl.textContent = agents.length + ' agents';
    if (!container) return;

    if (agents.length === 0) {
      container.innerHTML = '<div class="empty"><div class="icon">🤖</div>No agents registered</div>';
      return;
    }

    container.innerHTML = agents.map(a => {
      const ratePct = Math.round(a.success_rate * 100);
      const rateColor = ratePct >= 50 ? 'green' : ratePct >= 25 ? 'amber' : 'red';
      return `
        <div class="task-card">
          <div class="task-header">
            <span class="task-title">${a.name}</span>
            <span class="badge ${rateColor}">${ratePct}% success</span>
          </div>
          <div style="font-size:12px;color:var(--text-tertiary);margin:8px 0">${a.description || ''}</div>
          <div class="task-meta">
            <span>🌳 ${a.tree || '—'}</span>
            <span>⏱ ${a.schedule || 'on demand'}</span>
            <span>🔄 ${a.total_runs} runs</span>
            <span>⭐ ${a.avg_quality.toFixed(2)} avg quality</span>
          </div>
          <div class="task-meta">
            <span>Last: ${a.last_run || 'never'}</span>
            <span class="badge ${a.last_outcome === 'success' ? 'green' : a.last_outcome === 'failed' ? 'red' : 'blue'}">${a.last_outcome || '—'}</span>
          </div>
        </div>
      `;
    }).join('');
  } catch (e) {
    const container = document.getElementById('agents-container');
    if (container) container.innerHTML = '<div class="empty"><div class="icon">⚠</div>Error loading agents</div>';
  }
}

// Load on tab activation
setInterval(() => { if (state.activeTab === 'agents') loadAgents(); }, 30000);
setTimeout(loadAgents, 800);
