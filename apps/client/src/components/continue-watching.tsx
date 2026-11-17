import { useId } from 'react';
import { Button } from '@/components/ui/button';
import { VideoCard } from '@/features/video/components/video-card';
import { VideoCardMenu } from '@/features/video/components/video-card-menu';
import type { Resumable } from '@/hooks/useContinueWatching';
import { useI18n } from '@/providers/i18n-provider';

/** The unfinished videos and their independent page-load retry, shared by Feed and Subscriptions. */
export function ContinueWatching({ items, isError, onRetry }: { items: readonly Resumable[]; isError: boolean; onRetry: () => void }) {
  const { t } = useI18n();
  const headingId = useId();
  if (items.length === 0 && !isError) {
    return null;
  }
  return (
    <section aria-labelledby={headingId} className="space-y-3">
      <h2 id={headingId} className="text-base font-semibold">
        {t('continue.title')}
      </h2>
      {isError && (
        <div role="alert" className="flex flex-wrap items-center gap-3">
          <p className="text-sm text-destructive">{t('continue.loadError')}</p>
          <Button variant="outline" size="sm" onClick={onRetry}>
            {t('common.retry')}
          </Button>
        </div>
      )}
      <ul className="-mx-1 flex snap-x gap-5 overflow-x-auto px-1 pb-2">
        {items.map(({ row, video }) => {
          const duration = video.duration || row.video_duration;
          return (
            <li key={row.id} className="w-64 shrink-0 snap-start sm:w-72">
              <VideoCard
                video={video}
                watched={false}
                enrichMetadata={false}
                progress={duration > 0 ? Math.min(1, row.position / duration) : undefined}
                actions={<VideoCardMenu video={{ video: video.id, title: video.title }} canMarkWatched canDismiss />}
              />
            </li>
          );
        })}
      </ul>
    </section>
  );
}
