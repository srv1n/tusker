// Build-only config for the isolated WUX overview sample-data preview.
// Used to produce a static bundle for headless screenshot proof in
// environments where the Vite dev server cannot bind a port. Changes no
// shared routes, styles, or manifests.
import { fileURLToPath, URL } from "node:url";
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig({
  base: "./",
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("../../../src", import.meta.url)),
    },
  },
  build: {
    outDir: "/tmp/wux-overview-build",
    emptyOutDir: false,
    sourcemap: false,
    rollupOptions: {
      input: fileURLToPath(new URL("./index.html", import.meta.url)),
    },
  },
});
