// Regression (F6220): tr.json lacked 42 keys present in en.json, so the
// Vacation and Filters pages rendered raw keys ("vacation.enableVacation")
// for Turkish users because t() returns the key when a message is missing.
import fs from 'node:fs';
import assert from 'node:assert';
import test from 'node:test';

const load = (name) => JSON.parse(fs.readFileSync(new URL(name, import.meta.url), 'utf8'));
const flat = (o, p = '') =>
  Object.entries(o).flatMap(([k, v]) => (v && typeof v === 'object' ? flat(v, p + k + '.') : [p + k]));

test('tr.json defines every key en.json defines', () => {
  const tr = new Set(flat(load('./tr.json')));
  const missing = flat(load('./en.json')).filter((k) => !tr.has(k));
  assert.deepEqual(missing, []);
});
