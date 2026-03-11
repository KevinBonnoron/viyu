import type { ReactNode } from 'react';
import { Skeleton } from '@/components/ui/skeleton';
import { cn } from '@/lib/utils';

/** The page heading, at the top of the content. Its description is a desktop aid: mobile keeps the height for the content. */
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
  return (
    <header data-page-title="" className={cn('flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between', className)}>
      <div className="min-w-0 space-y-1.5">
        <h1
          title={typeof children === 'string' ? children : undefined}
          className="line-clamp-2 text-lg font-semibold leading-6 tracking-tight text-foreground [overflow-wrap:anywhere] desktop:line-clamp-none desktop:text-2xl desktop:leading-normal desktop:lg:text-[1.75rem]"
        >
          {children}
        </h1>
        {description && <p className="hidden max-w-2xl text-sm leading-relaxed text-muted-foreground [overflow-wrap:anywhere] desktop:block">{description}</p>}
      </div>
      {actions && <div className="flex flex-wrap items-center gap-2 sm:shrink-0">{actions}</div>}
    </header>
  );
}

/** What stands in for the title while it is unknown: in the same place as the real one and the same size, so nothing jumps. */
export function PageTitleSkeleton({ description = true }: { description?: boolean }) {
  return (
    <div aria-hidden="true" className="min-w-0 space-y-1.5">
      <Skeleton className="h-6 w-72 max-w-full desktop:h-8" />
      {description && <Skeleton className="hidden h-5 w-40 desktop:block" />}
    </div>
  );
}
