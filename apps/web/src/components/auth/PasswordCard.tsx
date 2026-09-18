import { useState } from 'react';
import { ApiError } from '@/api/client';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { useToast } from '@/hooks/use-toast';

export function PasswordCard({ onChange }: { onChange: (data: { currentPassword: string; newPassword: string }) => Promise<void> }) {
  const { toast } = useToast();
  const [current, setCurrent] = useState('');
  const [next, setNext] = useState('');
  const [repeat, setRepeat] = useState('');
  const [busy, setBusy] = useState(false);

  const submit = async (event: React.FormEvent) => {
    event.preventDefault();
    if (next !== repeat) {
      toast({ title: 'New passwords do not match', variant: 'destructive' });
      return;
    }
    if (Array.from(next).length < 12) {
      toast({
        title: 'Use at least 12 characters for your new password',
        variant: 'destructive',
      });
      return;
    }
    if (new TextEncoder().encode(next).length > 72) {
      toast({
        title: 'New password is too long',
        description: 'Choose a shorter password.',
        variant: 'destructive',
      });
      return;
    }
    setBusy(true);
    try {
      await onChange({ currentPassword: current, newPassword: next });
      setCurrent('');
      setNext('');
      setRepeat('');
      toast({ title: 'Password changed', description: 'Other sessions of this account have been signed out.' });
    } catch (error) {
      const message = error instanceof ApiError ? error.message : 'Could not change the password';
      toast({ title: 'Password not changed', description: message, variant: 'destructive' });
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="rounded-xl border border-border bg-card">
      <div className="border-b border-border px-5 py-4">
        <h3 className="font-semibold text-foreground">Password</h3>
        <p className="text-sm text-muted-foreground">Use at least 12 characters. A long, unique passphrase is best. Changing it signs out every other session.</p>
      </div>
      <form onSubmit={submit} className="grid gap-4 px-5 py-4 md:grid-cols-3">
        <div className="space-y-1.5">
          <Label htmlFor="pw-current">Current password</Label>
          <Input id="pw-current" type="password" autoComplete="current-password" value={current} onChange={(e) => setCurrent(e.target.value)} required />
        </div>
        <div className="space-y-1.5">
          <Label htmlFor="pw-next">New password</Label>
          <Input id="pw-next" type="password" autoComplete="new-password" value={next} onChange={(e) => setNext(e.target.value)} required />
        </div>
        <div className="space-y-1.5">
          <Label htmlFor="pw-repeat">Repeat new password</Label>
          <Input id="pw-repeat" type="password" autoComplete="new-password" value={repeat} onChange={(e) => setRepeat(e.target.value)} required />
        </div>
        <div className="md:col-span-3">
          <Button type="submit" disabled={busy}>
            {busy ? 'Changing…' : 'Change password'}
          </Button>
        </div>
      </form>
    </div>
  );
}
