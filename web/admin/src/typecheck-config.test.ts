import { describe, it, expect } from 'vitest';
import tsconfig from '../tsconfig.json';

// Regression: web/admin/tsconfig.json must contain only compilerOptions the
// installed TypeScript accepts. The config shipped "ignoreDeprecations": "6.0",
// which the TypeScript 5.x line rejects with TS5103 at option validation — so
// `tsc --noEmit` (npm run typecheck, the project's only type gate) exited 2
// before checking a single source file. (Sibling of the identical webmail bug
// fixed in webmail/tsconfig.json + webmail/src/typecheck-config.test.ts: do
// not copy the invalid value back into either config.) The only value
// TypeScript 5.x accepts is "5.0"; when the project upgrades past TS 5.x,
// revisit this table deliberately.
const VALID_IGNORE_DEPRECATIONS_FOR_TS_5 = new Set(['5.0']);

describe('typecheck gate config', () => {
  it('declares compilerOptions', () => {
    expect(
      tsconfig.compilerOptions,
      'web/admin/tsconfig.json must define compilerOptions for the type gate',
    ).toBeTruthy();
  });

  it('ignoreDeprecations, when present, is accepted by the installed TypeScript', () => {
    const options = tsconfig.compilerOptions as { ignoreDeprecations?: string };
    const value = options.ignoreDeprecations;
    if (value === undefined) {
      // Absent = no deprecation suppression = always valid.
      return;
    }
    expect(
      value,
      `"ignoreDeprecations: ${value}" is rejected by TypeScript 5.x with TS5103, ` +
        'which kills the whole typecheck gate before any file is checked',
    ).toBeInValidIgnoreDeprecationsSet();
  });
});

// Local matcher keeps the accepted-value table in exactly one place.
declare module 'vitest' {
  interface Assertion<T> {
    toBeInValidIgnoreDeprecationsSet(): T;
  }
}

expect.extend({
  toBeInValidIgnoreDeprecationsSet(received: unknown) {
    const pass =
      typeof received === 'string' && VALID_IGNORE_DEPRECATIONS_FOR_TS_5.has(received);
    return {
      pass,
      message: () =>
        `expected ignoreDeprecations ${String(received)} to be one of ` +
        `${[...VALID_IGNORE_DEPRECATIONS_FOR_TS_5].join(', ')}`,
    };
  },
});
