import { Bell, CheckCheck } from 'lucide-react';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { ChannelAvatar } from '@/components/channel-avatar';
import { PlatformBadge } from '@/components/platform-badge';
import { Button } from '@/components/ui/button';
import { useAbortableEffect } from '@/hooks/useAbortableEffect';
import { api } from '@/lib/api';
import { ChannelLink } from '@/lib/links';
import { filterEnabledPlatforms } from '@/lib/source-activation';
import { useI18n } from '@/providers/i18n-provider';
import { useProfiles } from '@/providers/profiles-provider';
import { useSources } from '@/providers/sources-provider';
import type { NewVideos, NewVideosResponse } from '@/types';

/** Releases are looked for this often while the page is visible. */
const CHECK_INTERVAL_MS = 5 * 60_000;

export function Notifications() {
  const { activeProfile } = useProfiles();
  return <ProfileNotifications key={activeProfile?.id ?? ''} profileId={activeProfile?.id} />;
}

function ProfileNotifications({ profileId }: { profileId?: string }) {
  const { t } = useI18n();
  const { isEnabled } = useSources();
  const [news, setNews] = useState<NewVideosResponse | null>(null);
  const [failed, setFailed] = useState(false);
  const [open, setOpen] = useState(false);
  const [marking, setMarking] = useState(false);
  const [markFailed, setMarkFailed] = useState(false);
  const latest = useRef(news);
  latest.current = news;
  const container = useRef<HTMLDivElement>(null);
  const trigger = useRef<HTMLButtonElement>(null);

  // Expired channel listings are refreshed first (the server only fetches the stale ones), so a creator's
  // new video is known before it is counted. The answers are not cancelled: a stale one is simply ignored.
  useAbortableEffect(
    (signal) => {
      if (!profileId) {
        return;
      }
      const check = () => {
        if (document.hidden) {
          return;
        }
        void api.subscriptions
          .refresh(profileId)
          .catch(() => undefined)
          .then(() => (signal.aborted ? undefined : api.notifications.list(profileId)))
          .then((result) => {
            if (result && !signal.aborted) {
              setNews(result);
              setFailed(false);
            }
          })
          .catch(() => {
            if (!signal.aborted) {
              setFailed(true);
            }
          });
      };
      check();
      const interval = window.setInterval(check, CHECK_INTERVAL_MS);
      return () => window.clearInterval(interval);
    },
    [profileId],
  );

  const close = useCallback(() => setOpen(false), []);

  // Marks what the panel lists as seen, up to the moment it was listed: a video released meanwhile stays new.
  const markSeen = useCallback(async () => {
    const shown = latest.current;
    if (!profileId || !shown || shown.items.length === 0) {
      return;
    }
    setMarking(true);
    setMarkFailed(false);
    try {
      await api.notifications.markSeen(profileId, shown.asOf);
      setNews((current) => (current && current.asOf === shown.asOf ? { ...current, items: [] } : current));
    } catch {
      setMarkFailed(true);
    } finally {
      setMarking(false);
    }
  }, [profileId]);

  // Opening one creator from the bell marks only that creator (its group, actually) as seen.
  const markCreatorSeen = useCallback(
    (item: NewVideos) => {
      const shown = latest.current;
      if (!profileId || !shown) {
        return;
      }
      void api.notifications
        .markSeen(profileId, shown.asOf, { channel: item.channelRecordId })
        .then(() => {
          setNews((current) =>
            current && current.asOf === shown.asOf
              ? { ...current, items: current.items.filter((entry) => entry.channelRecordId !== item.channelRecordId) }
              : current,
          );
        })
        .catch(() => {
          // The entry stays listed; the next look retries it.
        });
    },
    [profileId],
  );

  const toggle = () => {
    if (open) {
      close();
      return;
    }
    setOpen(true);
    if (profileId) {
      void api.notifications
        .list(profileId)
        .then((result) => {
          setNews(result);
          setFailed(false);
        })
        .catch(() => setFailed(true));
    }
  };

  useEffect(() => {
    if (!open) {
      return;
    }
    const onPointerDown = (event: PointerEvent) => {
      if (!container.current?.contains(event.target as Node)) {
        close();
      }
    };
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        close();
        trigger.current?.focus();
      }
    };
    document.addEventListener('pointerdown', onPointerDown);
    document.addEventListener('keydown', onKeyDown);
    return () => {
      document.removeEventListener('pointerdown', onPointerDown);
      document.removeEventListener('keydown', onKeyDown);
    };
  }, [open, close]);

  const items = useMemo(() => filterEnabledPlatforms(news?.items ?? [], (item) => item.platform, isEnabled), [news, isEnabled]);
  const total = items.reduce((sum, item) => sum + item.count, 0);
  // Nothing is marked by looking: the creators stay listed, and the dot stays, until they are marked as seen.
  return (
    <div ref={container} className="relative">
      <Button
        ref={trigger}
        variant="ghost"
        size="icon"
        className="relative rounded-full"
        aria-label={total > 0 ? t('notifications.unread', { count: total }) : t('notifications.title')}
        aria-expanded={open}
        aria-controls="notifications-panel"
        onClick={toggle}
      >
        <Bell aria-hidden="true" className="size-5" />
        {total > 0 && <span aria-hidden="true" className="absolute right-1.5 top-1.5 size-2.5 rounded-full bg-primary ring-2 ring-background" />}
      </Button>
      {open && (
        <section
          id="notifications-panel"
          aria-label={t('notifications.title')}
          className="absolute right-0 z-50 mt-2 w-[min(22rem,calc(100vw-2rem))] overflow-hidden rounded-lg border bg-card text-card-foreground shadow-lg"
        >
          <h2 className="px-4 pt-4 font-semibold">{t('notifications.title')}</h2>
          {/* biome-ignore lint/a11y/noNoninteractiveTabindex: Keyboard users need to scroll a long list. */}
          <div className="max-h-[min(50vh,24rem)] overflow-y-auto p-2 text-sm" tabIndex={0}>
            {!news && !failed && <p className="px-2 py-3 text-muted-foreground">{t('common.loading')}</p>}
            {!news && failed && <p className="px-2 py-3 text-muted-foreground">{t('notifications.loadFailed')}</p>}
            {news && items.length === 0 && <p className="px-2 py-3 text-muted-foreground">{t('notifications.empty')}</p>}
            {items.length > 0 && (
              <ul>
                {items.map((item) => (
                  <li key={`${item.platform}:${item.channelId}`}>
                    <ChannelLink
                      channel={{ id: item.channelRecordId, platform: item.platform }}
                      onClick={() => {
                        markCreatorSeen(item);
                        close();
                      }}
                      className="flex items-center gap-3 rounded-md px-2 py-2 hover:bg-muted"
                    >
                      <ChannelAvatar src={item.thumbnail} className="size-9 shrink-0" />
                      <span className="min-w-0 flex-1">
                        <span className="font-medium">{item.name}</span>{' '}
                        {t(item.count === 1 ? 'notifications.newVideo' : 'notifications.newVideos', { count: item.count })}
                      </span>
                      <PlatformBadge platform={item.platform} />
                    </ChannelLink>
                  </li>
                ))}
              </ul>
            )}
          </div>
          <div className="flex items-center justify-between gap-3 border-t px-4 py-3">
            <span aria-live="polite" className="min-w-0 text-xs text-destructive">
              {markFailed && t('notifications.markSeenFailed')}
            </span>
            <Button
              type="button"
              variant="outline"
              size="sm"
              className="shrink-0 rounded-full"
              disabled={total === 0 || marking}
              onClick={() => void markSeen()}
            >
              <CheckCheck aria-hidden="true" className="size-4" />
              {t('notifications.markSeen')}
            </Button>
          </div>
        </section>
      )}
    </div>
  );
}
