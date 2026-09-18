import { authApi } from '@/api/client';
import { AppHeader } from '@/components/layout/AppHeader';
import { PasswordCard } from '@/components/auth/PasswordCard';
import { Badge } from '@/components/ui/badge';
import { useAuth } from '@/contexts/AuthContext';

export default function Account() {
  const { user } = useAuth();

  return (
    <div className="flex flex-col">
      <AppHeader title="My account" subtitle="Your profile and sign-in settings" />
      <div className="flex-1 space-y-6 p-6">
        <section className="rounded-xl border border-border bg-card px-5 py-4">
          <h2 className="font-semibold text-foreground">Profile</h2>
          <dl className="mt-4 grid gap-4 text-sm md:grid-cols-3">
            <div>
              <dt className="text-muted-foreground">Name</dt>
              <dd className="mt-1">{[user?.firstName, user?.lastName].filter(Boolean).join(' ')}</dd>
            </div>
            <div>
              <dt className="text-muted-foreground">Email</dt>
              <dd className="mt-1">{user?.email}</dd>
            </div>
            <div>
              <dt className="text-muted-foreground">Role</dt>
              <dd className="mt-1"><Badge variant="secondary">{user?.role}</Badge></dd>
            </div>
          </dl>
          <p className="mt-4 text-sm text-muted-foreground">Contact an administrator to update your profile or access.</p>
        </section>
        <PasswordCard onChange={authApi.changePassword} />
      </div>
    </div>
  );
}
