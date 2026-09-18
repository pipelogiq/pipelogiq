import { cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { licenseApi } from '@/api/client';
import { LicensePanel } from '@/components/license/LicensePanel';
import type { LicenseStatus } from '@/types/license';

vi.mock('@/api/client', () => ({ licenseApi: { get: vi.fn() } }));

const community: LicenseStatus = {
  tier: 'community',
  licensed: false,
  features: [],
  inGrace: false,
  proCompiled: false,
  reason: 'no licence configured',
  catalogue: [
    { name: 'sso', description: 'Internal SSO description', available: true, unlocked: false },
    { name: 'audit.export', description: 'Internal audit description', available: true, unlocked: false },
    { name: 'retention', description: 'Internal retention description', available: true, unlocked: false },
    { name: 'alerting.oncall', description: 'Unimplemented roadmap feature', available: false, unlocked: false },
  ],
};
const active: LicenseStatus = {
  ...community,
  tier: 'pro',
  licensed: true,
  proCompiled: true,
  company: 'Acme Operations',
  features: ['sso', 'audit.export'],
  expiresAt: '2027-01-30T12:00:00Z',
  reason: undefined,
  catalogue: community.catalogue.map((feature) => ({
    ...feature,
    unlocked: ['sso', 'audit.export'].includes(feature.name),
  })),
};
const clients: QueryClient[] = [];

function setup() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  clients.push(client);
  return render(
    <QueryClientProvider client={client}>
      <LicensePanel />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  vi.resetAllMocks();
  vi.mocked(licenseApi.get).mockResolvedValue(community);
});
afterEach(() => {
  cleanup();
  clients.splice(0).forEach((client) => client.clear());
});

describe('LicensePanel', () => {
  it('keeps Community concise without a marketing or future-feature catalogue', async () => {
    setup();
    expect(await screen.findByRole('heading', { name: 'Community' })).toBeTruthy();
    expect(screen.getByText('No licence required. No expiry.')).toBeTruthy();
    expect(screen.queryByRole('region', { name: 'Licensed features' })).toBeNull();
    expect(screen.queryByText('Pro build')).toBeNull();
    expect(screen.queryByText('no licence configured')).toBeNull();
    expect(screen.queryByText('next release')).toBeNull();
    expect(screen.queryByRole('link')).toBeNull();
    expect(screen.queryByRole('button')).toBeNull();
  });

  it('shows organization, expiry and actual implemented feature availability for an active licence', async () => {
    vi.mocked(licenseApi.get).mockResolvedValue(active);
    setup();
    await screen.findByRole('heading', { name: 'Pro' });
    expect(screen.getByText('Active licence')).toBeTruthy();
    expect(screen.getByText('Acme Operations')).toBeTruthy();
    expect(screen.getByText('Licence expires')).toBeTruthy();
    const features = screen.getByRole('region', { name: 'Licensed features' });
    expect(within(features).getAllByRole('listitem')).toHaveLength(3);
    expect(within(features).getAllByText('Available')).toHaveLength(2);
    expect(within(features).getByText('Not included')).toBeTruthy();
    expect(within(features).getByText('Single sign-on')).toBeTruthy();
    expect(within(features).getByText('Signed audit export')).toBeTruthy();
    expect(within(features).getByText('Data retention')).toBeTruthy();
    expect(screen.queryByText('audit.export')).toBeNull();
    expect(screen.queryByText('Unimplemented roadmap feature')).toBeNull();
    expect(screen.queryByText(/grace period/i)).toBeNull();
  });

  it('shows a grace warning only for an expired licence within its grace period', async () => {
    vi.mocked(licenseApi.get).mockResolvedValue({
      ...active,
      inGrace: true,
      expiresAt: '2026-09-16T12:00:00Z',
      graceEndsAt: '2026-09-30T12:00:00Z',
    });
    setup();
    await screen.findByRole('heading', { name: 'Pro' });
    expect(screen.getByText('Grace period')).toBeTruthy();
    expect(screen.getByRole('status').textContent).toContain('The licence has expired. Its grace period ends on');
    expect(screen.getByText('Licence expired')).toBeTruthy();
    expect(screen.queryByText('Active licence')).toBeNull();
    expect(screen.getAllByText('Available')).toHaveLength(2);
  });

  it('does not infer Community from an API error and retries the request', async () => {
    vi.mocked(licenseApi.get).mockRejectedValueOnce(new Error('Internal sensitive configuration details'));
    setup();
    expect(await screen.findByRole('alert')).toBeTruthy();
    expect(screen.getByText('Licence status is unavailable.')).toBeTruthy();
    expect(screen.queryByRole('heading', { name: 'Community' })).toBeNull();
    expect(screen.queryByText('Internal sensitive configuration details')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    expect(await screen.findByRole('heading', { name: 'Community' })).toBeTruthy();
    expect(licenseApi.get).toHaveBeenCalledTimes(2);
  });

  it('does not advertise functioning paid features for a valid licence in a Community installation', async () => {
    vi.mocked(licenseApi.get).mockResolvedValue({ ...active, proCompiled: false });
    setup();
    await screen.findByRole('heading', { name: 'Community' });
    expect(screen.getByText('Licence valid')).toBeTruthy();
    expect(
      screen.getByText('This Community installation does not support the features of your Pro licence.'),
    ).toBeTruthy();
    expect(screen.queryByText('Available')).toBeNull();
    expect(screen.getAllByText('Unavailable')).toHaveLength(3);
    expect(screen.queryByText(/compiled/)).toBeNull();
  });

  it('omits unimplemented features even when a licence says they are unlocked', async () => {
    vi.mocked(licenseApi.get).mockResolvedValue({
      ...active,
      catalogue: active.catalogue.map((feature) => ({
        ...feature,
        available: feature.name !== 'sso' && feature.available,
        unlocked: true,
      })),
    });
    setup();
    await screen.findByRole('heading', { name: 'Pro' });
    expect(screen.queryByText('Single sign-on')).toBeNull();
    expect(screen.queryByText('alerting.oncall')).toBeNull();
    expect(screen.getAllByText('Available')).toHaveLength(2);
  });

  it.each(['licence token is not valid', 'PIPELOGIQ_LICENSE=secret-token; internal error'])(
    'summarizes verification failure without exposing raw reason %s',
    async (reason) => {
      vi.mocked(licenseApi.get).mockResolvedValue({ ...community, reason });
      setup();
      await screen.findByRole('heading', { name: 'Community' });
      expect(screen.getByText('The configured licence could not be verified.')).toBeTruthy();
      expect(screen.queryByText(reason)).toBeNull();
    },
  );

  it('shows a concise expired-licence notice after grace has ended', async () => {
    vi.mocked(licenseApi.get).mockResolvedValue({
      ...community,
      company: 'Acme Operations',
      expiresAt: '2026-08-01T12:00:00Z',
      reason: 'licence expired on 2026-08-01T12:00:00Z and its grace period ended on 2026-08-15T12:00:00Z',
    });
    setup();
    await screen.findByRole('heading', { name: 'Community' });
    expect(
      screen.getByText('The previous licence has expired. Its licensed features are no longer available.'),
    ).toBeTruthy();
    expect(screen.getByText('Licence expired')).toBeTruthy();
    expect(screen.queryByText('Grace period')).toBeNull();
    expect(screen.queryByText('Available')).toBeNull();
  });
});
