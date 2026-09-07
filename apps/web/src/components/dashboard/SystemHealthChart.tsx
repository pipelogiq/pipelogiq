import {
  AreaChart,
  Area,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
} from "recharts";

const data = [
  { time: "00:00", success: 420, failed: 12, delayed: 8 },
  { time: "04:00", success: 380, failed: 8, delayed: 15 },
  { time: "08:00", success: 650, failed: 22, delayed: 18 },
  { time: "12:00", success: 890, failed: 35, delayed: 25 },
  { time: "16:00", success: 780, failed: 28, delayed: 20 },
  { time: "20:00", success: 620, failed: 15, delayed: 12 },
  { time: "24:00", success: 450, failed: 10, delayed: 8 },
];

/**
 * Every colour here is a token, so the chart follows the theme instead of
 * pinning one palette. Recharts wants real colour values rather than classes,
 * and `hsl(var(--token))` resolves as an SVG paint the same way it does in CSS.
 *
 * These are the semantic hues, not the neutral ramp: the ramp is for splitting
 * one quantity into categories, where the hues would be arbitrary. Here each
 * series already means something, and Failed is the line you are scanning for.
 */
const series = [
  { key: "success", label: "Success", color: "hsl(var(--status-success))" },
  { key: "failed", label: "Failed", color: "hsl(var(--status-error))" },
  { key: "delayed", label: "Delayed", color: "hsl(var(--status-warning))" },
] as const;

export function SystemHealthChart() {
  return (
    <div className="rounded-xl border border-border bg-card p-6">
      <div className="mb-4 flex items-center justify-between">
        <h3 className="font-semibold text-foreground">Execution Trends</h3>
        <div className="flex items-center gap-4 text-xs">
          {series.map(s => (
            <div key={s.key} className="flex items-center gap-1.5">
              <div className="h-2 w-2 rounded-full" style={{ background: s.color }} />
              <span className="text-muted-foreground">{s.label}</span>
            </div>
          ))}
        </div>
      </div>

      <ResponsiveContainer width="100%" height={240}>
        <AreaChart data={data}>
          <CartesianGrid strokeDasharray="3 3" stroke="hsl(var(--border))" vertical={false} />
          <XAxis
            dataKey="time"
            axisLine={false}
            tickLine={false}
            tick={{ fill: "hsl(var(--muted-foreground))", fontSize: 12 }}
          />
          <YAxis
            axisLine={false}
            tickLine={false}
            // Five gridlines, not ten. The reader wants the shape, not a ruler.
            tickCount={5}
            tick={{ fill: "hsl(var(--muted-foreground))", fontSize: 12 }}
          />
          <Tooltip
            contentStyle={{
              backgroundColor: "hsl(var(--popover))",
              color: "hsl(var(--popover-foreground))",
              border: "1px solid hsl(var(--border))",
              borderRadius: "8px",
              boxShadow: "0 12px 32px -12px rgba(15, 18, 30, 0.18)",
            }}
          />
          {/* A flat wash rather than a gradient — the gradient was decoration
              and it is the one thing on the stop list a chart tends to smuggle in. */}
          {series.map(s => (
            <Area
              key={s.key}
              type="monotone"
              dataKey={s.key}
              stroke={s.color}
              strokeWidth={2}
              fill={s.color}
              fillOpacity={0.08}
            />
          ))}
        </AreaChart>
      </ResponsiveContainer>
    </div>
  );
}
