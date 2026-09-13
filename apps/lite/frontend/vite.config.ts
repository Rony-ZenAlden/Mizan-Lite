/// <reference types="vitest" />
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import { fileURLToPath, URL } from "node:url";

const locales = fileURLToPath(new URL("../../../internal/lite/locales", import.meta.url));

// Lite has NO browser-dev mock. `npm run dev` outside Wails shows the bridge-unavailable screen,
// which is the truth; a convincing imitation of data is what Mizan's 10.17 shipped (design G5).
export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("./src", import.meta.url)),
      // The catalogs Go embeds, imported by the build — one set of files for both sides.
      "@locales": locales,
    },
  },
  server: {
    fs: { allow: [".", locales] },
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
  },
  test: {
    environment: "jsdom",
    setupFiles: ["./src/test/setup.ts"],
    globals: true,
    include: ["src/**/*.test.{ts,tsx}"],
    exclude: ["src/**/*.bundle.test.ts"],
  },
});
