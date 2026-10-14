import js from '@eslint/js'
import globals from 'globals'
import tsParser from '@typescript-eslint/parser'
import react from 'eslint-plugin-react'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'

// ESLint 9 flat config (the project had none, so `npm run lint` could not
// start). TypeScript itself checks undefined names and unused locals
// (`npm run typecheck`), so those base rules are off for .ts/.tsx.
export default [
  { ignores: ['dist', 'node_modules', 'coverage'] },
  {
    files: ['**/*.{js,jsx,ts,tsx}'],
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
      // "recommended" set (static-components, set-state-in-effect, ...) flag
      // existing patterns that need behaviour-changing rewrites.
      'react-hooks/rules-of-hooks': 'error',
      'react-hooks/exhaustive-deps': 'warn',
      'no-undef': 'off',
      'no-unused-vars': 'off',
      'react-refresh/only-export-components': ['warn', { allowConstantExport: true }],
    },
  },
  {
    // Tests call hooks conditionally on purpose to probe provider guards.
    files: ['**/*.test.{ts,tsx}'],
    rules: { 'react-hooks/rules-of-hooks': 'off' },
  },
  {
    // Component libraries and providers export hooks/constants next to
    // components; that only costs a full reload instead of fast refresh.
    files: ['src/components/ui/**', 'src/components/theme-provider.tsx', 'src/contexts/**'],
    rules: { 'react-refresh/only-export-components': 'off' },
  },
]
