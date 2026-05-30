/* === ThinkTank Tab === */

function renderThinkTank() {
  return `
    <div class="header"><h1>ThinkTank</h1><span class="badge purple">5 Fellows</span></div>
    <div class="section-title">Run Analysis</div>
    <div style="display:flex;gap:8px;margin-bottom:24px">
      <input id="tt-topic" placeholder="What should the thinktank analyze?" value="Review the BT framework and recommend improvements" style="flex:1">
      <button class="btn btn-primary" onclick="runThinkTank()">▶ Run Analysis</button>
    </div>
    <div class="section-title">Analytical Fellows</div>
    <div id="fellows-container"><div class="loading">Loading fellows...</div></div>
    <div id="tt-results"></div>
  `;
}

function renderFellows() {
  const colors = { bull: '#27a644', bear: '#ef4444', technical: '#3b82f6', macro: '#7170ff', contrarian: '#f59e0b' };
  document.getElementById('fellows-container').innerHTML = state.fellows.map(f => `
    <div class="fellow-card">
      <div class="fellow-avatar" style="background:${colors[f.role] || '#62666d'}">${f.name[0]}</div>
      <div class="fellow-info">
        <div class="fellow-name">${f.name}</div>
        <div class="fellow-role">${f.role} · ${(f.perspective || '').slice(0, 50)}</div>
      </div>
      <div class="fellow-confidence">${Math.round((f.confidence || 0) * 100)}%</div>
    </div>
  `).join('');
}

async function runThinkTank() {
  const topic = document.getElementById('tt-topic').value;
  const res = document.getElementById('tt-results');
  res.innerHTML = '<div class="loading">Running 5-fellow analysis... (2-3 minutes)</div>';
  try {
    const r = await apiFetch('/thinktank/analyze?topic=' + encodeURIComponent(topic));
    res.innerHTML = '<div class="section-title">Results</div>' + r.findings.map(f => `
      <div class="task-card">
        <div class="task-header">
          <span class="task-title">${f.fellow}</span>
          <span class="badge blue">${f.role} · ${Math.round(f.confidence * 100)}%</span>
        </div>
        <div style="font-size:13px;color:var(--text-tertiary);margin-top:8px">${(f.insights || []).slice(0, 3).join('<br>')}</div>
      </div>
    `).join('');
    toast('Analysis complete — ' + r.findings.length + ' findings');
  } catch (e) {
    res.innerHTML = '<div class="empty"><div class="icon">⚠</div>Error: ' + e.message + '</div>';
  }
}
