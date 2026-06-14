/* === Workflows Tab — YAML pipeline runner === */

let workflowPollTimer = null;

function renderWorkflows() {
  return `
    <div class="header">
      <h1>Workflows</h1>
      <span class="badge blue" id="workflow-count">—</span>
    </div>
    <div style="margin-bottom:16px;padding:12px;background:var(--surface);border-radius:8px;border:1px solid var(--border)">
      <div style="font-size:14px;font-weight:600;margin-bottom:8px">▶ Run pipeline</div>
      <div style="display:flex;gap:8px;flex-wrap:wrap;align-items:flex-end">
        <select id="workflow-select" style="flex:1;min-width:180px;padding:6px 8px;border:1px solid var(--border);border-radius:4px;background:var(--bg);color:var(--text-primary);font-size:12px">
          <option value="">Select workflow...</option>
        </select>
        <input id="workflow-input" placeholder="Input text (optional)" style="flex:2;min-width:200px;padding:6px 8px;border:1px solid var(--border);border-radius:4px;background:var(--bg);color:var(--text-primary);font-size:12px">
        <button onclick="runWorkflow()" style="padding:6px 16px;background:var(--accent);color:#fff;border:none;border-radius:4px;cursor:pointer;font-size:12px;font-weight:600">Run</button>
      </div>
      <div id="workflow-run-error" style="color:var(--danger);font-size:11px;margin-top:6px;display:none"></div>
    </div>
    <div id="workflow-status" style="margin-bottom:16px"></div>
    <div id="workflows-list"><div class="loading">Loading workflows...</div></div>
  `;
}

async function loadWorkflows() {
  const listEl = document.getElementById('workflows-list');
  const selectEl = document.getElementById('workflow-select');
  const countEl = document.getElementById('workflow-count');
  if (!listEl) return;

  try {
    const pipelines = await apiFetch('/pipelines');
    if (state.activeTab !== 'workflows') return;

    if (countEl) countEl.textContent = (pipelines.length || 0) + ' workflows';

    if (selectEl) {
      const current = selectEl.value;
      selectEl.innerHTML = '<option value="">Select workflow...</option>' +
        pipelines.map(function(p) {
          const name = p.filename ? p.filename.replace(/\.yaml$/, '') : p.name;
          return '<option value="' + name + '">' + (p.name || name) + '</option>';
        }).join('');
      if (current) selectEl.value = current;
    }

    if (!pipelines.length) {
      listEl.innerHTML = '<div class="empty"><div class="icon">📋</div>No workflows found in ~/.go-bt-evolve/agents/workflows/</div>';
      return;
    }

    listEl.innerHTML = pipelines.map(function(p) {
      return ''
        + '<div class="task-card">'
        + '  <div class="task-header"><span class="task-title">' + (p.name || p.filename) + '</span>'
        + '    <span class="badge blue">' + p.step_count + ' steps</span></div>'
        + '  <div style="font-size:12px;color:var(--text-tertiary);margin:8px 0">' + (p.description || '') + '</div>'
        + '  <div class="task-meta"><span>📄 ' + (p.filename || '') + '</span><span>v' + (p.version || '1') + '</span></div>'
        + '</div>';
    }).join('');
  } catch (e) {
    listEl.innerHTML = '<div class="empty"><div class="icon">⚠</div>Failed to load workflows</div>';
  }
}

async function runWorkflow() {
  const selectEl = document.getElementById('workflow-select');
  const inputEl = document.getElementById('workflow-input');
  const errEl = document.getElementById('workflow-run-error');
  const statusEl = document.getElementById('workflow-status');
  if (!selectEl || !statusEl) return;

  const pipelineName = selectEl.value;
  const input = inputEl ? inputEl.value : '';
  errEl.style.display = 'none';

  if (!pipelineName) {
    errEl.textContent = 'Select a workflow first';
    errEl.style.display = 'block';
    return;
  }

  statusEl.innerHTML = '<div class="loading">Starting pipeline...</div>';

  try {
    const resp = await apiFetch('/pipelines/run', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ pipeline_name: pipelineName, input: input })
    });
    if (resp.error) {
      statusEl.innerHTML = '<div class="empty">Error: ' + resp.error + '</div>';
      return;
    }
    pollWorkflowStatus(resp.run_id);
  } catch (e) {
    statusEl.innerHTML = '<div class="empty">Run failed: ' + e.message + '</div>';
  }
}

function pollWorkflowStatus(runId) {
  if (workflowPollTimer) clearInterval(workflowPollTimer);
  const statusEl = document.getElementById('workflow-status');
  if (!statusEl) return;

  async function tick() {
    try {
      const s = await apiFetch('/pipelines/status?id=' + encodeURIComponent(runId));
      if (state.activeTab !== 'workflows') {
        clearInterval(workflowPollTimer);
        return;
      }

      let html = ''
        + '<div class="task-card">'
        + '  <div class="task-header"><span class="task-title">Run ' + runId + '</span>'
        + '    <span class="badge ' + (s.status === 'complete' ? 'green' : s.status === 'failed' ? 'red' : 'amber') + '">' + s.status + '</span></div>';

      if (s.status === 'running') {
        html += '<div style="font-size:12px;margin:8px 0;color:var(--text-tertiary)">Pipeline running… approve any HITL steps from the Tasks tab.</div>';
      }
      if (s.error) {
        html += '<div style="font-size:12px;color:var(--danger);margin:8px 0">' + s.error + '</div>';
      }
      if (s.steps && s.steps.length) {
        html += '<div style="margin-top:8px;font-size:12px">';
        s.steps.forEach(function(step) {
          html += '<div style="padding:6px 0;border-bottom:1px solid var(--border)">'
            + '<strong>' + step.step_id + '</strong> · ' + step.outcome
            + (step.hitl_task_id ? ' · <code style="font-size:10px">' + step.hitl_task_id + '</code>' : '')
            + '</div>';
        });
        html += '</div>';
      }
      html += '</div>';
      statusEl.innerHTML = html;

      if (s.status === 'complete' || s.status === 'failed') {
        clearInterval(workflowPollTimer);
        workflowPollTimer = null;
      }
    } catch (e) {
      statusEl.innerHTML = '<div class="empty">Status poll failed</div>';
      clearInterval(workflowPollTimer);
    }
  }

  tick();
  workflowPollTimer = setInterval(tick, 2000);
}

setTimeout(function() {
  if (state.activeTab === 'workflows') loadWorkflows();
}, 800);
