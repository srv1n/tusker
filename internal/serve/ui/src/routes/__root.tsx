import { Outlet } from "@tanstack/react-router";
import { Sidebar } from "@/components/Sidebar";

/** App shell: fixed sidebar + a single scrolling content pane. */
export function RootLayout() {
  return (
    <div className="flex h-screen w-full overflow-hidden bg-surface text-ink">
      <Sidebar />
      <main className="min-w-0 flex-1 overflow-hidden">
        <Outlet />
      </main>
    </div>
  );
}

/** Pass-through layout for project-scoped routes. */
export function ProjectLayout() {
  return <Outlet />;
}
