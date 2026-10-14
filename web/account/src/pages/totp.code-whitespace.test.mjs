// Regression F7600: TOTP codes shown by authenticator apps as "123 456" must reach
// the server as six digits (verify/disable/login). Plain-node spec like its
// siblings; only fetch is stubbed.
import fs from 'node:fs'
import assert from 'node:assert/strict'
import test from 'node:test'
import vm from 'node:vm'
import { createRequire } from 'node:module'
const ts = createRequire(new URL('../../package.json', import.meta.url))('typescript')
function load(file, routes) {
  const src = fs.readFileSync(new URL(`./${file}`, import.meta.url), 'utf8')
  const js = ts.transpileModule(src, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020, jsx: ts.JsxEmit.React } }).outputText
  const st = []; let cur = 0; const effects = []; let eff = false
  const calls = []
  const exp = {}
  const el = (type, props, ...children) => ({ type, props: props ? { ...props, children } : { children } })
  const sb = {
    module: { exports: exp }, exports: exp, console, React: { createElement: el }, window: { location: { href: '/', origin: 'http://x' } },
    require: (n) => n === '../hooks/useI18n' ? { useI18n: () => ({ t: (k) => k }) } : n === 'react' ? {
      useState(i) { const k = cur++; if (!(k in st)) st[k] = i; return [st[k], (v) => { st[k] = typeof v === 'function' ? v(st[k]) : v }] },
      useEffect(cb) { if (!eff) { effects.push(cb); eff = true } },
    } : new Proxy({}, { get: (_t, p) => p }),
    fetch: async (url, o = {}) => {
      const m = o.method ?? 'GET'; calls.push({ url, m, body: o.body })
      const r = routes[`${m} ${url}`]; if (!r) throw new Error('unexpected ' + m + ' ' + url)
      const { status, body } = typeof r === 'function' ? r() : r
      return { ok: status < 300, status, headers: { get: () => null }, json: async () => body }
    },
  }
  vm.createContext(sb); vm.runInContext(js, sb)
  const C = exp.default
  const settle = async () => { while (effects.length) await effects.shift()(); for (let i = 0; i < 5; i++) await new Promise((r) => setImmediate(r)) }
  return { calls, settle, render: () => { cur = 0; return C() }, mount() { cur = 0; eff = false; return C() } }
}
const find = (n, p) => {
  if (n == null || typeof n !== 'object') return null
  if (Array.isArray(n)) { for (const c of n) { const h = find(c, p); if (h) return h } return null }
  if (!n.props) return null
  if (p(n)) return n
  return find(n.props.children, p)
}
const text = (n) => n == null || typeof n === 'boolean' ? '' : typeof n !== 'object' ? String(n) : Array.isArray(n) ? n.map(text).join(' ') : text(n.props?.children)

const URI = 'otpauth://totp/x:u@e.com?secret=GEZDGNBVGY3TQOJQ'
const clip = (n, v) => v.slice(0, n.props.maxLength ?? 1e9) // browser truncates paste at maxLength

async function verifyFlow(entered) {
  const h = load('TwoFactor.tsx', {
    'GET /api/v1/account/totp': { status: 200, body: { enabled: false, pending_setup: false } },
    'POST /api/v1/account/totp/setup': { status: 200, body: { uri: URI } },
    'POST /api/v1/account/totp/verify': { status: 200, body: { enabled: true } },
  })
  h.mount(); await h.settle()
  await find(h.render(), (n) => n.props.onClick && /Enable/.test(text(n))).props.onClick(); await h.settle()
  const input = find(h.render(), (n) => n.props.onChange && /verification/i.test(n.props['aria-label'] || ''))
  input.props.onChange({ target: { value: clip(input, entered) } })
  await find(h.render(), (n) => n.props.onClick && /Verify/.test(text(n))).props.onClick(); await h.settle()
  const c = h.calls.find((x) => x.url.endsWith('/verify'))
  return c ? JSON.parse(c.body).code : null
}
async function disableFlow(entered) {
  const h = load('TwoFactor.tsx', {
    'GET /api/v1/account/totp': { status: 200, body: { enabled: true, pending_setup: false } },
    'POST /api/v1/account/totp/disable': { status: 200, body: { enabled: false } },
  })
  h.mount(); await h.settle()
  const input = find(h.render(), (n) => n.props.onChange && /current/i.test(n.props['aria-label'] || ''))
  input.props.onChange({ target: { value: clip(input, entered) } })
  await find(h.render(), (n) => n.props.onClick && /Disable/.test(text(n))).props.onClick(); await h.settle()
  const c = h.calls.find((x) => x.url.endsWith('/disable'))
  return c ? JSON.parse(c.body).code : null
}
async function loginFlow(entered) {
  const h = load('Login.tsx', {
    'POST http://x/api/v1/auth/login': () => (h.calls.length === 1
      ? { status: 401, body: { error: 'TOTP code required' } }
      : { status: 200, body: {} }),
  })
  const submit = async () => { await find(h.render(), (n) => n.type === 'form').props.onSubmit({ preventDefault() {} }); await h.settle() }
  await submit() // password accepted, TOTP requested
  const input = find(h.render(), (n) => n.props.id === 'totp')
  input.props.onChange({ target: { value: clip(input, entered) } })
  await submit()
  return JSON.parse(h.calls[1].body).totp_code
}

test('verify posts a six-digit code for a grouped/padded paste', async () => {
  assert.equal(await verifyFlow('123456'), '123456')
  assert.equal(await verifyFlow('123 456'), '123456')
  assert.equal(await verifyFlow(' 123456'), '123456')
  assert.equal(await verifyFlow('12345'), '12345')
})
test('disable posts a six-digit code and still blocks blank input', async () => {
  assert.equal(await disableFlow('654 321'), '654321')
  assert.equal(await disableFlow('   '), null)
})
test('login second step posts a six-digit code', async () => {
  assert.equal(await loginFlow('123456'), '123456')
  assert.equal(await loginFlow('123 456'), '123456')
})
