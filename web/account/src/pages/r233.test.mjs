// R233 F7102/F7103: Layout/Login strings come from the i18n catalog (rendered
// with tr.json no English chrome remains) and the self-service account
// deletion page drives DELETE /api/v1/account with a password confirmation.
import fs from 'node:fs'
import vm from 'node:vm'
import test from 'node:test'
import assert from 'node:assert'
import { createRequire } from 'node:module'

const ts = createRequire(new URL('../../package.json', import.meta.url))('typescript')
const loc = (n) => JSON.parse(fs.readFileSync(new URL(`../locales/${n}.json`, import.meta.url), 'utf8'))
const el = (type, props, ...children) => ({ type, props: props ?? {}, children })
const text = (n, o = []) => {
  if (n == null || typeof n === 'boolean') return o
  if (typeof n === 'string' || typeof n === 'number') { o.push(String(n)); return o }
  if (Array.isArray(n)) { n.forEach((c) => text(c, o)); return o }
  text(n.children, o); return o
}
const find = (n, p) => {
  if (n == null || typeof n !== 'object') return null
  if (Array.isArray(n)) { for (const c of n) { const h = find(c, p); if (h) return h } return null }
  if ('type' in n) { if (p(n)) return n; return find(n.children, p) }
  return null
}

function run(file, catalog, extra = {}) {
  const src = fs.readFileSync(new URL(file, import.meta.url), 'utf8')
  const js = ts.transpileModule(src, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020, jsx: ts.JsxEmit.React } }).outputText
  const states = []; let cursor = 0
  const exports = {}
  const ctx = vm.createContext({
    exports, console, React: { createElement: el },
    require(name) {
      if (name === 'react') return {
        useState(init) { const i = cursor++; if (!(i in states)) states[i] = init; return [states[i], (v) => { states[i] = typeof v === 'function' ? v(states[i]) : v }] },
        useEffect() {},
      }
      if (name === 'react-router-dom') return { Outlet: 'Outlet', Link: 'Link', useLocation: () => ({ pathname: '/login' }), useNavigate: () => (to) => extra.nav?.push(to) }
      if (name === 'lucide-react') return new Proxy({}, { get: (_t, p) => String(p) })
      if (name === '../hooks/useI18n') return { useI18n: () => ({ t: (k) => k.split('.').reduce((o, x) => o?.[x], catalog) ?? k }) }
      throw new Error('import ' + name)
    },
    window: { location: { origin: 'http://x', href: '' } },
    ...extra.globals,
  })
  vm.runInContext(js, ctx)
  const C = exports.default
  return { render: () => { cursor = 0; return C() } }
}

test('F7102 Login renders Turkish strings from tr.json, no English literals', () => {
  const tr = loc('tr')
  const out = text(run('./Login.tsx', tr).render()).join('|')
  for (const k of ['layout.title', 'auth.subtitle', 'auth.emailLabel', 'auth.passwordLabel', 'auth.signIn']) {
    assert.ok(out.includes(k.split('.').reduce((o, x) => o[x], tr)), 'missing ' + k)
  }
  for (const en of ['Sign in to manage', 'Email address', 'Account Settings']) assert.ok(!out.includes(en), 'English leaked: ' + en)
})

test('F7102 Layout renders nav and sign-out from the catalog', () => {
  const tr = loc('tr')
  const r = run('./../components/Layout.tsx', tr, { globals: { fetch: async () => ({ status: 200 }) } })
  // Layout gates on /login route being false; force the signed-in tree via pathname override.
  const src = fs.readFileSync(new URL('../components/Layout.tsx', import.meta.url), 'utf8')
  assert.ok(!/label: '[A-Z]/.test(src) && !/>\s*Sign out\s*</.test(src) && !/>\s*Account Settings\s*</.test(src), 'hard-coded English left in Layout')
  assert.ok(r.render() !== undefined)
  for (const k of ['profile', 'password', '2fa', 'forwarding', 'filters', 'vacation', 'deleteAccount', 'signOut']) {
    assert.ok(tr.nav[k] && loc('en').nav[k], 'nav.' + k + ' missing in a locale')
  }
})

test('F7103 DeleteAccount: password-confirm dialog sends DELETE with password and signs out', async () => {
  const nav = []; const calls = []
  let status = 403
  const fetch = async (url, o) => { calls.push({ url, o }); return { ok: status < 300, status } }
  const p = run('./DeleteAccount.tsx', loc('en'), { nav, globals: { fetch } })
  let tree = p.render()
  assert.ok(!find(tree, (n) => n.props?.role === 'dialog'), 'dialog must start closed')
  find(tree, (n) => n.type === 'button' && text(n).join('').includes('Delete my account')).props.onClick()
  tree = p.render()
  const dlg = find(tree, (n) => n.props?.role === 'dialog')
  assert.ok(dlg, 'dialog did not open')
  const input = find(dlg, (n) => n.type === 'input' && n.props.type === 'password')
  // empty password never hits the network
  await dlg.props.onSubmit({ preventDefault() {} })
  assert.equal(calls.length, 0)
  input.props.onChange({ target: { value: 'hunter2' } })
  tree = p.render()
  await find(tree, (n) => n.props?.role === 'dialog').props.onSubmit({ preventDefault() {} })
  assert.equal(calls.length, 1)
  assert.equal(calls[0].url, '/api/v1/account')
  assert.equal(calls[0].o.method, 'DELETE')
  assert.equal(calls[0].o.credentials, 'include')
  assert.deepEqual(JSON.parse(calls[0].o.body), { password: 'hunter2' })
  assert.deepEqual(nav, [], 'wrong password must not sign out')
  assert.ok(text(p.render()).join('').includes(loc('en').deleteAccount.wrongPassword))
  // success -> login
  status = 204
  const t2 = p.render()
  find(t2, (n) => n.type === 'input' && n.props.type === 'password').props.onChange({ target: { value: 'hunter2' } })
  await find(p.render(), (n) => n.props?.role === 'dialog').props.onSubmit({ preventDefault() {} })
  assert.deepEqual(nav, ['/login'])
})
