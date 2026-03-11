import type { ReactNode } from 'react';
import { createPortal } from 'react-dom';
import { Skeleton } from '@/components/ui/skeleton';
import { cn } from '@/lib/utils';
import { usePageHeader } from '@/providers/page-header-provider';

export function PageTitle({
  children,
  description,
  actions,
  className,
}: {
  children: ReactNode;
  description?: ReactNode;
  actions?: ReactNode;
  className?: string;
}) {
  const header = usePageHeader();
  const target = header?.target;
  const compact = header?.compact ?? false;
  const heading = (
    <header data-page-title="" className={cn('flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between', className)}>
      <div className="min-w-0 space-y-1.5">
        <h1
          title={typeof children === 'string' ? children : undefined}
          className={cn('font-semibold tracking-tight text-foreground', compact ? 'truncate text-base leading-6' : 'text-2xl lg:text-[1.75rem]')}
        >
          {children}
        </h1>
        {description && !compact && <p className="max-w-2xl text-sm leading-relaxed text-muted-foreground">{description}</p>}
      </div>
      {actions && <div className="flex flex-wrap items-center gap-2 sm:shrink-0">{actions}</div>}
    </header>
  );
  return target === undefined ? heading : target ? createPortal(heading, target) : null;
}

/** What stands in for the title while it is unknown: in the same place as the real one (the topbar) and the same size, so nothing jumps. */
export function PageTitleSkeleton({ description = true }: { description?: boolean }) {
  const header = usePageHeader();
  const target = header?.target;
  const compact = header?.compact ?? false;
  const heading = (
    <div aria-hidden="true" className="min-w-0 space-y-1.5">
      <Skeleton className={compact ? 'h-6 w-56' : 'h-8 w-72 max-w-full'} />
      {description && !compact && <Skeleton className="h-5 w-40" />}
    </div>
  );
  return target === undefined ? heading : target ? createPortal(heading, target) : null;
}
