import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { fileURLToPath, URL } from "node:url";

// Tusker Serve UI — embedded control-room SPA.
// Dev server runs standalone; production build (`bun run build`) emits to ./dist,
// which the Go serve package embeds via go:embed. See BACKEND-GAPS.md.
export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("./src", import.meta.url)),
    },
  },
  server: {
    port: 5173,
    // The real daemon serves the JSON API on :7420. During UI-only dev we run
    // against the in-browser mock layer, so no proxy is wired yet (see api.ts).
  },
  build: {
    outDir: "dist",
    // No sourcemaps in the shipped build: `dist/` is compiled into the Go
    // binary via go:embed, and the maps were ~14MB of the 22MB total while
    // being useful only when debugging minified prod JS by hand.
    sourcemap: false,
    rollupOptions: {
      output: {
        // Split large vendors into a handful of stable, logical chunks so the
        // initial load stays lean and long-term caching is friendly.
        //   react    — react / react-dom runtime (eager framework)
        //   tanstack — router + query (eager framework)
        //   editor   — the TipTap/ProseMirror stack (async: docs route only)
        //   mermaid  — diagram renderer + d3/dagre/cytoscape deps (async;
        //              also dynamic-imported by the editor NodeView). This is a
        //              safety net to keep mermaid out of the eager vendor chunk;
        //              it does NOT force it eager — nothing in the initial graph
        //              statically imports it.
        manualChunks(id) {
          if (!id.includes("/node_modules/")) return;
          if (
            id.includes("/node_modules/react/") ||
            id.includes("/node_modules/react-dom/") ||
            id.includes("/node_modules/scheduler/")
          )
            return "react";
          if (id.includes("/node_modules/@tanstack/")) return "tanstack";
          if (
            id.includes("/node_modules/@tiptap/") ||
            id.includes("/node_modules/prosemirror-") ||
            id.includes("/node_modules/tiptap-markdown/") ||
            id.includes("/node_modules/markdown-it/") ||
            id.includes("/node_modules/lowlight/") ||
            id.includes("/node_modules/highlight.js/")
          )
            return "editor";
          if (
            id.includes("/node_modules/mermaid/") ||
            id.includes("/node_modules/@mermaid-js/") ||
            id.includes("/node_modules/d3/") ||
            id.includes("/node_modules/d3-") ||
            id.includes("/node_modules/dagre") ||
            id.includes("/node_modules/cytoscape")
          )
            return "mermaid";
        },
      },
    },
  },
});
