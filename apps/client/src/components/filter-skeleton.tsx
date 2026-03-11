import { Skeleton } from '@/components/ui/skeleton';

const CHIPS = [
  { id: 'first', width: 'w-16' },
  { id: 'second', width: 'w-24' },
  { id: 'third', width: 'w-20' },
  { id: 'fourth', width: 'w-28' },
  { id: 'fifth', width: 'w-16' },
];

/** The row of a filter (platforms) while what it filters is still loading: same chip height as the real one, so nothing jumps when it arrives. */
export function FilterSkeleton() {
  return (
    <div aria-hidden="true" className="flex flex-wrap items-center gap-2">
      <Skeleton className="mr-1 h-3 w-12" />
      {CHIPS.map((chip) => (
        <Skeleton key={chip.id} className={`h-8 rounded-full ${chip.width}`} />
      ))}
    </div>
  );
}
