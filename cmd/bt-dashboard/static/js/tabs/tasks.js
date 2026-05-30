/* === Tasks Tab === */

let taskFilter = 'all', taskView = 'list';
let taskHistory = {};

function renderTasks() {
  return `
    <div class="header">
      <h1>Task Pipeline</h1>
      <div style="display:flex;gap:8px">
        <span id="task-count" class="badge green">0 approved</span>
        <button class="btn btn-primary btn-sm" onclick="executeSprint()">▶ Run Sprint</button>
      </div>
    </div>
    <div style="display:flex;gap:8px;margin-bottom:16px;flex-wrap:wrap">
      ${['all','pending','approved','completed','rejected'].map(f => `
        <button class="btn btn-ghost btn-sm ${taskFilter === f ? 'active' : ''}"
          onclick="taskFilter='${f}';refreshTasks()">${f.charAt(0).toUpperCase()+f.slice(1)}</button>
      `).join('')}
      <button class="btn btn-ghost btn-sm" style="margin-left:auto"
        onclick="taskView=taskView==='list'?'kanban':'list';refreshTasks()">${taskView==='list'?'📋 Kanban':'📋 List'}</button>
    </div>
    <div id="tasks-container"><div class="loading">Loading tasks...</div></div>
    <div id="task-modal" style="display:none"></div>
  `;
}

async function refreshTasks() {
  try {
    const tasks = await apiFetch('/tasks');
    state._cachedTasks = tasks;
    updateTaskHistory(tasks);
    const filtered = taskFilter === 'all' ? tasks : tasks.filter(t => t.status === taskFilter);
    const approved = tasks.filter(t => t.status === 'approved').length;
    document.getElementById('task-count').textContent = approved + ' approved';
    document.getElementById('tasks-container').innerHTML =
      taskView === 'list' ? renderTaskList(filtered) : renderKanban(filtered);
  } catch (e) {
    document.getElementById('tasks-container').innerHTML =
      '<div class="empty"><div class="icon">⚠</div>Error loading tasks</div>';
  }
}

function updateTaskHistory(tasks) {
  for (const t of tasks) {
    if (!taskHistory[t.id]) taskHistory[t.id] = [];
    const last = taskHistory[t.id][taskHistory[t.id].length - 1];
    if (!last || last.status !== t.status) {
      taskHistory[t.id].push({ status: t.status, time: new Date().toISOString() });
    }
  }
}

function renderTaskList(tasks) {
  const priorityColors = { critical: 'red', high: 'amber', medium: 'blue', low: 'purple' };
  const statusColors = { pending: 'blue', approved: 'green', rejected: 'red', in_progress: 'amber', completed: 'purple' };
  return tasks.map(t => `
    <div class="task-card" id="task-card-${t.id}">
      <div class="task-header">
        <span class="task-title" style="cursor:pointer" onclick="showTaskDetail('${t.id}')">${t.title}</span>
        <span class="badge ${priorityColors[t.priority]}">${(t.priority || 'medium').toUpperCase()}</span>
      </div>
      <div class="task-meta">
        <span>👤 ${t.role || 'unassigned'}</span>
        <span>🏃 Sprint ${t.sprint || 1}</span>
        <span>📏 ${t.sp || 5} SP</span>
        <span class="badge ${statusColors[t.status] || 'blue'}">${t.status || 'pending'}</span>
      </div>
      <div class="task-actions">
        ${t.status === 'pending' ? `
          <button class="btn btn-success btn-sm" onclick="approveTask('${t.id}',this)">✓ Approve</button>
          <button class="btn btn-danger btn-sm" onclick="rejectTask('${t.id}',this)">✗ Reject</button>
        ` : ''}
        ${t.status === 'approved' ? `<button class="btn btn-primary btn-sm" onclick="executeSprint()">▶ Execute Now</button>` : ''}
        ${t.status === 'completed' ? `<span class="badge green">✓ Done</span>` : ''}
        <button class="btn btn-ghost btn-sm" onclick="showTaskDetail('${t.id}')">🔍 Details</button>
      </div>
    </div>
  `).join('');
}

function renderKanban(tasks) {
  const cols = { pending: [], approved: [], in_progress: [], completed: [], rejected: [] };
  for (const t of tasks) if (cols[t.status]) cols[t.status].push(t);
  const colColors = { pending: 'var(--blue)', approved: 'var(--green)', in_progress: 'var(--amber)', completed: 'var(--accent)', rejected: 'var(--red)' };
  return `<div class="grid-2" style="grid-template-columns:repeat(auto-fit,minmax(180px,1fr))">
    ${Object.entries(cols).map(([status, items]) => `
      <div style="background:var(--bg-panel);border-radius:var(--radius-lg);padding:12px;min-height:200px;border-top:3px solid ${colColors[status]}">
        <div style="font-weight:600;margin-bottom:8px;color:${colColors[status]};font-size:13px">${status.toUpperCase()} (${items.length})</div>
        ${items.map(t => `
          <div class="task-card" style="margin-bottom:8px;padding:10px;cursor:pointer" onclick="showTaskDetail('${t.id}')">
            <div style="font-weight:500;font-size:13px;color:var(--text-secondary)">${t.title}</div>
            <div style="font-size:11px;color:var(--text-quaternary);margin-top:4px">👤 ${t.role} · Sprint ${t.sprint} · ${t.sp} SP</div>
          </div>
        `).join('')}
      </div>
    `).join('')}
  </div>`;
}

function showTaskDetail(id) {
  const tasks = state._cachedTasks || [];
  const t = tasks.find(t => t.id === id);
  if (!t) return;
  const history = taskHistory[id] || [];
  const modal = document.getElementById('task-modal');
  modal.style.display = 'block';
  const priorityColors = { critical: 'red', high: 'amber', medium: 'blue', low: 'purple' };
  const statusColors = { pending: 'blue', approved: 'green', rejected: 'red', in_progress: 'amber', completed: 'purple' };
  modal.innerHTML = `
    <div class="modal-overlay" onclick="document.getElementById('task-modal').style.display='none'"></div>
    <div class="modal">
      <div class="modal-body">
        <div class="modal-header">
          <h3>${t.title}</h3>
          <button class="btn btn-ghost btn-sm" onclick="document.getElementById('task-modal').style.display='none'" style="font-size:18px">&times;</button>
        </div>
        <div style="display:flex;gap:8px;margin-bottom:16px">
          <span class="badge ${priorityColors[t.priority]}">${(t.priority || 'medium').toUpperCase()}</span>
          <span class="badge ${statusColors[t.status]}">${t.status}</span>
        </div>
        <div class="grid-2" style="margin-bottom:16px">
          <div class="stat-card"><div class="label">Assignee</div><div class="value" style="font-size:16px">${t.role || 'unassigned'}</div></div>
          <div class="stat-card"><div class="label">Sprint</div><div class="value" style="font-size:16px">${t.sprint || 1}</div></div>
          <div class="stat-card"><div class="label">Story Points</div><div class="value" style="font-size:16px">${t.sp || 5}</div></div>
          <div class="stat-card"><div class="label">Source</div><div class="value" style="font-size:16px">ThinkTank</div></div>
        </div>
        <div class="section-title">Description</div>
        <p style="color:var(--text-tertiary);font-size:14px">${t.title} — derived from thinktank analysis. Assigned to ${t.role} for sprint ${t.sprint}.</p>
        <div class="section-title">Status History</div>
        <div style="border-left:2px solid var(--border-standard);padding-left:16px">
          ${history.map((h, i) => `
            <div style="margin-bottom:8px;position:relative">
              <div style="width:8px;height:8px;border-radius:50%;background:${({pending:'var(--blue)',approved:'var(--green)',rejected:'var(--red)',in_progress:'var(--amber)',completed:'var(--accent)'})[h.status]};position:absolute;left:-21px;top:4px"></div>
              <span class="badge blue" style="font-size:11px">${h.status}</span>
              <span style="font-size:11px;color:var(--text-quaternary);margin-left:4px">${new Date(h.time).toLocaleTimeString()}</span>
            </div>
          `).join('')}
          ${history.length === 0 ? '<span style="color:var(--text-quaternary)">No status changes yet</span>' : ''}
        </div>
        <div style="margin-top:16px">
          ${t.status === 'pending' ? `
            <button class="btn btn-success" onclick="approveTask('${t.id}');document.getElementById('task-modal').style.display='none'">✓ Approve & Close</button>
            <button class="btn btn-danger" onclick="rejectTask('${t.id}');document.getElementById('task-modal').style.display='none'" style="margin-left:8px">✗ Reject</button>
          ` : ''}
          <button class="btn btn-ghost" onclick="document.getElementById('task-modal').style.display='none'" style="margin-left:8px">Close</button>
        </div>
      </div>
    </div>
  `;
}

async function approveTask(id, btn) {
  if (btn) { btn.disabled = true; btn.textContent = '...'; }
  try {
    const r = await fetch(API + '/tasks/approve?id=' + id);
    const d = await r.json();
    if (d.status === 'approved') {
      if (btn) { btn.textContent = '✓ Approved'; btn.className = 'btn btn-ghost btn-sm'; btn.disabled = false; }
      refreshTasks();
      toast('Task ' + id + ' approved ✓');
    }
  } catch (e) {
    if (btn) { btn.disabled = false; btn.textContent = '✓ Approve'; }
    toast('Error: ' + e);
  }
}

async function rejectTask(id, btn) {
  if (btn) { btn.disabled = true; btn.textContent = '...'; }
  try {
    const r = await fetch(API + '/tasks/reject?id=' + id);
    const d = await r.json();
    if (d.status === 'rejected') {
      if (btn) { btn.textContent = '✗ Rejected'; btn.className = 'btn btn-ghost btn-sm'; btn.disabled = false; }
      refreshTasks();
      toast('Task ' + id + ' rejected');
    }
  } catch (e) {
    if (btn) { btn.disabled = false; btn.textContent = '✗ Reject'; }
    toast('Error: ' + e);
  }
}

async function executeSprint() {
  toast('Sprint started — running...');
  try {
    const r = await fetch(API + '/sprint/execute');
    const d = await r.json();
    if (d.status === 'sprint_started') {
      toast('Sprint executing — polling for completion');
      pollSprintStatus(0);
    } else if (d.status === 'no_approved_tasks') {
      toast('No approved tasks');
    }
  } catch (e) {
    toast('Error: ' + e);
  }
}

function pollSprintStatus(attempt) {
  fetch(API + '/sprint/status').then(r => r.json()).then(d => {
    const elapsed = Math.round(d.elapsed || 0);
    const done = Math.round((d.tasks_completed / d.tasks_total) * 100) || 0;
    if (d.running) {
      toast('Sprint running: ' + done + '% (' + elapsed + 's elapsed)...');
      setTimeout(() => pollSprintStatus(attempt + 1), 10000);
    } else {
      toast('Sprint complete! ' + d.tasks_completed + '/' + d.tasks_total + ' tasks done in ' + elapsed + 's');
      refreshTasks();
    }
  }).catch(() => setTimeout(() => pollSprintStatus(attempt + 1), 10000));
}
