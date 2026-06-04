/* === Agents Tab === */

function renderAgents() {
  return `
    <div class="header"><h1>BT Agents</h1><span class="badge green" id="agent-count">—</span></div>
    <div id="agents-create-form" style="margin:16px 0; padding:12px; background:var(--surface); border-radius:8px; border:1px solid var(--border)">
      <div style="font-size:14px;font-weight:600;margin-bottom:8px;color:var(--text-primary)">➕ Create Agent</div>
      <div style="display:flex;gap:8px;flex-wrap:wrap;align-items:flex-end">
        <input id="create-name" placeholder="Agent name" style="flex:1;min-width:120px;padding:6px 8px;border:1px solid var(--border);border-radius:4px;background:var(--bg);color:var(--text-primary);font-size:12px">
        <input id="create-desc" placeholder="Description" style="flex:2;min-width:180px;padding:6px 8px;border:1px solid var(--border);border-radius:4px;background:var(--bg);color:var(--text-primary);font-size:12px">
        <select id="create-tree" style="padding:6px 8px;border:1px solid var(--border);border-radius:4px;background:var(--bg);color:var(--text-primary);font-size:12px">
          <option value="">Select tree...</option>
          <option value="godev">godev</option>
          <option value="domain:code_review">domain:code_review</option>
          <option value="domain:devops_ci">domain:devops_ci</option>
          <option value="domain:agent_monitor">domain:agent_monitor</option>
          <option value="domain:refactoring">domain:refactoring</option>
          <option value="domain:security_audit">domain:security_audit</option>
          <option value="domain:data_pipeline">domain:data_pipeline</option>
          <option value="domain:meeting_notes">domain:meeting_notes</option>
          <option value="domain:crash_investigator">domain:crash_investigator</option>
          <option value="domain:game_ai">domain:game_ai</option>
          <option value="domain:trading_signal">domain:trading_signal</option>
          <option value="research:deep_research">research:deep_research</option>
          <option value="research:quick_research">research:quick_research</option>
          <option value="thinktank:council">thinktank:council</option>
          <option value="finance:pitch_agent">finance:pitch_agent</option>
          <option value="finance:earnings_reviewer">finance:earnings_reviewer</option>
          <option value="finance:market_researcher">finance:market_researcher</option>
        </select>
        <input id="create-schedule" placeholder="Schedule (e.g., every 1h)" style="flex:1;min-width:140px;padding:6px 8px;border:1px solid var(--border);border-radius:4px;background:var(--bg);color:var(--text-primary);font-size:12px">
        <button onclick="createAgent()" style="padding:6px 16px;background:var(--accent);color:#fff;border:none;border-radius:4px;cursor:pointer;font-size:12px;font-weight:600">Create</button>
      </div>
      <div id="create-error" style="color:var(--danger);font-size:11px;margin-top:6px;display:none"></div>
    </div>
    <div id="agents-container"><div class="loading">Loading agents...</div></div>
  `;
}

async function createAgent() {
  const name = document.getElementById('create-name').value.trim();
  const description = document.getElementById('create-desc').value.trim();
  const tree = document.getElementById('create-tree').value;
  const schedule = document.getElementById('create-schedule').value.trim();
  const errEl = document.getElementById('create-error');

  errEl.style.display = 'none';
  if (!name) { errEl.textContent = 'Agent name is required'; errEl.style.display = 'block'; return; }
  if (!tree) { errEl.textContent = 'Tree is required'; errEl.style.display = 'block'; return; }

  try {
    const resp = await apiFetch('/agents/create', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name, description, tree, schedule: schedule || 'on_demand' })
    });
    if (resp.error) { errEl.textContent = resp.error; errEl.style.display = 'block'; return; }
    document.getElementById('create-name').value = '';
    document.getElementById('create-desc').value = '';
    document.getElementById('create-tree').value = '';
    document.getElementById('create-schedule').value = '';
    loadAgents();
  } catch (e) {
    errEl.textContent = 'Failed to create agent: ' + e.message;
    errEl.style.display = 'block';
  }
}

async function deleteAgent(name) {
  if (!confirm('Delete agent "' + name + '"? This cannot be undone.')) return;
  try {
    const resp = await apiFetch('/agents/delete', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name })
    });
    if (resp.error) { alert('Delete failed: ' + resp.error); return; }
    loadAgents();
  } catch (e) {
    alert('Delete failed: ' + e.message);
  }
}

async function runAgent(name) {
  const buttons = document.querySelectorAll('.agent-run-btn');
  let btn = null;
  buttons.forEach(function(b) {
    if (b.getAttribute('data-agent') === name) btn = b;
  });
  if (btn) { btn.textContent = '⏳ Running...'; btn.disabled = true; }
  try {
    const resp = await apiFetch('/agents/run?agent=' + encodeURIComponent(name) + '&task=Execute+scheduled+task');
    if (btn) { btn.textContent = '▶ Run'; btn.disabled = false; }
    const outcome = resp.outcome || 'unknown';
    if (btn) {
      btn.textContent = outcome === 'success' ? '✅ Done' : '❌ Failed';
      setTimeout(function() { if (btn) btn.textContent = '▶ Run'; }, 2000);
    }
  } catch (e) {
    if (btn) { btn.textContent = '▶ Run'; btn.disabled = false; }
  }
}

function cbIcon(status) {
  switch (status) {
    case 'closed': return '🟢 closed';
    case 'open': return '🔴 open';
    case 'half_open': return '🟡 half_open';
    default: return '⚪ unknown';
  }
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

    container.innerHTML = agents.map(function(a) {
      var ratePct = Math.round(a.success_rate * 100);
      var rateColor = ratePct >= 50 ? 'green' : ratePct >= 25 ? 'amber' : 'red';
      var cbStatus = a.cb_status || 'unknown';
      var escName = a.name.replace(/'/g, "\\'");
      return ''
        + '<div class="task-card">'
        + '  <div class="task-header">'
        + '    <span class="task-title">' + a.name + '</span>'
        + '    <span class="badge ' + rateColor + '">' + ratePct + '% success</span>'
        + '    <span style="font-size:11px;margin-left:4px" title="Circuit breaker: ' + cbStatus + '">' + cbIcon(cbStatus) + '</span>'
        + '  </div>'
        + '  <div style="font-size:12px;color:var(--text-tertiary);margin:8px 0">' + (a.description || '') + '</div>'
        + '  <div class="task-meta">'
        + '    <span>🌳 ' + (a.tree || '—') + '</span>'
        + '    <span>⏱ ' + (a.schedule || 'on demand') + '</span>'
        + '    <span>🔄 ' + a.total_runs + ' runs</span>'
        + '    <span>⭐ ' + a.avg_quality.toFixed(2) + ' avg quality</span>'
        + '  </div>'
        + '  <div class="task-meta">'
        + '    <span>Last: ' + (a.last_run || 'never') + '</span>'
        + '    <span class="badge ' + (a.last_outcome === 'success' ? 'green' : a.last_outcome === 'failed' ? 'red' : 'blue') + '">' + (a.last_outcome || '—') + '</span>'
        + '  </div>'
        + '  <div style="margin-top:10px;display:flex;gap:8px">'
        + '    <button class="agent-run-btn" data-agent="' + escName + '" onclick="runAgent(\'' + escName + '\')" style="padding:4px 12px;background:var(--accent);color:#fff;border:none;border-radius:4px;cursor:pointer;font-size:11px;font-weight:600">▶ Run</button>'
        + '    <button onclick="deleteAgent(\'' + escName + '\')" style="padding:4px 12px;background:var(--danger);color:#fff;border:none;border-radius:4px;cursor:pointer;font-size:11px">🗑 Delete</button>'
        + '  </div>'
        + '</div>';
    }).join('');
  } catch (e) {
    var container = document.getElementById('agents-container');
    if (container) container.innerHTML = '<div class="empty"><div class="icon">⚠</div>Error loading agents</div>';
  }
}

// Load on tab activation
setInterval(function() { if (state.activeTab === 'agents') loadAgents(); }, 30000);
setTimeout(loadAgents, 800);
