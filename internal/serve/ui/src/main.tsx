import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";

// Self-hosted v2 type system: Archivo for product language and JetBrains Mono
// only for exact identifiers/commands.
import "@fontsource/archivo/400.css";
import "@fontsource/archivo/500.css";
import "@fontsource/archivo/600.css";
import "@fontsource/archivo/700.css";
import "@fontsource/jetbrains-mono/400.css";
import "@fontsource/jetbrains-mono/500.css";
import "@fontsource/jetbrains-mono/600.css";

import "@/styles/app.css";
import { ThemeProvider } from "@/lib/theme";
import { ConfirmProvider } from "@/components/ui/action-feedback";
import { router } from "@/router";
import { USE_MOCK } from "@/lib/api";
import { connectLiveStream } from "@/lib/stream";
import {
  restoreStartupQueryCache,
  subscribeStartupQueryCache,
} from "@/lib/queryPersistence";

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

restoreStartupQueryCache(queryClient, window.localStorage);
const disconnectStartupQueryCache = subscribeStartupQueryCache(queryClient, window.localStorage);

const rootEl = document.getElementById("root");
if (!rootEl) throw new Error("#root not found");

const disconnectLiveStream = connectLiveStream(queryClient, { enabled: !USE_MOCK });
if (import.meta.hot) {
  import.meta.hot.dispose(() => {
    disconnectLiveStream();
    disconnectStartupQueryCache();
  });
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
