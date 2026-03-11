import { useLayoutEffect, useRef, useState } from 'react';
import { Skeleton } from '@/components/ui/skeleton';

export function VideoGridSkeleton() {
  const root = useRef<HTMLDivElement>(null);
  const [count, setCount] = useState(1);
  useLayoutEffect(() => {
    const element = root.current;
    if (!element) {
      return;
    }
    const update = () => {
      const style = getComputedStyle(element);
      const columns = Math.max(1, style.gridTemplateColumns.split(' ').filter(Boolean).length);
      const height = element.firstElementChild?.getBoundingClientRect().height ?? 0;
      const gap = Number.parseFloat(style.rowGap) || 0;
      if (height <= 0) {
        return;
      }
      const available = window.innerHeight - Math.max(0, element.getBoundingClientRect().top);
      setCount(columns * Math.max(1, Math.ceil((available + gap) / (height + gap))));
    };
    update();
    const observer = new ResizeObserver(update);
    observer.observe(element);
    window.addEventListener('resize', update);
    return () => {
      observer.disconnect();
      window.removeEventListener('resize', update);
    };
  }, []);
  return (
    <div ref={root} className="media-grid" aria-hidden="true">
      {Array.from({ length: count }, (_, position) => (
        // biome-ignore lint/suspicious/noArrayIndexKey: Fixed placeholder slots have no item identity or state.
        <div key={position} className="space-y-3">
          <Skeleton className="aspect-video w-full rounded-xl" />
          <Skeleton className="h-4 w-5/6" />
          <Skeleton className="h-3 w-1/2" />
        </div>
      ))}
    </div>
  );
}
