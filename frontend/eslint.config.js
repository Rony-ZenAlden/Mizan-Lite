import js from "@eslint/js";
import tseslint from "@typescript-eslint/eslint-plugin";
import tsparser from "@typescript-eslint/parser";
import reactHooks from "eslint-plugin-react-hooks";

// Physical-direction Tailwind classes break RTL. This is a lint gate, not a
// convention — see ARCHITECTURE_v1.md §22.3 / PHASE_0_FOUNDATION.md §FE.
const FORBIDDEN_DIRECTION_CLASSES =
  /\b(pl-|pr-|ml-|mr-|text-left|text-right|float-left|float-right|left-|right-|rounded-l-|rounded-r-|border-l|border-r)\S*/;

const noPhysicalDirection = {
  selector: `Literal[value=/${FORBIDDEN_DIRECTION_CLASSES.source}/]`,
  message:
    "Use logical properties (ps-/pe-, ms-/me-, text-start/text-end, start-/end-) — physical-direction classes break RTL.",
};

// The Wails bridge has exactly one home. Step 0.11 §1.2: the Health wrapper kept declaring the
// pre-envelope shape after Step 0.10 wrapped the binding in Result[T], and nothing caught it —
// because "every call goes through one place" (§FE.4) was a sentence in a document rather than
// a failing build. This makes it a failing build. Test helpers live in src/lib/wails/testing.ts
// so tests elsewhere never need an exemption either.
const noWailsGlobals = {
  selector: String.raw`MemberExpression[object.name='window'][property.name=/^(go|runtime)$/]`,
  message:
    "window.go / window.runtime may only be touched in src/lib/wails/. Import a typed binding from @/lib/wails instead (Step 0.11 §3.4).",
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
        navigator: "readonly",
        console: "readonly",
        HTMLElement: "readonly",
        HTMLButtonElement: "readonly",
        HTMLInputElement: "readonly",
        HTMLIFrameElement: "readonly",
        // For the command palette: a ref to its scrolling list, and the global key listener
        // that opens it.
        HTMLDivElement: "readonly",
        KeyboardEvent: "readonly",
        Window: "readonly",
        URL: "readonly",
        setTimeout: "readonly",
        clearTimeout: "readonly",
        matchMedia: "readonly",
        MediaQueryListEvent: "readonly",
        Element: "readonly",
        Event: "readonly",
        ResizeObserver: "readonly",
        // File and btoa, for the import screen: a chosen file is read as bytes and encoded for
        // the boundary. Declared rather than disabled, so the next global that appears is still
        // caught.
        File: "readonly",
        btoa: "readonly",
        // Node globals, for the token gate that reads index.css off disk.
        process: "readonly",
      },
    },
    plugins: {
      "@typescript-eslint": tseslint,
      "react-hooks": reactHooks,
    },
    rules: {
      ...tseslint.configs.recommended.rules,
      ...reactHooks.configs.recommended.rules,
      "no-restricted-syntax": ["error", noPhysicalDirection, noWailsGlobals],
    },
  },
  {
    // The bridge's own home. It keeps the RTL gate and drops only the window.go restriction —
    // a targeted subtraction rather than a blanket exemption, so a stray `pl-4` in here is
    // still caught.
    files: ["src/lib/wails/**/*.{ts,tsx}"],
    rules: {
      "no-restricted-syntax": ["error", noPhysicalDirection],
    },
  },
];
