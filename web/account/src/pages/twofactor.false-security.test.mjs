// Regression: account-portal TwoFactorPage must never fabricate a security
// posture it did not establish.
// Root cause (fixed 2026-10-07 round 5): the page ran a purely local setup
// flow — a placeholder QR, a fake secret, an "Enable 2FA" button that only
// flipped useState — then rendered "Two-factor authentication is enabled"
// and displayed/copied EIGHT HARDCODED backup codes ('1234 5678 9012' ...)
// as the user's real recovery codes. Zero network calls. The server's 2FA
// sub-paths (/api/v1/accounts/{email}/totp/{setup,verify,disable}) are
// admin-gated (handleAccountDetail sits behind adminMiddleware — server.go
// "Accounts (admin only)"), so the portal cannot manage 2FA at all and must
// say so instead of fabricating status or credentials.
// Convention: plain-node spec, transpiles the real component (no vitest in
// this package). The page performs no network or timer work, so none is
// stubbed beyond the module boundary.
import fs from 'node:fs';
import vm from 'node:vm';
import assert from 'node:assert';
import { createRequire } from 'node:module';

const require_ = createRequire(new URL('../../package.json', import.meta.url));
const ts = require_('typescript');

const sourceURL = new URL('./TwoFactor.tsx', import.meta.url);
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
function renderComponent(TwoFactorPage) {
  states.cursor = 0;
  return TwoFactorPage();
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
    if (name === 'lucide-react') {
      return { Shield: 'lucide-Shield', Smartphone: 'lucide-Smartphone', Copy: 'lucide-Copy', Check: 'lucide-Check' };
    }
    throw new Error('Unexpected import: ' + name);
  },
  React: { createElement: el },
  fetch: fetchStub,
  navigator: { clipboard: { writeText: () => {} } },
  setTimeout,
  console,
});
vm.runInContext(js, contextified, { filename: sourceURL.pathname });

const TwoFactorPage = contextified.exports.default;
assert.equal(typeof TwoFactorPage, 'function', 'CONTROL FAILED: TwoFactorPage not loaded');

// CONTROL: the page renders its heading.
const tree = renderComponent(TwoFactorPage);
const text = collectText(tree).join('');
assert.ok(text.includes('Two-Factor Authentication'), 'CONTROL FAILED: heading missing');
console.log('CONTROL EXPECTED: Two-Factor Authentication page renders | ACTUAL: shown');

// CONTRACT: no fabricated enabled-status, no fabricated recovery codes, and
// the portal's limitation is stated — the server's 2FA sub-paths are
// admin-gated, so this page can neither know nor change the real status.
const claimedEnabled = text.includes('Two-factor authentication is enabled');
const showedCodes = ['1234 5678 9012', '3456 7890 1234'].some((code) => text.includes(code));
console.log(
  'EXPECTED: no enabled claim, no hardcoded codes, limitation explained | ACTUAL: claimed enabled:',
  claimedEnabled,
  '| hardcoded codes shown:',
  showedCodes,
  '| server calls:',
  fetchCalls.length
);
assert.equal(
  claimedEnabled,
  false,
  'PROBLEM CONFIRMED: TwoFactorPage rendered "Two-factor authentication is enabled" with no server call'
);
assert.equal(
  showedCodes,
  false,
  "PROBLEM CONFIRMED: TwoFactorPage displayed hardcoded placeholder codes as the user's real backup codes"
);
assert.equal(
  fetchCalls.length,
  0,
  'CONTROL FAILED: the page hit the network unexpectedly (the 2FA API is admin-gated)'
);
assert.ok(
  text.includes('administrator'),
  'PROBLEM CONFIRMED: the unavailability of portal 2FA management is not explained to the user'
);
console.log('FIX VERIFIED: no fabricated 2FA status or recovery codes; the limitation is stated');