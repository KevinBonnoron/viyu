import { cn } from '@/lib/utils';

export function Brand({ compact = false, className }: { compact?: boolean; className?: string }) {
  return (
    <span className={cn('inline-flex items-center gap-2.5', className)}>
      <img src="/favicon.svg" alt="" aria-hidden="true" className="size-9 shrink-0 rounded-[27%] shadow-sm" />
      {!compact && <span className="brand-name text-xl font-bold tracking-tight text-foreground">Viyu</span>}
    </span>
  );
}
