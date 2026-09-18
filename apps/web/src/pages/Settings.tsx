import { Outlet, useLocation, useNavigate } from 'react-router-dom';
import { KeyRound, ScrollText, ShieldCheck, Users, BadgeCheck } from 'lucide-react';
import { AppHeader } from '@/components/layout/AppHeader';
import { LicensePanel } from '@/components/license/LicensePanel';
import { PasswordCard } from '@/components/auth/PasswordCard';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { authApi } from '@/api/client';

const sections = [
  { value: 'api-keys', label: 'API Keys', icon: KeyRound },
  { value: 'users', label: 'Users', icon: Users },
  { value: 'audit', label: 'Audit', icon: ScrollText },
  { value: 'licence', label: 'Licence', icon: BadgeCheck },
  { value: 'security', label: 'Security', icon: ShieldCheck },
];

export default function Settings() {
  const { pathname } = useLocation();
  const navigate = useNavigate();
  const activeSection = sections.find(section =>
    pathname === `/settings/${section.value}` || pathname.startsWith(`/settings/${section.value}/`),
  )?.value ?? 'api-keys';

  return (
    <div className="flex min-w-0 flex-col">
      <AppHeader title="Settings" />
      <Tabs value={activeSection} onValueChange={value => navigate(`/settings/${value}`)} activationMode="manual">
        <div className="overflow-x-auto border-b border-border px-4 sm:px-6">
          <TabsList aria-label="Settings sections" className="w-max min-w-full border-b-0">
            {sections.map(section => (
              <TabsTrigger key={section.value} value={section.value} className="gap-2 py-4">
                <section.icon className="h-4 w-4" aria-hidden="true" />
                {section.label}
              </TabsTrigger>
            ))}
          </TabsList>
        </div>
        <TabsContent value={activeSection} className="m-0 p-4 sm:p-6">
          <Outlet />
        </TabsContent>
      </Tabs>
    </div>
  );
}

export function LicenceSettings() {
  return (
    <section className="space-y-4">
      <div>
        <h2 className="text-lg font-semibold">Licence</h2>
        <p className="text-sm text-muted-foreground">Your current plan and available features.</p>
      </div>
      <LicensePanel />
    </section>
  );
}

export function SecuritySettings() {
  return (
    <section className="space-y-4">
      <div>
        <h2 className="text-lg font-semibold">Security</h2>
        <p className="text-sm text-muted-foreground">Manage your password and sign-in security.</p>
      </div>
      <PasswordCard onChange={authApi.changePassword} />
    </section>
  );
}
