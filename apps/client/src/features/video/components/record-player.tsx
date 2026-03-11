import { ErrorSnackbar } from '@/components/error-snackbar';
import { RecordNotFound } from '@/components/record-not-found';
import { PlayerScreen, PlayerSkeleton } from '@/features/video/components/video-player';
import { useVideoRecord } from '@/lib/records';
import { useI18n } from '@/providers/i18n-provider';

/** Plays the video of a `videos` record: looks the record up, then mounts the player. `source` is the connected source the video is read through. */
export function RecordPlayer({ videoId, source, series, startAt }: { videoId: string; source?: string; series?: string; startAt?: number }) {
  const { t } = useI18n();
  const { lookup, retry } = useVideoRecord(videoId);
  if (lookup.status === 'missing') {
    return <RecordNotFound kind="video" />;
  }
  if (lookup.status === 'ready') {
    return <PlayerScreen video={lookup.value} source={source} series={series} startAt={startAt} />;
  }
  return (
    <>
      <PlayerSkeleton />
      {lookup.status === 'error' && <ErrorSnackbar message={t('errors.requestFailed')} action={t('common.retry')} onAction={retry} />}
    </>
  );
}
