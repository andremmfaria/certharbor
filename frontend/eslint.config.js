import js from '@eslint/js'
import reactHooks from 'eslint-plugin-react-hooks'
import globals from 'globals'
import tseslint from 'typescript-eslint'

const BAN_MESSAGE = 'Every request goes through the generated client. See src/api/client.ts.'

export default tseslint.config(
  { ignores: ['dist/**', 'node_modules/**', 'coverage/**', 'nginx-lint/**'] },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  reactHooks.configs.flat.recommended,
  {
    files: ['src/**/*.{ts,tsx}'],
    languageOptions: { globals: globals.browser },
    rules: {
      eqeqeq: ['error', 'always'],
      'no-eval': 'error',
      'no-implied-eval': 'error',
      'no-new-func': 'error',
      'no-restricted-globals': [
        'error',
        { name: 'fetch', message: BAN_MESSAGE },
        { name: 'XMLHttpRequest', message: BAN_MESSAGE },
        { name: 'EventSource', message: BAN_MESSAGE },
      ],
      'no-restricted-properties': [
        'error',
        { object: 'window', property: 'fetch', message: BAN_MESSAGE },
        { object: 'globalThis', property: 'fetch', message: BAN_MESSAGE },
        { object: 'window', property: 'XMLHttpRequest', message: BAN_MESSAGE },
        { object: 'window', property: 'EventSource', message: BAN_MESSAGE },
        {
          object: 'URL',
          property: 'createObjectURL',
          message: 'Downloads go through downloadBlob.',
        },
      ],
      'no-restricted-imports': ['error', { paths: [{ name: 'axios', message: BAN_MESSAGE }] }],
      'no-restricted-syntax': [
        'error',
        {
          selector: "JSXAttribute[name.name='dangerouslySetInnerHTML']",
          message:
            'The CSP has no unsafe-eval and React is the only escaping layer. Render text, not markup.',
        },
        {
          selector: "NewExpression[callee.name='Function']",
          message: 'Banned: the CSP forbids unsafe-eval.',
        },
      ],
      '@typescript-eslint/no-unused-vars': ['error', { argsIgnorePattern: '^_' }],
    },
  },
  {
    files: ['src/api/downloadBlob.ts'],
    rules: { 'no-restricted-properties': 'off' },
  },
  {
    files: ['src/api/client.ts'],
    rules: { 'no-restricted-globals': 'off', 'no-restricted-imports': 'off' },
  },
  {
    files: ['vite.config.ts', 'vite/**/*.ts', 'scripts/**/*.{mjs,ts}', 'eslint.config.js'],
    languageOptions: { globals: globals.node },
  },
)
