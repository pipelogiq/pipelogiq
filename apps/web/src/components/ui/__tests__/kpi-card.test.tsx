import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { KpiCard } from "@/components/ui/kpi-card";

afterEach(cleanup);

describe("KPI card accents", () => {
  it.each([0, "0", "—"])("uses a neutral accent for %s", (value) => {
    const { container } = render(
      <KpiCard title="Workers Degraded" value={value} tone="critical" accent="warning" />,
    );

    expect(container.firstElementChild?.classList.contains("before:bg-muted-foreground/60")).toBe(true);
    expect(screen.getByText(String(value)).classList.contains("text-status-warning")).toBe(false);
  });

  it.each(["warning", "error"] as const)("matches the critical figure to the %s accent", (accent) => {
    const { container } = render(<KpiCard title="Count" value={2} tone="critical" accent={accent} />);

    expect(container.firstElementChild?.classList.contains(`before:bg-status-${accent}`)).toBe(true);
    expect(screen.getByText("2").classList.contains(`text-status-${accent}`)).toBe(true);
  });
});
