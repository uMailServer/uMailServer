// F6866/F6867: account portal. (1) a 429 login must surface Retry-After;
// (2) the forwarding page must send an expired session (401) to /login like
// the 2FA and password pages do, not show "Could not load ... reload".
// Run: cd web/account && node --test src/pages/r209.session.test.mjs
import fs from 'node:fs'
import vm from 'node:vm'
import assert from 'node:assert'
import test from 'node:test'
import { createRequire } from 'node:module'
const EN = JSON.parse(fs.readFileSync(new URL('../locales/en.json', import.meta.url), 'utf8'));


const require_ = createRequire(new URL('../../package.json', import.meta.url))
const ts = require_('typescript')
const load = (name) => ts.transpileModule(fs.readFileSync(new URL(name, import.meta.url), 'utf8'), {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020, jsx: ts.JsxEmit.React },
}).outputText
const el = (type, props, ...children) => ({ type, props: props ?? {}, children })
function text(n, out = []) {
  if (n == null || n === false || n === true) return out
  if (typeof n === 'string' || typeof n === 'number') { out.push(String(n)); return out }
  if (Array.isArray(n)) { n.forEach((c) => text(c, out)); return out }
  text(n.children, out); if (n.props?.children) text(n.props.children, out)
  return out
}
function find(n, p) {
  if (n == null || typeof n !== 'object') return null
  if (Array.isArray(n)) { for (const c of n) { const h = find(c, p); if (h) return h } return null }
  if (p(n)) return n
  return find(n.children, p)
}
function shim() {
  const states = []; const effects = []; let cursor = 0; let mounted = false
  return {
    reset() { cursor = 0 },
    useState(init) { const i = cursor++; if (!(i in states)) states[i] = init; return [states[i], (v) => { states[i] = typeof v === 'function' ? v(states[i]) : v }] },
    useEffect(cb) { if (!mounted) effects.push(cb); },
    async mount() { mounted = true; for (const e of effects) await e(); await new Promise((r) => setImmediate(r)); await new Promise((r) => setImmediate(r)) },
  }
}
function run(file, extra) {
  const h = shim()
  const exports = {}
  const ctx = vm.createContext({
    exports, console, React: { createElement: el },
    require(name) {
      if (name === 'react') return { useState: h.useState, useEffect: h.useEffect }
      if (name === '../hooks/useI18n') return { useI18n: () => ({ t: (k, p = {}) => String(k.split('.').reduce((o, x) => o?.[x], EN) ?? k).replace(/\{\{(\w+)\}\}/g, (m, q) => p[q] ?? m) }) };
      if (name === 'lucide-react') return new Proxy({}, { get: (_t, p) => String(p) })
      throw new Error('import ' + name)
    },
    ...extra,
  })
  vm.runInContext(load(file), ctx)
  return { h, Page: exports.default }
}

test('F6866 login 429 shows Retry-After seconds', async () => {
  const { h, Page } = run('./Login.tsx', {
    window: { location: { origin: 'http://x', href: '' } },
    fetch: async () => ({ ok: false, status: 429, headers: { get: (k) => (k.toLowerCase() === 'retry-after' ? '90' : null) }, json: async () => ({ error: 'too many login attempts' }) }),
  })
  h.reset(); const t1 = Page()
  await find(t1, (n) => n.type === 'form').props.onSubmit({ preventDefault() {} })
  h.reset(); const out = text(Page()).join('')
  assert.ok(out.includes('too many login attempts'), 'control: server message')
  assert.ok(out.includes('90'), 'PROBLEM CONFIRMED: Retry-After ignored: ' + out)
})

test('F6867 forwarding 401 redirects to /login', async () => {
  const win = { location: { href: '/forwarding' } }
  const { h, Page } = run('./Forwarding.tsx', {
    window: win,
    fetch: async () => ({ ok: false, status: 401, json: async () => ({ error: 'unauthorized' }) }),
  })
  h.reset(); Page(); await h.mount()
  assert.strictEqual(win.location.href, '/login', 'PROBLEM CONFIRMED: expired session not redirected')
})
