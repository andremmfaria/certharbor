// Flat config lookup walks up from cwd, so this must exist here for
// openapi-ts.config.ts's output.lint: 'eslint' step to find one. ESM, unlike
// backend/eslint.config.js and test/e2e's, because this package.json
// declares "type": "module".
import tseslint from 'typescript-eslint';

export default tseslint.config(
  {
    ignores: ['node_modules/**', 'dist/**'],
  },
  ...tseslint.configs.recommended,
  {
    files: ['**/*.ts'],
    languageOptions: {
      parserOptions: { ecmaVersion: 2022, sourceType: 'module' },
    },
    rules: {
      'no-eval': 'error',
      'no-implied-eval': 'error',
      eqeqeq: ['error', 'always'],
      '@typescript-eslint/no-unused-vars': ['warn', { argsIgnorePattern: '^_' }],
      '@typescript-eslint/no-explicit-any': 'off',
    },
  },
  {
    // hey-api's infinite-query plugin emits @ts-ignore (not @ts-expect-error)
    // in generated pagination helpers for any paginated list operation
    // (listCertificates is the first). Generated code only, never
    // hand-written: scoping the exception to *.gen.ts keeps ban-ts-comment
    // enforced everywhere else.
    files: ['**/*.gen.ts'],
    rules: {
      '@typescript-eslint/ban-ts-comment': 'off',
    },
  },
);
