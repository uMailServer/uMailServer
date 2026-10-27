// Round 174: Password page must mirror the server strength policy
// (validatePassword: 8..128, upper, lower, digit, special) before hitting the
// network, and must send an expired session (401) back to /login.
import fs from 'node:fs';
import vm from 'node:vm';
import assert from 'node:assert';
import { createRequire } from 'node:module';

const ts = createRequire(new URL('../../package.json', import.meta.url))('typescript');
const src = fs.readFileSync(new URL('./Password.tsx', import.meta.url), 'utf8');
const js = ts.transpileModule(src, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020, jsx: ts.JsxEmit.React } }).outputText;

const states = [];
const hooks = { useState(init) { const i = states.cursor++; if (!(i in states)) states[i] = init; return [states[i], (v) => { states[i] = v; }]; } };
const el = (type, props, ...children) => ({ type, props: props ?? {}, children });
const find = (n, p) => { if (!n || typeof n !== 'object') return null; if (Array.isArray(n)) { for (const c of n) { const h = find(c, p); if (h) return h; } return null; } if (p(n)) return n; return find(n.children, p); };
const text = (n, o = []) => { if (n == null || typeof n === 'boolean') return o; if (typeof n !== 'object') { o.push(String(n)); return o; } if (Array.isArray(n)) { n.forEach((c) => text(c, o)); return o; } text(n.children, o); return o; };

const calls = [];
let status = 200;
const win = { location: { href: '/password' } };
const ctx = vm.createContext({ exports: {}, require: () => ({ useState: hooks.useState }), React: { createElement: el }, window: win, console,
  fetch: async (u, o) => { calls.push({ u, o }); return { ok: status < 400, status, json: async () => ({ error: 'x' }) }; } });
vm.runInContext(js, ctx);
const Page = ctx.exports.default;
const render = () => { states.cursor = 0; return Page(); };
async function submit(pw) {
  states[0] = 'cur'; states[1] = pw; states[2] = pw;
  await find(render(), (n) => n.type === 'form').props.onSubmit({ preventDefault() {} });
  return text(render()).join('');
}
render();
for (const [pw, frag] of [['alllowercase1!', 'uppercase'], ['ALLUPPER1!', 'lowercase'], ['NoDigits!!', 'digit'], ['NoSpecial12', 'special'], ['Aa1!' + 'x'.repeat(130), '128']]) {
  calls.length = 0;
  const t = await submit(pw);
  assert.ok(t.toLowerCase().includes(frag), `weak password (${frag}) not rejected client-side: ${t}`);
  assert.equal(calls.length, 0, `weak password (${frag}) reached the network`);
}
calls.length = 0; status = 401;
await submit('GoodPass123!');
assert.equal(calls.length, 1);
assert.equal(win.location.href, '/account/login', '401 must redirect to /account/login');
console.log('ok');
