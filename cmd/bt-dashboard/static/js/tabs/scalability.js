/* === Scalability Tab — Worker Pool, Concurrency, Queue, Router, Heartbeat === */

function renderScalability() {
  return `
    <div class="header"><h1>Scalability</h1><span class="badge blue" id="scalability-status">—</span></div>

    <div class="stats-grid" style="grid-template-columns: repeat(auto-fill, minmax(280px, 1fr)); gap: 16px;" id="scalability-cards">
      <div class="stat-card"><div class="stat-label">Worker Pool</div><div class="loading">Loading...</div></div>
      <div class="stat-card"><div class="stat-label">Concurrency</div><div class="loading">Loading...</div></div>
      <div class="stat-card"><div class="stat-label">Queue</div><div class="loading">Loading...</div></div>
      <div class="stat-card"><div class="stat-label">Router</div><div class="loading">Loading...</div></div>
      <div class="stat-card"><div class="stat-label">Connection Pool</div><div class="loading">Loading...</div></div>
      <div class="stat-card"><div class="stat-label">Heartbeat</div><div class="loading">Loading...</div></div>
    </div>

    <div class="card" style="margin-top: 16px;">
      <h3>Component Overview</h3>
      <table class="data-table" id="scalability-table">
        <thead>
          <tr>
            <th>Component</th>
            <th>Metric</th>
            <th>Value</th>
            <th>Status</th>
          </tr>
        </thead>
        <tbody id="scalability-table-body">
          <tr><td colspan="4"><div class="loading">Loading...</div></td></tr>
        </tbody>
      </table>
    </div>

    <div class="card" style="margin-top: 16px;">
      <h3>Scaling Configuration</h3>
      <div class="task-meta" style="margin-top: 8px;">
        <span>🧰 Worker Pool: 4 concurrent workers</span>
        <span>🔒 Concurrency Limit: 2 concurrent LLM-bound executions</span>
        <span>📋 Queue: Task queue adds backpressure when pool saturated</span>
        <span>🌐 Router: Round-robin or least-connections across agents</span>
        <span>💓 Heartbeat: TTL-based node aliveness tracking</span>
        <span>🔌 Conn Pool: HTTP connection reuse for remote executors</span>
      </div>
    </div>
  `;
}

async function loadScalability() {
  try {
    const data = await apiFetch('/scalability');
    if (state.activeTab !== 'scalability') return;

    const statusEl = document.getElementById('scalability-status');
    if (statusEl) statusEl.textContent = data.timestamp ? new Date(data.timestamp).toLocaleTimeString() : '—';

    // ── Worker Pool Card ──
    const wp = data.worker_pool;
    const wpCard = document.querySelector('.stat-card:nth-child(1) .loading');
    if (wpCard) {
      if (wp) {
        const utilization = wp.workers > 0 ? Math.round((wp.active / wp.workers) * 100) : 0;
        wpCard.outerHTML = `
          <div class="stat-value">${wp.active}/${wp.workers}</div>
          <div class="stat-sub">${utilization}% utilized · ${wp.queued} queued · ${wp.completed} completed</div>
          <div class="progress-bar" style="margin-top:8px">
            <div class="progress-fill ${utilization > 80 ? 'red' : utilization > 50 ? 'amber' : 'green'}" style="width:${Math.min(utilization, 100)}%"></div>
          </div>
        `;
      } else {
        wpCard.outerHTML = '<div class="stat-value" style="color:var(--text-tertiary)">Not configured</div>';
      }
    }

    // ── Concurrency Card ──
    const cl = data.concurrency_limiter;
    const clCard = document.querySelector('.stat-card:nth-child(2) .loading');
    if (clCard) {
      if (cl) {
        const usagePct = cl.capacity > 0 ? Math.round((cl.active / cl.capacity) * 100) : 0;
        clCard.outerHTML = `
          <div class="stat-value">${cl.active}/${cl.capacity}</div>
          <div class="stat-sub">${usagePct}% used · ${cl.waiting} waiting · ${cl.total} total · ${cl.available} available</div>
          <div class="progress-bar" style="margin-top:8px">
            <div class="progress-fill ${usagePct > 80 ? 'red' : usagePct > 50 ? 'amber' : 'green'}" style="width:${Math.min(usagePct, 100)}%"></div>
          </div>
        `;
      } else {
        clCard.outerHTML = '<div class="stat-value" style="color:var(--text-tertiary)">Not configured</div>';
      }
    }

    // ── Queue Card ──
    const q = data.queue;
    const qCard = document.querySelector('.stat-card:nth-child(3) .loading');
    if (qCard) {
      if (q) {
        const depthPct = q.max_len > 0 ? Math.round((q.pending / q.max_len) * 100) : Math.min(q.pending * 5, 100);
        qCard.outerHTML = `
          <div class="stat-value">${q.pending} pending</div>
          <div class="stat-sub">Max depth: ${q.max_len >= 0 ? q.max_len : 'unbounded'}</div>
          <div class="progress-bar" style="margin-top:8px">
            <div class="progress-fill ${depthPct > 80 ? 'red' : depthPct > 50 ? 'amber' : 'green'}" style="width:${Math.min(depthPct, 100)}%"></div>
          </div>
        `;
      } else {
        qCard.outerHTML = '<div class="stat-value" style="color:var(--text-tertiary)">Not configured</div>';
      }
    }

    // ── Router Card ──
    const router = data.router;
    const routerCard = document.querySelector('.stat-card:nth-child(4) .loading');
    if (routerCard) {
      if (router) {
        const healthPct = router.total > 0 ? Math.round((router.healthy / router.total) * 100) : 0;
        routerCard.outerHTML = `
          <div class="stat-value" style="color:${healthPct === 100 ? 'var(--green)' : healthPct > 50 ? 'var(--amber)' : 'var(--red)'}">${router.healthy}/${router.total}</div>
          <div class="stat-sub">${healthPct}% healthy · ${router.unhealthy} unhealthy · ${router.failures || 0} failures</div>
          <div class="progress-bar" style="margin-top:8px">
            <div class="progress-fill ${healthPct === 100 ? 'green' : healthPct > 50 ? 'amber' : 'red'}" style="width:${healthPct}%"></div>
          </div>
        `;
      } else {
        routerCard.outerHTML = '<div class="stat-value" style="color:var(--text-tertiary)">Not configured</div>';
      }
    }

    // ── ConnPool Card ──
    const cp = data.conn_pool;
    const cpCard = document.querySelector('.stat-card:nth-child(5) .loading');
    if (cpCard) {
      if (cp) {
        cpCard.outerHTML = `
          <div class="stat-value">${cp.created} created</div>
          <div class="stat-sub">${cp.idle} idle · ${cp.in_use} in use · ${cp.max_observed} max observed · ${cp.is_shared ? 'Shared' : 'Dedicated'}</div>
        `;
      } else {
        cpCard.outerHTML = '<div class="stat-value" style="color:var(--text-tertiary)">Not configured</div>';
      }
    }

    // ── Heartbeat Card ──
    const hb = data.heartbeat;
    const hbCard = document.querySelector('.stat-card:nth-child(6) .loading');
    if (hbCard) {
      if (hb) {
        const alivePct = hb.total > 0 ? Math.round((hb.alive / hb.total) * 100) : 0;
        hbCard.outerHTML = `
          <div class="stat-value" style="color:${hb.expired > 0 ? 'var(--red)' : 'var(--green)'}">${hb.alive}/${hb.total}</div>
          <div class="stat-sub">${alivePct}% alive · ${hb.expired} expired · TTL: ${hb.ttl || '—'}</div>
          <div class="progress-bar" style="margin-top:8px">
            <div class="progress-fill ${hb.expired > 0 ? 'red' : 'green'}" style="width:${alivePct}%"></div>
          </div>
        `;
      } else {
        hbCard.outerHTML = '<div class="stat-value" style="color:var(--text-tertiary)">Not configured</div>';
      }
    }

    // ── Detail Table ──
    const tbody = document.getElementById('scalability-table-body');
    if (tbody) {
      const rows = [];

      if (wp) {
        rows.push(
          { comp: 'Worker Pool', metric: 'Workers', value: `${wp.active}/${wp.workers} active`, status: wp.active > 0 ? 'green' : 'blue' },
          { comp: 'Worker Pool', metric: 'Queued', value: `${wp.queued}`, status: wp.queued > 10 ? 'red' : wp.queued > 0 ? 'amber' : 'green' },
          { comp: 'Worker Pool', metric: 'Completed', value: `${wp.completed}`, status: 'green' },
        );
      }
      if (cl) {
        rows.push(
          { comp: 'Concurrency', metric: 'Active', value: `${cl.active}/${cl.capacity}`, status: cl.active >= cl.capacity ? 'red' : 'green' },
          { comp: 'Concurrency', metric: 'Waiting', value: `${cl.waiting}`, status: cl.waiting > 5 ? 'red' : cl.waiting > 0 ? 'amber' : 'green' },
          { comp: 'Concurrency', metric: 'Available', value: `${cl.available}`, status: cl.available > 0 ? 'green' : 'red' },
        );
      }
      if (q) {
        rows.push(
          { comp: 'Queue', metric: 'Pending', value: `${q.pending}`, status: q.pending > 20 ? 'red' : q.pending > 0 ? 'amber' : 'green' },
          { comp: 'Queue', metric: 'Max Depth', value: q.max_len >= 0 ? `${q.max_len}` : 'unbounded', status: 'blue' },
        );
      }
      if (router) {
        rows.push(
          { comp: 'Router', metric: 'Executors', value: `${router.healthy}/${router.total} healthy`, status: router.unhealthy > 0 ? 'red' : 'green' },
          { comp: 'Router', metric: 'Failures', value: `${router.failures || 0}`, status: (router.failures || 0) > 0 ? 'red' : 'green' },
        );
      }
      if (cp) {
        rows.push(
          { comp: 'Conn Pool', metric: 'Created', value: `${cp.created}`, status: 'blue' },
          { comp: 'Conn Pool', metric: 'Max Observed', value: `${cp.max_observed}`, status: 'blue' },
          { comp: 'Conn Pool', metric: 'Shared', value: cp.is_shared ? 'Yes' : 'No', status: cp.is_shared ? 'green' : 'blue' },
        );
      }
      if (hb) {
        rows.push(
          { comp: 'Heartbeat', metric: 'Nodes', value: `${hb.alive}/${hb.total} alive`, status: hb.expired > 0 ? 'red' : 'green' },
          { comp: 'Heartbeat', metric: 'Expired', value: `${hb.expired}`, status: hb.expired > 0 ? 'red' : 'green' },
        );
      }

      if (rows.length === 0) {
        tbody.innerHTML = '<tr><td colspan="4"><div class="empty"><div class="icon">📊</div>No scalability components configured</div></td></tr>';
      } else {
        tbody.innerHTML = rows.map(r => `
          <tr>
            <td style="font-weight:600">${r.comp}</td>
            <td>${r.metric}</td>
            <td style="font-family:var(--font-mono)">${r.value}</td>
            <td><span class="badge ${r.status}">${r.status}</span></td>
          </tr>
        `).join('');
      }
    }
  } catch (e) {
    const statusEl = document.getElementById('scalability-status');
    if (statusEl) statusEl.textContent = 'Error';
    const cards = document.getElementById('scalability-cards');
    if (cards) cards.innerHTML = '<div class="empty"><div class="icon">⚠</div>Failed to load scalability data</div>';
  }
}

// Poll every 15s
setInterval(() => { if (state.activeTab === 'scalability') loadScalability(); }, 15000);
setTimeout(loadScalability, 200);
