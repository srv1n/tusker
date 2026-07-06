/**
 * Route-level pending fallback for lazy screen chunks.
 *
 * Wired as `defaultPendingComponent` on the router (see router.tsx). It renders
 * inside the persistent shell's content pane while a code-split route chunk is
 * fetched, so the sidebar frame stays put and only the screen area shows a
 * quiet placeholder. With `defaultPreload: "intent"` the chunk is usually warm
 * from link hover, so this only flashes on a cold navigation.
 *
 * Kept intentionally minimal and theme-aware, using existing design tokens and
 * the app's soft-pulse animation — the loading language here is a gentle pulse,
 * not a spinner, matching the skeleton states used elsewhere.
 */
export function RouteFallback() {
  return (
    <div className="flex h-full w-full items-center justify-center p-8">
      <div className="flex items-center gap-2.5 text-muted">
        <span className="h-2 w-2 flex-none animate-pulse-soft rounded-full bg-muted" />
        <span className="text-[13px] font-medium">Loading…</span>
      </div>
    </div>
  );
}
