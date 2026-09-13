import { defineConfig } from "vitest/config";
import { fileURLToPath, URL } from "node:url";

// G5 runs against the BUILT artefact, after `vite build`, in its own invocation — a gate whose input
// is a build cannot share a run with tests that must pass before one exists.
export default defineConfig({
  resolve: {
    alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) },
  },
  test: {
    environment: "node",
    include: ["src/**/*.bundle.test.ts"],
  },
});
