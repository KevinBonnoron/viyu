import { createFileRoute } from '@tanstack/react-router';
import { Radio } from 'lucide-react';
import { useState } from 'react';
import { EmptyState } from '@/components/empty-state';
import { ErrorSnackbar } from '@/components/error-snackbar';
import { PageTitle } from '@/components/page-title';
import { RecordNotFound } from '@/components/record-not-found';
import { Button } from '@/components/ui/button';
import { RecordPlayer } from '@/features/video/components/record-player';
import { PlayerSkeleton } from '@/features/video/components/video-player';
import { useAbortableEffect } from '@/hooks/useAbortableEffect';
import { api } from '@/lib/api';
import { requestError } from '@/lib/api-error';
import { ChannelLink } from '@/lib/links';
import { type ChannelRef, useChannelRecord } from '@/lib/records';
import { type TranslationKey, useI18n } from '@/providers/i18n-provider';
import type { ChannelLive } from '@/types';

export const Route = createFileRoute('/_app/live/$channelId')({ component: LivePage });

/** `channelId` is a `channels` record id; the page plays the stream that channel is broadcasting now. */
function LivePage() {
  const { channelId } = Route.useParams();
  const { t } = useI18n();
  const { lookup, retry } = useChannelRecord(channelId);
  if (lookup.status === 'missing') {
    return <RecordNotFound kind="channel" />;
  }
  if (lookup.status === 'ready') {
    return <ChannelStream key={lookup.value.id} channel={lookup.value} />;
  }
  return (
    <>
      <PlayerSkeleton />
      {lookup.status === 'error' && <ErrorSnackbar message={t('errors.requestFailed')} action={t('common.retry')} onAction={retry} />}
    </>
  );
}

function ChannelStream({ channel }: { channel: ChannelRef }) {
  const { t } = useI18n();
  const [live, setLive] = useState<ChannelLive | null>(null);
  const [error, setError] = useState<TranslationKey | null>(null);
  const [attempt, setAttempt] = useState(0);
  useAbortableEffect(
    (signal) => {
      setLive(null);
      setError(null);
      let repeat: number | undefined;
      const lookup = () =>
        api.channels
          .live(channel.id, { signal })
          .then((result) => {
            if (signal.aborted) {
              return;
            }
            setLive(result);
            if (result.live) {
              if (repeat !== undefined) {
                window.clearInterval(repeat);
                repeat = undefined;
              }
            } else if (repeat === undefined) {
              // A lookup that fails upstream is indistinguishable from "not live" (the server answers both
              // the same way) and the badge that brought the user here can be a minute old: keep asking so
              // a stream that is starting (or a lookup that hiccuped) replaces this page with the player.
              repeat = window.setInterval(() => void lookup(), 60_000);
            }
          })
          .catch((failure: unknown) => {
            if (!signal.aborted) {
              setError(requestError(failure));
            }
          });
      void lookup();
      return () => {
        if (repeat !== undefined) {
          window.clearInterval(repeat);
        }
      };
    },
    [channel.id, attempt],
  );

  if (live?.live && live.recordId) {
    return <RecordPlayer videoId={live.recordId} />;
  }
  if (live) {
    return (
      <div className="space-y-6">
        <PageTitle>{t('live.offlineTitle')}</PageTitle>
        <EmptyState icon={Radio} title={t('live.offlineTitle')} description={t('live.offlineDescription', { name: channel.name })}>
          <Button asChild variant="outline">
            <ChannelLink channel={channel}>{t('live.openChannel')}</ChannelLink>
          </Button>
        </EmptyState>
      </div>
    );
  }
  return (
    <>
      <PlayerSkeleton />
      {error && <ErrorSnackbar message={t(error)} action={t('common.retry')} onAction={() => setAttempt((value) => value + 1)} />}
    </>
  );
}
