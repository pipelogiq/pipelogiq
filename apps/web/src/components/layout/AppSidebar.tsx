import { useState } from "react";
import { NavLink, useLocation } from "react-router-dom";
import {
  LayoutDashboard,
  Workflow,
  Shield,
  Activity,
  Settings,
  ChevronRight,
  Zap,
  PanelLeftClose,
  PanelLeft,
} from "lucide-react";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";

const navigation = [
  { name: "Dashboard", href: "/", icon: LayoutDashboard },
  { name: "Pipelines", href: "/pipelines", icon: Workflow },
  /*{ name: "Executions", href: "/executions", icon: History },*/
  { name: "Action Policies", href: "/action-policies", icon: Shield },
  { name: "Observability", href: "/observability", icon: Activity },
  { name: "Settings", href: "/settings", icon: Settings },
];

export function AppSidebar() {
  const location = useLocation();
  const [collapsed, setCollapsed] = useState(true);

  return (
    <aside
      className={cn(
        "flex h-screen flex-col border-r border-border bg-sidebar transition-all duration-300",
        collapsed ? "w-16" : "w-64"
      )}
    >
      {/* Logo */}
      <div className={cn(
        "flex h-16 items-center border-b border-border",
        collapsed ? "justify-center px-2" : "gap-3 px-4"
      )}>
        <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-primary">
          <Zap className="h-5 w-5 text-primary-foreground" />
        </div>
        {!collapsed && (
          <span className="text-lg font-semibold text-foreground">Pipelogiq</span>
        )}
      </div>

      {/* Navigation */}
      <nav className="flex-1 space-y-1 p-2">
        {navigation.map((item) => {
          const isActive = location.pathname === item.href ||
            (item.href !== "/" && location.pathname.startsWith(item.href));

          const linkContent = (
            <NavLink
              key={item.name}
              to={item.href}
              className={cn(
                "group flex items-center gap-3 rounded-lg px-3 py-2.5 text-sm font-medium transition-all",
                isActive
                  ? "bg-primary text-primary-foreground"
                  : "text-sidebar-foreground hover:bg-sidebar-accent hover:text-sidebar-accent-foreground",
                collapsed && "justify-center px-2"
              )}
            >
              <item.icon className="h-5 w-5 shrink-0" />
              {!collapsed && (
                <>
                  <span className="flex-1">{item.name}</span>
                  {isActive && <ChevronRight className="h-4 w-4 opacity-50" />}
                </>
              )}
            </NavLink>
          );

          if (collapsed) {
            return (
              <Tooltip key={item.name} delayDuration={0}>
                <TooltipTrigger asChild>
                  {linkContent}
                </TooltipTrigger>
                <TooltipContent side="right" className="font-medium">
                  {item.name}
                </TooltipContent>
              </Tooltip>
            );
          }

          return linkContent;
        })}
      </nav>

      {/* Collapse Toggle */}
      <div className="border-t border-border p-2">
        <Button
          variant="ghost"
          size="sm"
          onClick={() => setCollapsed(!collapsed)}
          className={cn(
            "w-full justify-center text-muted-foreground hover:text-foreground",
            !collapsed && "justify-start gap-2"
          )}
        >
          {collapsed ? (
            <PanelLeft className="h-5 w-5" />
          ) : (
            <>
              <PanelLeftClose className="h-5 w-5" />
              <span>Collapse</span>
            </>
          )}
        </Button>
      </div>

      {/* Environment Selector */}
      {!collapsed && (
        <div className="border-t border-border p-3">
          <div className="flex items-center gap-3 rounded-lg bg-sidebar-accent px-3 py-2.5">
            <div className="h-2 w-2 rounded-full bg-status-success" />
            <div className="flex-1">
              <p className="text-sm font-medium text-sidebar-accent-foreground">Production</p>
              <p className="text-xs text-muted-foreground">us-east-1</p>
            </div>
            <ChevronRight className="h-4 w-4 text-muted-foreground" />
          </div>
        </div>
      )}
    </aside>
  );
}
