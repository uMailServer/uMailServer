// Regression: account-portal TwoFactorPage must drive the real self-service
// TOTP endpoints and never fabricate security state. History: rounds 4-5
// replaced a fully fabricated flow (placeholder QR, fake secret, eight
// hardcoded "backup codes", local-only Enable/Disable) with an honest stub;
// round 16 wires the page to POST /api/v1/account/totp/{setup,verify,disable}
// and GET /api/v1/account/totp (status). Pinned contract:
//   - the page loads its status from the server (credentials included);
//   - enabling calls setup and renders the SERVER's otpauth URI + secret;
//   - verification posts the entered code and only then reports enabled;
//   - disabling posts to the disable endpoint and returns to off;
//   - the page NEVER renders hardcoded backup codes (the server provides
//     none — fabricating them is the original defect).
// The fetch stub dispatches by endpoint+method (NOT FIFO): async handler
// continuations can flush late and out of order, so responses must be
// keyed per endpoint to stay deterministic across sections.
// Run: cd web/account && node --test src/pages/twofactor.false-security.test.mjs

import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, join } from 'node:path'
import vm from 'node:vm'
import assert from 'node:assert'
import { createRequire } from 'node:module'

const here = dirname(fileURLToPath(import.meta.url))
const require = createRequire(join(here, '..', '..', 'package.json'))
const ts = require('typescript')

let source = readFileSync(join(here, 'TwoFactor.tsx'), 'utf8')

// --- fetch stub: endpoint+method keyed, recording, once-per-entry ----------
const fetchCalls = []
const queued = [] // { url, method, status, body }
function queueFetch(url, method, status, body) {
  queued.push({ url, method, status, body })
}
async function fetchStub(url, options = {}) {
  const method = options.method ?? 'GET'
  fetchCalls.push({ url, method, options })
  const idx = queued.findIndex((q) => q.url === url && q.method === method)
  if (idx === -1) throw new Error('unexpected fetch: ' + method + ' ' + url)
  const next = queued.splice(idx, 1)[0]
  const body = next.body === undefined ? '{}' : JSON.stringify(next.body)
  return {
    ok: next.status >= 200 && next.status < 300,
    status: next.status,
    json: async () => JSON.parse(body),
  }
}

// --- hooks shim ------------------------------------------------------------
const hooksState = []
let hooksIndex = 0
const effectCallbacks = []
function useStateRecorder(initial) {
  const i = hooksIndex++
  hooksState[i] = hooksState[i] === undefined ? initial : hooksState[i]
  const setState = (v) => {
    hooksState[i] = typeof v === 'function' ? v(hooksState[i]) : v
  }
  return [hooksState[i], setState]
}
let effectRecorded = false
function useEffectRecorder(cb) {
  // Mount-once semantics (deps []): record on the first render of a fresh
  // mount only — re-renders re-register the effect in React but it does not
  // re-run, so duplicates here would re-fire the status fetch.
  if (!effectRecorded) {
    effectCallbacks.push(cb)
    effectRecorded = true
  }
}

// --- transpile + run --------------------------------------------------------
const js = ts.transpileModule(source, {
  compilerOptions: { module: ts.ModuleKind.CommonJS, jsx: ts.JsxEmit.React, target: ts.ScriptTarget.ES2020 },
}).outputText

const el = (type, props, ...children) => ({ type, props: props ? { ...props, children } : { children } })
const moduleExports = {}
const sandbox = {
  require: (name) => {
    if (name === 'react') return { useState: useStateRecorder, useEffect: useEffectRecorder }
    if (name === 'lucide-react') return new Proxy({}, { get: (_t, prop) => prop })
    throw new Error('Unexpected import: ' + name)
  },
  fetch: fetchStub,
  React: { createElement: el },
  module: { exports: moduleExports },
  exports: moduleExports,
  console,
}
vm.createContext(sandbox)
vm.runInContext(js, sandbox)
const TwoFactorPage = moduleExports.default
assert.strictEqual(typeof TwoFactorPage, 'function', 'CONTROL FAILED: TwoFactorPage must default-export a function')

function render() {
  hooksIndex = 0
  effectRecorded = false
  return TwoFactorPage()
}

function rerender() {
  hooksIndex = 0
  return TwoFactorPage()
}

function collectText(node, out = []) {
  if (node === null || node === undefined || node === false) return out
  if (typeof node === 'string' || typeof node === 'number') {
    out.push(String(node))
    return out
  }
  if (Array.isArray(node)) {
    node.forEach((child) => collectText(child, out))
    return out
  }
  if (typeof node === 'object' && node.props) {
    collectText(node.props.children, out)
  }
  return out
}

function textOf(node) {
  return collectText(node).join(' ')
}

function findNode(node, predicate) {
  if (node === null || node === undefined || node === false) return null
  if (Array.isArray(node)) {
    for (const child of node) {
      const hit = findNode(child, predicate)
      if (hit) return hit
    }
    return null
  }
  if (typeof node === 'object' && node.props) {
    if (predicate(node)) return node
    return findNode(node.props.children, predicate)
  }
  return null
}

const SETUP_URI = 'otpauth://totp/uMailServer:user@example.com?secret=GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ&issuer=uMailServer'

// Effects return cleanups, not promises — after running each effect, settle
// the async work it started by yielding to the macrotask queue a few times.
async function runEffects() {
  while (effectCallbacks.length > 0) {
    const cb = effectCallbacks.shift()
    await cb()
  }
  let guard = 0
  while (guard++ < 4) {
    await new Promise((r) => setImmediate(r))
  }
}

// --- 1. mount loads status from the server ---------------------------------
queueFetch('/api/v1/account/totp', 'GET', 200, { enabled: false, pending_setup: false })
render()
await runEffects()
const statusCall = fetchCalls.find((c) => c.url === '/api/v1/account/totp' && c.method === 'GET')
assert.ok(statusCall, 'PROBLEM CONFIRMED: the page never loaded its TOTP status from the server')
assert.strictEqual(statusCall.options.credentials, 'include', 'status request must include credentials')
console.log('CONTROL EXPECTED: mount loads TOTP status with credentials | ACTUAL: ok')

// --- 2. enable flow: setup -> URI + secret rendered, never fake codes ------
queueFetch('/api/v1/account/totp/setup', 'POST', 200, { uri: SETUP_URI })
const enableBtn = findNode(rerender(), (n) => n.props && typeof n.props.onClick === 'function' && textOf(n).includes('Enable'))
assert.ok(enableBtn, 'CONTROL FAILED: no Enable control rendered while TOTP is off')
await enableBtn.props.onClick()
await runEffects()
const setupCall = fetchCalls.find((c) => c.url === '/api/v1/account/totp/setup' && c.method === 'POST')
assert.ok(setupCall, 'PROBLEM CONFIRMED: Enable did not call the setup endpoint')
assert.strictEqual(setupCall.options.credentials, 'include', 'setup request must include credentials')
const text2 = textOf(rerender())
assert.ok(text2.includes(SETUP_URI) || text2.includes('GEZDGNBVGY3TQOJQ'), 'PROBLEM CONFIRMED: the server-provided otpauth URI/secret is not rendered for the user')
assert.ok(!/1234|5678|9012/.test(text2.replace(SETUP_URI, '')), 'PROBLEM CONFIRMED: hardcoded backup-code patterns are rendered (the server provides none)')
console.log('CONTROL EXPECTED: enable renders the server secret/URI, no fabricated codes | ACTUAL: ok')

// --- 3. verify: posts the entered code, then reports enabled ----------------
queueFetch('/api/v1/account/totp/verify', 'POST', 200, { enabled: true })
queueFetch('/api/v1/account/totp', 'GET', 200, { enabled: true, pending_setup: false })
const codeInput = findNode(rerender(), (n) => n.props && n.props.value !== undefined && String(n.props.value).length <= 8 && n.props.onChange)
assert.ok(codeInput, 'CONTROL FAILED: no verification-code input rendered')
codeInput.props.onChange({ target: { value: '123456' } })
const verifyBtn = findNode(rerender(), (n) => n.props && typeof n.props.onClick === 'function' && /verify|confirm/i.test(textOf(n)))
assert.ok(verifyBtn, 'CONTROL FAILED: no verify control rendered')
await verifyBtn.props.onClick()
await runEffects()
const verifyCall = fetchCalls.find((c) => c.url === '/api/v1/account/totp/verify' && c.method === 'POST')
assert.ok(verifyCall, 'PROBLEM CONFIRMED: the entered code was never posted to the verify endpoint')
const sentBody = JSON.parse(verifyCall.options.body)
assert.strictEqual(sentBody.code, '123456', 'the verify request must carry the entered code')
assert.ok(textOf(rerender()).match(/enabled/i), 'after a successful verify the page must report TOTP as enabled')
console.log('EXPECTED: verification posts the code and only then reports enabled | ACTUAL: ok')

// --- 4. disable flow --------------------------------------------------------
queueFetch('/api/v1/account/totp/disable', 'POST', 200, { enabled: false })
queueFetch('/api/v1/account/totp', 'GET', 200, { enabled: false, pending_setup: false })
const disableBtn = findNode(rerender(), (n) => n.props && typeof n.props.onClick === 'function' && /disable/i.test(textOf(n)))
assert.ok(disableBtn, 'CONTROL FAILED: no Disable control rendered while TOTP is enabled')
await disableBtn.props.onClick()
await runEffects()
const disableCall = fetchCalls.find((c) => c.url === '/api/v1/account/totp/disable' && c.method === 'POST')
assert.ok(disableCall, 'PROBLEM CONFIRMED: Disable did not call the disable endpoint')
assert.strictEqual(disableCall.options.credentials, 'include', 'disable request must include credentials')
console.log('CONTROL EXPECTED: disable posts to the disable endpoint | ACTUAL: ok')

// --- 5. pending setup is continued; server rejection surfaces the error -----
queueFetch('/api/v1/account/totp', 'GET', 200, { enabled: false, pending_setup: true })
queueFetch('/api/v1/account/totp/setup', 'POST', 200, { uri: SETUP_URI })
queueFetch('/api/v1/account/totp/verify', 'POST', 401, { error: 'invalid TOTP code' })
render()
await runEffects()
const setupCall2 = fetchCalls.find((c) => c.url === '/api/v1/account/totp/setup' && c.method === 'POST' && c !== setupCall)
assert.ok(setupCall2, 'CONTROL FAILED: a pending setup must be continued automatically')
const codeInput2 = findNode(rerender(), (n) => n.props && n.props.value !== undefined && String(n.props.value).length <= 8 && n.props.onChange)
assert.ok(codeInput2, 'CONTROL FAILED: no code input after setup')
codeInput2.props.onChange({ target: { value: '000000' } })
const verify2 = findNode(rerender(), (n) => n.props && typeof n.props.onClick === 'function' && /verify|confirm/i.test(textOf(n)))
await verify2.props.onClick()
await runEffects()
const text5 = textOf(rerender())
assert.ok(text5.includes('invalid TOTP code'), 'PROBLEM CONFIRMED: the server rejection message was not surfaced')
assert.ok(!/is enabled/i.test(text5), 'PROBLEM CONFIRMED: the page reports enabled after a rejected code')
console.log('CONTROL EXPECTED: rejection surfaces the server error, no false enabled | ACTUAL: ok')

console.log('FIX VERIFIED: TwoFactorPage drives the real self-service TOTP endpoints and fabricates nothing')
