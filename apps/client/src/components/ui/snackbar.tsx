import { Button } from '@/components/ui/button';
import { cn } from '@/lib/utils';

/** A floating message with one action. `error` (the default) alerts; `neutral` confirms an action, e.g. with an undo. */
export function Snackbar({
  message,
  action,
  onAction,
  disabled,
  tone = 'error',
}: {
  message: string;
  action: string;
  onAction: () => void;
  disabled?: boolean;
  tone?: 'error' | 'neutral';
}) {
  return (
    <div
      role={tone === 'error' ? 'alert' : 'status'}
      className={cn('flex w-full max-w-sm items-center gap-3 rounded-xl border bg-card p-4 text-sm shadow-xl', tone === 'error' && 'border-destructive/30')}
    >
      <p className="min-w-0 flex-1 text-foreground">{message}</p>
      <Button variant="outline" size="sm" disabled={disabled} onClick={onAction}>
        {action}
      </Button>
    </div>
  );
}
