/* === Trees Tab — with Search === */

function renderTrees() {
  const cats = {};
  state.trees.forEach(t => {
    if (!cats[t.category]) cats[t.category] = [];
    cats[t.category].push(t);
  });
  return `
    <div class="header">
      <h1>Behavior Trees</h1>
      <span class="badge green">${state.trees.length} total</span>
    </div>
    <div style="margin-bottom:16px">
      <input id="tree-search" placeholder="Search trees by name or ID..." oninput="filterTrees()" style="max-width:400px">
    </div>
    <div id="trees-list">
      ${Object.entries(cats).map(([cat, ts]) => `
        <div class="section-title">${cat.toUpperCase()} <span class="badge blue">${ts.length}</span></div>
        ${ts.map(t => `
          <div class="table-row" data-tree-cat="${cat}" data-tree-name="${(t.name || '').toLowerCase()} ${t.id.toLowerCase()}">
            <div class="icon-cell" style="background:${catColor(cat)}">${cat[0].toUpperCase()}</div>
            <div class="content">
              <div class="title">${t.name || t.id.split(':')[1] || t.id}</div>
              <div class="subtitle">${t.id}</div>
            </div>
            <div class="meta">${t.node_count || '?'} nodes</div>
          </div>
        `).join('')}
      `).join('')}
    </div>
  `;
}

function filterTrees() {
  const q = (document.getElementById('tree-search')?.value || '').toLowerCase();
  document.querySelectorAll('#trees-list .table-row').forEach(row => {
    const name = row.dataset.treeName || '';
    row.style.display = q === '' || name.includes(q) ? '' : 'none';
  });
  // Hide empty category headers
  document.querySelectorAll('#trees-list .section-title').forEach(title => {
    const next = title.nextElementSibling;
    let visible = false;
    let el = next;
    while (el && !el.classList.contains('section-title')) {
      if (el.style.display !== 'none') { visible = true; break; }
      el = el.nextElementSibling;
    }
    title.style.display = visible || q === '' ? '' : 'none';
  });
}
