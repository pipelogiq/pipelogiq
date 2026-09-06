import { Outlet } from "react-router-dom";
import { AppSidebar } from "./AppSidebar";

export function AppLayout() {
  return (
    // The app sits as a sheet on a canvas rather than filling the viewport.
    // It costs a little width and buys the whole product a frame — the single
    // cheapest change in the design system. Dropped on small screens, where
    // the inset is only lost space.
    <div className="h-screen bg-background p-0 sm:p-2.5">
      <div className="flex h-full overflow-hidden border-border bg-card sm:rounded-2xl sm:border">
        <AppSidebar />
        <main className="flex-1 overflow-auto">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
