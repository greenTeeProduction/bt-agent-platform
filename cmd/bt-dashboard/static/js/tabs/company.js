/* === Company Tab === */

function renderCompany() {
  const c = state.company || {};
  return `
    <div class="header">
      <h1>${c.name || 'HermesAI'}</h1>
      <span class="badge green">${c.product_stage || 'beta'}</span>
    </div>
    <div style="color:var(--text-tertiary);margin-bottom:24px">
      ${c.industry || 'AI Tools'} · ${c.funding_round || 'seed'} · ${c.team_size || 8} team · ${c.runway_months || 14}mo runway
    </div>
    <div class="grid-4">
      <div class="stat-card green"><div class="label">MRR</div><div class="value">$${Math.round((c.mrr || 0) / 1000)}k</div></div>
      <div class="stat-card blue"><div class="label">Users</div><div class="value">${((c.users || 0) / 1000).toFixed(1)}k</div></div>
      <div class="stat-card amber"><div class="label">Runway</div><div class="value">${c.runway_months || 0}mo</div></div>
      <div class="stat-card red"><div class="label">Burn Rate</div><div class="value">$${Math.round((c.burn_rate_monthly || 0) / 1000)}k</div></div>
    </div>
    <div class="grid-2">
      <div>
        <div class="section-title">Team</div>
        <div class="table-row"><div class="icon-cell" style="background:var(--accent-bg)">👨‍💻</div><div class="content"><div class="title">Engineers</div></div><div class="meta">${c.engineers || 0}</div></div>
        <div class="table-row"><div class="icon-cell" style="background:var(--green)">📊</div><div class="content"><div class="title">Sales</div></div><div class="meta">${c.sales_people || 0}</div></div>
        <div class="table-row"><div class="icon-cell" style="background:var(--amber)">📣</div><div class="content"><div class="title">Marketing</div></div><div class="meta">${c.marketing_staff || 0}</div></div>
      </div>
      <div>
        <div class="section-title">Current Sprint</div>
        <div class="task-card">
          <div class="task-header">
            <span class="task-title">Sprint ${c.current_sprint || 12}: ${c.sprint_goal || 'Launch enterprise SSO'}</span>
            <span class="badge amber">In Progress</span>
          </div>
          <div class="task-meta"><span>👥 4 engineers</span><span>⏱ 2 weeks</span></div>
        </div>
        <div class="section-title" style="margin-top:16px">Quarter Goals</div>
        ${(c.quarter_goals || []).map(g => `<div class="table-row"><div class="icon-cell" style="background:var(--accent-bg)">🎯</div><div class="content"><div class="title">${g}</div></div></div>`).join('')}
      </div>
    </div>
  `;
}
