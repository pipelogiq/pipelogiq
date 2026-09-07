import type { ReactNode } from 'react';
import { ShieldOff } from 'lucide-react';
import { useAuth } from '@/contexts/AuthContext';

/** Renders an explicit refusal instead of letting an admin-only screen show 403s as empty data. */
export function AccessDenied({ what = 'this section' }: { what?: string }) {
  return (
    <div className="flex flex-1 items-center justify-center p-6">
      <div className="max-w-md rounded-xl border border-border bg-card p-8 text-center">
        <ShieldOff className="mx-auto h-10 w-10 text-muted-foreground/60" />
        <h2 className="mt-4 text-lg font-semibold text-foreground">You don't have access to {what}</h2>
        <p className="mt-2 text-sm text-muted-foreground">
          It is limited to administrators. Ask an administrator of your Pipelogiq installation if you need it.
        </p>
      </div>
    </div>
  );
}

export function RequireAdmin({ children, what }: { children: ReactNode; what?: string }) {
  const { isAdmin } = useAuth();
  if (!isAdmin) {
    return <AccessDenied what={what} />;
  }
  return <>{children}</>;
}
