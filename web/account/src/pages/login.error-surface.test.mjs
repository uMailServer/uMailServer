// Regression: account-portal LoginPage must render the failed-login error.
// Root cause (fixed 2026-10-06): error state was set but destructured away
// (const [, setError]), so failed sign-ins gave no user feedback.
// Convention: plain-node spec, transpiles the real component (no vitest in
// this package); only the fetch boundary is mocked.
import fs from 'node:fs';
import vm from 'node:vm';
import assert from 'node:assert';
import { createRequire } from 'node:module';

const require_ = createRequire(new URL('../../package.json', import.meta.url));
const ts = require_('typescript');

const sourceURL = new URL('./Login.tsx', import.meta.url);
const source = fs.readFileSync(sourceURL, 'utf8');
const js = ts.transpileModule(source, {
  compilerOptions: {
    module: ts.ModuleKind.CommonJS,
    target: ts.ScriptTarget.ES2020,
    jsx: ts.JsxEmit.React,
  },
}).outputText;

// --- hooks shim with explicit re-render control ---
const states = [];
function renderComponent(LoginPage) {
  states.cursor = 0;
  return LoginPage();
}
const hooks = {
  useState(init) {
    const index = states.cursor++;
    if (!(index in states)) states[index] = typeof init === 'function' ? init() : init;
    return [
      states[index],
      (value) => {
        states[index] = value;
      },
    ];
  },
};

// --- element-tree builder (classic JSX runtime) ---
function el(type, props, ...children) {
  return { type, props: props ?? {}, children };
}
function findNode(node, predicate) {
  if (node == null || node === false || node === true) return null;
  if (Array.isArray(node)) {
    for (const child of node) {
      const hit = findNode(child, predicate);
      if (hit) return hit;
    }
    return null;
  }
  if (typeof node === 'object' && 'type' in node) {
    if (predicate(node)) return node;
    return findNode(node.children, predicate);
  }
  return null;
}
function collectText(node, out = []) {
  if (node == null || node === false || node === true) return out;
  if (typeof node === 'string' || typeof node === 'number') {
    out.push(String(node));
    return out;
  }
  if (Array.isArray(node)) {
    for (const child of node) collectText(child, out);
    return out;
  }
  if (typeof node === 'object' && 'children' in node) collectText(node.children, out);
  return out;
}

// --- fetch boundary stub: server rejects the credentials ---
const fetchCalls = [];
async function fetchStub(url, options) {
  fetchCalls.push({ url, body: options?.body });
  return {
    ok: false,
    status: 401,
    json: async () => ({ error: 'Invalid credentials' }),
  };
}

const contextified = vm.createContext({
  exports: {},
  require(name) {
    if (name === 'react') return { useState: hooks.useState };
    if (name === 'lucide-react') return { Mail: 'lucide-Mail' };
    throw new Error('Unexpected import: ' + name);
  },
  React: { createElement: el },
  fetch: fetchStub,
  window: { location: { href: 'about:blank' } },
  console,
});
vm.runInContext(js, contextified, { filename: sourceURL.pathname });

const LoginPage = contextified.exports.default;
assert.equal(typeof LoginPage, 'function', 'CONTROL FAILED: LoginPage not loaded');

// initial render (control)
const tree1 = renderComponent(LoginPage);
const text1 = collectText(tree1).join('');
assert.ok(text1.includes('Sign in'), 'CONTROL FAILED: initial render missing Sign in button');
console.log('CONTROL EXPECTED: initial render with Sign in button | ACTUAL: shown');

// drive the real submit handler through the failed login
const form = findNode(tree1, (node) => node.type === 'form');
assert.ok(form, 'CONTROL FAILED: login form not found');
assert.equal(typeof form.props.onSubmit, 'function', 'CONTROL FAILED: form has no submit handler');
await form.props.onSubmit({ preventDefault() {} });

// re-render with the state updated by the failure path
const tree2 = renderComponent(LoginPage);
const text2 = collectText(tree2).join('');

const errShown = text2.includes('Invalid email or password');
const stillLoading = text2.includes('Signing in...');
console.log(
  'EXPECTED: rendered output shows the failed-login message | ACTUAL: shown:',
  errShown,
  '| still loading:',
  stillLoading
);
assert.ok(errShown, 'PROBLEM CONFIRMED: failed login renders no error message');
assert.ok(!stillLoading, 'PROBLEM CONFIRMED: submit button stuck in loading state');
console.log('PROBLEM NOT REPRODUCED');

// controls: exactly one real login request hit the right endpoint
assert.equal(fetchCalls.length, 1, 'CONTROL FAILED: login fetch not issued exactly once');
assert.ok(
  fetchCalls[0].url.endsWith('/api/v1/auth/login'),
  'CONTROL FAILED: wrong login endpoint: ' + fetchCalls[0].url
);
console.log('CONTROL EXPECTED: one POST to /api/v1/auth/login | ACTUAL: issued');
console.log('FIX VERIFIED');
