import { Outlet } from "react-router-dom";
import { AppSidebar } from "./AppSidebar";

export function AppLayout() {
  return (
    // h-screen, not min-h-screen: the sidebar is h-full, and a percentage
    // height needs a parent with a definite one or it collapses to its own
    // content. The main column scrolls instead of the page.
    <div className="flex h-screen bg-background">
      <AppSidebar />
      <main className="flex-1 overflow-auto">
        <Outlet />
      </main>
    </div>
  );
}
