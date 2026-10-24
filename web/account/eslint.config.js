import js from '@eslint/js'
import globals from 'globals'
import tsParser from '@typescript-eslint/parser'
import react from 'eslint-plugin-react'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'

// ESLint 9 flat config. TypeScript itself checks undefined names and
// unused locals (`tsc --noEmit` runs inside `npm run build`), so those
// base rules are off for .ts/.tsx. linterOptions restores the removed
// ESLint-8 `--report-unused-disable-directives` intent: unused disable
// directives warn, and `--max-warnings 0` fails the gate on them.
export default [
  { ignores: ['dist', 'node_modules', 'coverage'] },
  {
    files: ['**/*.{js,jsx,ts,tsx}'],
    linterOptions: { reportUnusedDisableDirectives: 'warn' },
    languageOptions: {
      parser: tsParser,
      ecmaVersion: 'latest',
      sourceType: 'module',
      globals: { ...globals.browser, ...globals.node },
      parserOptions: { ecmaFeatures: { jsx: true } },
    },
    settings: { react: { version: 'detect' } },
    plugins: { react, 'react-hooks': reactHooks, 'react-refresh': reactRefresh },
    rules: {
      ...js.configs.recommended.rules,
      // Only the classic hook rules: the React Compiler rules in the v7
      // "recommended" set flag existing patterns that need behaviour-
      // changing rewrites.
      'react-hooks/rules-of-hooks': 'error',
      'react-hooks/exhaustive-deps': 'warn',
      'no-undef': 'off',
      'no-unused-vars': 'off',
      'react-refresh/only-export-components': ['warn', { allowConstantExport: true }],
    },
  },
  {
    // Vacation exports its datetime helpers next to the page component for
    // the plain-node specs (they read the module's exports); fast-refresh
    // granularity is not worth splitting the file.
    files: ['src/pages/Vacation.tsx'],
    rules: { 'react-refresh/only-export-components': 'off' },
  },
]
