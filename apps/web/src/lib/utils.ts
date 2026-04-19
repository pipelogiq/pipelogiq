import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

export function formatMs(ms: number): string {
  if (ms < 1000) return `${Math.round(ms)}ms`;
  if (ms < 60_000) return `${(ms / 1000).toFixed(1)}s`;
  return `${(ms / 60_000).toFixed(1)}m`;
}

export function statusToVariant(s?: string) {
  switch (s) {
    case "connected": return "success" as const;
    case "configured": return "warning" as const;
    case "disconnected": case "error": return "error" as const;
    default: return "default" as const;
  }
}

export function statusLabel(s?: string) {
  switch (s) {
    case "connected": return "Connected";
    case "configured": return "Configured";
    case "disconnected": return "Disconnected";
    case "error": return "Error";
    default: return "Not configured";
  }
}
