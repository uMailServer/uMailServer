// Regression: account-portal FilterEditor must re-initialize when the edit
// target changes (keyed remount). Root cause (fixed 2026-10-06): the editor
// element had no `key`, so its useState initializers ran only once — switching
// from editing filter A to filter B kept A's data under B's identity, and
// saving PUT A's rules to /api/v1/filters/B (silent filter corruption).
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

// --- instance-scoped hooks shim: key change == remount (React semantics) ---
const instances = new Map();
let current = null;
const effectQueue = [];
function getInstance(path) {
  if (!instances.has(path)) instances.set(path, { states: [], cursor: 0, effectsRun: false });
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
    if (!inst.effectsRun) effectQueue.push(() => fn());
  },
};

// --- lazy element tree: function components expand with instance state ---
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

// --- fetch boundary: deferred, recorded ---
const pending = [];
const calls = [];
function fetchStub(url, options = {}) {
  return new Promise((resolve, reject) => {
    calls.push({ url, options, resolve, reject });
    pending.push({ resolve, reject });
  });
}

// --- module shims for the transpiled sources ---
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

// --- fixtures: two filters with clearly different rules ---
const filterA = {
  id: 'a',
  name: 'A-rule',
  enabled: true,
  matchAll: true,
  conditions: [{ field: 'subject', operator: 'contains', value: 'invoice' }],
  actions: [{ type: 'move', target: 'Archive' }],
  priority: 0,
};
const filterB = {
  id: 'b',
  name: 'B-rule',
  enabled: true,
  matchAll: false,
  conditions: [{ field: 'from', operator: 'contains', value: 'boss' }],
  actions: [{ type: 'delete' }],
  priority: 1,
};

// initial load
let tree = render();
assert.equal(pending.length, 1, 'CONTROL FAILED: initial filters fetch missing');
assert.ok(calls[0].url.endsWith('/api/v1/filters'), 'CONTROL FAILED: wrong filters endpoint');
pending
  .shift()
  .resolve({ ok: true, status: 200, json: async () => ({ filters: [filterA, filterB] }) });
await flush();
tree = render();
const text = nodeText(tree).join('');
assert.ok(text.includes('A-rule') && text.includes('B-rule'), 'CONTROL FAILED: both filters not rendered');
console.log('CONTROL EXPECTED: both filters listed | ACTUAL: shown');

// locate the two Edit buttons (rows in order)
const editButtons = findNodes(tree, (n) => {
  if (n.type !== 'button') return false;
  return findNodes(n.children, (c) => c.type === 'lucide-Edit2').length > 0;
});
assert.equal(editButtons.length, 2, 'CONTROL FAILED: expected two edit buttons');
const nameInputs = () =>
  findNodes(tree, (n) => n.type === 'input' && n.props?.placeholder === 'filters.namePlaceholder');

// Edit filter A
await editButtons[0].props.onClick();
tree = render();
await flush();
let editorInput = nameInputs()[0];
assert.ok(editorInput, 'CONTROL FAILED: editor did not open for filter A');
assert.equal(editorInput.props.value, 'A-rule', 'CONTROL FAILED: editor does not show A data');
console.log('CONTROL EXPECTED: editor shows A-rule | ACTUAL: shown');

// Edit filter B while the editor is open (list remains clickable)
await editButtons[1].props.onClick();
tree = render();
await flush();
editorInput = nameInputs()[0];
assert.ok(editorInput, 'CONTROL FAILED: editor not present after switching target');
console.log(
  'EXPECTED: editor re-initializes to B-rule | ACTUAL:',
  JSON.stringify(editorInput.props.value)
);
assert.equal(editorInput.props.value, 'B-rule', 'PROBLEM CONFIRMED: editor kept stale A data');

// submit — the saved payload must match filter B
const form = findNodes(tree, (n) => n.type === 'form')[0];
assert.ok(form, 'CONTROL FAILED: editor form not found');
await form.props.onSubmit({ preventDefault() {} });
await flush();
const putCall = calls.find((c) => c.options?.method === 'PUT');
assert.ok(putCall, 'CONTROL FAILED: save issued no PUT');
assert.ok(putCall.url.endsWith('/api/v1/filters/b'), 'CONTROL FAILED: PUT targeted ' + putCall.url);
const body = JSON.parse(putCall.options.body);
console.log(
  'EXPECTED: PUT /api/v1/filters/b body carries B-rule data | ACTUAL: name =',
  JSON.stringify(body.name),
  '| conditions[0].field =',
  JSON.stringify(body.conditions?.[0]?.field)
);
assert.equal(body.name, 'B-rule', 'PROBLEM CONFIRMED: save submits stale A data to filter B');
assert.equal(body.conditions?.[0]?.field, 'from', 'PROBLEM CONFIRMED: conditions are stale A data');
console.log('PROBLEM NOT REPRODUCED');
console.log('FIX VERIFIED');
