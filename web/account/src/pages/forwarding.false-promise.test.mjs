// Regression: account-portal ForwardingPage must drive the real self-service
// forwarding endpoint and never fabricate mail-routing state. History:
// round 6 replaced a fictional flow (an "Important" banner describing
// routing behavior the page could not configure + a Save button that only
// awaited a placeholder timer, zero feedback, zero persistence) with an
// honest stub; round 17 wires the page to GET/PUT /api/v1/account/forwarding.
// Pinned contract:
//   - the page loads its forwarding state from the server (credentials
//     included) and populates the form from the server's values;
//   - saving posts the entered address + keep-copy flag and only then
//     reports success, applying the SERVER's response as truth;
//   - a server rejection surfaces the error without a false success;
//   - clearing the address saves an empty forward_to (forwarding off).
// The fetch stub dispatches by endpoint+method (NOT FIFO): async handler
// continuations can flush late and out of order, so responses must be
// keyed per endpoint to stay deterministic across sections.
// Run: cd web/account && node --test src/pages/forwarding.false-promise.test.mjs

import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, join } from 'node:path'
import vm from 'node:vm'
import assert from 'node:assert'
import { createRequire } from 'node:module'

const here = dirname(fileURLToPath(import.meta.url))
const require = createRequire(join(here, '..', '..', 'package.json'))
const ts = require('typescript')

const source = readFileSync(join(here, 'Forwarding.tsx'), 'utf8')

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
let effectRecorded = false
function useStateRecorder(initial) {
  const i = hooksIndex++
  hooksState[i] = hooksState[i] === undefined ? initial : hooksState[i]
  const setState = (v) => {
    hooksState[i] = typeof v === 'function' ? v(hooksState[i]) : v
  }
  return [hooksState[i], setState]
}
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
const ForwardingPage = moduleExports.default
assert.strictEqual(typeof ForwardingPage, 'function', 'CONTROL FAILED: ForwardingPage must default-export a function')

function render() {
  hooksIndex = 0
  effectRecorded = false
  return ForwardingPage()
}

function rerender() {
  hooksIndex = 0
  return ForwardingPage()
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

function findInput(node) {
  return findNode(node, (n) => n.props && n.props.value !== undefined && n.props.onChange)
}

// --- 1. mount loads forwarding state from the server -----------------------
queueFetch('/api/v1/account/forwarding', 'GET', 200, { forward_to: '', keep_copy: false, forwarding_on: false })
render()
await runEffects()
const statusCall = fetchCalls.find((c) => c.url === '/api/v1/account/forwarding' && c.method === 'GET')
assert.ok(statusCall, 'PROBLEM CONFIRMED: the page never loaded its forwarding state from the server')
assert.strictEqual(statusCall.options.credentials, 'include', 'status request must include credentials')
const input1 = findInput(rerender())
assert.ok(input1, 'CONTROL FAILED: no address input rendered')
console.log('CONTROL EXPECTED: mount loads forwarding state with credentials | ACTUAL: ok')

// --- 2. save posts address + keep_copy and applies the server's truth ------
queueFetch('/api/v1/account/forwarding', 'PUT', 200, { forward_to: 'backup@example.com', keep_copy: true, forwarding_on: true })
input1.props.onChange({ target: { value: 'backup@example.com' } })
const tree2 = rerender()
const keepCopyBox = findNode(tree2, (n) => n.props && typeof n.props.checked === 'boolean' && n.props.onChange)
assert.ok(keepCopyBox, 'CONTROL FAILED: no keep-copy checkbox rendered')
keepCopyBox.props.onChange({ target: { checked: true } })
const saveBtn = findNode(rerender(), (n) => n.props && typeof n.props.onClick === 'function' && /save/i.test(textOf(n)))
assert.ok(saveBtn, 'CONTROL FAILED: no Save control rendered')
await saveBtn.props.onClick()
await runEffects()
const putCall = fetchCalls.find((c) => c.url === '/api/v1/account/forwarding' && c.method === 'PUT')
assert.ok(putCall, 'PROBLEM CONFIRMED: Save did not call the forwarding endpoint')
assert.strictEqual(putCall.options.credentials, 'include', 'save request must include credentials')
const sentBody = JSON.parse(putCall.options.body)
assert.strictEqual(sentBody.forward_to, 'backup@example.com', 'the save request must carry the entered address')
assert.strictEqual(sentBody.keep_copy, true, 'the save request must carry the keep-copy flag')
const text2 = textOf(rerender())
assert.ok(/saved|success/i.test(text2), 'after a successful save the page must confirm it')
assert.ok(!/is managed by your administrator/i.test(text2), 'PROBLEM CONFIRMED: the honest-stub text survives on the wired page')
console.log('EXPECTED: save posts address + keep_copy and confirms | ACTUAL: ok')

// --- 3. server rejection surfaces the error, no false success ---------------
fetchCalls.length = 0
queueFetch('/api/v1/account/forwarding', 'GET', 200, { forward_to: '', keep_copy: false, forwarding_on: false })
queueFetch('/api/v1/account/forwarding', 'PUT', 400, { error: 'forward_to must be a valid email address' })
await runEffects()
const input3 = findInput(rerender())
assert.ok(input3, 'CONTROL FAILED: no address input rendered after status load')
input3.props.onChange({ target: { value: 'not-an-email' } })
const save3 = findNode(rerender(), (n) => n.props && typeof n.props.onClick === 'function' && /save/i.test(textOf(n)))
await save3.props.onClick()
await runEffects()
const text3 = textOf(rerender())
assert.ok(text3.includes('forward_to must be a valid email address'), 'PROBLEM CONFIRMED: the server rejection message was not surfaced')
assert.ok(!/saved|success/i.test(text3), 'PROBLEM CONFIRMED: the page reports success after a rejected save')
console.log('CONTROL EXPECTED: rejection surfaces the server error, no false success | ACTUAL: ok')

// --- 4. clearing the address saves forwarding off ---------------------------
fetchCalls.length = 0
queueFetch('/api/v1/account/forwarding', 'GET', 200, { forward_to: 'backup@example.com', keep_copy: true, forwarding_on: true })
queueFetch('/api/v1/account/forwarding', 'PUT', 200, { forward_to: '', keep_copy: false, forwarding_on: false })
await runEffects()
const input4 = findInput(rerender())
assert.ok(input4, 'CONTROL FAILED: no address input rendered after status load')
input4.props.onChange({ target: { value: '' } })
const save4 = findNode(rerender(), (n) => n.props && typeof n.props.onClick === 'function' && /save/i.test(textOf(n)))
await save4.props.onClick()
await runEffects()
const put4 = fetchCalls.find((c) => c.url === '/api/v1/account/forwarding' && c.method === 'PUT')
assert.ok(put4, 'CONTROL FAILED: clearing did not call the forwarding endpoint')
const body4 = JSON.parse(put4.options.body)
assert.strictEqual(body4.forward_to, '', 'clearing must save an empty forward_to')
console.log('CONTROL EXPECTED: clearing saves forwarding off | ACTUAL: ok')

console.log('FIX VERIFIED: ForwardingPage drives the real self-service forwarding endpoint and fabricates nothing')
