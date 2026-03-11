import { CheckCheck } from 'lucide-react';
import { createContext, type ReactNode, useCallback, useContext, useMemo, useRef, useState } from 'react';
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

type NotificationsState = {
  /** The listed creators of enabled platforms; empty until the first answer. */
  items: NewVideos[];
  /** New videos across the listed creators: what the unread dot stands for. */
  total: number;
  loaded: boolean;
  failed: boolean;
  marking: boolean;
  markFailed: boolean;
  /** Asks the server again, as when the list is opened. */
  refresh: () => void;
  markSeen: () => Promise<void>;
  markCreatorSeen: (item: NewVideos) => void;
};

type ProfileState = { profileId?: string; news: NewVideosResponse | null; failed: boolean; marking: boolean; markFailed: boolean };

const emptyState = (profileId?: string): ProfileState => ({ profileId, news: null, failed: false, marking: false, markFailed: false });

const NotificationsContext = createContext<NotificationsState | null>(null);

/**
 * The active profile's new releases, checked once for the whole shell so the sidebar on desktop and the More panel on mobile
 * read the same answer. Switching profile starts from nothing; an answer for the previous profile never shows for the new one.
 */
export function NotificationsProvider({ children }: { children: ReactNode }) {
  const { activeProfile } = useProfiles();
  const profileId = activeProfile?.id;
  const { isEnabled } = useSources();
  const [stored, setStored] = useState<ProfileState>(() => emptyState(profileId));
  const state = stored.profileId === profileId ? stored : emptyState(profileId);
  const latest = useRef(state.news);
  latest.current = state.news;

  const update = useCallback((forProfile: string, patch: Partial<ProfileState>) => {
    setStored((previous) => ({ ...(previous.profileId === forProfile ? previous : emptyState(forProfile)), ...patch }));
  }, []);

  const list = useCallback(
    (forProfile: string, signal?: AbortSignal) =>
      api.notifications
        .list(forProfile)
        .then((news) => {
          if (!signal?.aborted) {
            update(forProfile, { news, failed: false });
          }
        })
        .catch(() => {
          if (!signal?.aborted) {
            update(forProfile, { failed: true });
          }
        }),
    [update],
  );

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
          .then(() => (signal.aborted ? undefined : list(profileId, signal)));
      };
      check();
      const interval = window.setInterval(check, CHECK_INTERVAL_MS);
      return () => window.clearInterval(interval);
    },
    [profileId],
  );

  const refresh = useCallback(() => {
    if (profileId) {
      void list(profileId);
    }
  }, [profileId, list]);

  // Marks what the panel lists as seen, up to the moment it was listed: a video released meanwhile stays new.
  const markSeen = useCallback(async () => {
    const shown = latest.current;
    if (!profileId || !shown || shown.items.length === 0) {
      return;
    }
    update(profileId, { marking: true, markFailed: false });
    try {
      await api.notifications.markSeen(profileId, shown.asOf);
      setStored((previous) =>
        previous.profileId === profileId && previous.news?.asOf === shown.asOf ? { ...previous, news: { ...previous.news, items: [] } } : previous,
      );
    } catch {
      update(profileId, { markFailed: true });
    } finally {
      update(profileId, { marking: false });
    }
  }, [profileId, update]);

  // Opening one creator marks only that creator (its group, actually) as seen.
  const markCreatorSeen = useCallback(
    (item: NewVideos) => {
      const shown = latest.current;
      if (!profileId || !shown) {
        return;
      }
      void api.notifications
        .markSeen(profileId, shown.asOf, { channel: item.channelRecordId })
        .then(() => {
          setStored((previous) =>
            previous.profileId === profileId && previous.news?.asOf === shown.asOf
              ? { ...previous, news: { ...previous.news, items: previous.news.items.filter((entry) => entry.channelRecordId !== item.channelRecordId) } }
              : previous,
          );
        })
        .catch(() => {
          // The entry stays listed; the next look retries it.
        });
    },
    [profileId],
  );

  const items = useMemo(() => filterEnabledPlatforms(state.news?.items ?? [], (item) => item.platform, isEnabled), [state.news, isEnabled]);
  const total = items.reduce((sum, item) => sum + item.count, 0);
  const value = useMemo<NotificationsState>(
    () => ({
      items,
      total,
      loaded: state.news !== null,
      failed: state.failed,
      marking: state.marking,
      markFailed: state.markFailed,
      refresh,
      markSeen,
      markCreatorSeen,
    }),
    [items, total, state.news, state.failed, state.marking, state.markFailed, refresh, markSeen, markCreatorSeen],
  );
  return <NotificationsContext.Provider value={value}>{children}</NotificationsContext.Provider>;
}

export function useNotifications() {
  const context = useContext(NotificationsContext);
  if (!context) {
    throw new Error('useNotifications must be used within NotificationsProvider');
  }
  return context;
}

/** The creators with new releases and "Mark all as seen". Nothing is marked by looking: entries stay until marked as seen. */
export function NotificationsList({ onNavigate }: { onNavigate?: () => void }) {
  const { t } = useI18n();
  const { items, total, loaded, failed, marking, markFailed, markSeen, markCreatorSeen } = useNotifications();
  return (
    <>
      {/* biome-ignore lint/a11y/noNoninteractiveTabindex: Keyboard users need to scroll a long list. */}
      <div className="max-h-[min(50vh,24rem)] overflow-y-auto p-2 text-sm" tabIndex={0}>
        {!loaded && !failed && <p className="px-2 py-3 text-muted-foreground">{t('common.loading')}</p>}
        {!loaded && failed && <p className="px-2 py-3 text-muted-foreground">{t('notifications.loadFailed')}</p>}
        {loaded && items.length === 0 && <p className="px-2 py-3 text-muted-foreground">{t('notifications.empty')}</p>}
        {items.length > 0 && (
          <ul>
            {items.map((item) => (
              <li key={`${item.platform}:${item.channelId}`}>
                <ChannelLink
                  channel={{ id: item.channelRecordId, platform: item.platform }}
                  onClick={() => {
                    markCreatorSeen(item);
                    onNavigate?.();
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
        <Button type="button" variant="outline" size="sm" className="shrink-0 rounded-full" disabled={total === 0 || marking} onClick={() => void markSeen()}>
          <CheckCheck aria-hidden="true" className="size-4" />
          {t('notifications.markSeen')}
        </Button>
      </div>
    </>
  );
}
