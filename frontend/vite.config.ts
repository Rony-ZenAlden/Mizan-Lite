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
  build: {
    outDir: "dist",
    emptyOutDir: true,
  },
});
