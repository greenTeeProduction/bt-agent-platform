/* === Tree View Tab — Dynamic Tree Selector === */

const nodeColors = {
  Sequence: '#3b82f6', Selector: '#27a644', Condition: '#f59e0b', Action: '#7170ff',
  ChainAction: '#ec4899', Retry: '#ef4444', Default: '#62666d'
};

const NODE_W = 140, NODE_H = 32, LEVEL_DX = 220, MIN_GAP = 14;

let mindMapData = null, mindMapZoom = 1;

function renderMindMap() {
  const options = state.trees.map(t =>
    `<option value="${t.id}">${t.name || t.id.split(':')[1] || t.id}</option>`
  ).join('');
  return `
    <div class="header">
      <h1>Tree View</h1>
      <select id="tree-select" onchange="loadMindMap()" style="width:auto;margin-left:16px">
        ${options}
      </select>
    </div>
    <div style="display:flex;gap:8px;margin-bottom:16px;align-items:center">
      <button class="btn btn-ghost btn-sm" onclick="mindMapZoom=Math.min(3,mindMapZoom*1.2);renderTree()">🔍+</button>
      <button class="btn btn-ghost btn-sm" onclick="mindMapZoom=Math.max(0.3,mindMapZoom/1.2);renderTree()">🔍-</button>
      <button class="btn btn-ghost btn-sm" onclick="mindMapZoom=1;renderTree()">↺ Reset</button>
      <span style="font-size:11px;color:var(--text-quaternary)">${Math.round(mindMapZoom*100)}%</span>
      <span style="font-size:10px;color:var(--text-tertiary);margin-left:16px;background:var(--bg-surface);padding:2px 8px;border-radius:4px">🔵 Sequence = all in order</span>
      <span style="font-size:10px;color:var(--text-tertiary);background:var(--bg-surface);padding:2px 8px;border-radius:4px">🟢 Selector = try until success</span>
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

// --- Two-pass layout with execution order tracking ---

function subtreeSpan(node) {
  if (!node || node._collapsed) return 1;
  if (!node.children || node.children.length === 0) return 1;
  let total = 0;
  for (const ch of node.children) total += subtreeSpan(ch);
  return Math.max(1, total);
}

function computeLayout(node, x, y, availableSpan, layer, siblingIndex) {
  const results = [];
  const span = subtreeSpan(node);
  const nodeY = y + (availableSpan - (NODE_H + MIN_GAP)) / 2;
  const children = node._collapsed ? [] : (node.children || []);

  results.push({
    id: node.id || node.name,
    name: node.name,
    type: node.node_type || node.type,
    x, y: nodeY, layer,
    siblingIndex: siblingIndex !== undefined ? siblingIndex : 0,
    siblingCount: 0, // filled by parent
    parentX: undefined, parentY: undefined,
    _collapsed: node._collapsed,
    children: node.children
  });

  if (children.length > 0) {
    // Fill siblingCount on the parent
    results[0].siblingCount = children.length;

    const childSpans = children.map(ch => subtreeSpan(ch));
    const totalSpan = childSpans.reduce((a, b) => a + b, 0);
    let cy = y;
    for (let i = 0; i < children.length; i++) {
      const childBand = (childSpans[i] / totalSpan) * availableSpan;
      const childResults = computeLayout(children[i], x + LEVEL_DX + NODE_W, cy, childBand, layer + 1, i);
      for (const cr of childResults) {
        if (cr.parentX === undefined) { cr.parentX = x + NODE_W; cr.parentY = nodeY + NODE_H / 2; }
        results.push(cr);
      }
      cy += childBand;
    }
  }
  return results;
}

function resolveCollisions(layers) {
  const byLayer = {};
  for (const n of layers) {
    if (!byLayer[n.layer]) byLayer[n.layer] = [];
    byLayer[n.layer].push(n);
  }
  for (const [layer, nodes] of Object.entries(byLayer)) {
    nodes.sort((a, b) => a.y - b.y);
    for (let i = 1; i < nodes.length; i++) {
      const prev = nodes[i - 1];
      const curr = nodes[i];
      const minDist = NODE_H + MIN_GAP;
      if (curr.y - prev.y < minDist) {
        const shift = minDist - (curr.y - prev.y);
        for (let j = i; j < nodes.length; j++) { nodes[j].y += shift; }
        pushDescendantsDown(curr, shift, layers);
      }
    }
  }
}

function pushDescendantsDown(node, shift, allNodes) {
  const children = node._collapsed ? [] : (node.children || []);
  for (const ch of children) {
    const childNode = allNodes.find(n => (n.id || n.name) === (ch.id || ch.name) && n.layer === node.layer + 1);
    if (childNode) {
      childNode.y += shift;
      if (childNode.parentY !== undefined) childNode.parentY += shift;
      pushDescendantsDown(childNode, shift, allNodes);
    }
  }
}

// --- Render with execution-order indices ---

function renderTree() {
  if (!mindMapData) return;
  const svg = document.getElementById('mindmap-svg');
  const container = document.getElementById('mindmap-container');
  const totalLeaves = subtreeSpan(mindMapData);
  const totalHeight = totalLeaves * (NODE_H + MIN_GAP) + 60;

  const layers = computeLayout(mindMapData, 40, 30, Math.max(totalHeight, 200), 0, 0);
  resolveCollisions(layers);

  let maxY = 0, maxX = 0;
  for (const n of layers) {
    if (n.y + NODE_H > maxY) maxY = n.y + NODE_H;
    if (n.x + NODE_W > maxX) maxX = n.x + NODE_W;
  }
  const W = Math.max(800, maxX + 100);
  const H = Math.max(400, maxY + 60);

  svg.setAttribute('viewBox', '0 0 ' + W + ' ' + H);
  svg.style.minHeight = (H * mindMapZoom) + 'px';

  // Arrow marker for edge direction
  let html = `<defs>
    <marker id="arrow" viewBox="0 0 8 6" refX="8" refY="3" markerWidth="6" markerHeight="4" orient="auto">
      <polygon points="0 0, 8 3, 0 6" fill="var(--text-quaternary)" />
    </marker>
  </defs>
  <g transform="scale(${mindMapZoom})">`;

  // Edges with arrows
  for (const n of layers) {
    if (n.parentX !== undefined && n.parentY !== undefined) {
      const color = nodeColors[n.type] || nodeColors.Default;
      const sx = n.parentX, sy = n.parentY;
      const ex = n.x + 4, ey = n.y + NODE_H / 2;
      const mx = (sx + ex) / 2;
      html += `<path d="M${sx},${sy} C${mx},${sy} ${mx},${ey} ${ex},${ey}"
        stroke="${color}50" stroke-width="1.5" fill="none" marker-end="url(#arrow)"/>`;
    }
  }

  // Nodes with execution-order indices
  for (const n of layers) {
    const color = nodeColors[n.type] || nodeColors.Default;
    const collapsed = n._collapsed && n.children && n.children.length > 0;
    const id = (n.id || n.name).replace(/'/g, "\\'");
    const isRoot = n.siblingCount === 0 && n.layer === 0;

    // Execution order label: "[i/N]" for non-root, "" for root
    let orderLabel = '';
    if (!isRoot && n.siblingCount > 1) {
      orderLabel = `${n.siblingIndex + 1}/${n.siblingCount}`;
    }

    html += `<g transform="translate(${n.x},${n.y})" class="mind-node" data-id="${id}"
      onclick="toggleNode('${id}')" style="cursor:pointer">
      <rect x="0" y="0" width="${NODE_W}" height="${NODE_H}" rx="5"
        fill="${color}1a" stroke="${color}" stroke-width="1.2"
        ${n.type === 'Sequence' ? 'stroke-dasharray="4,2"' : ''}/>
      <!-- Order badge -->
      ${orderLabel ? `<rect x="2" y="2" width="20" height="${NODE_H-4}" rx="3" fill="${color}30"/>
      <text x="12" y="${NODE_H/2+4}" fill="${color}" font-size="9" font-weight="700" text-anchor="middle" font-family="sans-serif">${orderLabel}</text>` : ''}
      <!-- Node name -->
      <text x="${orderLabel ? 28 : 8}" y="${NODE_H/2+4}" fill="${color}" font-size="11" font-weight="600" font-family="sans-serif">${shorten(n.name, orderLabel ? 14 : 18)}</text>
      ${collapsed ? `<text x="${NODE_W-4}" y="${NODE_H/2+4}" fill="${color}" font-size="10" text-anchor="end">+${subtreeSpan(n)}</text>` : ''}
    </g>`;
  }

  html += '</g>';
  svg.innerHTML = html;

  svg.querySelectorAll('.mind-node').forEach(el => {
    el.addEventListener('mouseenter', e => showNodeDetail(el.dataset.id));
    el.addEventListener('mouseleave', () => hideNodeDetail());
  });
}

function countNodes(n) {
  if (!n) return 0;
  let c = 1;
  if (n.children) for (const ch of n.children) c += countNodes(ch);
  return c;
}

function shorten(s, n) { return s && s.length > n ? s.slice(0, n - 1) + '…' : (s || ''); }

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
  const color = nodeColors[n.node_type || n.type] || nodeColors.Default;
  el.innerHTML = `<div style="background:var(--bg-surface);border:1px solid var(--border-standard);padding:12px;border-radius:var(--radius-md)">
    <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:8px">
      <span style="font-weight:600;font-size:13px;color:var(--text-primary)">${n.name}</span>
      <span class="badge" style="background:${color}22;color:${color}">${n.node_type || n.type}</span>
    </div>
    <div style="font-size:11px;color:var(--text-tertiary)">
      <span>Children: ${(n.children || []).length}</span>
      ${n.description ? `<span style="margin-left:8px">${n.description.slice(0,80)}</span>` : ''}
    </div>
  </div>`;
  el.style.display = 'block';
  setTimeout(() => el.style.display = 'none', 5000);
}

function hideNodeDetail() { document.getElementById('node-detail').style.display = 'none'; }
