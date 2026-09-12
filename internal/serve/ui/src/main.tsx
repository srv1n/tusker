import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";

// UI type is the platform system face (see --font-sans). The serif display
// face is system serif; JetBrains Mono is only for exact identifiers and
// commands.
import "@fontsource/jetbrains-mono/400.css";
import "@fontsource/jetbrains-mono/500.css";
import "@fontsource/jetbrains-mono/600.css";

import "@/styles/app.css";
import { FontScaleProvider } from "@/lib/font-scale";
import { ThemeProvider } from "@/lib/theme";
import { ConfirmProvider } from "@/components/ui/action-feedback";
import { router } from "@/router";
import { connectLiveStream } from "@/lib/stream";
import {
  restoreStartupQueryCache,
  subscribeStartupQueryCache,
} from "@/lib/queryPersistence";

window.addEventListener("vite:preloadError", (event) => {
  event.preventDefault();
  const retryKey = "tusker:preload-reload";
  if (window.sessionStorage.getItem(retryKey)) return;
  window.sessionStorage.setItem(retryKey, "1");
  const url = new URL(window.location.href);
  url.searchParams.set("_reload", Date.now().toString());
  window.location.replace(url);
});

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

const disconnectLiveStream = connectLiveStream(queryClient, { enabled: true });
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
        <FontScaleProvider>
          <ConfirmProvider>
            <RouterProvider router={router} />
          </ConfirmProvider>
        </FontScaleProvider>
      </ThemeProvider>
    </QueryClientProvider>
  </StrictMode>,
);
