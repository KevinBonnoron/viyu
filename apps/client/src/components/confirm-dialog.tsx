import { useEffect, useId, useRef, useState } from 'react';
import { Button } from '@/components/ui/button';
import { Dialog } from '@/components/ui/dialog';

/** A confirmation that runs `onConfirm` and closes when it resolves; when it rejects the dialog stays open and shows `errorMessage`. */
export function ConfirmDialog({
  title,
  description,
  confirmLabel,
  pendingLabel,
  cancelLabel,
  errorMessage,
  onConfirm,
  onClose,
}: {
  title: string;
  description: string;
  confirmLabel: string;
  pendingLabel: string;
  cancelLabel: string;
  errorMessage: string | ((cause: unknown) => string);
  onConfirm: () => Promise<void>;
  onClose: () => void;
}) {
  const titleId = useId();
  const descriptionId = useId();
  const mounted = useRef(false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string>();

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  const confirm = async () => {
    if (pending) {
      return;
    }
    setPending(true);
    setError(undefined);
    try {
      await onConfirm();
      if (mounted.current) {
        onClose();
      }
    } catch (cause) {
      if (mounted.current) {
        setError(typeof errorMessage === 'function' ? errorMessage(cause) : errorMessage);
      }
    } finally {
      if (mounted.current) {
        setPending(false);
      }
    }
  };

  return (
    <Dialog role="alertdialog" aria-labelledby={titleId} aria-describedby={descriptionId} busy={pending} onClose={onClose} className="space-y-5">
      <h2 id={titleId} className="text-lg font-semibold">
        {title}
      </h2>
      <p id={descriptionId} className="text-sm text-muted-foreground">
        {description}
      </p>
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
      <div className="flex justify-end gap-2">
        <Button type="button" variant="ghost" disabled={pending} onClick={onClose}>
          {cancelLabel}
        </Button>
        <Button type="button" variant="destructive" disabled={pending} onClick={() => void confirm()}>
          {pending ? pendingLabel : confirmLabel}
        </Button>
      </div>
    </Dialog>
  );
}
