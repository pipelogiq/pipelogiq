import { Eye } from 'lucide-react';
import { useAuth } from '@/contexts/AuthContext';

export function ReadOnlyNotice() {
  const { canOperate } = useAuth();
  if (canOperate) return null;

  return (
    <p className="flex items-start gap-2 rounded-lg border border-border bg-muted/30 px-3 py-2 text-sm text-muted-foreground">
      <Eye className="mt-0.5 h-4 w-4 shrink-0" aria-hidden="true" />
      <span>Read-only access. Ask an administrator for Operator access to manage pipelines and schedules.</span>
    </p>
  );
}
