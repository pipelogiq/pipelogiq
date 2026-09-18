import { useState, type FormEvent } from 'react';
import { Link } from 'react-router-dom';
import { Check, KeyRound, Loader2, Pencil, Plus, Search, ShieldCheck, Users as UsersIcon } from 'lucide-react';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Checkbox } from '@/components/ui/checkbox';
import { useConfirm } from '@/components/ui/confirm-dialog';
import {
  Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle,
} from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { ApiError } from '@/api/client';
import { useAuth } from '@/contexts/AuthContext';
import { useToast } from '@/hooks/use-toast';
import { useCreateUser, useResetUserPassword, useUpdateUser, useUserApplications, useUserRoles, useUsers } from '@/hooks/use-users';
import type { ManagedUser, UpdateUserRequest, UserApplication, UserRole, UserRoleDefinition } from '@/types/users';

const roleNames: Record<UserRole, string> = { admin: 'Admin', operator: 'Operator', viewer: 'Viewer' };
const passwordHelp = 'Use at least 12 characters. A long, unique passphrase is best.';
const permissionLabels: Record<string, string> = {
  'pipelines:read': 'View pipelines and execution details',
  'pipelines:operate': 'Pause, resume, rerun, and skip pipelines or stages',
  'schedules:read': 'View schedules and their runs',
  'schedules:operate': 'Trigger and manage schedules',
  'users:manage': 'Manage users, roles, and application access',
  'settings:manage': 'Manage settings, API keys, and action policies',
  'audit:read': 'Read the audit log',
};

function passwordError(password: string): string | null {
  if (Array.from(password).length < 12) return 'The password must contain at least 12 characters.';
  if (new TextEncoder().encode(password).length > 72) return 'The password is too long; choose a shorter password.';
  return null;
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : 'Something went wrong. Please try again.';
}

function updatePayload(user: ManagedUser): UpdateUserRequest {
  return {
    firstName: user.firstName, lastName: user.lastName, email: user.email,
    role: user.role, isActive: user.isActive, applicationIds: user.applicationIds,
  };
}

export default function Users() {
  const { user: currentUser, checkAuth } = useAuth();
  const users = useUsers();
  const applications = useUserApplications();
  const roles = useUserRoles();
  const updateUser = useUpdateUser();
  const confirm = useConfirm();
  const { toast } = useToast();
  const [search, setSearch] = useState('');
  const [roleFilter, setRoleFilter] = useState('all');
  const [statusFilter, setStatusFilter] = useState('all');
  const [editor, setEditor] = useState<ManagedUser | 'new' | null>(null);
  const [passwordUser, setPasswordUser] = useState<ManagedUser | null>(null);
  const applicationNames = new Map(applications.data?.map(app => [app.id, app.name]));
  const missingEndpoint = [users.error, applications.error, roles.error].some(error => error instanceof ApiError && error.status === 404);
  const unavailable = missingEndpoint || (users.isError && applications.isError && roles.isError);
  const query = search.trim().toLowerCase();
  const filtered = (users.data ?? []).filter(user =>
    (!query || `${user.firstName} ${user.lastName ?? ''} ${user.email}`.toLowerCase().includes(query)) &&
    (roleFilter === 'all' || user.role === roleFilter) &&
    (statusFilter === 'all' || user.isActive === (statusFilter === 'active')),
  );

  const toggleActive = async (user: ManagedUser) => {
    if (user.id === currentUser?.id) return;
    if (user.isActive && !await confirm({
      title: `Deactivate ${user.firstName}?`,
      description: `${user.email} will be signed out and unable to sign in until reactivated.`,
      confirmLabel: 'Deactivate user', destructive: true,
    })) return;
    updateUser.mutate({ id: user.id, data: { ...updatePayload(user), isActive: !user.isActive } }, {
      onSuccess: () => toast({ title: user.isActive ? 'User deactivated' : 'User reactivated' }),
      onError: error => toast({ title: 'Could not update user', description: errorMessage(error), variant: 'destructive' }),
    });
  };

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center justify-between gap-4">
        <div>
          <h2 className="text-lg font-semibold">People and access</h2>
          <p className="text-sm text-muted-foreground">Manage accounts, roles, and application access.</p>
        </div>
        <Button onClick={() => setEditor('new')} className="gap-2" disabled={unavailable || !applications.isSuccess || !roles.isSuccess}>
          <Plus className="h-4 w-4" /> Add user
        </Button>
      </div>

      {unavailable ? (
        <div role="alert" className="flex flex-wrap items-center justify-between gap-4 rounded-xl border border-border bg-card p-6">
          <div className="space-y-1">
            <h3 className="font-medium">User management is unavailable</h3>
            <p className="text-sm text-muted-foreground">
              {missingEndpoint
                ? 'This server does not provide the user management API. Check that the dashboard and API are running the same version.'
                : 'Could not connect to user management. Try again when the server is available.'}
            </p>
          </div>
          <Button variant="outline" size="sm" disabled={users.isFetching || applications.isFetching || roles.isFetching} onClick={() => {
            void users.refetch();
            void applications.refetch();
            void roles.refetch();
          }}>Try again</Button>
        </div>
      ) : <>
      <section aria-label="Role permissions" className="space-y-3">
        <h3 className="text-sm font-semibold">Roles and permissions</h3>
        {roles.isPending ? <p className="text-sm text-muted-foreground">Loading roles...</p> : roles.isError ? (
          <LoadError message="Could not load roles." onRetry={() => roles.refetch()} />
        ) : (
          <div className="grid gap-3 lg:grid-cols-3">
            {roles.data.map(role => (
              <div key={role.id} className="rounded-xl border border-border bg-card p-4">
                <div className="flex items-center gap-2 font-medium"><ShieldCheck className="h-4 w-4 text-primary" />{role.name}</div>
                <p className="mt-2 text-sm text-muted-foreground">{role.description}</p>
                <ul className="mt-3 space-y-1.5 text-xs text-muted-foreground">
                  {role.permissions.map(permission => (
                    <li key={permission} className="flex gap-2"><Check className="mt-0.5 h-3 w-3 shrink-0 text-primary" />{permissionLabels[permission] ?? permission.replace(/[._:]/g, ' ')}</li>
                  ))}
                </ul>
              </div>
            ))}
          </div>
        )}
      </section>

      {applications.isError && <LoadError message="Could not load application access. Editing is unavailable until applications load." onRetry={() => applications.refetch()} />}

      <section className="overflow-hidden rounded-xl border border-border bg-card" aria-label="User accounts">
        <div className="flex flex-wrap items-center gap-3 border-b border-border p-4">
          <div className="relative min-w-56 flex-1">
            <Search className="absolute left-3 top-3 h-4 w-4 text-muted-foreground" />
            <Input aria-label="Search users" placeholder="Search by name or email..." value={search} onChange={event => setSearch(event.target.value)} className="pl-9" />
          </div>
          <Select value={roleFilter} onValueChange={setRoleFilter}>
            <SelectTrigger aria-label="Filter by role" className="h-10 w-[120px]"><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value="all">All roles</SelectItem>
              {Object.entries(roleNames).map(([id, name]) => <SelectItem key={id} value={id}>{name}</SelectItem>)}
            </SelectContent>
          </Select>
          <Select value={statusFilter} onValueChange={setStatusFilter}>
            <SelectTrigger aria-label="Filter by status" className="h-10 w-[140px]"><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value="all">All statuses</SelectItem><SelectItem value="active">Active</SelectItem><SelectItem value="inactive">Inactive</SelectItem>
            </SelectContent>
          </Select>
        </div>
        {users.isPending ? (
          <div role="status" className="flex items-center justify-center gap-2 p-12 text-muted-foreground"><Loader2 className="h-5 w-5 animate-spin" />Loading users...</div>
        ) : users.isError ? (
          <div className="p-6"><LoadError message={errorMessage(users.error)} onRetry={() => users.refetch()} /></div>
        ) : !filtered.length ? (
          <div className="p-12 text-center">
            <UsersIcon className="mx-auto h-9 w-9 text-muted-foreground/50" />
            <h3 className="mt-3 font-medium">{users.data.length ? 'No matching users' : 'No users yet'}</h3>
            <p className="mt-1 text-sm text-muted-foreground">{users.data.length ? 'Try a different search or filter.' : 'Add an account to give someone access to Pipelogiq.'}</p>
          </div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-left text-sm">
              <thead className="bg-muted/30 text-xs text-muted-foreground"><tr>
                <th scope="col" className="px-4 py-3 font-medium">User</th><th scope="col" className="px-4 py-3 font-medium">Role</th>
                <th scope="col" className="px-4 py-3 font-medium">Status</th><th scope="col" className="px-4 py-3 font-medium">Application access</th>
                <th scope="col" className="px-4 py-3 text-right font-medium">Actions</th>
              </tr></thead>
              <tbody className="divide-y divide-border">
                {filtered.map(user => (
                  <tr key={user.id}>
                    <td className="px-4 py-4"><div className="font-medium">{user.firstName} {user.lastName}{user.id === currentUser?.id && <span className="ml-2 text-xs text-muted-foreground">You</span>}</div><div className="text-xs text-muted-foreground">{user.email}</div></td>
                    <td className="px-4 py-4"><Badge variant={user.role === 'admin' ? 'default' : 'secondary'}>{roleNames[user.role] ?? user.role}</Badge></td>
                    <td className="px-4 py-4"><Badge variant="outline" className={user.isActive ? 'text-status-success' : 'text-muted-foreground'}>{user.isActive ? 'Active' : 'Inactive'}</Badge></td>
                    <td className="max-w-72 px-4 py-4 text-xs text-muted-foreground">{user.applicationIds.length ? user.applicationIds.map(id => applicationNames.get(id) ?? `Application #${id}`).join(', ') : 'No applications assigned'}</td>
                    <td className="px-4 py-4"><div className="flex justify-end gap-1">
                      <Button variant="ghost" size="sm" aria-label={`Edit ${user.email}`} disabled={!applications.isSuccess || !roles.isSuccess || updateUser.isPending} onClick={() => setEditor(user)}><Pencil className="mr-1.5 h-3.5 w-3.5" />Edit</Button>
                      {user.id === currentUser?.id ? <Button asChild variant="ghost" size="sm"><Link to="/settings/security" aria-label="Change your password"><KeyRound className="mr-1.5 h-3.5 w-3.5" />Password</Link></Button> : <Button variant="ghost" size="sm" aria-label={`Reset password for ${user.email}`} onClick={() => setPasswordUser(user)}><KeyRound className="mr-1.5 h-3.5 w-3.5" />Password</Button>}
                      <Button variant="ghost" size="sm" disabled={user.id === currentUser?.id || updateUser.isPending} onClick={() => toggleActive(user)} aria-label={`${user.isActive ? 'Deactivate' : 'Reactivate'} ${user.email}`} className={user.isActive ? 'text-destructive' : ''}>{user.isActive ? 'Deactivate' : 'Reactivate'}</Button>
                    </div></td>
                  </tr>
                ))}
              </tbody>
            </table>
            <p className="border-t border-border px-4 py-3 text-xs text-muted-foreground">{filtered.length} of {users.data.length} users</p>
          </div>
        )}
      </section>
      {editor && <UserEditor key={editor === 'new' ? 'new' : editor.id} user={editor === 'new' ? undefined : editor} currentUserId={currentUser?.id} applications={applications.data ?? []} roles={roles.data ?? []} onClose={() => setEditor(null)} onSelfUpdate={checkAuth} />}
      {passwordUser && <PasswordDialog user={passwordUser} onClose={() => setPasswordUser(null)} />}
      </>}
    </div>
  );
}

function LoadError({ message, onRetry }: { message: string; onRetry: () => unknown }) {
  return <div role="alert" className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-destructive/30 bg-destructive/5 p-4 text-sm"><span>{message}</span><Button size="sm" variant="outline" onClick={onRetry}>Try again</Button></div>;
}

function UserEditor({ user, currentUserId, applications, roles, onClose, onSelfUpdate }: {
  user?: ManagedUser; currentUserId?: number; applications: UserApplication[]; roles: UserRoleDefinition[];
  onClose: () => void; onSelfUpdate: () => Promise<void>;
}) {
  const createUser = useCreateUser();
  const updateUser = useUpdateUser();
  const { toast } = useToast();
  const [form, setForm] = useState<UpdateUserRequest>(user ? updatePayload(user) : {
    firstName: '', lastName: '', email: '', role: 'viewer', isActive: true, applicationIds: [],
  });
  const [password, setPassword] = useState('');
  const [error, setError] = useState<string | null>(null);
  const isSelf = user?.id === currentUserId;
  const isPending = createUser.isPending || updateUser.isPending;

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (isPending) return;
    setError(null);
    if (!form.firstName.trim()) { setError('First name is required.'); return; }
    if (!user) {
      const invalid = passwordError(password);
      if (invalid) { setError(invalid); return; }
    }
    const data = { ...form, firstName: form.firstName.trim(), lastName: form.lastName?.trim(), email: form.email.trim() };
    try {
      if (user) await updateUser.mutateAsync({ id: user.id, data });
      else await createUser.mutateAsync({ ...data, password });
      if (isSelf) await onSelfUpdate();
      toast({ title: user ? 'User updated' : 'User created' });
      onClose();
    } catch (err) { setError(errorMessage(err)); }
  };

  return (
    <Dialog open onOpenChange={open => !open && !isPending && onClose()}>
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-xl">
        <DialogHeader><DialogTitle>{user ? 'Edit user' : 'Add user'}</DialogTitle><DialogDescription>{user ? 'Update this account and its access.' : 'Create an account and choose what this person can access.'}</DialogDescription></DialogHeader>
        <form onSubmit={submit} className="space-y-5">
          <fieldset disabled={isPending} className="space-y-4">
            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-2"><Label htmlFor="user-first-name">First name</Label><Input id="user-first-name" autoComplete="given-name" required maxLength={100} value={form.firstName} onChange={event => setForm({ ...form, firstName: event.target.value })} /></div>
              <div className="space-y-2"><Label htmlFor="user-last-name">Last name</Label><Input id="user-last-name" autoComplete="family-name" maxLength={100} value={form.lastName ?? ''} onChange={event => setForm({ ...form, lastName: event.target.value })} /></div>
            </div>
            <div className="space-y-2"><Label htmlFor="user-email">Email</Label><Input id="user-email" type="email" autoComplete="email" required maxLength={254} value={form.email} onChange={event => setForm({ ...form, email: event.target.value })} /></div>
            {!user && <div className="space-y-2"><Label htmlFor="user-password">Initial password</Label><Input id="user-password" type="password" autoComplete="new-password" required value={password} onChange={event => setPassword(event.target.value)} aria-describedby="user-password-help" /><p id="user-password-help" className="text-xs text-muted-foreground">{passwordHelp}</p></div>}
            <div className="space-y-2">
              <Label htmlFor="user-role">Role</Label>
              <Select disabled={isSelf || isPending} value={form.role} onValueChange={role => setForm({ ...form, role: role as UserRole })}>
                <SelectTrigger id="user-role" className="h-10 w-full"><SelectValue /></SelectTrigger>
                <SelectContent>{roles.map(role => <SelectItem key={role.id} value={role.id}>{role.name}</SelectItem>)}</SelectContent>
              </Select>
              <p className="text-xs text-muted-foreground">{roles.find(role => role.id === form.role)?.description}</p>
            </div>
            <div className="flex items-center gap-2"><Checkbox id="user-active" checked={form.isActive} disabled={isSelf} onCheckedChange={checked => setForm({ ...form, isActive: checked === true })} /><Label htmlFor="user-active">Account is active</Label></div>
            {isSelf && <p className="text-xs text-muted-foreground">Another administrator must change your role or deactivate your account. Saving your profile will sign you out.</p>}
            <div className="space-y-2">
              <p className="text-sm font-medium">Application access</p>
                <p className="text-xs text-muted-foreground">{form.role === 'viewer' ? 'Can view pipelines and schedules in the selected applications.' : 'Can view and operate pipelines and schedules in the selected applications.'}{form.role === 'admin' && ' Admins can manage all users and settings, while pipeline and schedule access requires an application assignment.'}</p>
                <div className="max-h-44 space-y-3 overflow-y-auto rounded-lg border border-border p-3">
                  {applications.length ? applications.map(app => <div key={app.id} className="flex items-start gap-2"><Checkbox id={`user-app-${app.id}`} checked={form.applicationIds.includes(app.id)} onCheckedChange={checked => setForm({ ...form, applicationIds: checked ? [...form.applicationIds, app.id] : form.applicationIds.filter(id => id !== app.id) })} className="mt-0.5" /><div><Label htmlFor={`user-app-${app.id}`}>{app.name}</Label>{app.description && <p className="mt-0.5 text-xs text-muted-foreground">{app.description}</p>}</div></div>) : <p className="text-sm text-muted-foreground">No applications have been created yet.</p>}
                </div>
                {!form.applicationIds.length && <p className="text-xs text-muted-foreground">With no assigned applications, this user will have no pipeline or schedule access.</p>}
            </div>
          </fieldset>
          {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
          <DialogFooter><Button type="button" variant="outline" disabled={isPending} onClick={onClose}>Cancel</Button><Button type="submit" disabled={isPending}>{isPending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}{user ? 'Save changes' : 'Create user'}</Button></DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function PasswordDialog({ user, onClose }: { user: ManagedUser; onClose: () => void }) {
  const resetPassword = useResetUserPassword();
  const { toast } = useToast();
  const [password, setPassword] = useState('');
  const [repeat, setRepeat] = useState('');
  const [error, setError] = useState<string | null>(null);
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (resetPassword.isPending) return;
    const invalid = passwordError(password);
    if (invalid) { setError(invalid); return; }
    if (password !== repeat) { setError('Passwords do not match.'); return; }
    setError(null);
    try {
      await resetPassword.mutateAsync({ id: user.id, password });
      toast({ title: 'Password reset', description: 'Existing sessions have been revoked.' });
      onClose();
    } catch (err) { setError(errorMessage(err)); }
  };
  return (
    <Dialog open onOpenChange={open => !open && !resetPassword.isPending && onClose()}>
      <DialogContent><DialogHeader><DialogTitle>Reset password</DialogTitle><DialogDescription>Set a new password for {user.email}. Their existing sessions will be revoked.</DialogDescription></DialogHeader>
        <form className="space-y-4" onSubmit={submit}>
          <div className="space-y-2"><Label htmlFor="reset-password">New password</Label><Input id="reset-password" type="password" autoComplete="new-password" required disabled={resetPassword.isPending} value={password} onChange={event => setPassword(event.target.value)} aria-describedby="reset-password-help" /><p id="reset-password-help" className="text-xs text-muted-foreground">{passwordHelp}</p></div>
          <div className="space-y-2"><Label htmlFor="repeat-password">Confirm new password</Label><Input id="repeat-password" type="password" autoComplete="new-password" required disabled={resetPassword.isPending} value={repeat} onChange={event => setRepeat(event.target.value)} /></div>
          {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
          <DialogFooter><Button type="button" variant="outline" disabled={resetPassword.isPending} onClick={onClose}>Cancel</Button><Button type="submit" disabled={resetPassword.isPending}>{resetPassword.isPending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}Reset password</Button></DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
