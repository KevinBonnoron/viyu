import { MediaGrid } from '@/components/media-grid';
import { VideoCard } from '@/features/video';
import { useVisibleVideos } from '@/hooks/useVisibleVideos';
import { useI18n } from '@/providers/i18n-provider';
import { isChannelResult, type SearchResult } from '@/types';
import { ChannelCard } from './channel-card';

const videoOf = (result: SearchResult) => (isChannelResult(result) ? undefined : result);
const identityOf = (result: SearchResult) => `${isChannelResult(result) ? 'channel' : 'video'}:${result.platform}:${result.source ?? 'public'}:${result.id}`;

export function SearchGrid({ results, onEndReached }: { results: SearchResult[]; onEndReached?: () => void }) {
  const { t } = useI18n();
  const { visible, statuses } = useVisibleVideos(results, videoOf, { discovery: true, hideDismissed: false });
  if (results.length > 0 && visible.length === 0) {
    return <p className="text-sm text-muted-foreground">{t('video.allHidden')}</p>;
  }
  return (
    <MediaGrid
      items={visible}
      identity={identityOf}
      onEndReached={onEndReached}
      renderItem={(result) => {
        if (isChannelResult(result)) {
          return <ChannelCard channel={result} />;
        }
        const status = statuses.get(result.id);
        return <VideoCard video={result} watched={!!status?.watched} liked={status?.reaction === 'like'} progress={status?.progress} />;
      }}
    />
  );
}
