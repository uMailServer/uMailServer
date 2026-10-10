// Regression (F6221): Vacation sent datetime-local values ("2026-10-11T10:00",
// no zone) as start_date/end_date; the Go API parses RFC 3339 and silently
// dropped unparseable dates, so the schedule was never saved.
import fs from 'node:fs';
import vm from 'node:vm';
import assert from 'node:assert';
import test from 'node:test';
import { createRequire } from 'node:module';

const ts = createRequire(import.meta.url)('typescript');
const src = fs.readFileSync(new URL('./Vacation.tsx', import.meta.url), 'utf8');
const js = ts.transpileModule(src, {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020, jsx: ts.JsxEmit.React },
}).outputText;
const ctx = vm.createContext({ exports: {}, require: () => ({}), React: {}, console });
vm.runInContext(js, ctx);
const { toDatetimeLocal, fromDatetimeLocal } = ctx.exports;

test('fromDatetimeLocal yields an RFC 3339 instant the server can parse', () => {
  const out = fromDatetimeLocal('2026-10-11T10:00');
  assert.match(out, /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?Z$/);
  assert.equal(new Date(out).getTime(), new Date('2026-10-11T10:00').getTime());
});

test('round trip preserves the local wall-clock value', () => {
  assert.equal(toDatetimeLocal(fromDatetimeLocal('2026-10-11T10:00')), '2026-10-11T10:00');
});

test('empty and invalid values are handled', () => {
  assert.equal(fromDatetimeLocal(''), undefined);
  assert.equal(toDatetimeLocal(undefined), '');
  assert.equal(toDatetimeLocal('garbage'), '');
});
