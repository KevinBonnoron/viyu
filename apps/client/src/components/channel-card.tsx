import { ArrowUpRight, Loader2, UserCheck, UserPlus } from 'lucide-react';
import { ChannelAvatar } from '@/components/channel-avatar';
import { PlatformBadge } from '@/components/platform-badge';
import { Button } from '@/components/ui/button';
import { useChannelSubscription } from '@/hooks/useSubscriptions';
import { ChannelLink } from '@/lib/links';
import { cn } from '@/lib/utils';
import { useI18n } from '@/providers/i18n-provider';
import type { ChannelResult } from '@/types';

export function ChannelCard({ channel }: { channel: ChannelResult }) {
  const { t } = useI18n();
  const subscription = useChannelSubscription(channel);
  return (
    <div className="group relative flex h-full min-w-0 flex-col items-center justify-center rounded-xl border bg-card px-5 py-7 text-center transition-[border-color,box-shadow,transform] hover:border-primary/50 hover:shadow-lg motion-safe:hover:-translate-y-0.5">
      <ChannelLink channel={channel} className="flex min-w-0 flex-col items-center justify-center">
        <ChannelAvatar src={channel.thumbnail} className="mb-4 ring-4 ring-background" />
        <h3 className="max-w-full truncate text-sm font-semibold group-hover:text-primary">{channel.name}</h3>
        <span className="mt-1.5 flex items-center gap-1 text-xs text-muted-foreground">
          {t('channel.type')}
          <ArrowUpRight aria-hidden="true" className="size-3" />
        </span>
      </ChannelLink>
      {/* Beside the link, never inside it: toggling the subscription must not navigate. */}
      <Button
        type="button"
        variant={subscription.subscribed ? 'secondary' : 'default'}
        size="sm"
        disabled={subscription.disabled}
        aria-pressed={subscription.subscribed}
        aria-label={`${t(subscription.subscribed ? 'player.subscribed' : 'player.subscribe')} — ${channel.name}`}
        onMouseDown={(event) => event.preventDefault()}
        onClick={() => void subscription.toggle()}
        className={cn('mt-4 rounded-full', subscription.error && 'border-destructive text-destructive')}
      >
        {subscription.pending ? (
          <Loader2 aria-hidden="true" className="size-4 animate-spin" />
        ) : subscription.subscribed ? (
          <UserCheck aria-hidden="true" className="size-4" />
        ) : (
          <UserPlus aria-hidden="true" className="size-4" />
        )}
        {t(subscription.subscribed ? 'player.subscribed' : 'player.subscribe')}
      </Button>
      {subscription.error && (
        <p role="alert" className="mt-2 max-w-full text-xs text-destructive">
          {t('channel.subscription.failed')}
        </p>
      )}
      <PlatformBadge platform={channel.platform} className="absolute bottom-2 right-2" />
    </div>
  );
}
