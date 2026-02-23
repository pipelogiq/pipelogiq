import { useMemo, useState } from 'react';
import { format, formatDistanceToNow } from 'date-fns';
import {
  AlertTriangle,
  CheckCircle2,
  ChevronDown,
  Clock,
  Loader2,
  MoreHorizontal,
  Pause,
  Play,
  Plus,
  Shield,
  Timer,
  Trash2,
  Zap,
} from 'lucide-react';

import { AppHeader } from '@/components/layout/AppHeader';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { KpiCard } from '@/components/ui/kpi-card';
import { Skeleton } from '@/components/ui/skeleton';
import { StatusBadge } from '@/components/ui/status-badge';
import { Badge } from '@/components/ui/badge';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { ScrollArea } from '@/components/ui/scroll-area';
import { Label } from '@/components/ui/label';
import { Textarea } from '@/components/ui/textarea';
import { Switch } from '@/components/ui/switch';
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible';
import { useToast } from '@/hooks/use-toast';
import {
  useCreatePolicy,
  useDeletePolicy,
  useDuplicatePolicy,
  usePolicies,
  usePolicy,
  usePolicyAudit,
  usePolicyInsights,
  usePolicyStatusAction,
  usePolicyTargetOptions,
  usePreviewPolicyTargets,
  useUpdatePolicy,
} from '@/hooks/use-policies';
import type {
  CircuitBreakerRule,
  CreatePolicyRequest,
  Policy,
  PolicyEnvironment,
  PolicyEvent,
  PolicyRange,
  PolicyRule,
  PolicyStatus,
  PolicyTargeting,
  PolicyType,
  RateLimitRule,
  RetryRule,
  TimeoutRule,
  UpdatePolicyRequest,
} from '@/types/policies';

interface PolicyDraft {
  name: string;
  description: string;
  type: PolicyType;
  status: PolicyStatus;
  environment: PolicyEnvironment;
  targeting: PolicyTargeting;
  rateLimitRule: RateLimitRule;
  retryRule: RetryRule;
  timeoutRule: TimeoutRule;
  circuitBreakerRule: CircuitBreakerRule;
}

const rangeOptions: Array<{ value: PolicyRange; label: string }> = [
  { value: '15m', label: 'Last 15m' },
  { value: '1h', label: 'Last 1h' },
  { value: '24h', label: 'Last 24h' },
  { value: '7d', label: 'Last 7d' },
];

const policyTypes: Array<{ value: PolicyType; label: string; description: string; icon: typeof Timer }> = [
  {
    value: 'rate_limit',
    label: 'Rate limit',
    description: 'Control throughput by window and key scope.',
    icon: Timer,
  },
  {
    value: 'retry',
    label: 'Retry',
    description: 'Retry transient failures with backoff controls.',
    icon: Zap,
  },
  {
    value: 'timeout',
    label: 'Timeout',
    description: 'Bound execution time for steps or external calls.',
    icon: Clock,
  },
  {
    value: 'circuit_breaker',
    label: 'Circuit breaker',
    description: 'Open on repeated failures and protect dependencies.',
    icon: Shield,
  },
];

const policyStatusOptions: Array<{ value: PolicyStatus | 'all'; label: string }> = [
  { value: 'all', label: 'All statuses' },
  { value: 'active', label: 'Active' },
  { value: 'paused', label: 'Paused' },
  { value: 'disabled', label: 'Disabled' },
];

const policyTypeFilterOptions: Array<{ value: PolicyType | 'all'; label: string }> = [
  { value: 'all', label: 'All types' },
  ...policyTypes.map(type => ({ value: type.value, label: type.label })),
];

const sortOptions: Array<{ value: 'triggers' | 'lastTriggered' | 'updatedAt'; label: string }> = [
  { value: 'updatedAt', label: 'Updated at' },
  { value: 'triggers', label: 'Triggers' },
  { value: 'lastTriggered', label: 'Last triggered' },
];

function createDefaultDraft(): PolicyDraft {
  return {
    name: '',
    description: '',
    type: 'rate_limit',
    status: 'active',
    environment: 'all',
    targeting: {
      pipelines: [],
      stages: [],
      handlers: [],
      tagsInclude: [],
      tagsExclude: [],
    },
    rateLimitRule: {
      limit: 100,
      windowSeconds: 60,
      keyBy: 'tenant',
      burst: 0,
    },
    retryRule: {
      maxAttempts: 3,
      backoff: 'exponential',
      baseDelayMs: 500,
      maxDelayMs: 5000,
      jitter: true,
      retryOn: {
        httpStatus: [429, 500, 502, 503],
        errorCodes: [],
      },
    },
    timeoutRule: {
      timeoutMs: 30000,
      appliesTo: 'external_call',
    },
    circuitBreakerRule: {
      failureThreshold: 5,
      windowSeconds: 60,
      openSeconds: 30,
      halfOpenMaxCalls: 3,
    },
  };
}

function mapPolicyToDraft(policy: Policy): PolicyDraft {
  const base = createDefaultDraft();

  const draft: PolicyDraft = {
    ...base,
    name: policy.name,
    description: policy.description ?? '',
    type: policy.type,
    status: policy.status,
    environment: policy.environment,
    targeting: {
      pipelines: [...policy.targeting.pipelines],
      stages: [...policy.targeting.stages],
      handlers: [...policy.targeting.handlers],
      tagsInclude: [...policy.targeting.tagsInclude],
      tagsExclude: [...policy.targeting.tagsExclude],
    },
  };

  switch (policy.type) {
    case 'rate_limit':
      draft.rateLimitRule = {
        ...draft.rateLimitRule,
        ...policy.rule,
      };
      break;
    case 'retry':
      draft.retryRule = {
        ...draft.retryRule,
        ...policy.rule,
      };
      break;
    case 'timeout':
      draft.timeoutRule = {
        ...draft.timeoutRule,
        ...policy.rule,
      };
      break;
    case 'circuit_breaker':
      draft.circuitBreakerRule = {
        ...draft.circuitBreakerRule,
        ...policy.rule,
      };
      break;
  }

  return draft;
}

function getPolicyRuleFromDraft(draft: PolicyDraft): PolicyRule {
  switch (draft.type) {
    case 'rate_limit': {
      return {
        limit: Math.max(1, draft.rateLimitRule.limit),
        windowSeconds: Math.max(1, draft.rateLimitRule.windowSeconds),
        keyBy: draft.rateLimitRule.keyBy,
        burst: draft.rateLimitRule.burst && draft.rateLimitRule.burst > 0 ? draft.rateLimitRule.burst : undefined,
      };
    }
    case 'retry': {
      return {
        maxAttempts: Math.max(1, draft.retryRule.maxAttempts),
        backoff: draft.retryRule.backoff,
        baseDelayMs: Math.max(1, draft.retryRule.baseDelayMs),
        maxDelayMs: draft.retryRule.maxDelayMs && draft.retryRule.maxDelayMs > 0 ? draft.retryRule.maxDelayMs : undefined,
        jitter: Boolean(draft.retryRule.jitter),
        retryOn: {
          httpStatus: (draft.retryRule.retryOn?.httpStatus ?? []).filter(code => Number.isFinite(code)),
          errorCodes: (draft.retryRule.retryOn?.errorCodes ?? []).filter(Boolean),
        },
      };
    }
    case 'timeout': {
      return {
        timeoutMs: Math.max(1, draft.timeoutRule.timeoutMs),
        appliesTo: draft.timeoutRule.appliesTo,
      };
    }
    case 'circuit_breaker': {
      return {
        failureThreshold: Math.max(1, draft.circuitBreakerRule.failureThreshold),
        windowSeconds: Math.max(1, draft.circuitBreakerRule.windowSeconds),
        openSeconds: Math.max(1, draft.circuitBreakerRule.openSeconds),
        halfOpenMaxCalls: Math.max(1, draft.circuitBreakerRule.halfOpenMaxCalls),
      };
    }
  }
}

function buildPolicyPayload(draft: PolicyDraft): CreatePolicyRequest {
  const base = {
    name: draft.name.trim(),
    description: draft.description.trim() || undefined,
    type: draft.type,
    status: draft.status,
    environment: draft.environment,
    targeting: {
      pipelines: [...draft.targeting.pipelines],
      stages: [...draft.targeting.stages],
      handlers: [...draft.targeting.handlers],
      tagsInclude: [...draft.targeting.tagsInclude],
      tagsExclude: [...draft.targeting.tagsExclude],
    },
  };

  const rule = getPolicyRuleFromDraft(draft);

  switch (draft.type) {
    case 'rate_limit':
      return { ...base, type: 'rate_limit', rule: rule as RateLimitRule };
    case 'retry':
      return { ...base, type: 'retry', rule: rule as RetryRule };
    case 'timeout':
      return { ...base, type: 'timeout', rule: rule as TimeoutRule };
    case 'circuit_breaker':
      return { ...base, type: 'circuit_breaker', rule: rule as CircuitBreakerRule };
  }
}

function isDraftStepValid(draft: PolicyDraft, step: number): boolean {
  if (step === 1) {
    return Boolean(draft.type);
  }

  if (step === 2) {
    if (!draft.name.trim()) {
      return false;
    }

    switch (draft.type) {
      case 'rate_limit':
        return draft.rateLimitRule.limit > 0 && draft.rateLimitRule.windowSeconds > 0;
      case 'retry':
        return draft.retryRule.maxAttempts > 0 && draft.retryRule.baseDelayMs > 0;
      case 'timeout':
        return draft.timeoutRule.timeoutMs > 0;
      case 'circuit_breaker':
        return (
          draft.circuitBreakerRule.failureThreshold > 0 &&
          draft.circuitBreakerRule.windowSeconds > 0 &&
          draft.circuitBreakerRule.openSeconds > 0 &&
          draft.circuitBreakerRule.halfOpenMaxCalls > 0
        );
      default:
        return false;
    }
  }

  return true;
}

function policyTypeLabel(type: PolicyType): string {
  const found = policyTypes.find(item => item.value === type);
  return found?.label ?? type;
}

function policyStatusLabel(status: PolicyStatus): string {
  switch (status) {
    case 'active':
      return 'Active';
    case 'paused':
      return 'Paused';
    case 'disabled':
      return 'Disabled';
    default:
      return status;
  }
}

function policyStatusVariant(status: PolicyStatus): 'success' | 'paused' | 'waiting' {
  switch (status) {
    case 'active':
      return 'success';
    case 'paused':
      return 'paused';
    case 'disabled':
      return 'waiting';
    default:
      return 'waiting';
  }
}

function renderTimestamp(ts?: string): string {
  if (!ts) return '—';
  const date = new Date(ts);
  if (Number.isNaN(date.getTime())) return '—';
  return formatDistanceToNow(date, { addSuffix: true });
}

function formatAuditTime(ts: string): string {
  const date = new Date(ts);
  if (Number.isNaN(date.getTime())) return ts;
  return format(date, 'MMM d, yyyy HH:mm:ss');
}

function scopeSummary(policy: Policy): string {
  return `env:${policy.environment}, pipelines:${policy.targeting.pipelines.length}, stages:${policy.targeting.stages.length}, handlers:${policy.targeting.handlers.length}`;
}

function humanizePolicyEffect(policy: Policy): string {
  switch (policy.type) {
    case 'rate_limit':
      return `Allow ${policy.rule.limit} requests per ${policy.rule.windowSeconds}s, keyed by ${policy.rule.keyBy}.`;
    case 'retry':
      return `Retry up to ${policy.rule.maxAttempts} attempts with ${policy.rule.backoff} backoff starting at ${policy.rule.baseDelayMs}ms.`;
    case 'timeout':
      return `Terminate ${policy.rule.appliesTo === 'step' ? 'step' : 'external call'} after ${policy.rule.timeoutMs}ms.`;
    case 'circuit_breaker':
      return `Open circuit after ${policy.rule.failureThreshold} failures in ${policy.rule.windowSeconds}s and stay open for ${policy.rule.openSeconds}s.`;
    default:
      return 'No effect summary available.';
  }
}

function parseCommaSeparatedNumbers(value: string): number[] {
  return value
    .split(',')
    .map(v => Number(v.trim()))
    .filter(v => Number.isFinite(v));
}

function parseCommaSeparatedStrings(value: string): string[] {
  return value
    .split(',')
    .map(v => v.trim())
    .filter(Boolean);
}

function eventTitle(event: PolicyEvent): string {
  switch (event.type) {
    case 'created':
      return 'Policy created';
    case 'updated':
      return 'Policy edited';
    case 'enabled':
      return 'Policy enabled';
    case 'disabled':
      return 'Policy disabled';
    case 'paused':
      return 'Policy paused';
    case 'resumed':
      return 'Policy resumed';
    case 'deleted':
      return 'Policy deleted';
    case 'triggered':
      return 'Policy triggered';
    default:
      return event.type;
  }
}

export default function Policies() {
  const { toast } = useToast();

  const [range, setRange] = useState<PolicyRange>('24h');
  const [search, setSearch] = useState('');
  const [typeFilter, setTypeFilter] = useState<PolicyType | 'all'>('all');
  const [statusFilter, setStatusFilter] = useState<PolicyStatus | 'all'>('all');
  const [envFilter, setEnvFilter] = useState<PolicyEnvironment | 'all'>('all');
  const [pipelineFilter, setPipelineFilter] = useState<string | 'all'>('all');
  const [sortBy, setSortBy] = useState<'triggers' | 'lastTriggered' | 'updatedAt'>('updatedAt');
  const [sortDir, setSortDir] = useState<'asc' | 'desc'>('desc');

  const [selectedPolicyId, setSelectedPolicyId] = useState<string | null>(null);
  const [drawerOpen, setDrawerOpen] = useState(false);

  const [wizardOpen, setWizardOpen] = useState(false);
  const [wizardStep, setWizardStep] = useState(1);
  const [wizardMode, setWizardMode] = useState<'create' | 'edit'>('create');
  const [editingPolicyId, setEditingPolicyId] = useState<string | null>(null);
  const [draft, setDraft] = useState<PolicyDraft>(createDefaultDraft());
  const [helpOpen, setHelpOpen] = useState(false);

  const [appliedPreview, setAppliedPreview] = useState<{ pipelines: number; stages: number; handlers: number } | null>(null);

  const listParams = useMemo(
    () => ({
      search,
      type: typeFilter,
      status: statusFilter,
      env: envFilter,
      pipelineId: pipelineFilter,
      range,
      sortBy,
      sortDir,
    }),
    [search, typeFilter, statusFilter, envFilter, pipelineFilter, range, sortBy, sortDir],
  );

  const policiesQuery = usePolicies(listParams);
  const insightsQuery = usePolicyInsights(range);
  const optionsQuery = usePolicyTargetOptions();

  const selectedPolicyFromList = policiesQuery.data?.items.find(item => item.id === selectedPolicyId) ?? null;
  const policyDetailQuery = usePolicy(drawerOpen ? selectedPolicyId : null, range);
  const auditQuery = usePolicyAudit(drawerOpen ? selectedPolicyId : null);

  const selectedPolicy = policyDetailQuery.data ?? selectedPolicyFromList ?? null;

  const previewMutation = usePreviewPolicyTargets();
  const createMutation = useCreatePolicy();
  const updateMutation = useUpdatePolicy();
  const duplicateMutation = useDuplicatePolicy();
  const deleteMutation = useDeletePolicy();
  const statusMutation = usePolicyStatusAction();

  const policies = useMemo(() => policiesQuery.data?.items ?? [], [policiesQuery.data?.items]);

  const summary = useMemo(() => {
    const activePoliciesCount = insightsQuery.data?.activePoliciesCount ?? policies.filter(p => p.status === 'active').length;
    const policiesTriggered = insightsQuery.data?.policiesTriggered ?? policies.reduce((acc, item) => acc + item.triggerCountInRange, 0);
    const actionsBlocked = insightsQuery.data?.actionsBlockedThrottled ?? 0;
    const topPolicy = insightsQuery.data?.topPolicy?.name ?? '—';

    return {
      activePoliciesCount,
      policiesTriggered,
      actionsBlocked,
      topPolicy,
    };
  }, [insightsQuery.data, policies]);

  const pipelineOptions = optionsQuery.data?.pipelines ?? [];
  const stageOptions = optionsQuery.data?.stages ?? [];
  const handlerOptions = optionsQuery.data?.handlers ?? [];
  const tagOptions = optionsQuery.data?.tags ?? [];

  const openCreateWizard = () => {
    setWizardMode('create');
    setEditingPolicyId(null);
    setDraft(createDefaultDraft());
    setWizardStep(1);
    setWizardOpen(true);
  };

  const openEditWizard = (policy: Policy, startStep = 2) => {
    setWizardMode('edit');
    setEditingPolicyId(policy.id);
    setDraft(mapPolicyToDraft(policy));
    setWizardStep(startStep);
    setWizardOpen(true);
  };

  const openPolicyDrawer = (policyId: string) => {
    setAppliedPreview(null);
    setSelectedPolicyId(policyId);
    setDrawerOpen(true);
  };

  const executeStatusAction = (policy: Policy, action: 'pause' | 'resume' | 'enable' | 'disable') => {
    statusMutation.mutate(
      { id: policy.id, action },
      {
        onSuccess: () => {
          toast({
            title: `${policy.name} updated`,
            description: `Policy is now ${action === 'pause' ? 'paused' : action === 'resume' ? 'active' : action === 'enable' ? 'active' : 'disabled'}.`,
          });
        },
        onError: error => {
          toast({
            title: 'Failed to update policy status',
            description: error instanceof Error ? error.message : 'Unknown error',
            variant: 'destructive',
          });
        },
      },
    );
  };

  const runDeletePolicy = (policy: Policy) => {
    const confirmed = window.confirm(`Delete policy "${policy.name}"? This action cannot be undone.`);
    if (!confirmed) {
      return;
    }

    deleteMutation.mutate(policy.id, {
      onSuccess: () => {
        if (selectedPolicyId === policy.id) {
          setDrawerOpen(false);
          setSelectedPolicyId(null);
        }
        toast({
          title: 'Policy deleted',
          description: `${policy.name} has been removed.`,
        });
      },
      onError: error => {
        toast({
          title: 'Failed to delete policy',
          description: error instanceof Error ? error.message : 'Unknown error',
          variant: 'destructive',
        });
      },
    });
  };

  const runDuplicatePolicy = (policy: Policy) => {
    duplicateMutation.mutate(policy.id, {
      onSuccess: duplicated => {
        toast({
          title: 'Policy duplicated',
          description: `Created ${duplicated.name}.`,
        });
      },
      onError: error => {
        toast({
          title: 'Failed to duplicate policy',
          description: error instanceof Error ? error.message : 'Unknown error',
          variant: 'destructive',
        });
      },
    });
  };

  const submitWizard = () => {
    const payload = buildPolicyPayload(draft);

    if (wizardMode === 'create') {
      createMutation.mutate(payload, {
        onSuccess: policy => {
          setWizardOpen(false);
          setWizardStep(1);
          setDraft(createDefaultDraft());
          toast({
            title: 'Policy created',
            description: `${policy.name} is now available.`,
          });
          openPolicyDrawer(policy.id);
        },
        onError: error => {
          toast({
            title: 'Failed to create policy',
            description: error instanceof Error ? error.message : 'Unknown error',
            variant: 'destructive',
          });
        },
      });
      return;
    }

    if (!editingPolicyId) {
      return;
    }

    updateMutation.mutate(
      { id: editingPolicyId, payload: payload as UpdatePolicyRequest },
      {
        onSuccess: policy => {
          setWizardOpen(false);
          setWizardStep(1);
          setEditingPolicyId(null);
          toast({
            title: 'Policy updated',
            description: `${policy.name} version ${policy.version} saved.`,
          });
        },
        onError: error => {
          toast({
            title: 'Failed to update policy',
            description: error instanceof Error ? error.message : 'Unknown error',
            variant: 'destructive',
          });
        },
      },
    );
  };

  const previewWizardTargeting = () => {
    previewMutation.mutate(
      {
        environment: draft.environment,
        targeting: draft.targeting,
      },
      {
        onSuccess: result => {
          toast({
            title: 'Preview generated',
            description: `${result.pipelines} pipelines, ${result.stages} stages, ${result.handlers} handlers currently match.`,
          });
        },
        onError: error => {
          toast({
            title: 'Preview failed',
            description: error instanceof Error ? error.message : 'Unknown error',
            variant: 'destructive',
          });
        },
      },
    );
  };

  const previewAppliedTargeting = () => {
    if (!selectedPolicy) {
      return;
    }

    previewMutation.mutate(
      {
        environment: selectedPolicy.environment,
        targeting: selectedPolicy.targeting,
      },
      {
        onSuccess: result => {
          setAppliedPreview(result);
        },
      },
    );
  };

  const isWizardBusy =
    createMutation.isPending ||
    updateMutation.isPending ||
    previewMutation.isPending;

  const canContinue = isDraftStepValid(draft, wizardStep);

  return (
    <div className="flex flex-col min-h-screen">
      <AppHeader
        title="Action Policies"
        subtitle="Operational rules for retries, rate limits, timeouts, and circuit breakers"
      />

      <div className="flex-1 space-y-6 p-6">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="text-sm text-muted-foreground">
            Manage execution guardrails and enforcement behavior across pipelines.
          </div>
          <div className="flex items-center gap-2">
            <Select value={range} onValueChange={value => setRange(value as PolicyRange)}>
              <SelectTrigger className="w-[150px]">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {rangeOptions.map(option => (
                  <SelectItem key={option.value} value={option.value}>
                    {option.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Button className="gap-2" onClick={openCreateWizard}>
              <Plus className="h-4 w-4" />
              New Policy
            </Button>
          </div>
        </div>

        <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
          {insightsQuery.isLoading ? (
            Array.from({ length: 4 }).map((_, index) => (
              <Skeleton key={index} className="h-[120px] w-full rounded-xl" />
            ))
          ) : (
            <>
              <KpiCard title="Active policies" value={summary.activePoliciesCount} icon={CheckCircle2} />
              <KpiCard title="Policies triggered" value={summary.policiesTriggered} subtitle={`Range: ${rangeOptions.find(o => o.value === range)?.label}`} icon={Zap} />
              <KpiCard title="Actions blocked/throttled" value={summary.actionsBlocked} icon={AlertTriangle} />
              <KpiCard title="Top policy by triggers" value={summary.topPolicy} icon={Shield} />
            </>
          )}
        </div>

        <div className="rounded-xl border border-border bg-card">
          <div className="flex flex-wrap items-center gap-2 border-b border-border p-4">
            <Input
              value={search}
              onChange={event => setSearch(event.target.value)}
              placeholder="Search by name, tag, pipeline, or stage"
              className="min-w-[240px] flex-1"
            />

            <Select value={typeFilter} onValueChange={value => setTypeFilter(value as PolicyType | 'all')}>
              <SelectTrigger className="w-[170px]">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {policyTypeFilterOptions.map(option => (
                  <SelectItem key={option.value} value={option.value}>
                    {option.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>

            <Select value={statusFilter} onValueChange={value => setStatusFilter(value as PolicyStatus | 'all')}>
              <SelectTrigger className="w-[160px]">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {policyStatusOptions.map(option => (
                  <SelectItem key={option.value} value={option.value}>
                    {option.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>

            <Select value={envFilter} onValueChange={value => setEnvFilter(value as PolicyEnvironment | 'all')}>
              <SelectTrigger className="w-[150px]">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">All envs</SelectItem>
                <SelectItem value="prod">Prod</SelectItem>
                <SelectItem value="staging">Staging</SelectItem>
                <SelectItem value="dev">Dev</SelectItem>
              </SelectContent>
            </Select>

            <Select value={pipelineFilter} onValueChange={value => setPipelineFilter(value)}>
              <SelectTrigger className="w-[190px]">
                <SelectValue placeholder="Pipeline" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">All pipelines</SelectItem>
                {pipelineOptions.map(option => (
                  <SelectItem key={option.id} value={option.id}>
                    {option.name || `Pipeline ${option.id}`}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>

            <Select value={sortBy} onValueChange={value => setSortBy(value as 'triggers' | 'lastTriggered' | 'updatedAt')}>
              <SelectTrigger className="w-[150px]">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {sortOptions.map(option => (
                  <SelectItem key={option.value} value={option.value}>
                    {option.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>

            <Button
              variant="outline"
              onClick={() => setSortDir(previous => (previous === 'desc' ? 'asc' : 'desc'))}
            >
              {sortDir === 'desc' ? 'Desc' : 'Asc'}
            </Button>
          </div>

          {policiesQuery.isLoading ? (
            <div className="space-y-2 p-4">
              {Array.from({ length: 8 }).map((_, index) => (
                <Skeleton key={index} className="h-12 w-full" />
              ))}
            </div>
          ) : policiesQuery.error ? (
            <div className="p-10 text-center">
              <p className="font-medium text-destructive">Failed to load policies</p>
              <p className="text-sm text-muted-foreground">
                {policiesQuery.error instanceof Error ? policiesQuery.error.message : 'Unknown error'}
              </p>
            </div>
          ) : policies.length === 0 ? (
            <div className="p-10 text-center">
              <p className="text-base font-medium text-foreground">No policies yet</p>
              <p className="mt-1 text-sm text-muted-foreground">
                Create operational rules to guard retries, throughput, timeouts, and dependency stability.
              </p>
              <Button className="mt-4 gap-2" onClick={openCreateWizard}>
                <Plus className="h-4 w-4" />
                Create your first policy
              </Button>
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>Type</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Scope / Target</TableHead>
                  <TableHead>Last triggered</TableHead>
                  <TableHead>Triggers</TableHead>
                  <TableHead>Updated by</TableHead>
                  <TableHead className="w-[64px] text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {policies.map(policy => (
                  <TableRow
                    key={policy.id}
                    className="cursor-pointer"
                    onClick={() => openPolicyDrawer(policy.id)}
                  >
                    <TableCell>
                      <div className="font-medium text-foreground">{policy.name}</div>
                      {policy.description ? (
                        <div className="truncate text-xs text-muted-foreground">{policy.description}</div>
                      ) : null}
                    </TableCell>
                    <TableCell>
                      <Badge variant="outline">{policyTypeLabel(policy.type)}</Badge>
                    </TableCell>
                    <TableCell>
                      <StatusBadge status={policyStatusVariant(policy.status)}>
                        {policyStatusLabel(policy.status)}
                      </StatusBadge>
                    </TableCell>
                    <TableCell className="text-sm text-muted-foreground">{scopeSummary(policy)}</TableCell>
                    <TableCell className="text-sm text-muted-foreground">{renderTimestamp(policy.lastTriggeredAt)}</TableCell>
                    <TableCell className="font-medium">{policy.triggerCountInRange}</TableCell>
                    <TableCell className="text-sm text-muted-foreground">
                      {policy.updatedBy} · {format(new Date(policy.updatedAt), 'MMM d, HH:mm')}
                    </TableCell>
                    <TableCell className="text-right" onClick={event => event.stopPropagation()}>
                      <DropdownMenu>
                        <DropdownMenuTrigger asChild>
                          <Button variant="ghost" size="icon" className="h-8 w-8">
                            <MoreHorizontal className="h-4 w-4" />
                          </Button>
                        </DropdownMenuTrigger>
                        <DropdownMenuContent align="end">
                          <DropdownMenuItem onClick={() => openEditWizard(policy)}>
                            Edit
                          </DropdownMenuItem>
                          <DropdownMenuItem onClick={() => runDuplicatePolicy(policy)}>
                            Duplicate
                          </DropdownMenuItem>
                          <DropdownMenuSeparator />
                          {policy.status === 'paused' ? (
                            <DropdownMenuItem onClick={() => executeStatusAction(policy, 'resume')}>
                              Resume
                            </DropdownMenuItem>
                          ) : policy.status === 'active' ? (
                            <DropdownMenuItem onClick={() => executeStatusAction(policy, 'pause')}>
                              Pause
                            </DropdownMenuItem>
                          ) : null}
                          {policy.status === 'disabled' ? (
                            <DropdownMenuItem onClick={() => executeStatusAction(policy, 'enable')}>
                              Enable
                            </DropdownMenuItem>
                          ) : (
                            <DropdownMenuItem onClick={() => executeStatusAction(policy, 'disable')}>
                              Disable
                            </DropdownMenuItem>
                          )}
                          <DropdownMenuSeparator />
                          <DropdownMenuItem className="text-destructive" onClick={() => runDeletePolicy(policy)}>
                            <Trash2 className="mr-2 h-4 w-4" />
                            Delete
                          </DropdownMenuItem>
                        </DropdownMenuContent>
                      </DropdownMenu>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </div>

        <Collapsible open={helpOpen} onOpenChange={setHelpOpen} className="rounded-lg border border-border bg-muted/20">
          <CollapsibleTrigger asChild>
            <button className="flex w-full items-center justify-between px-4 py-3 text-left text-sm font-medium text-foreground">
              How it works
              <ChevronDown className={`h-4 w-4 transition-transform ${helpOpen ? 'rotate-180' : ''}`} />
            </button>
          </CollapsibleTrigger>
          <CollapsibleContent className="border-t border-border px-4 py-3 text-sm text-muted-foreground">
            <ul className="list-disc space-y-1 pl-4">
              <li>Policies apply at runtime based on environment and targeting criteria (pipelines, stages, handlers, and tags).</li>
              <li>When a step matches a policy, Pipelogiq annotates step details with the applied policy and reason.</li>
              <li>Trigger activity feeds this page’s counters and audit trail for fast operational troubleshooting.</li>
            </ul>
          </CollapsibleContent>
        </Collapsible>
      </div>

      <Sheet
        open={drawerOpen}
        onOpenChange={open => {
          setDrawerOpen(open);
          if (!open) {
            setAppliedPreview(null);
            setSelectedPolicyId(null);
          }
        }}
      >
        <SheetContent side="right" className="w-[min(92vw,820px)] max-w-none p-0">
          {selectedPolicy ? (
            <>
              <SheetHeader className="border-b border-border px-6 py-4">
                <SheetTitle className="pr-12">{selectedPolicy.name}</SheetTitle>
                <SheetDescription>{selectedPolicy.description || 'No description provided'}</SheetDescription>

                <div className="flex flex-wrap items-center gap-2 pt-2">
                  <Badge variant="outline">{policyTypeLabel(selectedPolicy.type)}</Badge>
                  <StatusBadge status={policyStatusVariant(selectedPolicy.status)}>
                    {policyStatusLabel(selectedPolicy.status)}
                  </StatusBadge>
                  <span className="text-xs text-muted-foreground">
                    Version {selectedPolicy.version} · Updated by {selectedPolicy.updatedBy} at {format(new Date(selectedPolicy.updatedAt), 'MMM d, HH:mm')}
                  </span>
                </div>

                <div className="flex flex-wrap items-center gap-2 pt-2">
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => openEditWizard(selectedPolicy)}
                  >
                    Edit
                  </Button>

                  {selectedPolicy.status === 'paused' ? (
                    <Button size="sm" onClick={() => executeStatusAction(selectedPolicy, 'resume')}>
                      <Play className="mr-2 h-4 w-4" />
                      Resume
                    </Button>
                  ) : selectedPolicy.status === 'active' ? (
                    <Button variant="outline" size="sm" onClick={() => executeStatusAction(selectedPolicy, 'pause')}>
                      <Pause className="mr-2 h-4 w-4" />
                      Pause
                    </Button>
                  ) : (
                    <Button size="sm" onClick={() => executeStatusAction(selectedPolicy, 'enable')}>
                      Enable
                    </Button>
                  )}

                  {selectedPolicy.status !== 'disabled' ? (
                    <Button variant="outline" size="sm" onClick={() => executeStatusAction(selectedPolicy, 'disable')}>
                      Disable
                    </Button>
                  ) : null}
                </div>
              </SheetHeader>

              <Tabs defaultValue="overview" className="flex h-[calc(100vh-180px)] flex-col">
                <div className="border-b border-border px-4">
                  <TabsList className="h-11 rounded-none bg-transparent">
                    <TabsTrigger value="overview">Overview</TabsTrigger>
                    <TabsTrigger value="rules">Rules</TabsTrigger>
                    <TabsTrigger value="applied">Applied To</TabsTrigger>
                    <TabsTrigger value="audit">Activity / Audit</TabsTrigger>
                  </TabsList>
                </div>

                <ScrollArea className="flex-1">
                  <TabsContent value="overview" className="space-y-4 px-6 py-5">
                    <div className="grid gap-4 md:grid-cols-2">
                      <div className="rounded-lg border border-border p-4">
                        <p className="text-xs uppercase tracking-wide text-muted-foreground">Effect summary</p>
                        <p className="mt-2 text-sm text-foreground">{humanizePolicyEffect(selectedPolicy)}</p>
                      </div>

                      <div className="rounded-lg border border-border p-4">
                        <p className="text-xs uppercase tracking-wide text-muted-foreground">Trigger stats</p>
                        <p className="mt-2 text-sm text-foreground">
                          Last triggered: {renderTimestamp(selectedPolicy.lastTriggeredAt)}
                        </p>
                        <p className="text-sm text-foreground">
                          Trigger count ({rangeOptions.find(option => option.value === range)?.label}): {selectedPolicy.triggerCountInRange}
                        </p>
                      </div>
                    </div>

                    <div className="rounded-lg border border-border p-4">
                      <p className="text-xs uppercase tracking-wide text-muted-foreground">Where applied</p>
                      <p className="mt-2 text-sm text-foreground">{scopeSummary(selectedPolicy)}</p>
                    </div>
                  </TabsContent>

                  <TabsContent value="rules" className="space-y-3 px-6 py-5">
                    {selectedPolicy.type === 'rate_limit' ? (
                      <RuleList
                        rows={[
                          ['Limit', String(selectedPolicy.rule.limit)],
                          ['Window', `${selectedPolicy.rule.windowSeconds}s`],
                          ['Keying', selectedPolicy.rule.keyBy],
                          ['Burst', selectedPolicy.rule.burst ? String(selectedPolicy.rule.burst) : '—'],
                        ]}
                      />
                    ) : null}

                    {selectedPolicy.type === 'retry' ? (
                      <RuleList
                        rows={[
                          ['Max attempts', String(selectedPolicy.rule.maxAttempts)],
                          ['Backoff strategy', selectedPolicy.rule.backoff],
                          ['Base delay', `${selectedPolicy.rule.baseDelayMs}ms`],
                          ['Max delay', selectedPolicy.rule.maxDelayMs ? `${selectedPolicy.rule.maxDelayMs}ms` : '—'],
                          ['Jitter', selectedPolicy.rule.jitter ? 'Enabled' : 'Disabled'],
                          ['Retryable HTTP status', (selectedPolicy.rule.retryOn?.httpStatus ?? []).join(', ') || '—'],
                          ['Retryable error codes', (selectedPolicy.rule.retryOn?.errorCodes ?? []).join(', ') || '—'],
                        ]}
                      />
                    ) : null}

                    {selectedPolicy.type === 'timeout' ? (
                      <RuleList
                        rows={[
                          ['Duration', `${selectedPolicy.rule.timeoutMs}ms`],
                          ['Scope', selectedPolicy.rule.appliesTo === 'step' ? 'Per step' : 'Per external call'],
                        ]}
                      />
                    ) : null}

                    {selectedPolicy.type === 'circuit_breaker' ? (
                      <RuleList
                        rows={[
                          ['Failure threshold', String(selectedPolicy.rule.failureThreshold)],
                          ['Rolling window', `${selectedPolicy.rule.windowSeconds}s`],
                          ['Open duration', `${selectedPolicy.rule.openSeconds}s`],
                          ['Half-open max calls', String(selectedPolicy.rule.halfOpenMaxCalls)],
                        ]}
                      />
                    ) : null}
                  </TabsContent>

                  <TabsContent value="applied" className="space-y-4 px-6 py-5">
                    <div className="rounded-lg border border-border p-4">
                      <p className="text-xs uppercase tracking-wide text-muted-foreground">Environment</p>
                      <p className="mt-2 text-sm text-foreground">{selectedPolicy.environment}</p>
                    </div>

                    <div className="rounded-lg border border-border p-4">
                      <p className="text-xs uppercase tracking-wide text-muted-foreground">Targeting</p>
                      <div className="mt-3 space-y-2 text-sm">
                        <TargetRow title="Pipelines" values={selectedPolicy.targeting.pipelines} />
                        <TargetRow title="Stages" values={selectedPolicy.targeting.stages} />
                        <TargetRow title="Handlers" values={selectedPolicy.targeting.handlers} />
                        <TargetRow title="Tags include" values={selectedPolicy.targeting.tagsInclude} />
                        <TargetRow title="Tags exclude" values={selectedPolicy.targeting.tagsExclude} />
                      </div>

                      <div className="mt-4 flex flex-wrap items-center gap-2">
                        <Button variant="outline" size="sm" onClick={previewAppliedTargeting}>
                          Preview matches
                        </Button>
                        <Button variant="outline" size="sm" onClick={() => openEditWizard(selectedPolicy, 3)}>
                          Edit targeting
                        </Button>
                      </div>

                      {appliedPreview ? (
                        <div className="mt-3 rounded-md bg-muted/60 px-3 py-2 text-sm text-foreground">
                          {appliedPreview.pipelines} pipelines, {appliedPreview.stages} stages, {appliedPreview.handlers} handlers currently match.
                        </div>
                      ) : null}
                    </div>
                  </TabsContent>

                  <TabsContent value="audit" className="space-y-4 px-6 py-5">
                    <p className="text-sm text-muted-foreground">
                      When a step matches a policy, Pipelogiq will annotate the step details with the applied policy.
                    </p>

                    {auditQuery.isLoading ? (
                      <div className="space-y-2">
                        {Array.from({ length: 5 }).map((_, index) => (
                          <Skeleton key={index} className="h-12 w-full" />
                        ))}
                      </div>
                    ) : (auditQuery.data?.events ?? []).length === 0 ? (
                      <div className="rounded-lg border border-border p-4 text-sm text-muted-foreground">
                        No audit events yet.
                      </div>
                    ) : (
                      <div className="space-y-3">
                        {(auditQuery.data?.events ?? []).map(event => {
                          const details = event.details ?? {};
                          const reason = typeof details.reason === 'string' ? details.reason : null;
                          const executionId =
                            typeof details.executionId === 'number' || typeof details.executionId === 'string'
                              ? String(details.executionId)
                              : null;

                          return (
                            <div key={event.id} className="rounded-lg border border-border p-4">
                              <div className="flex flex-wrap items-center justify-between gap-2">
                                <p className="text-sm font-medium text-foreground">{eventTitle(event)}</p>
                                <span className="text-xs text-muted-foreground">{formatAuditTime(event.ts)}</span>
                              </div>
                              <p className="mt-1 text-xs text-muted-foreground">Actor: {event.actor}</p>
                              {reason ? <p className="mt-1 text-sm text-foreground">Reason: {reason}</p> : null}
                              {executionId ? (
                                <a
                                  href={`/executions?executionId=${encodeURIComponent(executionId)}`}
                                  className="mt-2 inline-block text-sm text-primary underline-offset-4 hover:underline"
                                >
                                  Open execution
                                </a>
                              ) : null}
                            </div>
                          );
                        })}
                      </div>
                    )}
                  </TabsContent>
                </ScrollArea>
              </Tabs>
            </>
          ) : policyDetailQuery.isLoading ? (
            <div className="space-y-3 p-6">
              <Skeleton className="h-6 w-2/3" />
              <Skeleton className="h-4 w-full" />
              <Skeleton className="h-[300px] w-full" />
            </div>
          ) : (
            <div className="p-6 text-sm text-muted-foreground">Select a policy to inspect details.</div>
          )}
        </SheetContent>
      </Sheet>

      <Dialog
        open={wizardOpen}
        onOpenChange={open => {
          setWizardOpen(open);
          if (!open) {
            setWizardStep(1);
            setEditingPolicyId(null);
            if (wizardMode === 'create') {
              setDraft(createDefaultDraft());
            }
          }
        }}
      >
        <DialogContent className="max-h-[92vh] max-w-5xl overflow-y-auto p-0">
          <DialogHeader className="border-b border-border px-6 py-4">
            <DialogTitle>{wizardMode === 'create' ? 'New Policy' : 'Edit Policy'}</DialogTitle>
            <DialogDescription>
              Step {wizardStep} of 4 · {wizardMode === 'create' ? 'Create a new action policy' : 'Update policy definition'}
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-5 px-6 py-5">
            <div className="flex items-center gap-2">
              {[1, 2, 3, 4].map(step => (
                <div key={step} className="flex items-center gap-2">
                  <div
                    className={`flex h-8 w-8 items-center justify-center rounded-full text-xs font-semibold ${
                      step <= wizardStep ? 'bg-primary text-primary-foreground' : 'bg-muted text-muted-foreground'
                    }`}
                  >
                    {step}
                  </div>
                  {step < 4 ? <div className="h-px w-10 bg-border" /> : null}
                </div>
              ))}
            </div>

            {wizardStep === 1 ? (
              <div className="grid gap-3 md:grid-cols-2">
                {policyTypes.map(type => {
                  const Icon = type.icon;
                  const selected = draft.type === type.value;

                  return (
                    <button
                      key={type.value}
                      type="button"
                      className={`rounded-xl border p-4 text-left transition-colors ${
                        selected ? 'border-primary bg-primary/5' : 'border-border hover:border-primary/40'
                      }`}
                      onClick={() => setDraft(previous => ({ ...previous, type: type.value }))}
                    >
                      <div className="flex items-center gap-2">
                        <Icon className="h-4 w-4 text-primary" />
                        <p className="font-medium text-foreground">{type.label}</p>
                      </div>
                      <p className="mt-2 text-sm text-muted-foreground">{type.description}</p>
                    </button>
                  );
                })}
              </div>
            ) : null}

            {wizardStep === 2 ? (
              <div className="space-y-4">
                <div className="grid gap-4 md:grid-cols-2">
                  <div className="space-y-2">
                    <Label htmlFor="policy-name">Policy name</Label>
                    <Input
                      id="policy-name"
                      value={draft.name}
                      onChange={event => setDraft(previous => ({ ...previous, name: event.target.value }))}
                      placeholder="e.g. External API retry guard"
                    />
                  </div>

                  <div className="space-y-2">
                    <Label htmlFor="policy-status">Initial status</Label>
                    <Select
                      value={draft.status}
                      onValueChange={value => setDraft(previous => ({ ...previous, status: value as PolicyStatus }))}
                    >
                      <SelectTrigger id="policy-status">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="active">Active</SelectItem>
                        <SelectItem value="paused">Paused</SelectItem>
                        <SelectItem value="disabled">Disabled</SelectItem>
                      </SelectContent>
                    </Select>
                  </div>
                </div>

                <div className="space-y-2">
                  <Label htmlFor="policy-description">Description</Label>
                  <Textarea
                    id="policy-description"
                    value={draft.description}
                    onChange={event => setDraft(previous => ({ ...previous, description: event.target.value }))}
                    placeholder="Describe intent and impact of this policy."
                    rows={3}
                  />
                </div>

                {draft.type === 'rate_limit' ? (
                  <div className="grid gap-4 md:grid-cols-2">
                    <NumberField
                      label="Limit"
                      value={draft.rateLimitRule.limit}
                      onChange={value =>
                        setDraft(previous => ({
                          ...previous,
                          rateLimitRule: { ...previous.rateLimitRule, limit: value },
                        }))
                      }
                    />
                    <NumberField
                      label="Window (seconds)"
                      value={draft.rateLimitRule.windowSeconds}
                      onChange={value =>
                        setDraft(previous => ({
                          ...previous,
                          rateLimitRule: { ...previous.rateLimitRule, windowSeconds: value },
                        }))
                      }
                    />

                    <div className="space-y-2">
                      <Label>Key by</Label>
                      <Select
                        value={draft.rateLimitRule.keyBy}
                        onValueChange={value =>
                          setDraft(previous => ({
                            ...previous,
                            rateLimitRule: {
                              ...previous.rateLimitRule,
                              keyBy: value as RateLimitRule['keyBy'],
                            },
                          }))
                        }
                      >
                        <SelectTrigger>
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          <SelectItem value="global">Global</SelectItem>
                          <SelectItem value="tenant">Per tenant</SelectItem>
                          <SelectItem value="user">Per user</SelectItem>
                          <SelectItem value="custom">Custom key</SelectItem>
                        </SelectContent>
                      </Select>
                    </div>

                    <NumberField
                      label="Burst (optional)"
                      value={draft.rateLimitRule.burst ?? 0}
                      onChange={value =>
                        setDraft(previous => ({
                          ...previous,
                          rateLimitRule: {
                            ...previous.rateLimitRule,
                            burst: value > 0 ? value : undefined,
                          },
                        }))
                      }
                    />
                  </div>
                ) : null}

                {draft.type === 'retry' ? (
                  <div className="space-y-4">
                    <div className="grid gap-4 md:grid-cols-2">
                      <NumberField
                        label="Max attempts"
                        value={draft.retryRule.maxAttempts}
                        onChange={value =>
                          setDraft(previous => ({
                            ...previous,
                            retryRule: { ...previous.retryRule, maxAttempts: value },
                          }))
                        }
                      />

                      <div className="space-y-2">
                        <Label>Backoff strategy</Label>
                        <Select
                          value={draft.retryRule.backoff}
                          onValueChange={value =>
                            setDraft(previous => ({
                              ...previous,
                              retryRule: {
                                ...previous.retryRule,
                                backoff: value as RetryRule['backoff'],
                              },
                            }))
                          }
                        >
                          <SelectTrigger>
                            <SelectValue />
                          </SelectTrigger>
                          <SelectContent>
                            <SelectItem value="fixed">Fixed</SelectItem>
                            <SelectItem value="exponential">Exponential</SelectItem>
                          </SelectContent>
                        </Select>
                      </div>

                      <NumberField
                        label="Base delay (ms)"
                        value={draft.retryRule.baseDelayMs}
                        onChange={value =>
                          setDraft(previous => ({
                            ...previous,
                            retryRule: { ...previous.retryRule, baseDelayMs: value },
                          }))
                        }
                      />

                      <NumberField
                        label="Max delay (ms)"
                        value={draft.retryRule.maxDelayMs ?? 0}
                        onChange={value =>
                          setDraft(previous => ({
                            ...previous,
                            retryRule: {
                              ...previous.retryRule,
                              maxDelayMs: value > 0 ? value : undefined,
                            },
                          }))
                        }
                      />
                    </div>

                    <div className="flex items-center justify-between rounded-md border border-border px-3 py-2">
                      <div>
                        <p className="text-sm font-medium text-foreground">Jitter</p>
                        <p className="text-xs text-muted-foreground">Randomize delay to reduce synchronized retries</p>
                      </div>
                      <Switch
                        checked={Boolean(draft.retryRule.jitter)}
                        onCheckedChange={checked =>
                          setDraft(previous => ({
                            ...previous,
                            retryRule: {
                              ...previous.retryRule,
                              jitter: checked,
                            },
                          }))
                        }
                      />
                    </div>

                    <div className="grid gap-4 md:grid-cols-2">
                      <div className="space-y-2">
                        <Label>Retryable HTTP status (comma separated)</Label>
                        <Input
                          value={(draft.retryRule.retryOn?.httpStatus ?? []).join(', ')}
                          onChange={event =>
                            setDraft(previous => ({
                              ...previous,
                              retryRule: {
                                ...previous.retryRule,
                                retryOn: {
                                  ...previous.retryRule.retryOn,
                                  httpStatus: parseCommaSeparatedNumbers(event.target.value),
                                },
                              },
                            }))
                          }
                          placeholder="429, 500, 503"
                        />
                      </div>

                      <div className="space-y-2">
                        <Label>Retryable error codes (comma separated)</Label>
                        <Input
                          value={(draft.retryRule.retryOn?.errorCodes ?? []).join(', ')}
                          onChange={event =>
                            setDraft(previous => ({
                              ...previous,
                              retryRule: {
                                ...previous.retryRule,
                                retryOn: {
                                  ...previous.retryRule.retryOn,
                                  errorCodes: parseCommaSeparatedStrings(event.target.value),
                                },
                              },
                            }))
                          }
                          placeholder="ETIMEDOUT, ECONNRESET"
                        />
                      </div>
                    </div>
                  </div>
                ) : null}

                {draft.type === 'timeout' ? (
                  <div className="grid gap-4 md:grid-cols-2">
                    <NumberField
                      label="Timeout (ms)"
                      value={draft.timeoutRule.timeoutMs}
                      onChange={value =>
                        setDraft(previous => ({
                          ...previous,
                          timeoutRule: {
                            ...previous.timeoutRule,
                            timeoutMs: value,
                          },
                        }))
                      }
                    />

                    <div className="space-y-2">
                      <Label>Applies to</Label>
                      <Select
                        value={draft.timeoutRule.appliesTo}
                        onValueChange={value =>
                          setDraft(previous => ({
                            ...previous,
                            timeoutRule: {
                              ...previous.timeoutRule,
                              appliesTo: value as TimeoutRule['appliesTo'],
                            },
                          }))
                        }
                      >
                        <SelectTrigger>
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          <SelectItem value="step">Per step</SelectItem>
                          <SelectItem value="external_call">Per external call</SelectItem>
                        </SelectContent>
                      </Select>
                    </div>
                  </div>
                ) : null}

                {draft.type === 'circuit_breaker' ? (
                  <div className="grid gap-4 md:grid-cols-2">
                    <NumberField
                      label="Failure threshold"
                      value={draft.circuitBreakerRule.failureThreshold}
                      onChange={value =>
                        setDraft(previous => ({
                          ...previous,
                          circuitBreakerRule: {
                            ...previous.circuitBreakerRule,
                            failureThreshold: value,
                          },
                        }))
                      }
                    />

                    <NumberField
                      label="Rolling window (seconds)"
                      value={draft.circuitBreakerRule.windowSeconds}
                      onChange={value =>
                        setDraft(previous => ({
                          ...previous,
                          circuitBreakerRule: {
                            ...previous.circuitBreakerRule,
                            windowSeconds: value,
                          },
                        }))
                      }
                    />

                    <NumberField
                      label="Open duration (seconds)"
                      value={draft.circuitBreakerRule.openSeconds}
                      onChange={value =>
                        setDraft(previous => ({
                          ...previous,
                          circuitBreakerRule: {
                            ...previous.circuitBreakerRule,
                            openSeconds: value,
                          },
                        }))
                      }
                    />

                    <NumberField
                      label="Half-open max calls"
                      value={draft.circuitBreakerRule.halfOpenMaxCalls}
                      onChange={value =>
                        setDraft(previous => ({
                          ...previous,
                          circuitBreakerRule: {
                            ...previous.circuitBreakerRule,
                            halfOpenMaxCalls: value,
                          },
                        }))
                      }
                    />
                  </div>
                ) : null}
              </div>
            ) : null}

            {wizardStep === 3 ? (
              <div className="space-y-4">
                <div className="grid gap-4 md:grid-cols-2">
                  <div className="space-y-2">
                    <Label>Environment</Label>
                    <Select
                      value={draft.environment}
                      onValueChange={value =>
                        setDraft(previous => ({
                          ...previous,
                          environment: value as PolicyEnvironment,
                        }))
                      }
                    >
                      <SelectTrigger>
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="all">All</SelectItem>
                        <SelectItem value="prod">Prod</SelectItem>
                        <SelectItem value="staging">Staging</SelectItem>
                        <SelectItem value="dev">Dev</SelectItem>
                      </SelectContent>
                    </Select>
                  </div>
                </div>

                <div className="grid gap-4 md:grid-cols-3">
                  <MultiSelectField
                    label="Pipelines"
                    values={draft.targeting.pipelines}
                    options={pipelineOptions.map(option => ({ value: option.id, label: option.name || `Pipeline ${option.id}` }))}
                    onChange={values =>
                      setDraft(previous => ({
                        ...previous,
                        targeting: { ...previous.targeting, pipelines: values },
                      }))
                    }
                  />

                  <MultiSelectField
                    label="Stages"
                    values={draft.targeting.stages}
                    options={stageOptions.map(stage => ({ value: stage, label: stage }))}
                    onChange={values =>
                      setDraft(previous => ({
                        ...previous,
                        targeting: { ...previous.targeting, stages: values },
                      }))
                    }
                  />

                  <MultiSelectField
                    label="Handlers"
                    values={draft.targeting.handlers}
                    options={handlerOptions.map(handler => ({ value: handler, label: handler }))}
                    onChange={values =>
                      setDraft(previous => ({
                        ...previous,
                        targeting: { ...previous.targeting, handlers: values },
                      }))
                    }
                  />
                </div>

                <div className="grid gap-4 md:grid-cols-2">
                  <div className="space-y-2">
                    <Label>Tags include (comma separated)</Label>
                    <Input
                      value={draft.targeting.tagsInclude.join(', ')}
                      onChange={event =>
                        setDraft(previous => ({
                          ...previous,
                          targeting: {
                            ...previous.targeting,
                            tagsInclude: parseCommaSeparatedStrings(event.target.value),
                          },
                        }))
                      }
                      placeholder={tagOptions.slice(0, 4).join(', ') || 'critical, email'}
                    />
                  </div>

                  <div className="space-y-2">
                    <Label>Tags exclude (comma separated)</Label>
                    <Input
                      value={draft.targeting.tagsExclude.join(', ')}
                      onChange={event =>
                        setDraft(previous => ({
                          ...previous,
                          targeting: {
                            ...previous.targeting,
                            tagsExclude: parseCommaSeparatedStrings(event.target.value),
                          },
                        }))
                      }
                      placeholder="maintenance"
                    />
                  </div>
                </div>

                <div className="rounded-md border border-border bg-muted/30 p-3">
                  <p className="text-sm text-muted-foreground">
                    Preview matches estimates current impact using existing pipelines and stage metadata.
                  </p>
                  <Button className="mt-3" variant="outline" onClick={previewWizardTargeting} disabled={previewMutation.isPending}>
                    {previewMutation.isPending ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
                    Preview matches
                  </Button>
                  {previewMutation.data ? (
                    <p className="mt-2 text-sm text-foreground">
                      {previewMutation.data.pipelines} pipelines, {previewMutation.data.stages} stages, {previewMutation.data.handlers} handlers currently match.
                    </p>
                  ) : null}
                </div>
              </div>
            ) : null}

            {wizardStep === 4 ? (
              <div className="space-y-4">
                <div className="rounded-lg border border-border p-4">
                  <p className="text-sm font-medium text-foreground">Review</p>
                  <div className="mt-3 space-y-2 text-sm text-muted-foreground">
                    <p><span className="font-medium text-foreground">Name:</span> {draft.name || 'Untitled policy'}</p>
                    <p><span className="font-medium text-foreground">Type:</span> {policyTypeLabel(draft.type)}</p>
                    <p><span className="font-medium text-foreground">Status:</span> {policyStatusLabel(draft.status)}</p>
                    <p><span className="font-medium text-foreground">Environment:</span> {draft.environment}</p>
                    <p>
                      <span className="font-medium text-foreground">Effect:</span>{' '}
                      {humanizePolicyEffect({
                        id: 'preview',
                        name: draft.name,
                        description: draft.description,
                        type: draft.type,
                        status: draft.status,
                        environment: draft.environment,
                        targeting: draft.targeting,
                        rule: getPolicyRuleFromDraft(draft) as Policy['rule'],
                        createdAt: new Date().toISOString(),
                        createdBy: 'you',
                        updatedAt: new Date().toISOString(),
                        updatedBy: 'you',
                        version: 1,
                      } as Policy)}
                    </p>
                    <p><span className="font-medium text-foreground">Scope:</span> env:{draft.environment}, pipelines:{draft.targeting.pipelines.length}, stages:{draft.targeting.stages.length}, handlers:{draft.targeting.handlers.length}</p>
                  </div>
                </div>
              </div>
            ) : null}
          </div>

          <DialogFooter className="border-t border-border px-6 py-4">
            <div className="flex w-full items-center justify-between">
              <Button
                variant="outline"
                onClick={() => setWizardStep(previous => Math.max(1, previous - 1))}
                disabled={wizardStep === 1 || isWizardBusy}
              >
                Back
              </Button>

              {wizardStep < 4 ? (
                <Button
                  onClick={() => setWizardStep(previous => Math.min(4, previous + 1))}
                  disabled={!canContinue || isWizardBusy}
                >
                  Next
                </Button>
              ) : (
                <Button onClick={submitWizard} disabled={!canContinue || isWizardBusy}>
                  {isWizardBusy ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
                  {wizardMode === 'create' ? 'Create policy' : 'Save changes'}
                </Button>
              )}
            </div>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function RuleList({ rows }: { rows: Array<[string, string]> }) {
  return (
    <div className="rounded-lg border border-border">
      <dl className="divide-y divide-border">
        {rows.map(([label, value]) => (
          <div key={label} className="flex items-center justify-between px-4 py-3 text-sm">
            <dt className="text-muted-foreground">{label}</dt>
            <dd className="font-medium text-foreground">{value || '—'}</dd>
          </div>
        ))}
      </dl>
    </div>
  );
}

function TargetRow({ title, values }: { title: string; values: string[] }) {
  return (
    <div className="grid grid-cols-[120px_1fr] items-start gap-2">
      <p className="text-muted-foreground">{title}</p>
      <div className="flex flex-wrap gap-1">
        {values.length === 0 ? (
          <span className="text-muted-foreground">All</span>
        ) : (
          values.map(value => (
            <Badge key={`${title}-${value}`} variant="secondary">
              {value}
            </Badge>
          ))
        )}
      </div>
    </div>
  );
}

function NumberField({
  label,
  value,
  onChange,
}: {
  label: string;
  value: number;
  onChange: (value: number) => void;
}) {
  return (
    <div className="space-y-2">
      <Label>{label}</Label>
      <Input
        type="number"
        min={0}
        value={Number.isFinite(value) ? value : 0}
        onChange={event => onChange(Number(event.target.value) || 0)}
      />
    </div>
  );
}

function MultiSelectField({
  label,
  values,
  options,
  onChange,
}: {
  label: string;
  values: string[];
  options: Array<{ value: string; label: string }>;
  onChange: (values: string[]) => void;
}) {
  return (
    <div className="space-y-2">
      <Label>{label}</Label>
      <select
        multiple
        className="h-36 w-full rounded-md border border-input bg-background p-2 text-sm"
        value={values}
        onChange={event => {
          const selectedValues = Array.from(event.target.selectedOptions).map(option => option.value);
          onChange(selectedValues);
        }}
      >
        {options.map(option => (
          <option key={option.value} value={option.value}>
            {option.label}
          </option>
        ))}
      </select>
      <p className="text-xs text-muted-foreground">Hold Cmd/Ctrl to select multiple.</p>
    </div>
  );
}
