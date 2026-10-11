// Round 174: every TwoFactor fetch must route a 401 to /login.
import fs from 'node:fs';
import assert from 'node:assert';
const src = fs.readFileSync(new URL('./TwoFactor.tsx', import.meta.url), 'utf8');
const fetches = (src.match(/await fetch\(/g) || []).length;
const guards = (src.match(/redirectIfUnauthorized\(res\)\) return/g) || []).length;
assert.equal(fetches, 5);
assert.ok(guards >= fetches, `only ${guards}/${fetches} fetches handle 401`);
// 401 is also the server's answer for a wrong/replayed TOTP code (verify,
// disable): those must surface the message, not bounce to /login.
assert.equal((src.match(/!\/totp code\/i\.test/g) || []).length, 2);
