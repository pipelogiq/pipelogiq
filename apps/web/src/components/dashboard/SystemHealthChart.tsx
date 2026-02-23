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

export function SystemHealthChart() {
  return (
    <div className="rounded-xl border border-border bg-card p-5">
      <div className="mb-4 flex items-center justify-between">
        <h3 className="font-semibold text-foreground">Execution Trends</h3>
        <div className="flex items-center gap-4 text-xs">
          <div className="flex items-center gap-1.5">
            <div className="h-2 w-2 rounded-full bg-status-success" />
            <span className="text-muted-foreground">Success</span>
          </div>
          <div className="flex items-center gap-1.5">
            <div className="h-2 w-2 rounded-full bg-status-error" />
            <span className="text-muted-foreground">Failed</span>
          </div>
          <div className="flex items-center gap-1.5">
            <div className="h-2 w-2 rounded-full bg-status-warning" />
            <span className="text-muted-foreground">Delayed</span>
          </div>
        </div>
      </div>

      <ResponsiveContainer width="100%" height={240}>
        <AreaChart data={data}>
          <defs>
            <linearGradient id="colorSuccess" x1="0" y1="0" x2="0" y2="1">
              <stop offset="5%" stopColor="hsl(160, 84%, 39%)" stopOpacity={0.2} />
              <stop offset="95%" stopColor="hsl(160, 84%, 39%)" stopOpacity={0} />
            </linearGradient>
            <linearGradient id="colorFailed" x1="0" y1="0" x2="0" y2="1">
              <stop offset="5%" stopColor="hsl(0, 84%, 60%)" stopOpacity={0.2} />
              <stop offset="95%" stopColor="hsl(0, 84%, 60%)" stopOpacity={0} />
            </linearGradient>
          </defs>
          <CartesianGrid strokeDasharray="3 3" stroke="hsl(220, 13%, 91%)" vertical={false} />
          <XAxis
            dataKey="time"
            axisLine={false}
            tickLine={false}
            tick={{ fill: "hsl(220, 9%, 46%)", fontSize: 12 }}
          />
          <YAxis
            axisLine={false}
            tickLine={false}
            tick={{ fill: "hsl(220, 9%, 46%)", fontSize: 12 }}
          />
          <Tooltip
            contentStyle={{
              backgroundColor: "hsl(0, 0%, 100%)",
              border: "1px solid hsl(220, 13%, 91%)",
              borderRadius: "8px",
              boxShadow: "0 4px 12px rgba(0,0,0,0.08)",
            }}
          />
          <Area
            type="monotone"
            dataKey="success"
            stroke="hsl(160, 84%, 39%)"
            strokeWidth={2}
            fillOpacity={1}
            fill="url(#colorSuccess)"
          />
          <Area
            type="monotone"
            dataKey="failed"
            stroke="hsl(0, 84%, 60%)"
            strokeWidth={2}
            fillOpacity={1}
            fill="url(#colorFailed)"
          />
        </AreaChart>
      </ResponsiveContainer>
    </div>
  );
}
