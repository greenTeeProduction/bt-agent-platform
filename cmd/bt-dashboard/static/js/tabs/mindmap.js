/* === MindMap Tab — Dynamic Tree Selector === */

const nodeColors = {
  Sequence: '#3b82f6', Selector: '#27a644', Condition: '#f59e0b', Action: '#7170ff',
  ChainAction: '#ec4899', Retry: '#ef4444', Default: '#62666d'
};

let mindMapData = null, mindMapZoom = 1;

function renderMindMap() {
  // Build tree options from state
  const options = state.trees.map(t =>
    `<option value="${t.id}">${t.name || t.id.split(':')[1] || t.id}</option>`
  ).join('');
  return `
    <div class="header">
      <h1>Tree Mind Map</h1>
      <select id="tree-select" onchange="loadMindMap()" style="width:auto;margin-left:16px">
        ${options}
      </select>
    </div>
    <div style="display:flex;gap:8px;margin-bottom:16px">
      <button class="btn btn-ghost btn-sm" onclick="mindMapZoom=Math.min(3,mindMapZoom*1.2);renderTree()">🔍+</button>
      <button class="btn btn-ghost btn-sm" onclick="mindMapZoom=Math.max(0.3,mindMapZoom/1.2);renderTree()">🔍-</button>
      <button class="btn btn-ghost btn-sm" onclick="mindMapZoom=1;renderTree()">↺ Reset</button>
      <span style="font-size:11px;color:var(--text-quaternary);align-self:center">${Math.round(mindMapZoom*100)}%</span>
    </div>
    <div id="mindmap-container" style="overflow:auto;border:1px solid var(--border-standard);border-radius:var(--radius-lg);background:var(--bg-deepest);min-height:500px;position:relative">
      <svg id="mindmap-svg" style="width:100%;min-height:600px"></svg>
    </div>
    <div id="node-detail" style="display:none;position:fixed;bottom:80px;right:20px;max-width:300px;z-index:50"></div>
  `;
}

async function loadMindMap() {
  const sel = document.getElementById('tree-select');
  const treeID = sel ? sel.value : (state.trees[0]?.id || 'godev');
  try {
    mindMapData = await apiFetch('/tree/structure?id=' + encodeURIComponent(treeID));
    renderTree();
  } catch (e) {
    document.getElementById('mindmap-svg').innerHTML =
      '<text x="20" y="30" fill="var(--red)" font-size="14">Error loading tree: ' + e + '</text>';
  }
}

function renderTree() {
  if (!mindMapData) return;
  const svg = document.getElementById('mindmap-svg');
  const container = document.getElementById('mindmap-container');
  const W = Math.max(1200, container.clientWidth * 2);
  const H = Math.max(800, countNodes(mindMapData) * 40 + 200);
  svg.setAttribute('viewBox', '0 0 ' + W + ' ' + H);
  svg.style.minHeight = (H * mindMapZoom) + 'px';

  const layers = layoutHorizontal(mindMapData, 60, 60, 220, 50);
  let html = `<g transform="scale(${mindMapZoom})">`;

  // Edges
  for (const n of layers) {
    if (n.parentX !== undefined) {
      const midX = n.parentX + (n.x - n.parentX) / 2;
      html += `<path d="M${n.parentX + 120},${n.parentY} C${midX},${n.parentY} ${midX},${n.y} ${n.x},${n.y}"
        stroke="${(nodeColors[n.type] || nodeColors.Default)}40" stroke-width="2" fill="none"/>`;
    }
  }

  // Nodes
  for (const n of layers) {
    const color = nodeColors[n.type] || nodeColors.Default;
    const collapsed = n._collapsed && n.children && n.children.length > 0;
    html += `<g transform="translate(${n.x},${n.y - 14})" class="mind-node" data-id="${n.id || n.name}"
      onclick="toggleNode('${(n.id || n.name).replace(/'/g, "\\'")}')" style="cursor:pointer">
      <rect x="0" y="0" width="120" height="28" rx="6" fill="${color}22" stroke="${color}" stroke-width="1.5"/>
      <text x="8" y="18" fill="${color}" font-size="11" font-weight="600" font-family="sans-serif">${shorten(n.name, 16)}</text>
      ${collapsed ? `<text x="110" y="18" fill="${color}" font-size="10" text-anchor="end">+${countNodes(n)}</text>` : ''}
    </g>`;
  }

  html += '</g>';
  svg.innerHTML = html;

  svg.querySelectorAll('.mind-node').forEach(el => {
    el.addEventListener('mouseenter', e => showNodeDetail(el.dataset.id));
    el.addEventListener('mouseleave', () => hideNodeDetail());
  });
}

function layoutHorizontal(node, x, y, dx, dy, layer = 0) {
  const results = [];
  const nChildren = (node.children || []).length;
  const totalH = nChildren * dy;
  const startY = y - (totalH / 2) + dy / 2;

  results.push({
    id: node.id || node.name, name: node.name, type: node.node_type || node.type,
    x: x, y: y, layer, parentX: undefined, parentY: undefined,
    _collapsed: node._collapsed, children: node.children
  });

  if (!node._collapsed && node.children) {
    for (let i = 0; i < node.children.length; i++) {
      const cy = startY + i * dy;
      const childResults = layoutHorizontal(
        node.children[i], x + dx, cy, dx,
        Math.max(30, dy / Math.max(1, node.children[i].children?.length || 1)),
        layer + 1
      );
      for (const cr of childResults) {
        if (cr.parentX === undefined) { cr.parentX = x; cr.parentY = y; }
        results.push(cr);
      }
    }
  }
  return results;
}

function countNodes(n) {
  if (!n) return 0;
  let c = 1;
  if (n.children) for (const ch of n.children) c += countNodes(ch);
  return c;
}

function shorten(s, n) { return s.length > n ? s.slice(0, n - 1) + '…' : s; }

function toggleNode(id) {
  function toggle(n) {
    if ((n.id || n.name) === id) { n._collapsed = !n._collapsed; return true; }
    if (n.children) for (const c of n.children) if (toggle(c)) return true;
    return false;
  }
  toggle(mindMapData);
  renderTree();
}

function showNodeDetail(id) {
  function find(n) {
    if ((n.id || n.name) === id) return n;
    if (n.children) for (const c of n.children) { const f = find(c); if (f) return f; }
    return null;
  }
  const n = find(mindMapData);
  if (!n) return;
  const el = document.getElementById('node-detail');
  el.innerHTML = `<div class="task-header"><span class="task-title">${n.name}</span>
    <span class="badge" style="background:${(nodeColors[n.node_type || n.type] || '#62666d')}22;color:${nodeColors[n.node_type || n.type]}">${n.node_type || n.type}</span></div>
    <div class="task-meta"><span>Children: ${(n.children || []).length}</span>${n.metadata ? `<span>${n.metadata}</span>` : ''}</div>`;
  el.style.display = 'block';
  setTimeout(() => el.style.display = 'none', 4000);
}

function hideNodeDetail() { document.getElementById('node-detail').style.display = 'none'; }
