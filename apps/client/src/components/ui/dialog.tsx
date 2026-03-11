import { createContext, type ReactNode, type RefObject, useContext, useEffect, useRef, useState } from 'react';
import { cn } from '@/lib/utils';

/**
 * The open dialog element, for popovers (a select's list) rendered from inside it: a modal `<dialog>` sits in the top
 * layer and makes the rest of the page inert, so a list portalled to `<body>` would show behind it and not take clicks.
 */
const DialogContainerContext = createContext<HTMLElement | null>(null);

export function useDialogContainer(): HTMLElement | null {
  return useContext(DialogContainerContext);
}

export interface DialogProps {
  /** Whether the dialog is shown. A dialog mounted only while needed leaves it at its default. */
  open?: boolean;
  /** Asked when the viewer presses Escape (or clicks the backdrop, with `closeOnBackdrop`); never while `busy`. */
  onClose: () => void;
  /** An operation is running: the dialog cannot be dismissed. */
  busy?: boolean;
  closeOnBackdrop?: boolean;
  /** Focused after showModal, when the rest of the page is no longer allowed to take focus. */
  initialFocus?: RefObject<HTMLElement | null>;
  role?: 'dialog' | 'alertdialog';
  id?: string;
  'aria-label'?: string;
  'aria-labelledby'?: string;
  'aria-describedby'?: string;
  className?: string;
  children: ReactNode;
}

/** The one modal dialog of the app: a native `<dialog>` opened with `showModal`, themed, closed through `onClose`. */
export function Dialog({ open = true, onClose, busy = false, closeOnBackdrop = false, initialFocus, className, children, ...attributes }: DialogProps) {
  const ref = useRef<HTMLDialogElement>(null);
  const [container, setContainer] = useState<HTMLDialogElement | null>(null);
  useEffect(() => {
    const dialog = ref.current;
    if (!dialog) {
      return;
    }
    if (open && !dialog.open) {
      dialog.showModal();
      initialFocus?.current?.focus();
    } else if (!open && dialog.open) {
      dialog.close();
    }
    return () => {
      if (dialog.open) {
        dialog.close();
      }
    };
  }, [open, initialFocus]);
  return (
    <dialog
      ref={(element) => {
        ref.current = element;
        setContainer(element);
      }}
      {...attributes}
      aria-busy={busy || undefined}
      onCancel={(event) => {
        event.preventDefault();
        if (!busy) {
          onClose();
        }
      }}
      onClick={(event) => {
        if (!closeOnBackdrop || busy || event.target !== event.currentTarget) {
          return;
        }
        const bounds = event.currentTarget.getBoundingClientRect();
        if (event.clientX < bounds.left || event.clientX > bounds.right || event.clientY < bounds.top || event.clientY > bounds.bottom) {
          onClose();
        }
      }}
      className={cn('m-auto w-[calc(100%-2rem)] max-w-md overscroll-contain rounded-2xl border bg-card p-5 text-foreground shadow-2xl backdrop:bg-black/45 backdrop:backdrop-blur-sm sm:p-6', className)}
    >
      <DialogContainerContext.Provider value={container}>{children}</DialogContainerContext.Provider>
    </dialog>
  );
}
