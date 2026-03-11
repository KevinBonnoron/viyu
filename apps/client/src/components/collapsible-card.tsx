import { ChevronDown } from 'lucide-react';
import { type ReactNode, useId, useState } from 'react';
import { cn } from '@/lib/utils';

const STORAGE_PREFIX = 'viyu:card:';

function storedOpen(id: string, fallback: boolean): boolean {
  try {
    const value = localStorage.getItem(STORAGE_PREFIX + id);
    return value === null ? fallback : value === '1';
  } catch {
    return fallback;
  }
}

/** A titled card whose body can be folded. Whether it is open is remembered per card on this device. */
export function CollapsibleCard({
  id,
  title,
  badge,
  actions,
  defaultOpen = true,
  children,
}: {
  id: string;
  title: ReactNode;
  badge?: ReactNode;
  actions?: ReactNode;
  defaultOpen?: boolean;
  children: ReactNode;
}) {
  const uid = useId();
  const [open, setOpen] = useState(() => storedOpen(id, defaultOpen));
  const toggle = () => {
    const next = !open;
    setOpen(next);
    try {
      localStorage.setItem(STORAGE_PREFIX + id, next ? '1' : '0');
    } catch {
      // The card still works for this visit without storage.
    }
  };
  return (
    <section aria-labelledby={`${uid}-title`} className="overflow-hidden rounded-2xl border bg-card">
      <h2 className="flex items-center text-base">
        <button
          type="button"
          id={`${uid}-title`}
          aria-expanded={open}
          aria-controls={`${uid}-body`}
          onClick={toggle}
          className="flex min-w-0 flex-1 items-center justify-between gap-3 px-4 py-3 text-left hover:bg-muted/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-primary"
        >
          <span className="break-words font-medium">{title}</span>
          <span className="flex shrink-0 items-center gap-2">
            {badge}
            <ChevronDown aria-hidden="true" className={cn('size-4 text-muted-foreground transition-transform', open && 'rotate-180')} />
          </span>
        </button>
        {actions && <span className="shrink-0 pr-2">{actions}</span>}
      </h2>
      <div id={`${uid}-body`} hidden={!open} className="border-t">
        {children}
      </div>
    </section>
  );
}
