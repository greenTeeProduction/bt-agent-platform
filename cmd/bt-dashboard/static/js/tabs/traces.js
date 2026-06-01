/* === Traces Tab — Distributed Trace Viewer === */

let tracesPollInterval = null;

function renderTraces() {
  return `
    <div class="header">
      <h1>Traces</h1>
      <div class="header-stats">
        <span class="status-dot"></span>Distributed Tracing
        <button class="btn btn-sm btn-outline" style="margin-left:1rem" onclick="refreshTraces()">⟳ Refresh</button>
      </div>
    </div>
    <div class="grid-2" style="grid-template-columns: 1fr 280px;">
      <div>
        <div class="section-title">🌐 Trace List</div>
        <div id="trace-list"><div class="loading">Loading traces...</div></div>
      </div>
      <div>
        <div class="section-title">🔍 Trace Detail</div>
        <div id="trace-detail"><div class="empty">Select a trace to view details</div></div>
        <div style="margin-top:1rem">
          <div class="section-title">📊 Summary</div>
          <div id="trace-summary"><div class="loading">—</div></div>
        </div>
      </div>
    </div>
    <div class="section-title">⏱ Recent Spans</div>
    <div id="recent-spans"><div class="loading">Loading spans...</div></div>
  `;
}

async function refreshTraces() {
  const listEl = document.getElementById('trace-list');
  const spansEl = document.getElementById('recent-spans');
  const summaryEl = document.getElementById('trace-summary');
  if (listEl) listEl.innerHTML = '<div class="loading">Refreshing...</div>';
  if (spansEl) spansEl.innerHTML = '<div class="loading">Refreshing...</div>';

  try {
    // Load trace list (aggregated traces)
    const traceData = await apiFetch('/traces?list=true&limit=20');
    renderTraceList(traceData);

    // Load recent raw spans
    const spanData = await apiFetch('/traces?limit=50');
    renderRecentSpans(spanData);

    // Summary
    if (summaryEl && traceData) {
      const traces = traceData.traces || [];
      const totalDuration = traces.reduce((s, t) => s + (t.total_duration_ms || 0), 0);
      const avgDur = traces.length > 0 ? (totalDuration / traces.length) : 0;
      summaryEl.innerHTML = `
        <div class="table-row"><div class="icon-cell" style="background:var(--blue)">🔀</div>
          <div class="content"><div class="title">Total Traces</div><div class="subtitle">${traceData.count || 0}</div></div></div>
        <div class="table-row"><div class="icon-cell" style="background:var(--accent-bg)">⏱</div>
          <div class="content"><div class="title">Avg Duration</div><div class="subtitle">${formatDuration(avgDur)}</div></div></div>
        <div class="table-row"><div class="icon-cell" style="background:var(--green)">✓</div>
          <div class="content"><div class="title">Status</div><div class="subtitle">Trace Reader Active</div></div>
          <div class="meta"><span class="badge green">OK</span></div></div>
      `;
    }
  } catch (e) {
    if (listEl) listEl.innerHTML = `<div class="empty">Failed to load traces: ${e.message}</div>`;
    if (spansEl) spansEl.innerHTML = `<div class="empty">Failed to load spans</div>`;
  }
}

function renderTraceList(data) {
  const el = document.getElementById('trace-list');
  if (!el) return;

  if (!data || !data.traces || data.traces.length === 0) {
    el.innerHTML = '<div class="empty">No traces recorded yet. Access the dashboard to generate them.</div>';
    return;
  }

  const traces = data.traces;
  el.innerHTML = traces.map((t, i) => {
    const ops = (t.operations || []).join(', ');
    const status = t.root_span && t.root_span.span && t.root_span.span.error ? 'red' : 'green';
    const statusLabel = t.root_span && t.root_span.span && t.root_span.span.error ? 'ERROR' : 'OK';
    return `
      <div class="table-row" style="cursor:pointer" onclick="selectTrace('${t.trace_id}')" id="trace-row-${i}">
        <div class="icon-cell" style="background:var(--${status})">${t.span_count}</div>
        <div class="content">
          <div class="title" style="font-family:var(--mono);font-size:0.8rem">${shortID(t.trace_id)}</div>
          <div class="subtitle">${ops.length > 60 ? ops.slice(0,60)+'…' : ops}</div>
        </div>
        <div class="meta">
          <span class="badge ${status}">${statusLabel}</span>
          <div style="font-size:0.75rem;color:var(--text-muted);margin-top:2px">${formatDuration(t.total_duration_ms || 0)}</div>
        </div>
      </div>
    `;
  }).join('');
}

async function selectTrace(traceID) {
  const detailEl = document.getElementById('trace-detail');
  if (!detailEl) return;
  detailEl.innerHTML = '<div class="loading">Loading trace detail...</div>';

  try {
    const trace = await apiFetch('/traces?trace_id=' + encodeURIComponent(traceID));
    renderTraceDetail(trace);
  } catch (e) {
    detailEl.innerHTML = `<div class="empty">Failed to load trace: ${e.message}</div>`;
  }
}

function renderTraceDetail(trace) {
  const el = document.getElementById('trace-detail');
  if (!el || !trace) {
    if (el) el.innerHTML = '<div class="empty">Trace not found</div>';
    return;
  }

  const root = trace.root_span;
  const spans = root ? renderSpanTree(root, 0) : '<div class="empty">No root span</div>';

  el.innerHTML = `
    <div class="card" style="padding:0.75rem">
      <div style="font-family:var(--mono);font-size:0.8rem;margin-bottom:0.5rem;word-break:break-all">
        ${trace.trace_id}
      </div>
      <div class="trace-tree">${spans}</div>
      <div style="margin-top:0.5rem;font-size:0.75rem;color:var(--text-muted)">
        ${trace.span_count} spans · ${formatDuration(trace.total_duration_ms || 0)} total
      </div>
    </div>
  `;
}

function renderSpanTree(node, depth) {
  if (!node || !node.span) return '';
  const s = node.span;
  const indent = depth * 16;
  const statusColor = s.error ? 'var(--red)' : 'var(--green)';
  const statusIcon = s.error ? '✗' : '✓';
  const children = (node.children || []).map(c => renderSpanTree(c, depth + 1)).join('');

  let attrsHTML = '';
  if (s.attributes && Object.keys(s.attributes).length > 0) {
    attrsHTML = Object.entries(s.attributes).map(([k, v]) =>
      `<span class="trace-attr">${k}=<strong>${escHTML(v)}</strong></span>`
    ).join(' ');
  }

  let eventsHTML = '';
  if (s.events && s.events.length > 0) {
    eventsHTML = s.events.map(ev =>
      `<div class="trace-event">📌 ${escHTML(ev.name)}</div>`
    ).join('');
  }

  return `
    <div class="trace-span" style="margin-left:${indent}px">
      <div class="trace-span-header">
        <span style="color:${statusColor}">${statusIcon}</span>
        <span class="trace-span-op" style="font-family:var(--mono);font-size:0.8rem">${escHTML(s.operation)}</span>
        <span class="trace-span-dur">${formatDuration(s.duration_ms || 0)}</span>
        ${s.error ? `<span class="badge red">${escHTML(s.error)}</span>` : ''}
      </div>
      ${attrsHTML ? `<div class="trace-attrs">${attrsHTML}</div>` : ''}
      ${eventsHTML ? `<div class="trace-events">${eventsHTML}</div>` : ''}
      ${children ? `<div class="trace-children">${children}</div>` : ''}
    </div>
  `;
}

function renderRecentSpans(data) {
  const el = document.getElementById('recent-spans');
  if (!el) return;

  if (!data || !data.entries || data.entries.length === 0) {
    el.innerHTML = '<div class="empty">No recent spans</div>';
    return;
  }

  const entries = data.entries.slice(0, 20);
  el.innerHTML = entries.map(e => {
    const statusColor = e.error ? 'var(--red)' : 'var(--green)';
    const idShort = e.trace_id ? shortID(e.trace_id) : '—';
    const timeStr = e.timestamp ? formatTime(e.timestamp) : '—';
    return `
      <div class="table-row" style="cursor:pointer" onclick="selectTrace('${e.trace_id}')">
        <div class="icon-cell" style="background:${statusColor}">▶</div>
        <div class="content">
          <div class="title" style="font-family:var(--mono);font-size:0.8rem">${escHTML(e.operation)}</div>
          <div class="subtitle">${idShort} · ${timeStr}</div>
        </div>
        <div class="meta">
          <span class="badge ${e.error ? 'red' : 'green'}">${e.error ? 'ERR' : 'OK'}</span>
          <div style="font-size:0.75rem;color:var(--text-muted)">${formatDuration(e.duration_ms || 0)}</div>
        </div>
      </div>
    `;
  }).join('');
}

// ─── Helpers ───

function shortID(id) {
  if (!id || id.length <= 12) return id || '—';
  return id.slice(0, 6) + '…' + id.slice(-6);
}

function formatDuration(ms) {
  if (ms == null || ms === 0) return '<1ms';
  if (ms < 1000) return ms + 'ms';
  if (ms < 60000) return (ms / 1000).toFixed(1) + 's';
  const min = Math.floor(ms / 60000);
  const sec = Math.round((ms % 60000) / 1000);
  return min + 'm ' + sec + 's';
}

function formatTime(ts) {
  if (!ts) return '—';
  try {
    const d = new Date(ts);
    if (isNaN(d.getTime())) return String(ts).slice(0, 19).replace('T', ' ') || '—';
    return d.toLocaleTimeString();
  } catch {
    return '—';
  }
}

function escHTML(s) {
  if (s == null) return '';
  return String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
}

// ─── Polling ───

function startTracesPolling() {
  stopTracesPolling();
  refreshTraces();
  tracesPollInterval = setInterval(refreshTraces, 15000);
}

function stopTracesPolling() {
  if (tracesPollInterval) {
    clearInterval(tracesPollInterval);
    tracesPollInterval = null;
  }
}
