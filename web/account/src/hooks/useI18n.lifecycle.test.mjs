import fs from 'node:fs';
import vm from 'node:vm';
import { createRequire } from 'node:module';

function deferred() {
  let resolve;
  let reject;
  const promise = new Promise((onResolve, onReject) => {
    resolve = onResolve;
    reject = onReject;
  });
  return { promise, resolve, reject };
}

function createHarness(loaders) {
  const sourceURL = new URL('./useI18n.ts', import.meta.url);
  const require = createRequire(new URL('../../package.json', sourceURL));
  const ts = require('typescript');
  const source = fs.readFileSync(sourceURL, 'utf8');
  const js = ts.transpileModule(source, {
    compilerOptions: {
      module: ts.ModuleKind.CommonJS,
      target: ts.ScriptTarget.ES2020,
    },
  }).outputText;
  const states = [];
  let cursor = 0;
  let lastDeps;
  let cleanup;
  let writes = 0;
  // Render/effect cleanup follows React's lifecycle; loaders are channel gates.
  const hooks = {
    useState(init) {
      const index = cursor++;
      if (!(index in states)) {
        states[index] = typeof init === 'function' ? init() : init;
      }
      return [states[index], value => {
        states[index] = value;
        writes++;
      }];
    },
    useCallback(fn) { return fn; },
    useEffect(fn, deps) {
      if (!lastDeps || deps.some((value, index) => value !== lastDeps[index])) {
        cleanup?.();
        lastDeps = [...deps];
        cleanup = fn();
      }
    },
  };
  const context = vm.createContext({
    exports: {},
    require(name) {
      if (name === 'react') return hooks;
      throw new Error('Unexpected import: ' + name);
    },
    localStorage: { getItem: () => 'en', setItem() {} },
    navigator: { language: 'en-US' },
    document: { documentElement: { lang: 'en' } },
    console: { error() {} },
    __loaders: loaders,
  });
  new vm.Script(js).runInContext(context);
  new vm.Script('translations.en=__loaders.en;translations.tr=__loaders.tr;')
    .runInContext(context);
  return {
    render() { cursor = 0; return context.exports.useI18n(); },
    unmount() { cleanup?.(); },
    get writes() { return writes; },
  };
}

async function settled(gate) {
  await gate.promise;
  await Promise.resolve();
}

import assert from 'node:assert/strict';
const controlGate = deferred();
const control = createHarness({ en: () => controlGate.promise, tr: () => Promise.resolve({ default: { greeting: 'TR' } }) });
control.render();
controlGate.resolve({ default: { greeting: 'EN' } });
await settled(controlGate);
assert.equal(control.render().t('greeting'), 'EN');
console.log('CONTROL EXPECTED: EN after ordinary load ACTUAL:', control.render().t('greeting'));
const old = deferred(), current = deferred();
const hook = createHarness({ en: () => old.promise, tr: () => current.promise });
hook.render().changeLocale('tr');
hook.render();
current.resolve({ default: { greeting: 'TR' } });
await settled(current);
assert.equal(hook.render().t('greeting'), 'TR');
old.resolve({ default: { greeting: 'EN' } });
await settled(old);
const actual = hook.render().t('greeting');
console.log('EXPECTED: TR after stale EN completes ACTUAL:', actual);
if (actual !== 'TR') {
  console.log('PROBLEM CONFIRMED');
  process.exit(1);
}
console.log('PROBLEM NOT REPRODUCED');
const removedGate = deferred();
const removed = createHarness({ en: () => removedGate.promise, tr: () => Promise.resolve({ default: { greeting: 'TR' } }) });
removed.render();
removed.unmount();
const before = removed.writes;
removedGate.resolve({ default: { greeting: 'old' } });
await settled(removedGate);
assert.equal(removed.writes, before);
console.log('EDGE EXPECTED: no state updates after unmount ACTUAL: none');
const staleLoading = deferred(), pending = deferred();
const loading = createHarness({ en: () => staleLoading.promise, tr: () => pending.promise });
loading.render().changeLocale('tr');
loading.render();
staleLoading.resolve({ default: { greeting: 'old' } });
await settled(staleLoading);
assert.equal(loading.render().loading, true);
pending.resolve({ default: { greeting: 'new' } });
await settled(pending);
assert.equal(loading.render().loading, false);
assert.equal(loading.render().t('greeting'), 'new');
console.log('EDGE EXPECTED: loading stays true until current load finishes ACTUAL: true then false');
const first = deferred(), second = deferred(), third = deferred();
let enCalls = 0;
const rapid = createHarness({ en: () => enCalls++ === 0 ? first.promise : third.promise, tr: () => second.promise });
rapid.render().changeLocale('tr');
rapid.render().changeLocale('en');
rapid.render();
third.resolve({ default: { greeting: 'new EN' } });
await settled(third);
second.resolve({ default: { greeting: 'old TR' } });
await settled(second);
first.resolve({ default: { greeting: 'old EN' } });
await settled(first);
assert.equal(rapid.render().t('greeting'), 'new EN');
console.log('EDGE EXPECTED: latest of three locales retained ACTUAL:', rapid.render().t('greeting'));
console.log('FIX VERIFIED');
