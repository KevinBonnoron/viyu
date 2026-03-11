import type * as React from 'react';

import { cn } from '@/lib/utils';

type SwitchProps = Omit<React.ComponentProps<'button'>, 'onChange' | 'role' | 'type'> & { checked: boolean; onCheckedChange: (checked: boolean) => void };

/** A labelled on/off toggle (`aria-labelledby` or `aria-label` must be given), styled like the switches of the settings pages. */
function Switch({ checked, onCheckedChange, className, ...props }: SwitchProps) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      data-slot="switch"
      onClick={() => onCheckedChange(!checked)}
      className={cn('relative inline-flex h-6 w-11 shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:cursor-wait disabled:opacity-50', checked ? 'bg-primary' : 'bg-input', className)}
      {...props}
    >
      <span className={cn('pointer-events-none inline-block size-5 rounded-full bg-background shadow-lg transition-transform', checked ? 'translate-x-5' : 'translate-x-0')} />
    </button>
  );
}

export { Switch };
