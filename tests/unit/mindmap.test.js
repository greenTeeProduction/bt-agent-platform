const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');
const vm = require('node:vm');

function fixture(tree) {
  const elements = {
    'mindmap-svg': { innerHTML: '', style: {}, setAttribute() {}, querySelectorAll() {
      this.nodes = Array.from(this.innerHTML.matchAll(/data-id="([^"]+)"/g), match => ({
        dataset: { id: match[1] }, listeners: {},
        addEventListener(name, fn) { this.listeners[name] = fn; },
      }));
      return this.nodes;
    } },
    'mindmap-container': {}, 'node-detail': { innerHTML: '', style: {} },
    'tree-select': { value: 'domain:arc42:section1' },
  };
  const context = vm.createContext({
    window: new EventTarget(), Headers, Event, setTimeout() {},
    document: { cookie: '', getElementById: id => elements[id] }, state: { trees: [] },
  });
  const root = path.join(__dirname, '../../cmd/bt-dashboard/static/js');
  for (const file of ['lib/api.js', 'tabs/mindmap.js']) {
    vm.runInContext(fs.readFileSync(path.join(root, file), 'utf8'), context);
  }
  context.fixtureTree = tree;
  vm.runInContext('mindMapData = fixtureTree; renderTree()', context);
  return { context, elements };
}

test('tree labels, details and errors remain text; node actions contain no source identifiers', async () => {
  const { context, elements } = fixture({
    name: '<img src=x>', id: '\" onclick=\"alert(1)', type: 'constructor',
    description: '<svg onload=alert(1)>', children: [],
  });
  const svg = elements['mindmap-svg'].innerHTML;
  assert.doesNotMatch(svg, /<img|onclick=|function Object/);
  assert.match(svg, /&lt;img/);
  context.showNodeDetail('0');
  const detail = elements['node-detail'].innerHTML;
  assert.match(detail, /&lt;img/);
  assert.match(detail, /&lt;svg/);
  assert.doesNotMatch(detail, /<img|<svg|function Object/);
  context.apiFetch = async () => { throw new Error('<img src=x>'); };
  await context.loadMindMap();
  assert.doesNotMatch(elements['mindmap-svg'].innerHTML, /<img/);
  assert.match(elements['mindmap-svg'].innerHTML, /&lt;img/);
});

test('duplicate node names select the intended branch and preserve sibling order', () => {
  const tree = { name: 'root', type: 'Sequence', children: [
    { name: 'same', type: 'Sequence', description: 'first', children: [{ name: 'leaf', type: 'Action' }] },
    { name: 'same', type: 'Sequence', description: 'second', children: [{ name: 'leaf', type: 'Action' }] },
  ] };
  const { context, elements } = fixture(tree);
  assert.match(elements['mindmap-svg'].innerHTML, />1\/2<\/text>/);
  assert.match(elements['mindmap-svg'].innerHTML, />2\/2<\/text>/);
  const second = elements['mindmap-svg'].nodes.find(node => node.dataset.id === '0.1');
  assert.equal(typeof second.listeners.click, 'function');
  second.listeners.click();
  assert.equal(tree.children[0]._collapsed, undefined);
  assert.equal(tree.children[1]._collapsed, true);
  assert.match(elements['mindmap-svg'].innerHTML, /data-id="0.0.0"/);
  assert.doesNotMatch(elements['mindmap-svg'].innerHTML, /data-id="0.1.0"/);
  context.showNodeDetail('0.1');
  assert.match(elements['node-detail'].innerHTML, /second/);
});

test('collision shifts move descendants of the selected branch, including repeated names', () => {
  const { context } = fixture({ name: 'root', type: 'Sequence' });
  const nodes = [
    { id: '0.0', parentID: '0', name: 'same', layer: 1, y: 0 },
    { id: '0.1', parentID: '0', name: 'same', layer: 1, y: 0 },
    { id: '0.0.0', parentID: '0.0', name: 'leaf', layer: 2, y: 0, parentY: 16 },
    { id: '0.1.0', parentID: '0.1', name: 'leaf', layer: 2, y: 0, parentY: 16 },
  ];
  context.pushDescendantsDown(nodes[1], 46, nodes);
  assert.equal(nodes[2].y, 0);
  assert.equal(nodes[3].y, 46);
  assert.equal(nodes[3].parentY, 62);
});

test('qualified identifiers are encoded and the bare definition is rendered', async () => {
  const { context, elements } = fixture({ name: 'root', type: 'Sequence' });
  let requested;
  context.apiFetch = async url => { requested = url; return { name: 'section-one', type: 'Action' }; };
  await context.loadMindMap();
  assert.equal(requested, '/tree/structure?id=domain%3Aarc42%3Asection1');
  assert.match(elements['mindmap-svg'].innerHTML, /section-one/);
});
