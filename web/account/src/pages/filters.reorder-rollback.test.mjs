// Regression: account-portal filter reorder must roll back and surface the
// error when the reorder request fails. Root cause (fixed 2026-10-06):
// handleMove optimistically setFilters() the new order (mutating the shared
// filter objects' priority via forEach), never checked response.ok, and
// swallowed failures with console.error only — the UI showed an order the
// server refused, with zero feedback, silently diverging until the next load.
// Convention: plain-node spec, transpiles the real component (no vitest in
// this package); only the fetch boundary is mocked.
import fs from 'node:fs';
import vm from 'node:vm';
import assert from 'node:assert';
import { createRequire } from 'node:module';

const accountDir = new URL('../../', import.meta.url).pathname;
const require_ = createRequire(new URL('./Filters.tsx', import.meta.url));
const ts = require_('typescript');

function transpile(path) {
  const source = fs.readFileSync(path, 'utf8');
  return ts.transpileModule(source, {
    compilerOptions: {
      module: ts.ModuleKind.CommonJS,
      target: ts.ScriptTarget.ES2020,
      jsx: ts.JsxEmit.React,
    },
  }).outputText;
}

// --- instance-scoped hooks shim (effects run once, like React [] deps) ---
const instances = new Map();
let current = null;
const effectQueue = [];
function getInstance(path) {
  if (!instances.has(path)) {
    instances.set(path, { states: [], cursor: 0, effectCursor: 0, effects: [] });
  }
  return instances.get(path);
}
const hooks = {
  useState(init) {
    const inst = current;
    const index = inst.cursor++;
    if (!(index in inst.states)) inst.states[index] = typeof init === 'function' ? init() : init;
    return [
      inst.states[index],
      (value) => {
        inst.states[index] = value;
      },
    ];
  },
  useCallback(fn) {
    return fn;
  },
  useEffect(fn) {
    const inst = current;
    const effectIndex = inst.effectCursor++;
    // Slot-based run-once: each effect queues exactly once per instance;
    // a keyed remount (fresh instance) re-runs effects, like React.
    if (!inst.effects[effectIndex]) {
      inst.effects[effectIndex] = fn;
      effectQueue.push(() => fn());
    }
  },
};

// --- lazy element tree ---
function el(type, props, ...children) {
  return { type, props: props ?? {}, key: props?.key, children };
}
function expand(node, path) {
  if (node == null || node === false || node === true) return null;
  if (Array.isArray(node)) {
    return node.map((child, i) => expand(child, path + '/' + i));
  }
  if (typeof node !== 'object' || !('type' in node)) return node;
  const { type, props, key } = node;
  if (typeof type === 'function') {
    const instPath = path + '>' + (type.name || 'anon') + '#' + String(key ?? '');
    const inst = getInstance(instPath);
    const prev = current;
    current = inst;
    inst.cursor = 0;
    inst.effectCursor = 0;
    let out;
    try {
      out = type(props);
    } finally {
      current = prev;
    }
    return { type: 'slot', instPath, children: expand(out, instPath) };
  }
  return { type, props, children: expand(node.children, path + '/c') };
}
function findNodes(node, predicate, out = []) {
  if (node == null) return out;
  if (Array.isArray(node)) {
    for (const child of node) findNodes(child, predicate, out);
    return out;
  }
  if (predicate(node)) out.push(node);
  if (node.children !== undefined) findNodes(node.children, predicate, out);
  return out;
}
function nodeText(node, out = []) {
  if (node == null || node === false || node === true) return out;
  if (typeof node === 'string' || typeof node === 'number') {
    out.push(String(node));
    return out;
  }
  if (Array.isArray(node)) {
    for (const child of node) nodeText(child, out);
    return out;
  }
  if (node.children !== undefined) nodeText(node.children, out);
  return out;
}

// --- fetch boundary: GET /filters succeeds; reorder POST fails at network level ---
const calls = [];
async function fetchStub(url, options = {}) {
  calls.push({ url, options });
  if (url.endsWith('/api/v1/filters') && (options.method ?? 'GET') === 'GET') {
    return { ok: true, status: 200, json: async () => ({ filters: [filterA, filterB, filterC] }) };
  }
  if (url.endsWith('/api/v1/filters/reorder')) {
    throw new Error('network down');
  }
  return { ok: true, status: 200, json: async () => ({}) };
}

const i18nJs = transpile(accountDir + 'src/hooks/useI18n.ts');
const i18nContext = vm.createContext({
  exports: {},
  require(name) {
    if (name === 'react') return hooks;
    if (name === '../locales/en.json') return { default: {} };
    if (name === '../locales/tr.json') return { default: {} };
    throw new Error('Unexpected i18n import: ' + name);
  },
  localStorage: { getItem: () => null, setItem() {} },
  navigator: { language: 'en-US' },
  document: { documentElement: { lang: 'en' } },
  console,
});
vm.runInContext(i18nJs, i18nContext, { filename: 'useI18n.ts' });

const filtersJs = transpile(accountDir + 'src/pages/Filters.tsx');
const contextified = vm.createContext({
  exports: {},
  require(name) {
    if (name === 'react') return hooks;
    if (name === '../hooks/useI18n') return i18nContext.exports;
    if (name === 'lucide-react') {
      return {
        Filter: 'lucide-Filter',
        Plus: 'lucide-Plus',
        Trash2: 'lucide-Trash2',
        Edit2: 'lucide-Edit2',
        X: 'lucide-X',
        MoveUp: 'lucide-MoveUp',
        MoveDown: 'lucide-MoveDown',
      };
    }
    throw new Error('Unexpected import: ' + name);
  },
  React: { createElement: el },
  fetch: fetchStub,
  confirm: () => true,
  console,
});
vm.runInContext(filtersJs, contextified, { filename: 'Filters.tsx' });
const FiltersPage = contextified.exports.default;
assert.equal(typeof FiltersPage, 'function', 'CONTROL FAILED: FiltersPage not loaded');

function render() {
  const inst = getInstance('root');
  const prev = current;
  current = inst;
  inst.cursor = 0;
  inst.effectCursor = 0;
  let tree;
  try {
    tree = FiltersPage({});
  } finally {
    current = prev;
  }
  const expanded = expand(tree, '');
  const effects = effectQueue.splice(0);
  for (const effect of effects) effect();
  return expanded;
}
async function flush() {
  await new Promise((resolve) => setImmediate(resolve));
}

const filterA = {
  id: 'a', name: 'A-rule', enabled: true, matchAll: true,
  conditions: [{ field: 'subject', operator: 'contains', value: 'x' }],
  actions: [{ type: 'move', target: 'Archive' }], priority: 0,
};
const filterB = {
  id: 'b', name: 'B-rule', enabled: true, matchAll: true,
  conditions: [{ field: 'from', operator: 'contains', value: 'y' }],
  actions: [{ type: 'markRead' }], priority: 1,
};
const filterC = {
  id: 'c', name: 'C-rule', enabled: true, matchAll: true,
  conditions: [{ field: 'to', operator: 'contains', value: 'z' }],
  actions: [{ type: 'flag' }], priority: 2,
};

function rowOrder(tree) {
  return findNodes(tree, (n) => n.type === 'h3').map((h) => nodeText(h).join('').trim());
}

// initial load (the mount effect's single GET /filters)
let tree = render();
assert.equal(calls.length, 1, 'CONTROL FAILED: expected exactly the initial GET');
await flush();
tree = render();
assert.deepEqual(
  rowOrder(tree),
  ['A-rule', 'B-rule', 'C-rule'],
  'CONTROL FAILED: initial filter order wrong'
);
console.log('CONTROL EXPECTED: A, B, C listed in order | ACTUAL: shown');

// Move C up via the real row button (third row's MoveUp).
const upButtons = findNodes(tree, (n) => {
  if (n.type !== 'button') return false;
  return findNodes(n.children, (c) => c.type === 'lucide-MoveUp').length > 0;
});
assert.equal(upButtons.length, 3, 'CONTROL FAILED: expected three move-up buttons');
calls.length = 0;
const clickDone = upButtons[2].props.onClick();
await flush();
await clickDone;
tree = render();

// Control: the reorder was attempted with the optimistic order...
const reorder = calls.find((c) => c.url.endsWith('/api/v1/filters/reorder'));
assert.ok(reorder, 'CONTROL FAILED: reorder request not issued');
assert.deepEqual(
  JSON.parse(reorder.options.body).filterIds,
  ['a', 'c', 'b'],
  'CONTROL FAILED: reorder payload wrong'
);
console.log('CONTROL EXPECTED: reorder POST [a, c, b] issued | ACTUAL: issued');

// CONTRACT: after the failed reorder the component must (a) re-sync from the
// server (rollback), (b) leave the filter objects' priorities untouched, and
// (c) surface the failure to the user instead of console-only silence.
const resyncIssued = calls.some(
  (c) => c.url.endsWith('/api/v1/filters') && (c.options.method ?? 'GET') === 'GET'
);
const prioritiesUntouched =
  filterA.priority === 0 && filterB.priority === 1 && filterC.priority === 2;
const text = nodeText(tree).join('');
const errorShown = text.includes('Failed to reorder filters');
const order = rowOrder(tree);
console.log(
  'EXPECTED: rollback re-sync issued, priorities untouched, failure surfaced | ACTUAL: resync:',
  resyncIssued,
  '| priorities untouched:',
  prioritiesUntouched,
  `(A=${filterA.priority}, B=${filterB.priority}, C=${filterC.priority})`,
  '| error shown:',
  errorShown,
  '| rows:',
  JSON.stringify(order)
);
if (!resyncIssued || !prioritiesUntouched || !errorShown) {
  console.log('PROBLEM CONFIRMED');
  assert.fail(
    'PROBLEM CONFIRMED: failed reorder left the UI on the optimistic order with no user feedback'
  );
}
console.log('PROBLEM NOT REPRODUCED');
console.log('FIX VERIFIED');
