const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');
const vm = require('node:vm');

test('stopped waiting renders nested approval identifiers safely and stops polling', async () => {
  const element = { innerHTML: '' };
  const cleared = [];
  let requests = 0;
  const context = vm.createContext({
    window: new EventTarget(), Headers, Event,
    document: { cookie: '', getElementById: () => element },
    state: { activeTab: 'workflows' },
    setTimeout: () => {}, setInterval: () => 17, clearInterval: id => cleared.push(id),
  });
  const root = path.join(__dirname, '../../cmd/bt-dashboard/static/js');
  vm.runInContext(fs.readFileSync(path.join(root, 'lib/api.js'), 'utf8'), context);
  vm.runInContext(fs.readFileSync(path.join(root, 'tabs/workflows.js'), 'utf8'), context);
  context.loadWorkflowBlackboard = () => {};
  context.apiFetch = async () => {
    requests++;
    return {
      status: 'waiting', outcome: 'pending_approval', error: '<img src=x onerror=alert(1)>',
      steps: [{ step_id: 'container', outcome: 'partial', steps: [
        { step_id: 'approval', outcome: 'pending_approval', hitl_task_id: 'owned-task', hitl_request_id: '<request>' },
      ] }],
    };
  };
  vm.runInContext('pollWorkflowStatus("fixture-run")', context);
  await new Promise(resolve => setImmediate(resolve));
  assert.equal(requests, 1);
  assert.ok(cleared.includes(17));
  assert.match(element.innerHTML, /Waiting for input or authentication/);
  assert.match(element.innerHTML, /owned-task/);
  assert.match(element.innerHTML, /&lt;request&gt;/);
  assert.match(element.innerHTML, /&lt;img/);
  assert.doesNotMatch(element.innerHTML, /<img|<request>/);
});

test('workflow names and descriptions remain text in list and selector markup', async () => {
  const elements = {
    'workflows-list': { innerHTML: '' },
    'workflow-select': { innerHTML: '', value: '' },
    'workflow-count': { textContent: '' },
  };
  const context = vm.createContext({
    window: new EventTarget(), Headers, Event,
    document: { cookie: '', getElementById: id => elements[id] },
    state: { activeTab: 'workflows' }, setTimeout: () => {},
  });
  const root = path.join(__dirname, '../../cmd/bt-dashboard/static/js');
  vm.runInContext(fs.readFileSync(path.join(root, 'lib/api.js'), 'utf8'), context);
  vm.runInContext(fs.readFileSync(path.join(root, 'tabs/workflows.js'), 'utf8'), context);
  context.apiFetch = async () => [{
    name: '<img src=x>', description: '<script>alert(1)</script>',
    filename: 'bad" onclick="alert(1).yaml', version: '1', step_count: 2,
  }];
  await vm.runInContext('loadWorkflows()', context);
  assert.doesNotMatch(elements['workflows-list'].innerHTML, /<img|<script>/);
  assert.match(elements['workflows-list'].innerHTML, /&lt;script&gt;/);
  assert.match(elements['workflow-select'].innerHTML, /&quot;/);
  assert.doesNotMatch(elements['workflow-select'].innerHTML, /value="bad" onclick=/);
});
