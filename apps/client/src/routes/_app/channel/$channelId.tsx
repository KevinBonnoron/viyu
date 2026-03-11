import { createFileRoute, useLocation } from '@tanstack/react-router';
import { ErrorSnackbar } from '@/components/error-snackbar';
import { RecordNotFound } from '@/components/record-not-found';
import { ChannelLoading, ChannelScreen } from '@/features/channel/components/channel-page';
import { useChannelRecord } from '@/lib/records';
import { useI18n } from '@/providers/i18n-provider';

export const Route = createFileRoute('/_app/channel/$channelId')({
  validateSearch: (search: Record<string, unknown>): { tab?: 'videos' | 'series' } => ({
    tab: search.tab === 'series' ? 'series' : undefined,
  }),
  component: ChannelPage,
});

/** `channelId` is a `channels` record id. A connected source's channel carries that source in the navigation state: it is read privately through it. */
function ChannelPage() {
  const { channelId } = Route.useParams();
  const { tab } = Route.useSearch();
  const source = useLocation({ select: (location) => location.state.viyuSource });
  const { t } = useI18n();
  const { lookup, retry } = useChannelRecord(channelId);
  if (lookup.status === 'missing') {
    return <RecordNotFound kind="channel" />;
  }
  if (lookup.status === 'ready') {
    return (
      <ChannelScreen channelId={lookup.value.id} platform={lookup.value.platform} nativeId={lookup.value.nativeId} source={source} series={tab === 'series'} />
    );
  }
  return (
    <>
      <ChannelLoading />
      {lookup.status === 'error' && <ErrorSnackbar message={t('errors.requestFailed')} action={t('common.retry')} onAction={retry} />}
    </>
  );
}
