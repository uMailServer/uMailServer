// The Profile page once showed a hardcoded 'user@example.com' and a Save
// button that persisted nothing; then a placeholder because no endpoint told
// it who was signed in. It now shows GET /api/v1/account, read-only.
// Run: cd web/account && node --test src/pages/profile.false-identity.test.mjs
import fs from 'node:fs'
import vm from 'node:vm'
import assert from 'node:assert'
import test from 'node:test'
import { createRequire } from 'node:module'

const require_ = createRequire(new URL('../../package.json', import.meta.url))
const ts = require_('typescript')
const js = ts.transpileModule(fs.readFileSync(new URL('./Profile.tsx', import.meta.url), 'utf8'), {
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
function render(fetchImpl, win = { location: { href: '/account/' } }) {
  const states = []; const effects = []; let cursor = 0
  const h = {
    useState(init) { const i = cursor++; if (!(i in states)) states[i] = typeof init === 'function' ? init() : init; return [states[i], (v) => { states[i] = typeof v === 'function' ? v(states[i]) : v }] },
    useEffect(cb) { effects.push(cb) },
  }
  const exports = {}
  const ctx = vm.createContext({
    exports, console, React: { createElement: el }, fetch: fetchImpl, window: win,
    require(name) {
      if (name === 'react') return h
      if (name === 'lucide-react') return new Proxy({}, { get: (_t, p) => String(p) })
      throw new Error('import ' + name)
    },
  })
  vm.runInContext(js, ctx)
  const Page = exports.default
  return {
    win,
    async mount() { cursor = 0; Page(); for (const e of effects.splice(0)) await e(); await new Promise((r) => setImmediate(r)); await new Promise((r) => setImmediate(r)) },
    text() { cursor = 0; return text(Page()).join(' ') },
  }
}
const okJSON = (body) => async (url) => {
  assert.strictEqual(url, '/api/v1/account')
  return { ok: true, status: 200, json: async () => body }
}

test('shows the signed-in account, read-only', async () => {
  const p = render(okJSON({
    email: 'jane@example.org', quota_used: 512 * 1024, quota_limit: 1024 * 1024 * 1024, max_message_size: 25 * 1024 * 1024,
    two_factor_enabled: true, forward_to: 'j@elsewhere.test', created_at: '2026-01-02T03:04:05Z', last_login_at: '0001-01-01T00:00:00Z',
  }))
  await p.mount()
  const out = p.text()
  for (const want of ['jane@example.org', '512 KB of 1.0 GB', '25 MB', 'On', 'To j@elsewhere.test', 'Never']) {
    assert.ok(out.includes(want), `missing ${want}: ${out}`)
  }
  assert.ok(!out.includes('user@example.com'), 'fabricated identity')
  assert.ok(!/Save/.test(out), 'a save flow that persists nothing')
})

test('unlimited quota and no forwarding', async () => {
  const p = render(okJSON({ email: 'a@b.test', quota_used: 10, quota_limit: 0, max_message_size: 0, two_factor_enabled: false, created_at: '2026-01-02T03:04:05Z' }))
  await p.mount()
  const out = p.text()
  assert.ok(out.includes('10 B (no limit)') && out.includes('Off'), out)
})

test('an expired session goes to the login page; a failure says so', async () => {
  const p = render(async () => ({ ok: false, status: 401, json: async () => ({}) }))
  await p.mount()
  assert.strictEqual(p.win.location.href, '/account/login')
  const q = render(async () => ({ ok: false, status: 500, json: async () => ({}) }))
  await q.mount()
  assert.ok(q.text().includes('Could not load your account'), q.text())
})
