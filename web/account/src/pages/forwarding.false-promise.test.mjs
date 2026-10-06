// Regression: account-portal ForwardingPage must not describe or pretend to
// configure mail forwarding it cannot set.
// Root cause (fixed 2026-10-07 round 6): the page presented an "Important"
// notice describing real mail-routing behavior ("Forwarded emails will be
// sent to the specified address..."), a forwarding form, and a Save button
// whose handler only awaited a placeholder setTimeout(1000) — no network
// call, no feedback, no persistence. A user enabling forwarding would expect
// mail at another address that never arrives. The server exposes no
// self-service forwarding endpoint: the account model's ForwardTo/
// ForwardKeepCopy fields exist only on the admin-gated PUT
// /api/v1/accounts/{email} (server_accounts.go, adminMiddleware), and the
// portal has no session context holding the account address. The page must
// state the limitation instead of describing behavior it cannot set.
// Convention: plain-node spec, transpiles the real component (no vitest in
// this package). The page performs no network or timer work once honest, so
// no boundaries are stubbed beyond the module scope.
import fs from 'node:fs';
import vm from 'node:vm';
import assert from 'node:assert';
import { createRequire } from 'node:module';

const require_ = createRequire(new URL('../../package.json', import.meta.url));
const ts = require_('typescript');

const sourceURL = new URL('./Forwarding.tsx', import.meta.url);
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
function renderComponent(ForwardingPage) {
  states.cursor = 0;
  return ForwardingPage();
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
    if (name === 'lucide-react') return { AlertCircle: 'lucide-AlertCircle' };
    throw new Error('Unexpected import: ' + name);
  },
  React: { createElement: el },
  fetch: fetchStub,
  setTimeout,
  console,
});
vm.runInContext(js, contextified, { filename: sourceURL.pathname });

const ForwardingPage = contextified.exports.default;
assert.equal(typeof ForwardingPage, 'function', 'CONTROL FAILED: ForwardingPage not loaded');

// CONTROL: the page renders its heading.
const tree = renderComponent(ForwardingPage);
const text = collectText(tree).join('');
assert.ok(text.includes('Mail Forwarding'), 'CONTROL FAILED: heading missing');
console.log('CONTROL EXPECTED: Mail Forwarding page renders | ACTUAL: shown');

// CONTRACT: the page must not describe forwarding behavior it cannot set
// ("Forwarded emails will be sent...") and must not present a save flow that
// persists nothing; the portal's limitation must be stated. The server's
// forwarding fields are admin-gated only.
const fictionalBehavior = text.includes('Forwarded emails will be sent');
const fakeSaveFlow = text.includes('Save Changes');
console.log(
  'EXPECTED: no fictional routing description, no fake save flow, limitation explained | ACTUAL: fictional description:',
  fictionalBehavior,
  '| fake save flow:',
  fakeSaveFlow,
  '| server calls:',
  fetchCalls.length
);
assert.equal(
  fictionalBehavior,
  false,
  'PROBLEM CONFIRMED: ForwardingPage describes mail-routing behavior it cannot configure'
);
assert.equal(
  fakeSaveFlow,
  false,
  'PROBLEM CONFIRMED: ForwardingPage presents a Save flow that persists nothing'
);
assert.equal(
  fetchCalls.length,
  0,
  'CONTROL FAILED: the page hit the network unexpectedly (no self-service forwarding endpoint exists)'
);
assert.ok(
  text.includes('administrator'),
  'PROBLEM CONFIRMED: the unavailability of portal forwarding is not explained to the user'
);
console.log('FIX VERIFIED: no fictional forwarding description; the limitation is stated');