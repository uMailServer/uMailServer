// Regression: account-portal ProfilePage must not fabricate the user's
// identity or present a save flow that persists nothing.
// Root cause (fixed 2026-10-07 round 7): the page — which is also the
// portal's landing page (App.tsx renders it on the index route AND
// /profile) — displayed a HARDCODED email 'user@example.com' as the signed-in
// user's address regardless of who was logged in, and a Save button whose
// handler only awaited a placeholder setTimeout(1000) — no network call, no
// feedback, no persistence. The server has no profile fields at all (no
// displayName/timezone in the account API or DB models) and no self-service
// profile endpoint (the account routes are admin-gated), so the page cannot
// show or edit anything real. It must state the limitation instead.
// Convention: plain-node spec, transpiles the real component (no vitest in
// this package). The page performs no network or timer work once honest, so
// no boundaries are stubbed beyond the module scope.
import fs from 'node:fs';
import vm from 'node:vm';
import assert from 'node:assert';
import { createRequire } from 'node:module';

const require_ = createRequire(new URL('../../package.json', import.meta.url));
const ts = require_('typescript');

const sourceURL = new URL('./Profile.tsx', import.meta.url);
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
function renderComponent(ProfilePage) {
  states.cursor = 0;
  return ProfilePage();
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

const fetchCalls = [];
async function fetchStub(url, options) {
  fetchCalls.push({ url, options });
  return { ok: false, status: 404, json: async () => ({ error: 'not found' }) };
}

const contextified = vm.createContext({
  exports: {},
  require(name) {
    if (name === 'react') return { useState: hooks.useState };
    if (name === 'lucide-react') {
      return { User: 'lucide-User', Camera: 'lucide-Camera' };
    }
    throw new Error('Unexpected import: ' + name);
  },
  React: { createElement: el },
  fetch: fetchStub,
  setTimeout,
  console,
});
vm.runInContext(js, contextified, { filename: sourceURL.pathname });

const ProfilePage = contextified.exports.default;
assert.equal(typeof ProfilePage, 'function', 'CONTROL FAILED: ProfilePage not loaded');

// CONTROL: the page renders its heading.
const tree = renderComponent(ProfilePage);
const text = collectText(tree).join('');
assert.ok(text.includes('Profile Settings'), 'CONTROL FAILED: heading missing');
console.log('CONTROL EXPECTED: Profile Settings page renders | ACTUAL: shown');

// CONTRACT: no fabricated identity (a hardcoded address shown as the user's
// own), no save flow that persists nothing, and the portal's limitation is
// stated — the server has no profile fields or self-service endpoint.
const fabricatedEmail =
  findNode(tree, (node) => node.type === 'input' && node.props?.value === 'user@example.com') !== null;
const fakeSaveFlow = text.includes('Save Changes');
console.log(
  'EXPECTED: no fabricated identity, no fake save flow, limitation explained | ACTUAL: fabricated email:',
  fabricatedEmail,
  '| fake save flow:',
  fakeSaveFlow,
  '| server calls:',
  fetchCalls.length
);
assert.equal(
  fabricatedEmail,
  false,
  "PROBLEM CONFIRMED: ProfilePage displayed the hardcoded placeholder 'user@example.com' as the user's email address"
);
assert.equal(
  fakeSaveFlow,
  false,
  'PROBLEM CONFIRMED: ProfilePage presents a Save flow that persists nothing'
);
assert.equal(
  fetchCalls.length,
  0,
  'CONTROL FAILED: the page hit the network unexpectedly (no self-service profile endpoint exists)'
);
assert.ok(
  text.includes('administrator'),
  'PROBLEM CONFIRMED: the unavailability of portal profile management is not explained to the user'
);
console.log('FIX VERIFIED: no fabricated identity or fake save flow; the limitation is stated');