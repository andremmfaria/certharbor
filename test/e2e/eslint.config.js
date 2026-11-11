// ESLint v9 flat config for the e2e harness, following the same shape as
// backend/eslint.config.js rather than introducing a second style.
//
// This file is ESM (package.json sets "type": "module"), unlike
// backend/eslint.config.js: a CommonJS require() here throws
// "require is not defined in ES module scope" under Node's ESM loader,
// since a plain .js file under a "type": "module" package is always
// loaded as a module, never CommonJS, regardless of its contents.
import tseslint from "typescript-eslint";

export default tseslint.config(
  {
    ignores: ["node_modules/**", "artifacts/**", "playwright-report/**"],
  },
  ...tseslint.configs.recommended,
  {
    files: ["**/*.ts"],
    languageOptions: {
      parserOptions: { ecmaVersion: 2022, sourceType: "module" },
    },
    rules: {
      "no-eval": "error",
      "no-implied-eval": "error",
      eqeqeq: ["error", "always"],
      "@typescript-eslint/no-unused-vars": ["warn", { argsIgnorePattern: "^_" }],
      // Playwright fixtures are declared with an empty destructuring pattern.
      "@typescript-eslint/no-empty-object-type": "off",
    },
  },
);
