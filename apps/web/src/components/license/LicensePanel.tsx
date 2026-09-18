import { AlertTriangle, Check, Minus, RefreshCw } from 'lucide-react';

import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { useLicense } from '@/hooks/use-license';

const tierLabel: Record<string, string> = {
  community: 'Community',
  pro: 'Pro',
  enterprise: 'Enterprise',
};

const featureLabels: Record<string, string> = {
  sso: 'Single sign-on',
  'audit.export': 'Signed audit export',
  retention: 'Data retention',
};

function formatDate(iso?: string): string | undefined {
  if (!iso) return undefined;
  const date = new Date(iso);
  return Number.isNaN(date.getTime())
    ? undefined
    : date.toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' });
}

function licenceIssue(reason?: string): string | undefined {
  if (!reason || reason === 'no licence configured') return undefined;
  if (reason.startsWith('licence expired on ')) {
    return 'The previous licence has expired. Its licensed features are no longer available.';
  }
  if (reason === 'licence not resolved yet') return 'Licence status has not been resolved yet.';
  return 'The configured licence could not be verified.';
}

export function LicensePanel() {
  const { data, isLoading, isError, isFetching, refetch } = useLicense();

  if (isLoading) {
    return <Skeleton role="status" aria-label="Loading licence status" className="h-40 max-w-2xl rounded-xl" />;
  }

  if (isError || !data) {
    return (
      <div role="alert" className="max-w-2xl space-y-3 rounded-xl border border-border bg-card p-5">
        <p className="text-sm font-medium">Licence status is unavailable.</p>
        <Button variant="outline" size="sm" onClick={() => void refetch()} disabled={isFetching}>
          <RefreshCw className="h-4 w-4" />
          Retry
        </Button>
      </div>
    );
  }

  const licenceTier = tierLabel[data.tier] ?? 'Commercial';
  const unsupportedEdition = data.licensed && !data.proCompiled;
  const edition = data.licensed && data.proCompiled ? licenceTier : 'Community';
  const expiresAt = formatDate(data.expiresAt);
  const graceEndsAt = formatDate(data.graceEndsAt);
  const issue = data.licensed ? undefined : licenceIssue(data.reason);
  const features = data.catalogue.filter((feature) => feature.available && Object.hasOwn(featureLabels, feature.name));

  return (
    <div className="max-w-2xl overflow-hidden rounded-xl border border-border bg-card">
      <div className="space-y-4 p-5">
        <div>
          <div className="flex flex-wrap items-center gap-3">
            <h3 className="text-lg font-semibold">{edition}</h3>
            {data.licensed && (
              <Badge variant={data.inGrace ? 'outline' : 'secondary'}>
                {data.inGrace ? 'Grace period' : unsupportedEdition ? 'Licence valid' : 'Active licence'}
              </Badge>
            )}
          </div>
          {!data.licensed && <p className="mt-1 text-sm text-muted-foreground">No licence required. No expiry.</p>}
        </div>

        {unsupportedEdition && (
          <p className="text-sm text-muted-foreground">
            This Community installation does not support the features of your {licenceTier} licence.
          </p>
        )}

        {data.inGrace && (
          <div
            role="status"
            className="flex items-start gap-2 rounded-lg bg-status-warning-bg p-3 text-sm text-status-warning"
          >
            <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
            <p>
              The licence has expired.{' '}
              {graceEndsAt ? (
                <>
                  Its grace period ends on <strong>{graceEndsAt}</strong>.
                </>
              ) : (
                'It is currently in its grace period.'
              )}
            </p>
          </div>
        )}

        {issue && <p className="text-sm text-muted-foreground">{issue}</p>}

        {(data.company || expiresAt) && (
          <dl className="grid gap-4 text-sm sm:grid-cols-2">
            {data.company && (
              <div>
                <dt className="text-muted-foreground">Organization</dt>
                <dd className="mt-1 break-words font-medium">{data.company}</dd>
              </div>
            )}
            {expiresAt && (
              <div>
                <dt className="text-muted-foreground">
                  {data.inGrace || !data.licensed ? 'Licence expired' : 'Licence expires'}
                </dt>
                <dd className="mt-1 font-medium">{expiresAt}</dd>
              </div>
            )}
          </dl>
        )}
      </div>

      {data.licensed && features.length > 0 && (
        <section className="border-t border-border px-5 py-4" aria-label="Licensed features">
          <h4 className="mb-3 text-sm font-medium">Features</h4>
          <ul className="space-y-3">
            {features.map((feature) => {
              const available = data.proCompiled && feature.available && feature.unlocked;
              return (
                <li key={feature.name} className="flex flex-wrap items-center justify-between gap-2 text-sm">
                  <span>{featureLabels[feature.name]}</span>
                  <span
                    className={
                      available
                        ? 'flex items-center gap-1.5 text-status-success'
                        : 'flex items-center gap-1.5 text-muted-foreground'
                    }
                  >
                    {available ? <Check className="h-4 w-4" /> : <Minus className="h-4 w-4" />}
                    {available ? 'Available' : !data.proCompiled ? 'Unavailable' : 'Not included'}
                  </span>
                </li>
              );
            })}
          </ul>
        </section>
      )}
    </div>
  );
}
