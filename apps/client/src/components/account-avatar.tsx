import type { RecordModel } from 'pocketbase';
import { useEffect, useState } from 'react';
import { pb } from '@/lib/pocketbase';
import { cn } from '@/lib/utils';

export function accountDisplayName(user: RecordModel): string {
  return String(user.name ?? '').trim() || String(user.email ?? '');
}

export function AccountAvatar({ user, className }: { user: RecordModel; className?: string }) {
  const name = accountDisplayName(user);
  const url = user.avatar ? pb.files.getURL(user, user.avatar) : '';
  const [failed, setFailed] = useState(false);
  useEffect(() => setFailed(false), [url]);
  return (
    <span
      className={cn(
        'flex shrink-0 items-center justify-center overflow-hidden rounded-full bg-accent text-accent-foreground ring-1 ring-border font-semibold',
        className,
      )}
    >
      {url && !failed ? (
        <img src={url} alt={name} className="size-full object-cover" onError={() => setFailed(true)} />
      ) : (
        <span aria-hidden="true">{name[0]?.toUpperCase() ?? '?'}</span>
      )}
    </span>
  );
}
