import { AlertTriangle, Check, ExternalLink, Lock } from 'lucide-react';

import { Badge } from '@/components/ui/badge';
import { Skeleton } from '@/components/ui/skeleton';
import { useLicense } from '@/hooks/use-license';
import { cn } from '@/lib/utils';

const PRICING_URL = 'https://pipelogiq.dev/pricing';

const tierLabel: Record<string, string> = {
  community: 'Community',
  pro: 'Pro',
  enterprise: 'Enterprise',
};

function formatDate(iso?: string): string {
  if (!iso) return '—';
  const date = new Date(iso);
  return Number.isNaN(date.getTime()) ? '—' : date.toLocaleDateString();
}

/**
 * LicensePanel shows the tier and the full feature catalogue. Locked features are
 * rendered with an explanation and a link to pricing, never hidden: an operator
 * should be able to see what a licence would give them.
 */
export function LicensePanel() {
  const { data, isLoading, isError } = useLicense();

  if (isLoading) {
    return <Skeleton className="h-64 w-full" />;
  }

  if (isError || !data) {
    // A licence that cannot be read is a Community deployment, not an outage.
    return (
      <div className="rounded-xl border border-border bg-card p-6 text-sm text-muted-foreground">
        Licence status is unavailable. The deployment runs as Community; pipelines
        are unaffected.
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <div className="rounded-xl border border-border bg-card p-6">
        <div className="flex flex-wrap items-center gap-3">
          <h2 className="text-lg font-semibold">{tierLabel[data.tier] ?? data.tier}</h2>
          {data.licensed ? (
            <Badge variant="default">Licensed</Badge>
          ) : (
            <Badge variant="outline">Free tier</Badge>
          )}
          {data.inGrace && (
            <Badge variant="destructive" className="gap-1">
              <AlertTriangle className="h-3 w-3" />
              Expired — grace period
            </Badge>
          )}
        </div>

        {data.inGrace && (
          <p className="mt-3 rounded-lg border border-amber-500/40 bg-amber-500/10 p-3 text-sm">
            This licence expired and its features stay unlocked until{' '}
            <strong>{formatDate(data.graceEndsAt)}</strong>. After that the
            deployment becomes Community.{' '}
            <strong>Pipelines keep running either way.</strong>
          </p>
        )}

        {!data.licensed && data.reason && (
          <p className="mt-3 text-sm text-muted-foreground">{data.reason}</p>
        )}

        <dl className="mt-4 grid grid-cols-2 gap-3 text-sm md:grid-cols-4">
          {data.company && (
            <div>
              <dt className="text-xs uppercase tracking-wide text-muted-foreground">Licensed to</dt>
              <dd className="mt-1">{data.company}</dd>
            </div>
          )}
          {data.engineers ? (
            <div>
              <dt className="text-xs uppercase tracking-wide text-muted-foreground">Engineer band</dt>
              <dd className="mt-1">up to {data.engineers}</dd>
            </div>
          ) : null}
          {data.expiresAt && (
            <div>
              <dt className="text-xs uppercase tracking-wide text-muted-foreground">Expires</dt>
              <dd className="mt-1">{formatDate(data.expiresAt)}</dd>
            </div>
          )}
          <div>
            <dt className="text-xs uppercase tracking-wide text-muted-foreground">Pro build</dt>
            <dd className="mt-1">{data.proCompiled ? 'compiled in' : 'not compiled in'}</dd>
          </div>
        </dl>
      </div>

      <div className="rounded-xl border border-border bg-card">
        <div className="border-b border-border px-6 py-4">
          <h3 className="text-sm font-semibold uppercase tracking-wide text-muted-foreground">
            Features
          </h3>
        </div>
        <ul className="divide-y divide-border">
          {data.catalogue.map((feature) => (
            <li key={feature.name} className="flex items-start gap-3 px-6 py-4">
              <span className="mt-0.5">
                {feature.unlocked ? (
                  <Check className="h-4 w-4 text-emerald-500" />
                ) : (
                  <Lock className="h-4 w-4 text-muted-foreground" />
                )}
              </span>
              <div className="flex-1">
                <div className="flex flex-wrap items-center gap-2">
                  <code className={cn('text-sm', !feature.unlocked && 'text-muted-foreground')}>
                    {feature.name}
                  </code>
                  {!feature.available && <Badge variant="outline">next release</Badge>}
                </div>
                <p className="mt-1 text-sm text-muted-foreground">{feature.description}</p>
              </div>
              {!feature.unlocked && (
                <a
                  href={PRICING_URL}
                  target="_blank"
                  rel="noreferrer"
                  className="flex shrink-0 items-center gap-1 text-sm text-foreground hover:underline"
                >
                  Pricing
                  <ExternalLink className="h-3 w-3" />
                </a>
              )}
            </li>
          ))}
        </ul>
      </div>

      <p className="text-sm text-muted-foreground">
        The core is Apache-2.0 and stays that way. Everything a single developer
        needs to run and debug a pipeline is free; a licence adds what an
        organization needs to govern, comply, and scale. A licence is verified
        offline — Pipelogiq never phones home — and no pipeline ever stops
        because of one.
      </p>
    </div>
  );
}
