// Regression: Vacation page must not leak raw i18n keys into the subject.
// Root cause (fixed 2026-10-06): loadConfig captured t('vacation.defaultSubject')
// into config.subject; when the locale chunk load lost the race to the vacation
// GET (deep-link/refresh with cold chunk), t() returned the literal key, which
// was prefilled into the subject and persisted on save.
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
          json: async () => ({ enabled: true, subject: '' }),
        });
    });
  }
  if (url.endsWith('/api/v1/vacation')) {
    return { ok: true, status: 200, json: async () => ({}) };
  }
  return { ok: true, status: 200, json: async () => ({}) };
}

// --- locale modules as THENABLES: the i18n load stays pending on demand ---
let resolveEn = null;
function pendingEnModule() {
  return {
    then(res) {
      resolveEn = res;
    },
  };
}

const i18nJs = transpile(accountDir + 'src/hooks/useI18n.ts');
const i18nContext = vm.createContext({
  exports: {},
  require(name) {
    if (name === 'react') return hooks;
    if (name === '../locales/en.json') return pendingEnModule();
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

function subjectInput(tree) {
  return findNodes(tree, (n) => n.type === 'input' && n.props?.type === 'text')[0];
}

// Mount: both loads start; i18n stays PENDING (thenable), vacation GET deferred.
let tree = render();
assert.ok(resolveVacation, 'CONTROL FAILED: vacation GET not issued');
// The vacation response resolves FIRST — this is the deep-link/refresh race.
resolveVacation();
await flush();
await flush();
tree = render();

// CONTROL: the enabled section rendered with the subject field present.
const input = subjectInput(tree);
assert.ok(input, 'CONTROL FAILED: subject input not rendered');
console.log('CONTROL EXPECTED: subject field rendered | ACTUAL: shown');

// CONTRACT: the field must never carry a raw translation key.
const shown = input.props.value;
console.log(
  'EXPECTED: subject field empty while translations are pending | ACTUAL:',
  JSON.stringify(shown)
);
if (shown === 'vacation.defaultSubject') {
  console.log('PROBLEM CONFIRMED');
  assert.fail(
    'PROBLEM CONFIRMED: the raw i18n key leaked into the subject field while translations were pending'
  );
}
console.log('PROBLEM NOT REPRODUCED');

// Persistence facet: saving must not store the leaked key on the server.
const form = findNodes(tree, (n) => n.type === 'form')[0];
assert.ok(form, 'CONTROL FAILED: form not found');
await form.props.onSubmit({ preventDefault() {} });
await flush();
const put = calls.find((c) => c.options?.method === 'PUT');
assert.ok(put, 'CONTROL FAILED: save issued no PUT');
const body = JSON.parse(put.options.body);
console.log(
  'EXPECTED: PUT body.subject is not the raw key | ACTUAL:',
  JSON.stringify(body.subject)
);
if (body.subject === 'vacation.defaultSubject') {
  console.log('PROBLEM CONFIRMED');
  assert.fail('PROBLEM CONFIRMED: the raw i18n key was persisted as the vacation subject');
}
console.log('PROBLEM NOT REPRODUCED');
console.log('FIX VERIFIED');
