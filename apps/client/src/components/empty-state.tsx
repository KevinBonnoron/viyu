import type { LucideIcon } from 'lucide-react';
import type { ReactNode } from 'react';
import { cn } from '@/lib/utils';

export function EmptyState({
  icon: Icon,
  title,
  description,
  children,
  className,
}: {
  icon: LucideIcon;
  title: ReactNode;
  description?: ReactNode;
  children?: ReactNode;
  className?: string;
}) {
  return (
    <section className={cn('flex min-h-72 flex-col items-center justify-center rounded-2xl border bg-card px-5 py-12 text-center sm:px-10', className)}>
      <span className="mb-5 flex size-14 items-center justify-center rounded-2xl bg-muted text-muted-foreground">
        <Icon aria-hidden="true" className="size-6" strokeWidth={1.6} />
      </span>
      <h2 className="text-lg font-semibold tracking-tight">{title}</h2>
      {description && <p className="mt-2 max-w-md text-sm leading-relaxed text-muted-foreground">{description}</p>}
      {children && <div className="mt-6 flex flex-wrap items-center justify-center gap-2.5">{children}</div>}
    </section>
  );
}
