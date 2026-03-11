import { Users } from 'lucide-react';
import { useId, useMemo, useState } from 'react';
import { ChannelAvatar } from '@/components/channel-avatar';
import { PlatformBadge } from '@/components/platform-badge';
import { Button } from '@/components/ui/button';
import { Dialog } from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { groupCandidates } from '@/lib/channel-groups';
import { platformName } from '@/lib/platforms';
import { useI18n } from '@/providers/i18n-provider';
import type { SubscriptionItem } from '@/providers/subscriptions-provider';

/** Lists the subscriptions a single channel can be grouped with by hand and reports the one the viewer picks. */
export function GroupWithDialog({
  subscriptions,
  channel,
  pending,
  onChoose,
  onClose,
}: {
  subscriptions: SubscriptionItem[];
  channel: { name: string; platform: string };
  pending: boolean;
  onChoose: (subscription: SubscriptionItem) => void;
  onClose: () => void;
}) {
  const { t, language } = useI18n();
  const titleId = useId();
  const [search, setSearch] = useState('');
  const candidates = useMemo(() => groupCandidates(subscriptions, channel.platform, search, language), [subscriptions, channel.platform, search, language]);
  const hasCandidates = useMemo(() => groupCandidates(subscriptions, channel.platform, '').length > 0, [subscriptions, channel.platform]);
  return (
    <Dialog aria-labelledby={titleId} busy={pending} closeOnBackdrop onClose={onClose} className="space-y-4">
      <div className="space-y-1">
        <h2 id={titleId} className="text-lg font-semibold">
          {t('subscriptions.groupWith.title', { name: channel.name })}
        </h2>
        <p className="text-sm text-muted-foreground">{t('subscriptions.groupWith.description')}</p>
      </div>
      {hasCandidates ? (
        <>
          <Input
            type="search"
            aria-label={t('subscriptions.groupWith.search')}
            placeholder={t('subscriptions.groupWith.search')}
            value={search}
            onChange={(event) => setSearch(event.target.value)}
          />
          {candidates.length > 0 ? (
            <ul className="max-h-80 space-y-1 overflow-y-auto" aria-busy={pending || undefined}>
              {candidates.map((subscription) => {
                const info = subscription.channelInfo;
                if (!info) {
                  return null;
                }
                return (
                  <li key={subscription.id}>
                    <button
                      type="button"
                      disabled={pending}
                      aria-label={t('subscriptions.groupWith.choose', { name: info.name, platform: platformName(info.platform), channel: channel.name })}
                      onClick={() => onChoose(subscription)}
                      className="flex w-full items-center gap-3 rounded-lg px-2 py-2 text-left text-sm transition-colors hover:bg-muted focus-visible:outline-2 focus-visible:outline-ring disabled:opacity-60"
                    >
                      <ChannelAvatar src={info.thumbnail} className="size-10" />
                      <span className="min-w-0 flex-1 truncate font-medium">{info.name}</span>
                      <PlatformBadge platform={info.platform} decorative />
                    </button>
                  </li>
                );
              })}
            </ul>
          ) : (
            <p role="status" className="py-6 text-center text-sm text-muted-foreground">
              {t('subscriptions.groupWith.noMatch')}
            </p>
          )}
        </>
      ) : (
        <div role="status" className="flex flex-col items-center gap-3 rounded-lg border border-dashed px-4 py-8 text-center">
          <Users aria-hidden="true" className="size-6 text-muted-foreground" strokeWidth={1.6} />
          <p className="text-sm text-muted-foreground">{t('subscriptions.groupWith.empty', { name: channel.name })}</p>
        </div>
      )}
      <div className="flex items-center justify-end gap-3">
        {pending && (
          <span role="status" className="text-sm text-muted-foreground">
            {t('subscriptions.groupWith.pending')}
          </span>
        )}
        <Button type="button" variant="ghost" disabled={pending} onClick={onClose}>
          {t('common.cancel')}
        </Button>
      </div>
    </Dialog>
  );
}
