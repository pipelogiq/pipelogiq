import { useState, useMemo } from "react";
import { AppHeader } from "@/components/layout/AppHeader";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { StatusBadge } from "@/components/ui/status-badge";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  Search,
  ChevronRight,
  RefreshCw,
  X,
  FileText,
  ArrowRight,
  Box,
  AlertCircle,
  CheckCircle2,
  Loader2,
  Pause,
  Timer,
  Clock,
} from "lucide-react";
import { cn } from "@/lib/utils";

type ExecutionStatus = "success" | "error" | "running" | "paused" | "throttled" | "waiting";

interface PipelineAction {
  id: string;
  name: string;
  type: string;
  status: ExecutionStatus;
  startedAt: string;
  duration: string;
  retries: number;
  logs: string[];
  input: Record<string, unknown>;
  output: Record<string, unknown>;
}

interface PipelineContext {
  key: string;
  value: string;
}

interface Execution {
  id: string;
  pipelineName: string;
  triggerEvent: string;
  status: ExecutionStatus;
  startedAt: string;
  duration: string;
  actionsCount: number;
  failedActions: number;
  environment: string;
  correlationId: string;
  context: PipelineContext[];
  actions: PipelineAction[];
}

const mockExecutions: Execution[] = [
  {
    id: "exec-001",
    pipelineName: "order-processing",
    triggerEvent: "order.created",
    status: "success",
    startedAt: "2024-01-15 14:32:15",
    duration: "1.2s",
    actionsCount: 5,
    failedActions: 0,
    environment: "production",
    correlationId: "ord-12345-abc",
    context: [
      { key: "orderId", value: "ORD-2024-12345" },
      { key: "customerId", value: "CUST-789" },
      { key: "totalAmount", value: "$299.99" },
      { key: "currency", value: "USD" },
    ],
    actions: [
      {
        id: "act-1",
        name: "Validate Order",
        type: "validation",
        status: "success",
        startedAt: "14:32:15.001",
        duration: "45ms",
        retries: 0,
        logs: ["Validating order schema...", "Order schema valid", "Checking inventory availability...", "All items in stock"],
        input: { orderId: "ORD-2024-12345", items: [{ sku: "ITEM-001", qty: 2 }] },
        output: { valid: true, inventoryReserved: true },
      },
      {
        id: "act-2",
        name: "Calculate Pricing",
        type: "calculation",
        status: "success",
        startedAt: "14:32:15.050",
        duration: "120ms",
        retries: 0,
        logs: ["Fetching price rules...", "Applying discount: SUMMER20", "Tax calculation complete"],
        input: { items: [{ sku: "ITEM-001", qty: 2, basePrice: 149.99 }], discountCode: "SUMMER20" },
        output: { subtotal: 299.98, discount: 59.99, tax: 19.20, total: 259.19 },
      },
      {
        id: "act-3",
        name: "Create Invoice",
        type: "document",
        status: "success",
        startedAt: "14:32:15.180",
        duration: "350ms",
        retries: 0,
        logs: ["Generating invoice PDF...", "Invoice created: INV-2024-67890", "Stored in document service"],
        input: { orderId: "ORD-2024-12345", total: 259.19 },
        output: { invoiceId: "INV-2024-67890", pdfUrl: "/invoices/INV-2024-67890.pdf" },
      },
      {
        id: "act-4",
        name: "Send Confirmation Email",
        type: "notification",
        status: "success",
        startedAt: "14:32:15.540",
        duration: "280ms",
        retries: 0,
        logs: ["Preparing email template...", "Email queued for delivery", "Email sent successfully"],
        input: { to: "customer@example.com", template: "order-confirmation", invoiceId: "INV-2024-67890" },
        output: { messageId: "msg-abc123", delivered: true },
      },
      {
        id: "act-5",
        name: "Update Analytics",
        type: "analytics",
        status: "success",
        startedAt: "14:32:15.830",
        duration: "95ms",
        retries: 0,
        logs: ["Sending event to analytics...", "Event recorded"],
        input: { event: "order_completed", properties: { value: 259.19, currency: "USD" } },
        output: { tracked: true },
      },
    ],
  },
  {
    id: "exec-002",
    pipelineName: "kyc-verification",
    triggerEvent: "user.kyc_submitted",
    status: "error",
    startedAt: "2024-01-15 14:28:42",
    duration: "8.5s",
    actionsCount: 4,
    failedActions: 1,
    environment: "production",
    correlationId: "kyc-98765-def",
    context: [
      { key: "userId", value: "USR-456789" },
      { key: "verificationType", value: "identity" },
      { key: "documentType", value: "passport" },
    ],
    actions: [
      {
        id: "act-1",
        name: "Extract Document Data",
        type: "ocr",
        status: "success",
        startedAt: "14:28:42.001",
        duration: "2.3s",
        retries: 0,
        logs: ["Processing document image...", "OCR extraction complete", "Data extracted successfully"],
        input: { documentUrl: "/uploads/passport-456.jpg", documentType: "passport" },
        output: { name: "John Doe", dob: "1990-05-15", passportNumber: "AB123456", expiryDate: "2028-05-14" },
      },
      {
        id: "act-2",
        name: "Verify Identity",
        type: "verification",
        status: "success",
        startedAt: "14:28:44.350",
        duration: "3.1s",
        retries: 1,
        logs: ["Connecting to verification service...", "First attempt failed, retrying...", "Identity verified successfully"],
        input: { name: "John Doe", dob: "1990-05-15", passportNumber: "AB123456" },
        output: { verified: true, confidence: 0.95, matchScore: 98 },
      },
      {
        id: "act-3",
        name: "AML Check",
        type: "compliance",
        status: "error",
        startedAt: "14:28:47.500",
        duration: "1.8s",
        retries: 3,
        logs: ["Initiating AML screening...", "Connection timeout, retry 1/3...", "Connection timeout, retry 2/3...", "Connection timeout, retry 3/3...", "ERROR: AML service unavailable after 3 retries"],
        input: { name: "John Doe", dob: "1990-05-15", country: "US" },
        output: { error: "AML_SERVICE_UNAVAILABLE", message: "Connection timeout after 3 retries" },
      },
      {
        id: "act-4",
        name: "Update User Status",
        type: "database",
        status: "waiting",
        startedAt: "-",
        duration: "-",
        retries: 0,
        logs: ["Waiting for previous action to complete..."],
        input: {},
        output: {},
      },
    ],
  },
  {
    id: "exec-003",
    pipelineName: "notification-dispatcher",
    triggerEvent: "notification.scheduled",
    status: "throttled",
    startedAt: "2024-01-15 14:30:00",
    duration: "3.2s (waiting)",
    actionsCount: 3,
    failedActions: 0,
    environment: "production",
    correlationId: "ntf-11111-ghi",
    context: [
      { key: "notificationId", value: "NTF-789012" },
      { key: "channel", value: "email" },
      { key: "priority", value: "normal" },
    ],
    actions: [
      {
        id: "act-1",
        name: "Prepare Template",
        type: "template",
        status: "success",
        startedAt: "14:30:00.001",
        duration: "85ms",
        retries: 0,
        logs: ["Loading template: weekly-digest", "Template compiled successfully"],
        input: { templateId: "weekly-digest", locale: "en-US" },
        output: { html: "<html>...</html>", subject: "Your Weekly Summary" },
      },
      {
        id: "act-2",
        name: "Send Email",
        type: "email",
        status: "throttled",
        startedAt: "14:30:00.090",
        duration: "waiting 3.1s",
        retries: 0,
        logs: ["Rate limit reached for email sending", "Waiting 3.1s due to policy: max 1 email per 5 seconds", "Position in queue: 3"],
        input: { to: "user@example.com", subject: "Your Weekly Summary", html: "<html>...</html>" },
        output: { status: "queued", waitTime: "3.1s", reason: "rate_limit" },
      },
      {
        id: "act-3",
        name: "Log Delivery",
        type: "logging",
        status: "waiting",
        startedAt: "-",
        duration: "-",
        retries: 0,
        logs: ["Waiting for email to be sent..."],
        input: {},
        output: {},
      },
    ],
  },
  {
    id: "exec-004",
    pipelineName: "betting-settlement",
    triggerEvent: "event.finished",
    status: "running",
    startedAt: "2024-01-15 14:32:45",
    duration: "0.4s",
    actionsCount: 6,
    failedActions: 0,
    environment: "production",
    correlationId: "bet-55555-jkl",
    context: [
      { key: "eventId", value: "EVT-FOOTBALL-123" },
      { key: "marketId", value: "MKT-WIN-HOME" },
      { key: "result", value: "HOME_WIN" },
      { key: "totalBets", value: "1,247" },
    ],
    actions: [
      {
        id: "act-1",
        name: "Validate Result",
        type: "validation",
        status: "success",
        startedAt: "14:32:45.001",
        duration: "25ms",
        retries: 0,
        logs: ["Validating event result...", "Result confirmed: HOME_WIN"],
        input: { eventId: "EVT-FOOTBALL-123", result: "HOME_WIN" },
        output: { valid: true, confirmedResult: "HOME_WIN" },
      },
      {
        id: "act-2",
        name: "Fetch Winning Bets",
        type: "database",
        status: "success",
        startedAt: "14:32:45.030",
        duration: "180ms",
        retries: 0,
        logs: ["Querying winning bets...", "Found 523 winning tickets"],
        input: { marketId: "MKT-WIN-HOME", result: "HOME_WIN" },
        output: { winningBets: 523, totalPayout: 45678.90 },
      },
      {
        id: "act-3",
        name: "Calculate Payouts",
        type: "calculation",
        status: "running",
        startedAt: "14:32:45.220",
        duration: "processing...",
        retries: 0,
        logs: ["Calculating individual payouts...", "Processing bet 234 of 523..."],
        input: { bets: "523 items", odds: "various" },
        output: {},
      },
      {
        id: "act-4",
        name: "Process Withdrawals",
        type: "payment",
        status: "waiting",
        startedAt: "-",
        duration: "-",
        retries: 0,
        logs: [],
        input: {},
        output: {},
      },
      {
        id: "act-5",
        name: "Send Winner Notifications",
        type: "notification",
        status: "waiting",
        startedAt: "-",
        duration: "-",
        retries: 0,
        logs: [],
        input: {},
        output: {},
      },
      {
        id: "act-6",
        name: "Update Statistics",
        type: "analytics",
        status: "waiting",
        startedAt: "-",
        duration: "-",
        retries: 0,
        logs: [],
        input: {},
        output: {},
      },
    ],
  },
  {
    id: "exec-005",
    pipelineName: "fraud-detection",
    triggerEvent: "transaction.created",
    status: "success",
    startedAt: "2024-01-15 14:31:58",
    duration: "0.15s",
    actionsCount: 3,
    failedActions: 0,
    environment: "production",
    correlationId: "frd-77777-mno",
    context: [
      { key: "transactionId", value: "TXN-999888" },
      { key: "amount", value: "$1,250.00" },
      { key: "riskScore", value: "12 (low)" },
    ],
    actions: [
      {
        id: "act-1",
        name: "Analyze Transaction",
        type: "ml",
        status: "success",
        startedAt: "14:31:58.001",
        duration: "85ms",
        retries: 0,
        logs: ["Running fraud detection model...", "Model inference complete", "Risk score: 12 (low risk)"],
        input: { amount: 1250.00, currency: "USD", merchantId: "MERCH-123", cardBin: "424242" },
        output: { riskScore: 12, riskLevel: "low", factors: [] },
      },
      {
        id: "act-2",
        name: "Apply Rules",
        type: "rules",
        status: "success",
        startedAt: "14:31:58.090",
        duration: "35ms",
        retries: 0,
        logs: ["Checking velocity rules...", "Checking amount thresholds...", "All rules passed"],
        input: { riskScore: 12, transactionHistory: "last 24h" },
        output: { rulesMatched: 0, action: "approve" },
      },
      {
        id: "act-3",
        name: "Update Decision",
        type: "database",
        status: "success",
        startedAt: "14:31:58.130",
        duration: "20ms",
        retries: 0,
        logs: ["Recording decision: APPROVED"],
        input: { transactionId: "TXN-999888", decision: "approve" },
        output: { recorded: true },
      },
    ],
  },
];

export default function Executions() {
  const [searchQuery, setSearchQuery] = useState("");
  const [statusFilter, setStatusFilter] = useState("all");
  const [envFilter, setEnvFilter] = useState("all");
  const [pipelineFilter, setPipelineFilter] = useState("all");
  const [selectedExecution, setSelectedExecution] = useState<Execution | null>(null);
  const [selectedAction, setSelectedAction] = useState<PipelineAction | null>(null);

  const filteredExecutions = useMemo(() => {
    return mockExecutions.filter((exec) => {
      if (searchQuery) {
        const query = searchQuery.toLowerCase();
        const matchesId = exec.id.toLowerCase().includes(query);
        const matchesPipeline = exec.pipelineName.toLowerCase().includes(query);
        const matchesEvent = exec.triggerEvent.toLowerCase().includes(query);
        const matchesCorrelation = exec.correlationId.toLowerCase().includes(query);
        const matchesContext = exec.context.some(
          (c) => c.key.toLowerCase().includes(query) || c.value.toLowerCase().includes(query)
        );
        if (!matchesId && !matchesPipeline && !matchesEvent && !matchesCorrelation && !matchesContext) {
          return false;
        }
      }
      if (statusFilter !== "all" && exec.status !== statusFilter) return false;
      if (envFilter !== "all" && exec.environment !== envFilter) return false;
      if (pipelineFilter !== "all" && exec.pipelineName !== pipelineFilter) return false;
      return true;
    });
  }, [searchQuery, statusFilter, envFilter, pipelineFilter]);

  const uniquePipelines = [...new Set(mockExecutions.map((e) => e.pipelineName))];

  return (
    <div className="flex flex-col h-full">
      <AppHeader
        title="Executions"
        subtitle={`${mockExecutions.length.toLocaleString()} executions today`}
      />

      <div className="flex-1 p-6 space-y-4">
        {/* Search and Filters */}
        <div className="flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between">
          <div className="relative flex-1 max-w-xl">
            <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
            <Input
              placeholder="Search by ID, pipeline, event, correlation ID, or context values..."
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              className="pl-10 pr-10"
            />
            {searchQuery && (
              <button
                onClick={() => setSearchQuery("")}
                className="absolute right-3 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
              >
                <X className="h-4 w-4" />
              </button>
            )}
          </div>

          <div className="flex flex-wrap gap-2">
            <Select value={pipelineFilter} onValueChange={setPipelineFilter}>
              <SelectTrigger className="w-[180px]">
                <SelectValue placeholder="All Pipelines" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">All Pipelines</SelectItem>
                {uniquePipelines.map((p) => (
                  <SelectItem key={p} value={p}>{p}</SelectItem>
                ))}
              </SelectContent>
            </Select>

            <Select value={statusFilter} onValueChange={setStatusFilter}>
              <SelectTrigger className="w-[140px]">
                <SelectValue placeholder="All Status" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">All Status</SelectItem>
                <SelectItem value="success">Success</SelectItem>
                <SelectItem value="error">Error</SelectItem>
                <SelectItem value="running">Running</SelectItem>
                <SelectItem value="throttled">Throttled</SelectItem>
                <SelectItem value="waiting">Waiting</SelectItem>
              </SelectContent>
            </Select>

            <Select value={envFilter} onValueChange={setEnvFilter}>
              <SelectTrigger className="w-[140px]">
                <SelectValue placeholder="All Envs" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">All Environments</SelectItem>
                <SelectItem value="production">Production</SelectItem>
                <SelectItem value="staging">Staging</SelectItem>
                <SelectItem value="development">Development</SelectItem>
              </SelectContent>
            </Select>

            <Button variant="outline" size="icon">
              <RefreshCw className="h-4 w-4" />
            </Button>
          </div>
        </div>

        {/* Results count */}
        <div className="text-sm text-muted-foreground">
          Showing {filteredExecutions.length} of {mockExecutions.length} executions
        </div>

        {/* Executions Table */}
        <div className="border rounded-lg overflow-hidden bg-card">
          <Table>
            <TableHeader>
              <TableRow className="bg-muted/50">
                <TableHead className="w-[140px]">Execution ID</TableHead>
                <TableHead>Pipeline / Event</TableHead>
                <TableHead className="w-[100px]">Status</TableHead>
                <TableHead className="w-[160px]">Started</TableHead>
                <TableHead className="w-[100px]">Duration</TableHead>
                <TableHead className="w-[100px]">Actions</TableHead>
                <TableHead className="w-[50px]"></TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {filteredExecutions.map((execution) => (
                <TableRow
                  key={execution.id}
                  className="cursor-pointer hover:bg-muted/50"
                  onClick={() => {
                    setSelectedExecution(execution);
                    setSelectedAction(null);
                  }}
                >
                  <TableCell className="font-mono text-xs">{execution.id}</TableCell>
                  <TableCell>
                    <div>
                      <p className="font-medium">{execution.pipelineName}</p>
                      <p className="text-xs text-muted-foreground">{execution.triggerEvent}</p>
                    </div>
                  </TableCell>
                  <TableCell>
                    <StatusBadge status={execution.status} />
                  </TableCell>
                  <TableCell className="text-sm">{execution.startedAt}</TableCell>
                  <TableCell className="font-mono text-sm">{execution.duration}</TableCell>
                  <TableCell>
                    <div className="flex items-center gap-1">
                      <span className="text-sm">{execution.actionsCount}</span>
                      {execution.failedActions > 0 && (
                        <Badge variant="destructive" className="text-xs px-1.5 py-0">
                          {execution.failedActions} failed
                        </Badge>
                      )}
                    </div>
                  </TableCell>
                  <TableCell>
                    <ChevronRight className="h-4 w-4 text-muted-foreground" />
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      </div>

      {/* Execution Details Sheet */}
      <Sheet open={!!selectedExecution} onOpenChange={() => setSelectedExecution(null)}>
        <SheetContent className="w-full sm:max-w-2xl overflow-y-auto">
          {selectedExecution && (
            <>
              <SheetHeader className="pb-4 border-b">
                <div className="flex items-center gap-3">
                  <StatusBadge status={selectedExecution.status} />
                  <SheetTitle className="font-mono text-lg">{selectedExecution.id}</SheetTitle>
                </div>
                <div className="text-sm text-muted-foreground mt-2">
                  <span className="font-medium text-foreground">{selectedExecution.pipelineName}</span>
                  <span className="mx-2">•</span>
                  <span>{selectedExecution.triggerEvent}</span>
                </div>
              </SheetHeader>

              <div className="py-4 space-y-6">
                {/* Pipeline Context */}
                <div>
                  <h4 className="text-sm font-semibold mb-3 flex items-center gap-2">
                    <Box className="h-4 w-4" />
                    Pipeline Context
                  </h4>
                  <div className="grid grid-cols-2 gap-2">
                    {selectedExecution.context.map((ctx) => (
                      <div key={ctx.key} className="bg-muted rounded-lg px-3 py-2">
                        <p className="text-xs text-muted-foreground">{ctx.key}</p>
                        <p className="text-sm font-mono font-medium">{ctx.value}</p>
                      </div>
                    ))}
                  </div>
                </div>

                {/* Metadata */}
                <div className="grid grid-cols-2 gap-4 p-4 bg-muted/50 rounded-lg">
                  <div>
                    <p className="text-xs text-muted-foreground">Correlation ID</p>
                    <p className="text-sm font-mono">{selectedExecution.correlationId}</p>
                  </div>
                  <div>
                    <p className="text-xs text-muted-foreground">Environment</p>
                    <p className="text-sm font-medium capitalize">{selectedExecution.environment}</p>
                  </div>
                  <div>
                    <p className="text-xs text-muted-foreground">Started At</p>
                    <p className="text-sm">{selectedExecution.startedAt}</p>
                  </div>
                  <div>
                    <p className="text-xs text-muted-foreground">Total Duration</p>
                    <p className="text-sm font-mono">{selectedExecution.duration}</p>
                  </div>
                </div>

                {/* Actions Timeline */}
                <div>
                  <h4 className="text-sm font-semibold mb-3 flex items-center gap-2">
                    <FileText className="h-4 w-4" />
                    Actions ({selectedExecution.actions.length})
                  </h4>
                  <div className="space-y-2">
                    {selectedExecution.actions.map((action, index) => (
                      <div
                        key={action.id}
                        className={cn(
                          "border rounded-lg p-3 cursor-pointer transition-all",
                          selectedAction?.id === action.id
                            ? "border-primary bg-primary/5"
                            : "hover:border-primary/50 hover:bg-muted/50"
                        )}
                        onClick={() => setSelectedAction(action)}
                      >
                        <div className="flex items-center justify-between">
                          <div className="flex items-center gap-3">
                            <div className={cn(
                              "w-6 h-6 rounded-full flex items-center justify-center text-xs font-medium",
                              action.status === "success" && "bg-status-success-bg text-status-success",
                              action.status === "error" && "bg-status-error-bg text-status-error",
                              action.status === "running" && "bg-status-running-bg text-status-running",
                              action.status === "throttled" && "bg-status-throttled-bg text-status-throttled",
                              action.status === "waiting" && "bg-muted text-muted-foreground",
                            )}>
                              {index + 1}
                            </div>
                            <div>
                              <p className="font-medium text-sm">{action.name}</p>
                              <p className="text-xs text-muted-foreground">{action.type}</p>
                            </div>
                          </div>
                          <div className="flex items-center gap-3">
                            <StatusBadge status={action.status} size="sm" />
                            <span className="text-xs font-mono text-muted-foreground">{action.duration}</span>
                            <ChevronRight className="h-4 w-4 text-muted-foreground" />
                          </div>
                        </div>
                      </div>
                    ))}
                  </div>
                </div>

                {/* Selected Action Details */}
                {selectedAction && (
                  <div className="border-t pt-4">
                    <h4 className="text-sm font-semibold mb-3 flex items-center gap-2">
                      <ArrowRight className="h-4 w-4" />
                      {selectedAction.name} Details
                    </h4>
                    <Tabs defaultValue="logs" className="w-full">
                      <TabsList className="w-full grid grid-cols-3">
                        <TabsTrigger value="logs">Logs</TabsTrigger>
                        <TabsTrigger value="input">Input</TabsTrigger>
                        <TabsTrigger value="output">Output</TabsTrigger>
                      </TabsList>
                      <TabsContent value="logs" className="mt-3">
                        <div className="bg-muted rounded-lg p-3 font-mono text-xs space-y-1 max-h-60 overflow-y-auto">
                          {selectedAction.logs.length > 0 ? (
                            selectedAction.logs.map((log, i) => (
                              <div key={i} className={cn(
                                "py-1",
                                log.includes("ERROR") && "text-status-error",
                                log.includes("retry") && "text-status-warning",
                              )}>
                                <span className="text-muted-foreground mr-2">[{selectedAction.startedAt}]</span>
                                {log}
                              </div>
                            ))
                          ) : (
                            <p className="text-muted-foreground">No logs available</p>
                          )}
                        </div>
                      </TabsContent>
                      <TabsContent value="input" className="mt-3">
                        <pre className="bg-muted rounded-lg p-3 font-mono text-xs overflow-x-auto max-h-60 overflow-y-auto">
                          {JSON.stringify(selectedAction.input, null, 2)}
                        </pre>
                      </TabsContent>
                      <TabsContent value="output" className="mt-3">
                        <pre className="bg-muted rounded-lg p-3 font-mono text-xs overflow-x-auto max-h-60 overflow-y-auto">
                          {JSON.stringify(selectedAction.output, null, 2)}
                        </pre>
                      </TabsContent>
                    </Tabs>

                    {selectedAction.retries > 0 && (
                      <div className="mt-3 p-3 bg-status-warning-bg rounded-lg">
                        <p className="text-sm text-status-warning font-medium">
                          This action was retried {selectedAction.retries} time(s)
                        </p>
                      </div>
                    )}
                  </div>
                )}
              </div>
            </>
          )}
        </SheetContent>
      </Sheet>
    </div>
  );
}
