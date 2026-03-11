import { Users } from 'lucide-react';
import { useState } from 'react';
import { cn } from '@/lib/utils';

/** Shows a channel avatar. The server fills missing avatars and PocketBase delivers them, so a missing or broken image only shows the generic icon. */
export function ChannelAvatar({ src, className }: { src?: string; className?: string }) {
  const [failed, setFailed] = useState('');
  const image = src && src !== failed ? src : '';
  return (
    <span
      aria-hidden="true"
      className={cn('flex size-20 shrink-0 items-center justify-center overflow-hidden rounded-full bg-muted text-muted-foreground', className)}
    >
      {image ? (
        <img src={image} alt="" loading="lazy" decoding="async" className="size-full object-cover" onError={() => setFailed(image)} />
      ) : (
        <Users className="size-1/2" />
      )}
    </span>
  );
}
