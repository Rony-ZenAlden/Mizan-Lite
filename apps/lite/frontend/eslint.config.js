import js from "@eslint/js";
import tseslint from "@typescript-eslint/eslint-plugin";
import tsparser from "@typescript-eslint/parser";
import reactHooks from "eslint-plugin-react-hooks";

// Physical-direction classes break right-to-left. Copied from Mizan, where it has held since 10.3.
const FORBIDDEN_DIRECTION_CLASSES =
  /\b(pl-|pr-|ml-|mr-|text-left|text-right|float-left|float-right|left-|right-|rounded-l-|rounded-r-|border-l|border-r)\S*/;

const noPhysicalDirection = {
  selector: `Literal[value=/${FORBIDDEN_DIRECTION_CLASSES.source}/]`,
  message:
    "Use logical properties (ps-/pe-, ms-/me-, text-start/text-end, start-/end-): physical-direction classes break RTL.",
};

// G1. Nothing in Lite's own code accesses a property named `go` or `runtime` — on ANY object, in ANY
// spelling. Keyed on the property rather than on `window`, because a drill showed a rule keyed on
// `window` misses `(window as unknown as Record<string, unknown>)["go"]`, and an alias would defeat it
// too. The generated wailsjs files are the only code that reaches the bridge, and they are not linted.
const noWailsGlobals = [
  {
    selector: String.raw`MemberExpression[property.name=/^(go|runtime)$/]`,
    message: "The Wails bridge is reached only by the generated wailsjs files. Use the client (src/api/client.ts).",
  },
  {
    selector: String.raw`MemberExpression[computed=true][property.value=/^(go|runtime)$/]`,
    message: "The Wails bridge is reached only by the generated wailsjs files. Use the client (src/api/client.ts).",
  },
];

// G1. The generated modules are imported in exactly one file.
const noWailsjsImports = {
  patterns: [
    {
      group: ["**/wailsjs/**", "**/wailsjs"],
      message: "Only src/api/client.ts imports the generated bindings. Use the client instead.",
    },
  ],
};

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
        setTimeout: "readonly",
        clearTimeout: "readonly",
        HTMLElement: "readonly",
        HTMLButtonElement: "readonly",
        Element: "readonly",
        // Node globals, for the gates that read the source tree off disk.
        process: "readonly",
        URL: "readonly",
      },
    },
    plugins: {
      "@typescript-eslint": tseslint,
      "react-hooks": reactHooks,
    },
    rules: {
      ...tseslint.configs.recommended.rules,
      ...reactHooks.configs.recommended.rules,
      "no-restricted-syntax": ["error", noPhysicalDirection, ...noWailsGlobals],
      "no-restricted-imports": ["error", noWailsjsImports],
    },
  },
  {
    // The one file allowed to import the generated modules. It keeps every other rule — including
    // the window.go ban, which it has no reason to need.
    files: ["src/api/client.ts"],
    rules: {
      "no-restricted-imports": "off",
    },
  },
  {
    // Gate tests read files and run the TypeScript compiler over the source; they legitimately
    // mention the patterns the rules above forbid, as data.
    files: ["src/**/*.gates.test.ts", "src/**/*.bundle.test.ts"],
    rules: {
      "no-restricted-syntax": "off",
      "no-restricted-imports": "off",
    },
  },
];
