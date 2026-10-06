// Regression: account-portal Layout "Sign out" must actually sign the user
// out — revoke the server session and return to the login page.
// Root cause (fixed 2026-10-07 round 8): the Sign out button had NO onClick
// handler at all (grep: zero logout logic in web/account/src), while the
// portal's login page creates real sessions (POST /api/v1/auth/login ->
// HttpOnly "jwt" cookie). Clicking Sign out did nothing: the cookie stayed
// valid and every portal page remained usable until the JWT expired. The
// server logout endpoint exists and revokes the token
// (/api/v1/auth/logout -> handleLogout; authMiddleware enforces
// IsTokenRevoked). Best-effort contract (webmail/admin precedent): a failing
// logout request must not block the local sign-out.
// Convention: plain-node spec, transpiles the real component (no vitest in
// this package); react-router-dom is stubbed at the module boundary.
import fs from 'node:fs';
import vm from 'node:vm';
import assert from 'node:assert';
import { createRequire } from 'node:module';

const require_ = createRequire(new URL('../../package.json', import.meta.url));
const ts = require_('typescript');

const sourceURL = new URL('./Layout.tsx', import.meta.url);
const source = fs.readFileSync(sourceURL, 'utf8');
const js = ts.transpileModule(source, {
  compilerOptions: {
    module: ts.ModuleKind.CommonJS,
    target: ts.ScriptTarget.ES2020,
    jsx: ts.JsxEmit.React,
  },
}).outputText;

// --- hooks shim ---
const states = [];
function renderComponent(Layout) {
  states.cursor = 0;
  return Layout();
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

// --- boundaries ---
const fetchCalls = [];
let fetchShouldFail = false;
async function fetchStub(url, options) {
  fetchCalls.push({ url, options });
  if (fetchShouldFail) throw new Error('network down');
  return { ok: true, status: 200, json: async () => ({}) };
}
const navigateCalls = [];

const contextified = vm.createContext({
  exports: {},
  require(name) {
    if (name === 'react') return { useState: hooks.useState };
    if (name === 'react-router-dom') {
      return {
        Outlet: 'Outlet',
        Link: 'Link',
        useLocation: () => ({ pathname: '/profile' }),
        useNavigate: () => (to) => navigateCalls.push(to),
      };
    }
    if (name === 'lucide-react') {
      return {
        User: 'lucide-User', Lock: 'lucide-Lock', Shield: 'lucide-Shield',
        Forward: 'lucide-Forward', Palmtree: 'lucide-Palmtree',
        LogOut: 'lucide-LogOut', Filter: 'lucide-Filter',
      };
    }
    throw new Error('Unexpected import: ' + name);
  },
  React: { createElement: el },
  fetch: fetchStub,
  console,
});
vm.runInContext(js, contextified, { filename: sourceURL.pathname });

const Layout = contextified.exports.default;
assert.equal(typeof Layout, 'function', 'CONTROL FAILED: Layout not loaded');

// CONTROL: the layout renders its chrome including the Sign out control.
const tree = renderComponent(Layout);
const text = collectText(tree).join('');
assert.ok(text.includes('Account Settings'), 'CONTROL FAILED: heading missing');
const signOutButton = findNode(tree, (node) => node.type === 'button' && collectText(node).join('').includes('Sign out'));
assert.ok(signOutButton, 'CONTROL FAILED: Sign out button not found');
console.log('CONTROL EXPECTED: layout renders with a Sign out button | ACTUAL: shown');

// CONTRACT: the control must initiate sign-out.
assert.equal(
  typeof signOutButton.props.onClick,
  'function',
  "PROBLEM CONFIRMED: the Sign out button has no onClick handler — clicking it does nothing and the session stays valid"
);
await signOutButton.props.onClick({ preventDefault() {} });

// The server session must be revoked via the logout endpoint...
assert.equal(fetchCalls.length, 1, 'PROBLEM CONFIRMED: sign-out issued no logout request');
assert.ok(
  fetchCalls[0].url.endsWith('/api/v1/auth/logout'),
  'PROBLEM CONFIRMED: wrong logout endpoint: ' + fetchCalls[0].url
);
assert.equal(
  fetchCalls[0].options?.credentials,
  'include',
  'PROBLEM CONFIRMED: logout request missing credentials (the HttpOnly cookie must be sent)'
);

// ...and the user must be returned to the login page.
assert.deepEqual(
  navigateCalls,
  ['/login'],
  'PROBLEM CONFIRMED: sign-out did not return the user to the login page'
);
console.log('FIX VERIFIED (happy path): POST /api/v1/auth/logout with credentials, then navigate /login');

// Best-effort contract: a failing logout request must not block sign-out.
fetchShouldFail = true;
fetchCalls.length = 0;
navigateCalls.length = 0;
const tree2 = renderComponent(Layout);
const signOutButton2 = findNode(tree2, (node) => node.type === 'button' && collectText(node).join('').includes('Sign out'));
await signOutButton2.props.onClick({ preventDefault() {} });
await new Promise((resolve) => setImmediate(resolve));
assert.equal(fetchCalls.length, 1, 'CONTROL FAILED: rejecting fetch was not attempted');
assert.deepEqual(
  navigateCalls,
  ['/login'],
  'PROBLEM CONFIRMED: a failed logout request blocked the local sign-out'
);
console.log('FIX VERIFIED (best-effort): failing logout request still signs the user out');