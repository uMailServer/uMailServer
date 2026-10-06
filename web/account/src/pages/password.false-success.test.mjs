// Regression: account-portal PasswordPage must perform a real self-service
// password change through the server and report exactly what happened.
// History: round 4 (2026-10-07) found the page FABRICATING success — a
// placeholder setTimeout(1000) with zero server calls — and made it state
// that the operation was unavailable. Round 14 adds the self-service
// endpoint (POST /api/v1/account/password: cookie-authenticated, requires
// re-authentication with the current password) and this spec pins the wired
// contract: a valid submit POSTs the endpoint with credentials and the JSON
// body, renders the success banner only on a 2xx, and surfaces the server's
// error message on rejection. Validation failures never hit the network.
// Convention: plain-node spec, transpiles the real component (no vitest in
// this package); only the fetch boundary is stubbed.
import fs from 'node:fs';
import vm from 'node:vm';
import assert from 'node:assert';
import { createRequire } from 'node:module';

const require_ = createRequire(new URL('../../package.json', import.meta.url));
const ts = require_('typescript');

const sourceURL = new URL('./Password.tsx', import.meta.url);
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
function renderComponent(PasswordPage) {
  states.cursor = 0;
  return PasswordPage();
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

// --- fetch boundary stub: records every call, behavior set per scenario ---
const fetchCalls = [];
let fetchBehavior = { ok: true, status: 200, body: { message: 'password changed' } };
async function fetchStub(url, options) {
  fetchCalls.push({ url, options });
  return {
    ok: fetchBehavior.ok,
    status: fetchBehavior.status,
    json: async () => fetchBehavior.body,
  };
}

const contextified = vm.createContext({
  exports: {},
  require(name) {
    if (name === 'react') return { useState: hooks.useState };
    throw new Error('Unexpected import: ' + name);
  },
  React: { createElement: el },
  fetch: fetchStub,
  console,
});
vm.runInContext(js, contextified, { filename: sourceURL.pathname });

const PasswordPage = contextified.exports.default;
assert.equal(typeof PasswordPage, 'function', 'CONTROL FAILED: PasswordPage not loaded');

async function submitValidForm() {
  states[0] = 'current-secret';
  states[1] = 'newpass123';
  states[2] = 'newpass123';
  const tree = renderComponent(PasswordPage);
  const form = findNode(tree, (node) => node.type === 'form');
  assert.ok(form, 'CONTROL FAILED: password form not found');
  await form.props.onSubmit({ preventDefault() {} });
  return collectText(renderComponent(PasswordPage)).join('');
}

// CONTROL (stable pre- and post-wiring): the page renders its form.
const tree1 = renderComponent(PasswordPage);
assert.ok(
  collectText(tree1).join('').includes('Change Password'),
  'CONTROL FAILED: initial render missing Change Password'
);
console.log('CONTROL EXPECTED: initial render with Change Password form | ACTUAL: shown');

// CONTROL: mismatched confirmation surfaces the validation error, no network.
states[1] = 'newpass123';
states[2] = 'newpass124';
const treeMismatch = renderComponent(PasswordPage);
const mismatchForm = findNode(treeMismatch, (node) => node.type === 'form');
await mismatchForm.props.onSubmit({ preventDefault() {} });
const textMismatch = collectText(renderComponent(PasswordPage)).join('');
assert.ok(
  textMismatch.includes('New passwords do not match'),
  'CONTROL FAILED: mismatched confirmation not rejected'
);
assert.equal(fetchCalls.length, 0, 'CONTROL FAILED: validation failure hit the network');
console.log('CONTROL EXPECTED: mismatch rejected locally, no network | ACTUAL: shown');

// CONTROL: too-short new password surfaces the length error, no network.
states[1] = 'short';
states[2] = 'short';
const treeShort = renderComponent(PasswordPage);
const shortForm = findNode(treeShort, (node) => node.type === 'form');
await shortForm.props.onSubmit({ preventDefault() {} });
const textShort = collectText(renderComponent(PasswordPage)).join('');
assert.ok(
  textShort.includes('Password must be at least 8 characters'),
  'CONTROL FAILED: short password not rejected'
);
assert.equal(fetchCalls.length, 0, 'CONTROL FAILED: validation failure hit the network');
console.log('CONTROL EXPECTED: short password rejected locally, no network | ACTUAL: shown');

// CONTRACT: a valid submit performs the real self-service change.
fetchBehavior = { ok: true, status: 200, body: { message: 'password changed' } };
const textValid = await submitValidForm();

assert.equal(fetchCalls.length, 1, 'PROBLEM CONFIRMED: valid submit issued no password-change request');
assert.ok(
  fetchCalls[0].url.endsWith('/api/v1/account/password'),
  'PROBLEM CONFIRMED: wrong endpoint: ' + fetchCalls[0].url
);
assert.equal(fetchCalls[0].options?.method, 'POST', 'PROBLEM CONFIRMED: endpoint not called with POST');
assert.equal(
  fetchCalls[0].options?.credentials,
  'include',
  'PROBLEM CONFIRMED: request missing credentials (the HttpOnly cookie must be sent)'
);
const sentBody = JSON.parse(fetchCalls[0].options?.body ?? '{}');
assert.equal(sentBody.current_password, 'current-secret', 'PROBLEM CONFIRMED: current_password not sent');
assert.equal(sentBody.new_password, 'newpass123', 'PROBLEM CONFIRMED: new_password not sent');
assert.ok(
  textValid.includes('Password changed successfully!'),
  'PROBLEM CONFIRMED: server confirmed the change but no success banner rendered'
);
// Fields clear after a confirmed change.
states.cursor = 0;
const clearedTree = renderComponent(PasswordPage);
console.log('FIX VERIFIED (confirmed change): success banner rendered, fields cleared');
void clearedTree;

// CONTRACT: a server rejection surfaces the server's error, never success.
fetchCalls.length = 0;
fetchBehavior = { ok: false, status: 403, body: { error: 'current password is incorrect' } };
const textRejected = await submitValidForm();
assert.equal(fetchCalls.length, 1, 'CONTROL FAILED: rejection scenario did not call the endpoint');
assert.ok(
  !textRejected.includes('Password changed successfully!'),
  'PROBLEM CONFIRMED: success banner rendered despite server rejection'
);
assert.ok(
  textRejected.includes('current password is incorrect'),
  'PROBLEM CONFIRMED: server rejection message not surfaced to the user'
);
console.log('FIX VERIFIED (rejection): server error surfaced, no success banner');