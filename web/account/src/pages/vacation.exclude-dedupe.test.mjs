// Regression: the Vacation exclude list must dedupe case-insensitively.
// Root cause (fixed 2026-10-06): handleAddExclude used
// `exclude_addresses.includes(excludeInput)` — an exact-match dedupe — so
// 'Boss@Company.com' and 'boss@company.com' both entered the list even though
// email addresses are conventionally case-insensitive and the check's own
// intent is list uniqueness.
// Convention: plain-node spec, transpiles the real components (no vitest in
// this package); only the fetch boundary and locale-module loading are mocked.
import fs from 'node:fs';
import vm from 'node:vm';
import assert from 'node:assert';
import { createRequire } from 'node:module';

const accountDir = new URL('../../', import.meta.url).pathname;
const require_ = createRequire(new URL('./Vacation.tsx', import.meta.url));
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

// --- instance-scoped hooks shim (slot-based run-once effects) ---
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

// --- fetch boundary: deferred GET /vacation ---
const calls = [];
let resolveVacation = null;
async function fetchStub(url, options = {}) {
  calls.push({ url, options });
  if (url.endsWith('/api/v1/vacation') && (options.method ?? 'GET') === 'GET') {
    return new Promise((resolve) => {
      resolveVacation = () =>
        resolve({
          ok: true,
          status: 200,
          json: async () => ({
            enabled: true,
            subject: 'OOO',
            message: 'Away',
            start_date: '',
            end_date: '',
            send_interval: 24,
            exclude_addresses: [],
            ignore_lists: false,
            contacts_only: false,
          }),
        });
    });
  }
  return { ok: true, status: 200, json: async () => ({}) };
}

const locale = {
  default: {
    vacation: {
      title: 'Vacation',
      excludeAddresses: 'Exclude addresses',
      excludePlaceholder: 'Enter an address',
    },
    common: { add: 'Add' },
  },
};

const i18nJs = transpile(accountDir + 'src/hooks/useI18n.ts');
const i18nContext = vm.createContext({
  exports: {},
  require(name) {
    if (name === 'react') return hooks;
    if (name === '../locales/en.json') return locale;
    if (name === '../locales/tr.json') return { default: {} };
    throw new Error('Unexpected i18n import: ' + name);
  },
  localStorage: { getItem: () => null, setItem() {} },
  navigator: { language: 'en-US' },
  document: { documentElement: { lang: 'en' } },
  console,
});
vm.runInContext(i18nJs, i18nContext, { filename: 'useI18n.ts' });

const vacationJs = transpile(accountDir + 'src/pages/Vacation.tsx');
const contextified = vm.createContext({
  exports: {},
  require(name) {
    if (name === 'react') return hooks;
    if (name === '../hooks/useI18n') return i18nContext.exports;
    if (name === 'lucide-react') {
      return {
        Palmtree: 'lucide-Palmtree',
        Calendar: 'lucide-Calendar',
        Clock: 'lucide-Clock',
        Mail: 'lucide-Mail',
        AlertCircle: 'lucide-AlertCircle',
      };
    }
    throw new Error('Unexpected import: ' + name);
  },
  React: { createElement: el },
  fetch: fetchStub,
  console,
});
vm.runInContext(vacationJs, contextified, { filename: 'Vacation.tsx' });
const VacationPage = contextified.exports.default;
assert.equal(typeof VacationPage, 'function', 'CONTROL FAILED: VacationPage not loaded');

function render() {
  const inst = getInstance('root');
  const prev = current;
  current = inst;
  inst.cursor = 0;
  inst.effectCursor = 0;
  let tree;
  try {
    tree = VacationPage({});
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

const emailInput = (tree) =>
  findNodes(tree, (n) => n.type === 'input' && n.props?.type === 'email')[0];
const addBtn = (tree) =>
  findNodes(tree, (n) => n.type === 'button' && String(n.props?.className).includes('bg-gray-100'))[0];
const chips = (tree) =>
  findNodes(tree, (n) => n.type === 'span' && String(n.props?.className).includes('inline-flex'));

function addExclude(tree, value) {
  const input = emailInput(tree);
  assert.ok(input, 'CONTROL FAILED: exclude input not rendered');
  input.props.onChange({ target: { value } });
  tree = render();
  const btn = addBtn(tree);
  assert.ok(btn, 'CONTROL FAILED: Add button not rendered');
  btn.props.onClick();
  return render();
}

// Mount: resolve the vacation config so the enabled section renders.
let tree = render();
assert.ok(resolveVacation, 'CONTROL FAILED: vacation GET not issued');
resolveVacation();
await flush();
await flush();
tree = render();

// CONTROL: the enabled section rendered with the exclude input present.
assert.ok(emailInput(tree), 'CONTROL FAILED: exclude input not rendered');
console.log('CONTROL EXPECTED: exclude section rendered | ACTUAL: shown');

// Add two case-variants of the same address.
tree = addExclude(tree, 'Boss@Company.com');
tree = addExclude(tree, 'boss@company.com');

// CONTRACT: the list holds ONE entry — dedupe is case-insensitive.
let shown = chips(tree).map((n) => String(n.children?.[0]));
console.log(
  'EXPECTED: one exclude entry for the case-variant pair | ACTUAL:',
  JSON.stringify(shown),
);
if (shown.length !== 1) {
  console.log('PROBLEM CONFIRMED: the exclude dedupe is case-sensitive — both case-variants entered the list');
  assert.equal(shown.length, 1, 'PROBLEM CONFIRMED: case-variant duplicates in the exclude list');
}

// CONTROL: the exact-match dedupe still rejects an identical re-add.
const beforeExact = chips(render()).length;
tree = addExclude(tree, 'boss@company.com');
const afterExact = chips(tree).length;
console.log(
  'CONTROL EXPECTED: exact duplicate re-add is a no-op | ACTUAL:',
  `${beforeExact} -> ${afterExact}`,
);
assert.equal(afterExact, beforeExact, 'CONTROL FAILED: exact-match dedupe regressed');

console.log('PROBLEM NOT REPRODUCED');
console.log('FIX VERIFIED');
