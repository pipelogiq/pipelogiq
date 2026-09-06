/** Mirrors internal/license and internal/api/license.go. */

export type LicenseTier = 'community' | 'pro' | 'enterprise';

export interface FeatureListing {
  name: string;
  description: string;
  /** Unlocked by the current licence. */
  unlocked: boolean;
  /** Has an implementation today. The rest are named so pricing and code agree. */
  available: boolean;
}

export interface LicenseStatus {
  tier: LicenseTier;
  licensed: boolean;
  company?: string;
  engineers?: number;
  features: string[];
  issuedAt?: string;
  expiresAt?: string;
  /** Expired but inside the 14-day grace period: features stay unlocked. */
  inGrace: boolean;
  graceEndsAt?: string;
  /** Why the deployment is Community, or why it is in grace. Safe to show. */
  reason?: string;
  /** Whether the Pro implementations are linked into the running binary. */
  proCompiled: boolean;
  catalogue: FeatureListing[];
}
