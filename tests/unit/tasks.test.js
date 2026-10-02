const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');
const vm = require('node:vm');

function fixture() {
  const messages = [], timers = [];
  let refreshed = 0;
  const context = vm.createContext({
    window: new EventTarget(), Headers, Event,
    document: { cookie: '' },
    setTimeout: callback => timers.push(callback),
    state: {}, toast: message => messages.push(message),
  });
  const root = path.join(__dirname, '../../cmd/bt-dashboard/static/js');
  for (const file of ['lib/api.js', 'tabs/tasks.js']) {
    vm.runInContext(fs.readFileSync(path.join(root, file), 'utf8'), context);
  }
  context.refreshTasks = () => refreshed++;
  return { context, messages, timers, refreshed: () => refreshed };
}

test('sprint completion distinguishes failed commits, finished batches and idle state', async () => {
  for (const observation of [
    { running: false, progress: 'failed', error: 'Task record unavailable', diagnostics: [{ task_committed: false }], tasks_completed: 0, tasks_total: 1 },
    { running: false, progress: 'done', tasks_completed: 2, tasks_total: 4 },
    { running: false, progress: 'idle', tasks_completed: 0, tasks_total: 4 },
  ]) {
    const f = fixture();
    f.context.apiFetch = async () => observation;
    await vm.runInContext('pollSprintStatus(0)', f.context);
    assert.equal(f.timers.length, 0);
    assert.equal(f.refreshed(), 1);
    if (observation.error) {
      assert.match(f.messages[0], /stopped with errors.*await persistence repair/);
      assert.doesNotMatch(f.messages[0], /complete|finished/);
    } else if (observation.progress === 'done') {
      assert.match(f.messages[0], /2\/4 stored tasks completed/);
    } else {
      assert.equal(f.messages[0], 'No active sprint.');
    }
  }
});

test('sprint polling preserves authorization rejection and never reports it as completion', async () => {
  for (const status of [401, 403]) {
    const f = fixture();
    let unauthorized = 0, requests = 0;
    f.context.window.addEventListener('bt:unauthorized', () => unauthorized++);
    f.context.fetch = async () => {
      requests++;
      return { status, ok: false, json: async () => ({ error: 'authentication required' }) };
    };
    await vm.runInContext('pollSprintStatus(0)', f.context);
    assert.equal(requests, 1);
    assert.equal(unauthorized, status === 401 ? 1 : 0);
    assert.equal(f.timers.length, 0);
    assert.equal(f.refreshed(), 0);
    assert.match(f.messages[0], /status unavailable.*authentication required/);
    assert.doesNotMatch(f.messages[0], /complete|finished/);
  }
});

test('running status schedules another read while preserving task-store counter semantics', async () => {
  const f = fixture();
  f.context.apiFetch = async () => ({ running: true, elapsed: 2.3, tasks_completed: 99, tasks_total: 100 });
  await vm.runInContext('pollSprintStatus(0)', f.context);
  assert.equal(f.timers.length, 1);
  assert.equal(f.refreshed(), 0);
  assert.equal(f.messages[0], 'Sprint running (2s elapsed)...');
  assert.doesNotMatch(f.messages[0], /99%/);
});
