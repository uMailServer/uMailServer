// F6940/F6941/F6942: account portal. (1) Vacation and Filters must send an
// expired session (401) to /login; (2) Layout is an auth gate: an
// unauthenticated visit is redirected to /login, and /login renders without
// the signed-in nav.
// Run: cd web/account && node --test src/pages/r217.session.test.mjs
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
  text(n.children, out)
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
    useState(init) { const i = cursor++; if (!(i in states)) states[i] = typeof init === 'function' ? init() : init; return [states[i], (v) => { states[i] = typeof v === 'function' ? v(states[i]) : v }] },
    useEffect(cb) { if (!mounted) effects.push(cb) },
    useCallback: (f) => f,
    async mount() { mounted = true; for (const e of effects) await e(); await new Promise((r) => setImmediate(r)); await new Promise((r) => setImmediate(r)) },
  }
}
function run(file, extra, mods = {}) {
  const h = shim()
  const exports = {}
  const ctx = vm.createContext({
    exports, console, React: { createElement: el },
    require(name) {
      if (name === 'react') return { useState: h.useState, useEffect: h.useEffect, useCallback: h.useCallback }
      if (name === '../hooks/useI18n') return { useI18n: () => ({ t: (k, p = {}) => String(k.split('.').reduce((o, x) => o?.[x], EN) ?? k).replace(/\{\{(\w+)\}\}/g, (m, q) => p[q] ?? m) }) };
      if (name === 'lucide-react') return new Proxy({}, { get: (_t, p) => String(p) })
      if (name === '../hooks/useI18n') return { useI18n: () => ({ t: (k) => k, loading: false }) }
      if (name in mods) return mods[name]
      throw new Error('import ' + name)
    },
    ...extra,
  })
  vm.runInContext(load(file), ctx)
  return { h, Page: exports.default }
}
const unauth = async () => ({ ok: false, status: 401, json: async () => ({ error: 'unauthorized' }) })

test('F6940 vacation load 401 redirects to /login', async () => {
  const win = { location: { href: '/vacation' } }
  const { h, Page } = run('./Vacation.tsx', { window: win, fetch: unauth })
  h.reset(); Page(); await h.mount()
  assert.strictEqual(win.location.href, '/account/login', 'PROBLEM CONFIRMED: expired session not redirected')
})

test('F6940 vacation save 401 redirects to /login', async () => {
  const win = { location: { href: '/vacation' } }
  const { h, Page } = run('./Vacation.tsx', {
    window: win,
    fetch: async (_u, o) => (o?.method === 'PUT' ? unauth() : { ok: true, status: 200, json: async () => ({}) }),
  })
  h.reset(); Page(); await h.mount()
  h.reset(); const t = Page()
  await find(t, (n) => n.type === 'form').props.onSubmit({ preventDefault() {} })
  assert.strictEqual(win.location.href, '/account/login', 'PROBLEM CONFIRMED: save 401 not redirected')
})

test('F6941 filters load 401 redirects to /login', async () => {
  const win = { location: { href: '/filters' } }
  const { h, Page } = run('./Filters.tsx', { window: win, fetch: unauth })
  h.reset(); Page(); await h.mount()
  assert.strictEqual(win.location.href, '/account/login', 'PROBLEM CONFIRMED: expired session not redirected')
})

function layout(pathname, fetchImpl) {
  const nav = []
  const r = run('../components/Layout.tsx', { fetch: fetchImpl, window: { location: { href: '' } } }, {
    'react-router-dom': { Outlet: 'Outlet', Link: 'Link', useLocation: () => ({ pathname }), useNavigate: () => (to) => nav.push(to) },
  })
  return { ...r, nav }
}

test('F6942 layout redirects unauthenticated visit to /login', async () => {
  const { h, Page, nav } = layout('/profile', unauth)
  h.reset(); Page(); await h.mount()
  assert.deepStrictEqual(nav, ['/login'], 'PROBLEM CONFIRMED: no auth gate')
})

test('F6942 layout does not render protected content before the session is verified', async () => {
  const { h, Page } = layout('/profile', unauth)
  h.reset(); const t = Page()
  assert.ok(!find(t, (n) => n.type === 'Outlet'), 'PROBLEM CONFIRMED: protected outlet rendered pre-check')
})

test('F6942 /login renders without nav or sign-out', async () => {
  const { h, Page, nav } = layout('/login', unauth)
  h.reset(); const t = Page(); await h.mount(); h.reset()
  const out = text(Page()).join('') + text(t).join('')
  assert.ok(find(Page(), (n) => n.type === 'Outlet') || find(t, (n) => n.type === 'Outlet'), 'login outlet must render')
  assert.ok(!out.includes('Sign out') && !out.includes('Profile'), 'PROBLEM CONFIRMED: nav on login: ' + out)
  assert.deepStrictEqual(nav, [], 'no redirect loop on /login')
})

test('F6942 authenticated user sees chrome and outlet', async () => {
  const { h, Page, nav } = layout('/profile', async () => ({ ok: true, status: 200, json: async () => ({}) }))
  h.reset(); Page(); await h.mount(); h.reset()
  const t = Page()
  assert.ok(find(t, (n) => n.type === 'Outlet'))
  assert.ok(text(t).join('').includes('Sign out'))
  assert.deepStrictEqual(nav, [])
})

test('F6942 network error on probe does not lock the user out', async () => {
  const { h, Page, nav } = layout('/profile', async () => { throw new Error('down') })
  h.reset(); Page(); await h.mount(); h.reset()
  assert.ok(find(Page(), (n) => n.type === 'Outlet'))
  assert.deepStrictEqual(nav, [])
})
