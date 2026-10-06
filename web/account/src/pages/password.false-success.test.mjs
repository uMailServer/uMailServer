// Regression: account-portal PasswordPage must never report a password change
// it did not perform.
// Root cause (fixed 2026-10-07 round 4): handleSubmit validated its inputs,
// then awaited a placeholder `setTimeout(1000)` standing in for a network
// call that never happened, and set success unconditionally — rendering
// "Password changed successfully!" with zero API calls. The page ships inside
// the server binary (embed.go embeds web/account/dist) and is reachable via
// App.tsx:19 + the Layout nav entry. No self-service password endpoint exists
// to wire to (internal/api handleAccountDetail serves only /totp/* plus
// GET/PUT/DELETE; the documented POST /api/v1/accounts/{email}/password is
// unimplemented and admin-gated), so the truthful behavior is to report that
// the operation is unavailable instead of fabricating success.
// Convention: plain-node spec, transpiles the real component (no vitest in
// this package); only the fetch and timer boundaries are stubbed.
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

// --- boundaries: no network, and timers fire immediately (the page's old
// placeholder delay is pure wall-clock, so resolving it inline is faithful) ---
const fetchCalls = [];
async function fetchStub(url, options) {
  fetchCalls.push({ url, body: options?.body });
  return { ok: false, status: 404, json: async () => ({ error: 'not found' }) };
}
function setTimeoutStub(fn) {
  fn();
  return 0;
}

const contextified = vm.createContext({
  exports: {},
  require(name) {
    if (name === 'react') return { useState: hooks.useState };
    throw new Error('Unexpected import: ' + name);
  },
  React: { createElement: el },
  fetch: fetchStub,
  setTimeout: setTimeoutStub,
  console,
});
vm.runInContext(js, contextified, { filename: sourceURL.pathname });

const PasswordPage = contextified.exports.default;
assert.equal(typeof PasswordPage, 'function', 'CONTROL FAILED: PasswordPage not loaded');

async function submit(tree) {
  const form = findNode(tree, (node) => node.type === 'form');
  assert.ok(form, 'CONTROL FAILED: password form not found');
  assert.equal(typeof form.props.onSubmit, 'function', 'CONTROL FAILED: form has no submit handler');
  await form.props.onSubmit({ preventDefault() {} });
}

// CONTROL (stable pre- and post-fix): the page renders its form.
const tree1 = renderComponent(PasswordPage);
const text1 = collectText(tree1).join('');
assert.ok(text1.includes('Change Password'), 'CONTROL FAILED: initial render missing Change Password');
console.log('CONTROL EXPECTED: initial render with Change Password form | ACTUAL: shown');

// CONTROL: mismatched confirmation surfaces the validation error, no success.
states[1] = 'newpass123';
states[2] = 'newpass124';
const treeMismatch = renderComponent(PasswordPage);
await submit(treeMismatch);
const textMismatch = collectText(renderComponent(PasswordPage)).join('');
assert.ok(
  textMismatch.includes('New passwords do not match'),
  'CONTROL FAILED: mismatched confirmation not rejected'
);
assert.ok(
  !textMismatch.includes('Password changed successfully!'),
  'CONTROL FAILED: mismatch path rendered the success banner'
);
console.log('CONTROL EXPECTED: mismatch rejected with a validation error | ACTUAL: shown');

// CONTROL: too-short new password surfaces the length error, no success.
states[1] = 'short';
states[2] = 'short';
const treeShort = renderComponent(PasswordPage);
await submit(treeShort);
const textShort = collectText(renderComponent(PasswordPage)).join('');
assert.ok(
  textShort.includes('Password must be at least 8 characters'),
  'CONTROL FAILED: short password not rejected'
);
assert.ok(
  !textShort.includes('Password changed successfully!'),
  'CONTROL FAILED: short-password path rendered the success banner'
);
console.log('CONTROL EXPECTED: short password rejected with a validation error | ACTUAL: shown');

// THE PROBLEM: a fully valid submit must not fabricate success. The server has
// no self-service password endpoint, so any success banner is a lie about a
// credential the user believes has changed.
states[0] = 'current-secret';
states[1] = 'newpass123';
states[2] = 'newpass123';
const treeValid = renderComponent(PasswordPage);
await submit(treeValid);
const textValid = collectText(renderComponent(PasswordPage)).join('');

const claimedSuccess = textValid.includes('Password changed successfully!');
console.log(
  'EXPECTED: no success banner for an unpersisted change | ACTUAL: claimed success:',
  claimedSuccess,
  '| server calls:',
  fetchCalls.length,
  '| error shown:',
  textValid.includes('not available')
);
assert.equal(
  claimedSuccess,
  false,
  'PROBLEM CONFIRMED: PasswordPage rendered "Password changed successfully!" with no server call'
);
assert.equal(
  fetchCalls.length,
  0,
  'CONTROL FAILED: the page hit the network unexpectedly (no self-service password endpoint exists)'
);
assert.ok(
  textValid.includes('not available'),
  'PROBLEM CONFIRMED: the unpersisted change is not explained to the user (no success claim, no truth)'
);
console.log('FIX VERIFIED: no fabricated success; the unavailable operation is reported');