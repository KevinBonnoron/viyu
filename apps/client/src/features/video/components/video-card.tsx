import { Check, Heart, Lock, Play, Video } from 'lucide-react';
import { type ReactNode, useId } from 'react';
import { PlatformBadge } from '@/components/platform-badge';
import { useThumbnail } from '@/features/video/hooks/use-thumbnail';
import { useVideoMetadata } from '@/features/video/hooks/use-video-metadata';
import { hasPublicationTime, mergePublication, publicationInstant } from '@/features/video/utils/publication';
import { displayTitle, needsEnrichment, thumbnailCandidates } from '@/features/video/utils/video-presentation';
import { useUserSettings } from '@/hooks/useUserSettings';
import { requestError } from '@/lib/api-error';
import { ChannelLink, VideoLink } from '@/lib/links';
import { formatCompactNumber, formatDuration, formatRelativePublication } from '@/lib/utils';
import { useAuth } from '@/providers/auth-provider';
import { useI18n } from '@/providers/i18n-provider';
import type { VideoResult } from '@/types';

export function VideoCard({
  video,
  watched,
  liked = false,
  actions,
  progress,
  series,
  enrichMetadata = true,
}: {
  video: VideoResult;
  watched: boolean;
  liked?: boolean;
  actions?: ReactNode;
  progress?: number;
  series?: string;
  enrichMetadata?: boolean;
}) {
  const { t, language } = useI18n();
  const watchedBadgeId = useId();
  const { user } = useAuth();
  const { settings, isReady } = useUserSettings();
  const preferOriginal = settings?.use_original_titles ?? true;
  // A stream in progress has no publication date to look up: its card is complete as it is.
  const enrich = enrichMetadata && !video.live;
  const metadata = useVideoMetadata(video, enrich && isReady && (needsEnrichment(video, preferOriginal) || !video.published), user?.id ?? '');
  const waitingForTitle = enrich && preferOriginal && !video.originalTitleVerified && !metadata.title && !metadata.failed;
  const title = displayTitle(video, preferOriginal, metadata.title);
  const thumbnail = useThumbnail(thumbnailCandidates(video, metadata));
  const published = mergePublication(video.published, metadata.published);
  const instant = publicationInstant(published);
  const hasTime = !!instant && hasPublicationTime(published);
  const publicationDate = instant ? instant.toLocaleDateString(language, { day: 'numeric', month: 'short', year: 'numeric', timeZone: 'UTC' }) : null;
  const waitingForDate = enrich && !publicationDate && !metadata.failed && !metadata.complete;
  return (
    <article ref={metadata.ref} aria-busy={waitingForTitle || waitingForDate} className="group min-w-0">
      <div className="relative">
        <VideoLink video={video} series={series} aria-label={title} aria-describedby={watched ? watchedBadgeId : undefined} className="block rounded-xl">
          <div className="relative aspect-video overflow-hidden rounded-xl border border-border/50 bg-muted transition-[border-color,box-shadow] group-hover:border-primary/50 group-hover:shadow-lg group-focus-within:border-primary/50">
            {thumbnail.src ? (
              <img
                src={thumbnail.src}
                alt=""
                loading="lazy"
                decoding="async"
                className="size-full object-cover transition-transform duration-300 group-hover:scale-[1.025]"
                onError={thumbnail.onError}
              />
            ) : (
              <div role="img" aria-label={t('video.noThumbnail')} className="flex size-full items-center justify-center text-muted-foreground">
                <Video aria-hidden="true" className="size-8 opacity-50" />
              </div>
            )}
            {watched && <span aria-hidden="true" className="pointer-events-none absolute inset-0 bg-black/45" />}
            <span
              aria-hidden="true"
              className="pointer-events-none absolute inset-0 flex items-center justify-center bg-black/15 opacity-0 transition-opacity group-hover:opacity-100 group-focus-within:opacity-100"
            >
              <span className="flex size-11 items-center justify-center rounded-full bg-white/95 text-black shadow-lg">
                <Play className="ml-0.5 size-4 fill-current" />
              </span>
            </span>
            {watched && (
              <span
                id={watchedBadgeId}
                className="absolute left-2 top-2 inline-flex items-center gap-1 rounded-md border border-white/30 bg-black px-2 py-1 text-[11px] font-semibold text-white shadow-md"
              >
                <Check aria-hidden="true" className="size-3" />
                {t('video.watched')}
              </span>
            )}
            {liked && (
              <span aria-hidden="true" className="absolute right-2 top-2 inline-flex items-center rounded-md bg-black/80 p-1 text-white shadow-md">
                <Heart className="size-3.5 fill-current text-red-500" />
              </span>
            )}
            {video.live && (
              <span className="absolute bottom-2 left-2 inline-flex items-center gap-1.5 rounded-md bg-red-600 px-1.5 py-0.5 text-[11px] font-semibold uppercase text-white">
                <span aria-hidden="true" className="size-1.5 animate-pulse rounded-full bg-white" />
                {t('channel.live')}
              </span>
            )}
            {video.duration > 0 && (
              <span className="absolute bottom-2 right-2 rounded-md bg-black/80 px-1.5 py-0.5 text-[11px] font-medium tabular-nums text-white">
                {formatDuration(video.duration)}
              </span>
            )}
            {video.subscriberOnly && (
              <span className="absolute bottom-2 left-2 inline-flex items-center gap-1 rounded-md bg-black/80 px-1.5 py-0.5 text-[11px] font-semibold text-white">
                <Lock aria-hidden="true" className="size-3" />
                {t('video.subscriberOnly')}
              </span>
            )}
            {progress !== undefined && Number.isFinite(progress) && (
              <div
                role="progressbar"
                aria-label={t('history.progress')}
                aria-valuemin={0}
                aria-valuemax={100}
                aria-valuenow={Math.round(Math.max(0, Math.min(1, progress)) * 100)}
                className="pointer-events-none absolute inset-x-0 bottom-0 h-1 bg-black/50"
              >
                <div className="h-full bg-primary" style={{ width: `${Math.max(0, Math.min(1, progress)) * 100}%` }} />
              </div>
            )}
          </div>
          <h3
            title={title}
            className={`mt-3 truncate text-[13px] font-semibold leading-5 text-foreground transition-colors group-hover:text-primary sm:text-sm ${actions ? 'pr-9' : ''}`}
          >
            {title}
          </h3>
        </VideoLink>
        {actions && <div className="absolute -bottom-1 right-0">{actions}</div>}
      </div>
      <div className="relative mt-1.5 grid min-h-9 gap-1 pr-9 text-xs text-muted-foreground">
        <div className="flex h-4 min-w-0 items-center gap-1.5 leading-4">
          {video.channelName &&
            (video.channelId ? (
              <ChannelLink
                channel={{ id: video.channelId, platform: video.platform, source: video.source }}
                className="block min-w-0 truncate rounded-sm hover:text-foreground"
              >
                {video.channelName}
              </ChannelLink>
            ) : (
              <span className="block min-w-0 truncate">{video.channelName}</span>
            ))}
          {/* What a stream is about (its game) is what tells two streams of one channel apart; a stored video's category is not worth a line on every card. */}
          {video.live && video.category && (
            <>
              <span aria-hidden="true">·</span>
              <span title={video.category} className="min-w-0 max-w-[55%] shrink-0 truncate font-medium text-foreground/80">
                {video.category}
              </span>
            </>
          )}
        </div>
        <div className="flex flex-wrap items-center gap-x-1.5 gap-y-0.5">
          {video.viewCount > 0 && (
            <span title={t(video.live ? 'video.watching' : 'video.views', { count: video.viewCount.toLocaleString(language) })}>
              {t(video.live ? 'video.watching' : 'video.views', { count: formatCompactNumber(video.viewCount, language) })}
            </span>
          )}
          {video.viewCount > 0 && publicationDate && <span aria-hidden="true">·</span>}
          {publicationDate ? (
            <time dateTime={published} title={publicationDate}>
              {instant && formatRelativePublication(instant, language, undefined, hasTime)}
            </time>
          ) : waitingForDate ? (
            <span role="img" aria-label={t('video.publicationDateLoading')} className="block h-4 w-28 animate-pulse rounded bg-muted" />
          ) : enrich && metadata.failed ? (
            <button
              type="button"
              onClick={metadata.retry}
              title={t(requestError(metadata.error))}
              className="rounded text-left underline decoration-dotted underline-offset-2 hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            >
              {t('video.publicationDateRetry')}
            </button>
          ) : enrich && metadata.complete ? (
            <span>{t('series.unknownDate')}</span>
          ) : null}
        </div>
        <PlatformBadge platform={video.platform} className="absolute bottom-0 right-0.5" />
      </div>
    </article>
  );
}
