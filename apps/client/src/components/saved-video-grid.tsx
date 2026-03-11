import { Play } from 'lucide-react';
import { useMemo, useState } from 'react';
import { ConfirmDialog } from '@/components/confirm-dialog';
import { MediaGrid } from '@/components/media-grid';
import { VideoCard } from '@/features/video/components/video-card';
import { VideoCardMenu } from '@/features/video/components/video-card-menu';
import { recordTitles } from '@/features/video/utils/titles';
import { videoRecordToResult } from '@/features/video/utils/video-utils';
import { useVideoStatuses } from '@/hooks/useVideoStatuses';
import { VideoLink } from '@/lib/links';
import { useI18n } from '@/providers/i18n-provider';
import type { VideoRecord } from '@/types';

/** A saved video (a history row, a like): addressed by its `videos` record id; `record` is that record (with its channel) when the server could expand it. */
export interface SavedVideoEntry {
  id: string;
  video: string;
  record?: VideoRecord;
  watched_at?: string;
  position?: number;
  video_duration?: number;
  series?: string;
}

const identityOf = (entry: SavedVideoEntry) => entry.id;

export function SavedVideoGrid({
  entries,
  onRemove,
  showProgress = false,
  onEndReached,
}: {
  entries: readonly SavedVideoEntry[];
  onRemove?: (entry: SavedVideoEntry) => Promise<void>;
  showProgress?: boolean;
  onEndReached?: () => void;
}) {
  const { t, language } = useI18n();
  const ids = useMemo(() => entries.map((entry) => entry.video), [entries]);
  const { statuses } = useVideoStatuses(ids);
  const [removingEntry, setRemovingEntry] = useState<SavedVideoEntry | null>(null);
  return (
    <>
      <MediaGrid
        items={entries}
        identity={identityOf}
        onEndReached={onEndReached}
        renderItem={(entry) => {
          const video = entry.record;
          const watchedAt = entry.watched_at?.replace(' ', 'T');
          const actions = onRemove ? (
            <VideoCardMenu
              video={{ video: entry.video, title: (video && recordTitles(video.titles).title) || t('ui.library.uncachedVideo') }}
              onRemove={() => setRemovingEntry(entry)}
            />
          ) : undefined;
          const duration = video?.duration || entry.video_duration || 0;
          return (
            <div className="min-w-0">
              {video ? (
                <VideoCard
                  video={videoRecordToResult(video)}
                  watched={!!statuses.get(entry.video)?.watched}
                  actions={actions}
                  series={entry.series}
                  enrichMetadata={!showProgress}
                  progress={showProgress && duration > 0 ? Math.min(1, Math.max(0, (entry.position || 0) / duration)) : undefined}
                />
              ) : (
                <div className="relative">
                  <VideoLink
                    video={{ id: entry.video }}
                    series={entry.series}
                    className="group block rounded-xl border bg-card p-3 transition-colors hover:border-primary/40"
                  >
                    <span className="flex aspect-video items-center justify-center rounded-lg bg-muted text-muted-foreground">
                      <Play aria-hidden="true" className="size-8 transition-transform group-hover:scale-110" />
                    </span>
                    <p className="mt-3 pr-9 text-sm font-medium">{t('ui.library.uncachedVideo')}</p>
                    <p className="mt-1 pr-9 text-xs text-muted-foreground">{t('ui.library.openVideo')}</p>
                  </VideoLink>
                  {actions && <div className="absolute right-2 top-2">{actions}</div>}
                </div>
              )}
              {watchedAt && (
                <p className="mt-2 text-xs text-muted-foreground">
                  <time dateTime={watchedAt}>
                    {t('ui.library.watchedOn', { date: new Date(watchedAt).toLocaleString(language, { dateStyle: 'medium', timeStyle: 'short' }) })}
                  </time>
                </p>
              )}
            </div>
          );
        }}
      />
      {onRemove && removingEntry && (
        <ConfirmDialog
          title={t('history.remove')}
          description={t('history.removeConfirm')}
          confirmLabel={t('common.delete')}
          pendingLabel={t('history.removing')}
          cancelLabel={t('common.cancel')}
          errorMessage={t('history.removeError')}
          onConfirm={() => onRemove(removingEntry)}
          onClose={() => setRemovingEntry(null)}
        />
      )}
    </>
  );
}
