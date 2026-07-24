import js from "@eslint/js";
import tseslint from "@typescript-eslint/eslint-plugin";
import tsparser from "@typescript-eslint/parser";
import reactHooks from "eslint-plugin-react-hooks";

// Physical-direction Tailwind classes break RTL. This is a lint gate, not a
// convention — see ARCHITECTURE_v1.md §22.3 / PHASE_0_FOUNDATION.md §FE.
const FORBIDDEN_DIRECTION_CLASSES =
  /\b(pl-|pr-|ml-|mr-|text-left|text-right|float-left|float-right|left-|right-|rounded-l-|rounded-r-|border-l|border-r)\S*/;

export default [
  { ignores: ["dist/**", "wailsjs/**", "node_modules/**"] },
  js.configs.recommended,
  {
    files: ["src/**/*.{ts,tsx}"],
    languageOptions: {
      parser: tsparser,
      parserOptions: { ecmaFeatures: { jsx: true }, sourceType: "module" },
      globals: {
        window: "readonly",
        document: "readonly",
        console: "readonly",
        HTMLElement: "readonly",
        URL: "readonly",
        setTimeout: "readonly",
        clearTimeout: "readonly",
      },
    },
    plugins: {
      "@typescript-eslint": tseslint,
      "react-hooks": reactHooks,
    },
    rules: {
      ...tseslint.configs.recommended.rules,
      ...reactHooks.configs.recommended.rules,
      "no-restricted-syntax": [
        "error",
        {
          selector: `Literal[value=/${FORBIDDEN_DIRECTION_CLASSES.source}/]`,
          message:
            "Use logical properties (ps-/pe-, ms-/me-, text-start/text-end, start-/end-) — physical-direction classes break RTL.",
        },
      ],
    },
  },
];
