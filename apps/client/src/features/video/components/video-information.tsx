import { Share2, ThumbsDown, ThumbsUp, UserCheck, UserPlus } from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';
import { ChannelAvatar } from '@/components/channel-avatar';
import { Button } from '@/components/ui/button';
import { useCommunityDislikes } from '@/features/video/hooks/use-community-dislikes';
import { hasPublicationTime, publicationInstant } from '@/features/video/utils/publication';
import { api } from '@/lib/api';
import { ChannelLink } from '@/lib/links';
import { foldSearch } from '@/lib/text';
import { cn, formatCompactNumber, formatRelativePublication } from '@/lib/utils';
import { useI18n } from '@/providers/i18n-provider';
import { type ChannelResult, isChannelResult, type VideoReactionRecord, type VideoResult } from '@/types';

const creatorChannelCache = new Map<string, Promise<ChannelResult | null>>();

export function VideoInformation({
  video,
  channelAvatar,
  subscriberCount,
  subscribed,
  subscriptionDisabled,
  reaction,
  pending,
  onSubscribe,
  onReact,
  onShare,
  viewers,
  nativeId,
}: {
  video: VideoResult;
  channelAvatar: string;
  subscriberCount: number | null;
  subscribed: boolean;
  subscriptionDisabled: boolean;
  reaction?: VideoReactionRecord['reaction'];
  pending: boolean;
  onSubscribe: () => void;
  onReact: (reaction: Exclude<VideoReactionRecord['reaction'], 'not_interested'>) => void;
  onShare: () => void;
  /** The audience of a stream in progress, when a lookup fresher than the video's own count exists. */
  viewers?: number;
  /** The platform's own id of the video: Return YouTube Dislike is asked about YouTube videos by it. */
  nativeId?: string;
}) {
  const { t, language } = useI18n();
  const communityDislikes = useCommunityDislikes(video.platform === 'youtube' && video.dislikeCount == null ? nativeId : undefined);
  const published = publicationInstant(video.published);
  const hasTime = !!published && hasPublicationTime(video.published);
  const date = published ? published.toLocaleDateString(language, { day: 'numeric', month: 'long', year: 'numeric', timeZone: 'UTC' }) : null;
  const creators = useMemo(() => (video.creators ?? []).map((creator) => creator.trim()).filter(Boolean), [video.creators]);
  const showCreators = creators.length > 1 || (creators.length === 1 && creators[0] !== video.channelName);
  const [creatorChannels, setCreatorChannels] = useState<Record<string, ChannelResult | null>>({});
  useEffect(() => {
    if (!showCreators) {
      return;
    }
    let cancelled = false;
    const lookups = creators
      .filter((creator) => creator !== video.channelName)
      .map(async (creator) => {
        let lookup = creatorChannelCache.get(creator);
        if (!lookup) {
          lookup = api
            .search(creator)
            .then(
              (page) =>
                page.results.find((result): result is ChannelResult => isChannelResult(result) && foldSearch(result.name) === foldSearch(creator)) ?? null,
            )
            .catch(() => null);
          creatorChannelCache.set(creator, lookup);
        }
        return [creator, await lookup] as const;
      });
    void Promise.all(lookups).then((channels) => {
      if (!cancelled) {
        setCreatorChannels(Object.fromEntries(channels));
      }
    });
    return () => {
      cancelled = true;
    };
  }, [showCreators, creators, video.channelName]);
  const owner = (
    <>
      <ChannelAvatar src={channelAvatar} className="size-12" />
      <span className="min-w-0">
        <span className="block truncate text-sm font-semibold">{video.channelName || t('player.unknownChannel')}</span>
        {subscriberCount != null && (
          <span className="mt-0.5 block text-xs text-muted-foreground" title={t('channel.subscribers', { count: subscriberCount.toLocaleString(language) })}>
            {t('channel.subscribers', { count: formatCompactNumber(subscriberCount, language) })}
          </span>
        )}
      </span>
    </>
  );
  // A stream in progress counts the people watching it now; any other video counts its views.
  const audience = video.live ? (viewers ?? video.viewCount) : video.viewCount;
  const audienceKey = video.live ? 'video.watching' : 'video.views';
  return (
    <header className="space-y-3">
      {showCreators && (
        <div className="flex flex-wrap items-center gap-2">
          {creators.map((creator) => {
            const channel =
              creator === video.channelName
                ? { id: video.channelId, name: creator, thumbnail: video.channelThumbnail || channelAvatar, platform: video.platform }
                : creatorChannels[creator];
            return channel?.id ? (
              <ChannelLink
                key={creator}
                channel={{ id: channel.id, platform: channel.platform, source: video.source }}
                className="flex items-center gap-1.5 rounded-full border bg-muted/40 py-1 pl-1 pr-3 text-sm font-medium hover:bg-muted"
              >
                <ChannelAvatar src={channel.thumbnail} className="size-6" />
                <span className="max-w-44 truncate">{creator}</span>
              </ChannelLink>
            ) : (
              <span key={creator} className="flex items-center gap-1.5 rounded-full border bg-muted/40 px-3 py-1 text-sm text-muted-foreground">
                <span aria-hidden="true" className="flex size-6 items-center justify-center rounded-full bg-muted text-xs font-semibold uppercase">
                  {creator.slice(0, 1)}
                </span>
                <span className="max-w-44 truncate">{creator}</span>
              </span>
            );
          })}
        </div>
      )}
      <div className="flex flex-wrap items-center justify-between gap-x-6 gap-y-3">
        <div className="flex min-w-0 flex-wrap items-center gap-x-5 gap-y-3">
          {video.channelId ? (
            <ChannelLink
              channel={{ id: video.channelId, platform: video.platform, source: video.source }}
              className="flex min-w-0 items-center gap-3 rounded-full hover:text-primary"
            >
              {owner}
            </ChannelLink>
          ) : (
            <div className="flex min-w-0 items-center gap-3">{owner}</div>
          )}
          {video.channelId && (
            <Button
              variant={subscribed ? 'secondary' : 'default'}
              size="sm"
              className="rounded-full"
              disabled={subscriptionDisabled}
              aria-pressed={subscribed}
              onClick={onSubscribe}
            >
              {subscribed ? <UserCheck aria-hidden="true" className="size-4" /> : <UserPlus aria-hidden="true" className="size-4" />}
              {t(subscribed ? 'player.subscribed' : 'player.subscribe')}
            </Button>
          )}
          <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-sm text-muted-foreground">
            {audience > 0 && (
              <span title={t(audienceKey, { count: audience.toLocaleString(language) })}>
                {t(audienceKey, { count: formatCompactNumber(audience, language) })}
              </span>
            )}
            {audience > 0 && date && <span aria-hidden="true">·</span>}
            {date && published && (
              <time dateTime={video.published} title={date}>
                {formatRelativePublication(published, language, undefined, hasTime)}
              </time>
            )}
            {video.category && <span className="rounded-full bg-muted px-2.5 py-0.5 text-xs font-medium text-foreground">{video.category}</span>}
          </div>
        </div>
        <div className="flex shrink-0 items-center gap-2">
          <Button variant="outline" size="sm" className="rounded-full" onClick={onShare}>
            <Share2 aria-hidden="true" className="size-4" />
            {t('player.share')}
          </Button>
          <fieldset className="inline-flex shrink-0 overflow-hidden rounded-full border bg-card" aria-label={t('player.reactions.label')}>
            {(['like', 'dislike'] as const).map((kind) => {
              const count = kind === 'like' ? video.likeCount : (video.dislikeCount ?? communityDislikes);
              const communityEstimate = kind === 'dislike' && video.dislikeCount == null && communityDislikes != null;
              const Icon = kind === 'like' ? ThumbsUp : ThumbsDown;
              return (
                <button
                  key={kind}
                  type="button"
                  aria-label={t(kind === 'like' ? 'player.like' : 'player.dislike')}
                  aria-pressed={reaction === kind}
                  disabled={pending}
                  title={
                    [count == null ? '' : count.toLocaleString(language), communityEstimate ? t('player.reactions.communityDislikes') : '']
                      .filter(Boolean)
                      .join('\n') || undefined
                  }
                  aria-description={communityEstimate ? t('player.reactions.communityDislikes') : undefined}
                  onClick={() => onReact(kind)}
                  className={cn(
                    'inline-flex h-10 items-center gap-2 px-4 text-sm tabular-nums transition-colors hover:bg-muted focus-visible:ring-inset disabled:cursor-wait disabled:opacity-50',
                    kind === 'dislike' && 'border-l',
                    reaction === kind && 'bg-primary/10 text-primary',
                  )}
                >
                  <Icon aria-hidden="true" className={cn('size-4', reaction === kind && 'fill-current')} />
                  {count != null && <span>{`${communityEstimate ? '≈ ' : ''}${formatCompactNumber(count, language)}`}</span>}
                </button>
              );
            })}
          </fieldset>
        </div>
      </div>
    </header>
  );
}
