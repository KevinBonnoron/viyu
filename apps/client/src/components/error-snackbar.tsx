import { Snackbar } from '@/components/ui/snackbar';

/** Floating error with a retry action, used instead of inline red text; it clears the mobile bottom navigation. */
export function ErrorSnackbar({ message, action, onAction, disabled }: { message: string; action: string; onAction: () => void; disabled?: boolean }) {
  return (
    <div className="fixed bottom-20 right-4 z-50 flex w-[min(24rem,calc(100vw-2rem))] flex-col gap-2 md:bottom-4">
      <Snackbar message={message} action={action} onAction={onAction} disabled={disabled} />
    </div>
  );
}
