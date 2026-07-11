import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";

// Self-hosted fonts (no CDN — packet §6 hard constraint). Weights match the
// design: Source Serif 4 400/500/600 + italic; Source Code Pro 400/500/600.
import "@fontsource/source-serif-4/400.css";
import "@fontsource/source-serif-4/500.css";
import "@fontsource/source-serif-4/600.css";
import "@fontsource/source-serif-4/400-italic.css";
import "@fontsource/source-code-pro/400.css";
import "@fontsource/source-code-pro/500.css";
import "@fontsource/source-code-pro/600.css";

import "@/styles/app.css";
import { ThemeProvider } from "@/lib/theme";
import { ConfirmProvider } from "@/components/ui/action-feedback";
import { router } from "@/router";
import { USE_MOCK } from "@/lib/api";
import { connectLiveStream } from "@/lib/stream";

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      // The SSE stream owns freshness. Keep visited project projections warm so
      // switching back is an immediate cache read, not another loading cycle.
      staleTime: 30_000,
      retry: 1,
      refetchOnWindowFocus: false,
    },
  },
});

const rootEl = document.getElementById("root");
if (!rootEl) throw new Error("#root not found");

const disconnectLiveStream = connectLiveStream(queryClient, { enabled: !USE_MOCK });
if (import.meta.hot) {
  import.meta.hot.dispose(disconnectLiveStream);
}

createRoot(rootEl).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <ThemeProvider>
        <ConfirmProvider>
          <RouterProvider router={router} />
        </ConfirmProvider>
      </ThemeProvider>
    </QueryClientProvider>
  </StrictMode>,
);
