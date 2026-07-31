import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import { fileURLToPath, URL } from "node:url";

// Wails serves the built `dist/` and provides window.go bindings in the desktop
// webview. In a plain browser (npm run dev without wails), the bindings wrapper
// falls back to mock data — see src/lib/wails.
export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("./src", import.meta.url)),
    },
  },
  server: {
    fs: {
      // locales/ is the single source shared with the Go backend (ARCHITECTURE_v1 §22.1)
      // and sits above the Vite root, so dev-mode file serving must be allowed to reach it.
      // Production builds inline the JSON at build time and need no allowance.
      allow: [".."],
    },
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
  },
});
