import { Link } from '@tanstack/react-router';
import { CircleAlert, EyeOff, FileText, ListVideo, MessageSquare, RotateCcw, SkipForward, Sparkles, VolumeX } from 'lucide-react';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { PlaybackPrivacyFrame } from '@/components/layout/app-layout';
import { PageTitle } from '@/components/page-title';
import { ProfilesError } from '@/components/profiles-error';
import { SeriesEpisodeList, SeriesSyncStatus } from '@/components/series';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { SidePanel, type SidePanelSection } from '@/features/video/components/side-panel';
import { VideoChat } from '@/features/video/components/video-chat';
import { VideoComments } from '@/features/video/components/video-comments';
import { VideoControls } from '@/features/video/components/video-controls';
import { VideoDescription } from '@/features/video/components/video-description';
import { NEXT_EPISODE_COUNTDOWN, NextEpisodeCard, SuggestionsCard } from '@/features/video/components/video-end-screen';
import { VideoInformation } from '@/features/video/components/video-information';
import { VideoRecommendations } from '@/features/video/components/video-recommendations';
import { ShareDialog } from '@/features/video/components/video-share-dialog';
import { useAutoplay } from '@/features/video/hooks/use-autoplay';
import { useHlsPlayback } from '@/features/video/hooks/use-hls-playback';
import { usePlaybackCheckpoint } from '@/features/video/hooks/use-playback-checkpoint';
import { usePlayerLayout, useSidePanelPlacement } from '@/features/video/hooks/use-player-layout';
import { useRecommendations } from '@/features/video/hooks/use-recommendations';
import { useResumePosition } from '@/features/video/hooks/use-resume-position';
import { useSourceWatch } from '@/features/video/hooks/use-source-watch';
import { useSponsorSegments } from '@/features/video/hooks/use-sponsor-segments';
import { useAbortableEffect } from '@/hooks/useAbortableEffect';
import { useChannelLive } from '@/hooks/useChannelLive';
import { useChannelProfile } from '@/hooks/useChannelProfile';
import { useSeriesDetails } from '@/hooks/useSeriesRecords';
import { useChannelSubscription } from '@/hooks/useSubscriptions';
import { useUserSettings } from '@/hooks/useUserSettings';
import { useVideoReaction, useVideoStatuses } from '@/hooks/useVideoStatuses';
import { api } from '@/lib/api';
import { type ApiError, requestError } from '@/lib/api-error';
import { useOpenVideo } from '@/lib/links';
import { setReaction } from '@/lib/reactions';
import { endScreenKind, endScreenSuggestions, videoKey } from '@/lib/recommendations';
import { seededVideoMeta, type VideoRef } from '@/lib/records';
import { COMMENTS_SECTION, commentsMode, isLiveChat } from '@/lib/side-panel';
import { type TranslationKey, useI18n } from '@/providers/i18n-provider';
import { usePlaybackPrivacy } from '@/providers/playback-privacy-provider';
import { useProfiles } from '@/providers/profiles-provider';
import { useSources } from '@/providers/sources-provider';
import type { SponsorSegment, VideoResult } from '@/types';

/** The page before the video is known: topbar title, player frame, then the channel line and description. */
export function PlayerSkeleton() {
  const { t } = useI18n();
  return (
    <div aria-busy="true" className="w-full space-y-5">
      <PageTitle>
        <Skeleton aria-label={t('common.loading')} className="h-7 w-64 max-w-full" />
      </PageTitle>
      <Skeleton aria-hidden="true" className="aspect-video w-full rounded-2xl" />
      <div aria-hidden="true" className="space-y-4">
        <Skeleton className="h-7 w-3/4" />
        <div className="flex items-center gap-3">
          <Skeleton className="size-10 rounded-full" />
          <Skeleton className="h-5 w-32" />
        </div>
        <Skeleton className="h-24 w-full rounded-lg" />
      </div>
    </div>
  );
}

/**
 * Plays the video of a `videos` record: waits for the profile, then mounts the player.
 *
 * Switching profile on an open video keeps the page but remounts the player for the new profile. That profile did not
 * choose to watch it, so it starts paused and nothing is recorded in its history until it presses play.
 */
export function PlayerScreen({ video, source, series, startAt }: { video: VideoRef; source?: string; series?: string; startAt?: number }) {
  const { activeProfile, isError, retry } = useProfiles();
  const identity = `${source ?? 'public'}:${video.id}:${series ?? ''}`;
  const profileId = activeProfile?.id;
  const [context, setContext] = useState({ identity, profile: profileId, switched: false });
  if (context.identity !== identity) {
    setContext({ identity, profile: profileId, switched: false });
  } else if (profileId && context.profile !== profileId) {
    setContext({ identity, profile: profileId, switched: context.profile !== undefined });
  }
  if (isError) {
    return <ProfilesError onRetry={retry} />;
  }
  if (!activeProfile) {
    return <PlayerSkeleton />;
  }
  return (
    <VideoPlayer
      key={`${activeProfile.id}:${identity}`}
      profileId={activeProfile.id}
      videoId={video.id}
      nativeId={video.nativeId}
      source={source}
      seriesId={series}
      holdHistory={context.switched}
      startAt={startAt}
    />
  );
}

function VideoPlayer({
  profileId,
  videoId,
  nativeId,
  source,
  seriesId,
  holdHistory,
  startAt,
}: {
  profileId: string;
  videoId: string;
  nativeId: string;
  source?: string;
  seriesId?: string;
  holdHistory: boolean;
  startAt?: number;
}) {
  const { t } = useI18n();
  const openVideo = useOpenVideo();
  const { privatePlayback } = usePlaybackPrivacy();
  const { series, error: seriesError, retry: retrySeries } = useSeriesDetails(profileId, seriesId);
  const { sources } = useSources();
  const sourceInfo = sources.find((item) => item.platform === source);
  const canMarkWatched = !!(source && sourceInfo?.enabled && sourceInfo.capabilities.includes('history-write'));
  const videoRef = useRef<HTMLVideoElement>(null);
  const frameRef = useRef<HTMLElement>(null);
  const informationRef = useRef<HTMLDivElement>(null);
  const pageRef = useRef<HTMLDivElement>(null);
  const placement = useSidePanelPlacement(pageRef);
  const mounted = useRef(true);
  const advancedOnEnd = useRef(false);
  const sponsorSkipPending = useRef(false);
  const [ended, setEnded] = useState(false);
  const [activeSponsorSegment, setActiveSponsorSegment] = useState<SponsorSegment | null>(null);
  const { reaction, isReady: reactionReady } = useVideoReaction(videoId);
  const { settings, isReady: preferencesLoaded, isError: preferencesFailed } = useUserSettings();
  const [qualityOverride, setQualityOverride] = useState<string>();
  const [audioLanguage, setAudioLanguage] = useState('original');
  const quality = qualityOverride ?? (settings?.playback_quality || 'auto');
  const autoSkipSponsors = settings?.auto_skip_sponsors ?? false;
  const [videoMeta, setVideoMeta] = useState<VideoResult | null>(null);
  const [metaError, setMetaError] = useState<TranslationKey | null>(null);
  const [metaRetry, setMetaRetry] = useState(0);
  const [streamUrl, setStreamUrl] = useState<string>();
  const [streamVersion, setStreamVersion] = useState(0);
  const [streamError, setStreamError] = useState<TranslationKey | null>(null);

  const resume = useResumePosition({ profileId, videoId, startAt, video: videoMeta });
  const { moveTo: moveResumeTo, applyTo: applyResume } = resume;
  const checkpoint = usePlaybackCheckpoint({
    videoRef,
    profileId,
    videoId,
    seriesId,
    live: videoMeta ? !!videoMeta.live : undefined,
    holdHistory,
    resumeAt: resume.resumeAt,
    resumeReady: resume.ready,
    initialRow: resume.initialRow,
  });
  const { persist, deactivate, resetSample } = checkpoint;
  const sourceWatch = useSourceWatch({ source, videoId, enabled: canMarkWatched, videoRef, progress: checkpoint.progress, persist });
  const autoplay = useAutoplay({ videoRef, videoId, profileId, source, holdHistory, defaultAutoplay: settings?.autoplay ?? true, streamVersion });
  const { fullscreen, maxHeight: playerMaxHeight } = usePlayerLayout(frameRef, informationRef, [videoMeta, source, canMarkWatched, privatePlayback]);

  const sponsorSegments = useSponsorSegments(videoMeta?.platform === 'youtube' ? nativeId : undefined);
  const channelProfile = useChannelProfile(videoMeta?.channelId || null);
  const channelAvatar = channelProfile?.thumbnail ?? '';
  const channelSubscriberCount = channelProfile && channelProfile.subscriber_count > 0 ? channelProfile.subscriber_count : null;
  // The audience of a stream moves: it is asked again every minute (the server caches a channel's status that long).
  const liveNow = useChannelLive(videoMeta?.live ? videoMeta.channelId : '', 60_000);
  const [reactionPending, setReactionPending] = useState(false);
  const [shareOpen, setShareOpen] = useState(false);
  const [reactionError, setReactionError] = useState(false);
  const subscription = useChannelSubscription(
    videoMeta
      ? { id: videoMeta.channelId, platform: videoMeta.platform, name: videoMeta.channelName, thumbnail: videoMeta.channelThumbnail || channelAvatar }
      : null,
  );
  const seriesIndex = series && videoMeta ? series.items.findIndex((item) => item.video.id === videoId) : -1;
  const nextEpisode = seriesIndex >= 0 ? series?.items[seriesIndex + 1] : undefined;
  const contextError = seriesError ?? (series && videoMeta && seriesIndex < 0 ? 'series.invalidContext' : null);
  const seriesContinuation = useRef<{ video: VideoResult; seriesId: string } | null>(null);

  useEffect(() => {
    seriesContinuation.current = seriesId && nextEpisode && !contextError ? { video: nextEpisode.video, seriesId } : null;
  }, [seriesId, nextEpisode, contextError]);

  // What shows over the video once it ended: the next episode, or a replay with recommendations (never for a stream).
  const hasNext = !!(seriesId && nextEpisode && !contextError);
  const endKind = endScreenKind({ live: !!videoMeta?.live, hasNextEpisode: hasNext });
  const autoplayOn = settings?.autoplay ?? true;
  const seriesKeys = seriesId && series ? new Set(series.items.map((item) => videoKey(item.video))) : undefined;
  const recommendations = useRecommendations(profileId, videoId, !!videoMeta);
  const recommendationIds = useMemo(() => recommendations.videos.map((video) => video.id), [recommendations.videos]);
  const recommendationStatuses = useVideoStatuses(recommendationIds);
  const endSuggestions =
    ended && endKind === 'suggestions'
      ? endScreenSuggestions(recommendations.videos, recommendationStatuses.statuses, { exclude: seriesKeys, currentId: videoId })
      : [];

  // The player of another video starts from a clean end: no overlay, no pending advance.
  useEffect(() => {
    advancedOnEnd.current = false;
    setEnded(false);
  }, [videoId, streamVersion]);

  const advance = useCallback(() => {
    if (advancedOnEnd.current) {
      return;
    }
    advancedOnEnd.current = true;
    void checkpoint.whenSaved().then(() => {
      const continuation = seriesContinuation.current;
      if (mounted.current && continuation) {
        void openVideo(continuation.video, { series: continuation.seriesId, source });
      } else {
        advancedOnEnd.current = false;
      }
    });
  }, [checkpoint, openVideo, source]);

  const replay = () => {
    const video = videoRef.current;
    if (!video) {
      return;
    }
    setEnded(false);
    autoplay.setPlaybackPaused(false);
    video.currentTime = 0;
    void video.play().catch(() => undefined);
  };

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  useAbortableEffect(
    async (signal) => {
      setMetaError(null);
      // A redirect that already fetched these details (a pasted link) hands them over instead of asking again.
      const seeded = source ? undefined : seededVideoMeta(videoId);
      if (seeded && metaRetry === 0) {
        setVideoMeta(seeded);
        return;
      }
      setVideoMeta(null);
      try {
        const data = await api.videos.get(videoId, { source, signal, chapters: true });
        if (!signal.aborted) {
          setVideoMeta(data);
        }
      } catch (failure) {
        if (!signal.aborted) {
          setMetaError(requestError(failure));
        }
      }
    },
    [videoId, source, metaRetry],
  );

  // A new stream: the element and the hls.js engine are made again for the new version.
  const startStream = useCallback(() => {
    setStreamError(null);
    deactivate();
    setStreamUrl(api.videos.stream(videoId, { source }));
    setStreamVersion((value) => value + 1);
  }, [videoId, source, deactivate]);

  // The stream starts once everything that decides how it starts is known: the video, the settings and the resume position.
  const streamReady = !!videoMeta && (preferencesLoaded || preferencesFailed) && resume.ready;
  useEffect(() => {
    if (streamReady) {
      startStream();
    }
  }, [streamReady, startStream]);

  const samplePlayback = (includePlaybackBoundary = false) => {
    checkpoint.sample(includePlaybackBoundary);
    void sourceWatch.submit();
  };

  const startPlayback = () => {
    checkpoint.releaseHold();
    const video = videoRef.current;
    if (!video || video.seeking) {
      return;
    }
    samplePlayback(true);
    checkpoint.activate();
  };

  const stopPlayback = () => {
    samplePlayback();
    deactivate();
  };

  const retryStream = () => {
    moveResumeTo(videoRef.current?.currentTime ?? 0);
    startStream();
  };

  const mediaError = (failure?: ApiError) => {
    deactivate();
    // A refusal the server explains (rate limit, subscribers only, disabled source…) is shown as such.
    const explained = failure ? requestError(failure) : null;
    setStreamError(explained && explained !== 'errors.requestFailed' ? explained : 'player.streamFailed');
  };

  const {
    qualities,
    audioTracks,
    loading: manifestLoading,
  } = useHlsPlayback({
    videoRef,
    url: streamUrl,
    version: streamVersion,
    quality,
    audioLanguage,
    originalLanguage: videoMeta?.originalLanguage ?? '',
    resumeAt: resume.resumeAt,
    onFatal: mediaError,
  });
  const displayedQuality =
    quality === 'auto' || qualities.length === 0 ? 'auto' : String(qualities.find((height) => height <= Number(quality)) ?? qualities.at(-1));

  const toggleReaction = async (kind: 'like' | 'dislike') => {
    if (!videoMeta || !reactionReady || reactionPending) {
      return;
    }
    setReactionPending(true);
    setReactionError(false);
    try {
      await setReaction(profileId, videoId, reaction === kind ? null : kind);
    } catch {
      setReactionError(true);
    } finally {
      setReactionPending(false);
    }
  };

  const seekTo = useCallback(
    (seconds: number) => {
      const video = videoRef.current;
      if (video && video.readyState > 0) {
        video.currentTime = seconds;
      } else {
        moveResumeTo(seconds);
      }
    },
    [moveResumeTo],
  );

  const skipSponsor = useCallback((segment: SponsorSegment) => {
    const video = videoRef.current;
    if (!video || sponsorSkipPending.current) {
      return;
    }
    sponsorSkipPending.current = true;
    setActiveSponsorSegment(null);
    video.currentTime = Math.min(segment.end, Number.isFinite(video.duration) ? video.duration : segment.end);
  }, []);

  const updateSponsorSegment = useCallback(() => {
    const video = videoRef.current;
    if (!video || video.readyState === 0) {
      return;
    }
    const segment = sponsorSegments.find(({ start, end }) => video.currentTime >= start && video.currentTime < end) ?? null;
    setActiveSponsorSegment(segment);
    if (segment && autoSkipSponsors) {
      skipSponsor(segment);
    }
  }, [sponsorSegments, autoSkipSponsors, skipSponsor]);

  useEffect(() => {
    sponsorSkipPending.current = false;
    updateSponsorSegment();
  }, [updateSponsorSegment, streamVersion]);

  const playerError = streamError ?? metaError;
  const seriesPanel = seriesId ? (
    <>
      {seriesError && !series && <SeriesSyncStatus sync={null} error={seriesError} onRetry={retrySeries} />}
      {series && seriesIndex >= 0 ? (
        <SeriesEpisodeList series={series} index={seriesIndex} source={source} />
      ) : (
        !seriesError && (
          <div className="space-y-3 p-4 text-sm" aria-live="polite">
            {contextError ? (
              <>
                <p role="alert" className="text-destructive">
                  {t(contextError)}
                </p>
                <div className="flex flex-wrap gap-3">
                  <Button variant="outline" onClick={retrySeries}>
                    {t('common.retry')}
                  </Button>
                  <Button asChild variant="outline">
                    <Link to="/series/$seriesId" params={{ seriesId }}>
                      {t('series.open')}
                    </Link>
                  </Button>
                </div>
              </>
            ) : (
              <div className="space-y-3" aria-hidden="true">
                <Skeleton className="h-5 w-44" />
                <Skeleton className="h-4 w-2/3" />
              </div>
            )}
          </div>
        )
      )}
    </>
  ) : null;
  // What the side panel offers for this video; the panel is closed unless the user chose a section or the video suggests one.
  const sections: SidePanelSection[] = [];
  if (seriesId) {
    sections.push({
      id: 'series',
      label: series?.title ?? t('nav.series'),
      icon: <ListVideo aria-hidden="true" className="size-5" />,
      shortcut: 's',
      badge:
        series && seriesIndex >= 0 ? (
          <span className="rounded-full bg-muted px-3 py-1 text-xs font-medium text-muted-foreground">
            {t('series.position', { position: seriesIndex + 1, count: series.items.length })}
          </span>
        ) : undefined,
      scroll: false,
      children: () => seriesPanel,
    });
  }
  if (videoMeta?.description) {
    const { description, duration } = videoMeta;
    sections.push({
      id: 'description',
      label: t('player.description.title'),
      icon: <FileText aria-hidden="true" className="size-5" />,
      shortcut: 'd',
      children: () => (
        <div className="p-4">
          <VideoDescription description={description} duration={duration} collapsed={false} onSeek={seekTo} />
        </div>
      ),
    });
  }
  // The conversation about the video is "Comments" on every platform; what it shows depends on what the video supports.
  const comments = commentsMode(videoMeta);
  if (comments === 'list') {
    sections.push({
      id: COMMENTS_SECTION,
      label: t('player.comments.title'),
      icon: <MessageSquare aria-hidden="true" className="size-5" />,
      shortcut: 'c',
      children: () => <VideoComments videoId={videoId} source={source} />,
    });
  } else if (comments === 'chat') {
    const live = !!videoMeta?.live;
    sections.push({
      id: COMMENTS_SECTION,
      label: t('player.comments.title'),
      icon: <MessageSquare aria-hidden="true" className="size-5" />,
      shortcut: 'c',
      scroll: false,
      children: (open, report) => (
        <VideoChat videoId={videoId} login={nativeId} live={live} videoRef={videoRef} version={streamVersion} open={open} onUnread={report} />
      ),
    });
  }
  if (videoMeta) {
    sections.push({
      id: 'recommendations',
      label: t('player.recommendations.title'),
      icon: <Sparkles aria-hidden="true" className="size-5" />,
      shortcut: 'r',
      children: () => (
        <VideoRecommendations
          videos={recommendations.videos}
          loading={recommendations.loading}
          error={recommendations.error}
          onRetry={recommendations.retry}
          statuses={recommendationStatuses.statuses}
          statusesReady={recommendationStatuses.isReady && !recommendationStatuses.isError}
          exclude={seriesKeys}
        />
      ),
    });
  }
  const liveChat = isLiveChat(videoMeta);
  const preferredSection = liveChat ? COMMENTS_SECTION : seriesId ? 'series' : undefined;
  // A stream, a series episode and a plain video each remember what was open.
  const panelContext = liveChat ? 'live' : seriesId ? 'series' : 'video';
  const showSide = videoMeta ? sections.length > 0 : !metaError;
  return (
    <div ref={pageRef} data-player-layout={placement} className="w-full space-y-5">
      <PageTitle>{videoMeta?.title ?? <Skeleton aria-label={t('common.loading')} className="h-7 w-64 max-w-full" />}</PageTitle>
      <div className={showSide ? 'beside:flex beside:items-stretch beside:gap-4' : undefined}>
        <div className="min-w-0 space-y-5 beside:flex-1">
          {source && (
            <p className="rounded-xl border bg-card px-4 py-3 text-xs leading-relaxed text-muted-foreground">
              {t('player.connectedSession', { name: sourceInfo?.name ?? source })}
            </p>
          )}
          <section
            ref={frameRef}
            style={{ maxHeight: fullscreen ? undefined : playerMaxHeight }}
            aria-label={t('player.controls.player')}
            className="player-frame relative aspect-video overflow-hidden rounded-2xl bg-black shadow-sm"
          >
            {/* biome-ignore lint/a11y/useMediaCaption: HLS text tracks are provided dynamically by hls.js */}
            <video
              key={streamVersion}
              ref={videoRef}
              tabIndex={playerError ? -1 : 0}
              aria-label={t('player.controls.player')}
              autoPlay={autoplay.autoPlay}
              className="w-full h-full object-contain outline-none"
              onPlay={() => {
                setEnded(false);
                startPlayback();
              }}
              onCanPlay={autoplay.handleCanPlay}
              onVolumeChange={(event) => {
                if (!event.currentTarget.muted) {
                  autoplay.clearMutedFallback();
                }
              }}
              onPlaying={() => {
                if (!checkpoint.progress.current.active) {
                  startPlayback();
                }
              }}
              onTimeUpdate={() => {
                samplePlayback();
                updateSponsorSegment();
                checkpoint.persistIfDue();
              }}
              onWaiting={stopPlayback}
              onSeeking={() => {
                setEnded(false);
                resetSample();
              }}
              onSeeked={() => {
                sponsorSkipPending.current = false;
                resetSample();
                persist();
                updateSponsorSegment();
                if (!videoRef.current?.paused) {
                  startPlayback();
                }
              }}
              onRateChange={() => {
                stopPlayback();
                if (!videoRef.current?.paused) {
                  startPlayback();
                }
              }}
              onPause={() => {
                stopPlayback();
                persist();
              }}
              onEnded={(event) => {
                if (!event.currentTarget.ended) {
                  return;
                }
                autoplay.setPlaybackPaused(true);
                stopPlayback();
                persist({ ended: true });
                if (endKind) {
                  setEnded(true);
                }
              }}
              onError={(event) => {
                if (event.currentTarget === videoRef.current && event.currentTarget.getAttribute('src')) {
                  mediaError();
                }
              }}
              onLoadedMetadata={() => {
                checkpoint.noteLoaded();
                if (videoRef.current) {
                  applyResume(videoRef.current);
                }
                updateSponsorSegment();
              }}
            />
            {privatePlayback && fullscreen && (
              <>
                <PlaybackPrivacyFrame />
                <span className="pointer-events-none absolute left-4 top-4 inline-flex items-center gap-2 rounded-full bg-black/80 px-3 py-1.5 text-xs font-medium text-white">
                  <EyeOff aria-hidden="true" className="size-4" />
                  {t('player.privatePlaybackActive')}
                </span>
              </>
            )}
            {activeSponsorSegment && !playerError && (
              <Button type="button" size="sm" onClick={() => skipSponsor(activeSponsorSegment)} className="absolute right-3 top-3 z-10 shadow-lg">
                <SkipForward aria-hidden="true" className="size-4" />
                {t('player.sponsor.skip')}
              </Button>
            )}
            {autoplay.mutedFallback && !playerError && (
              <Button type="button" size="sm" variant="secondary" onClick={autoplay.unmute} className="absolute left-3 top-3 z-10 shadow-lg">
                <VolumeX aria-hidden="true" className="size-4" />
                {t('player.controls.unmute')}
              </Button>
            )}
            {ended && !playerError && endKind === 'next' && nextEpisode && (
              <NextEpisodeCard
                key={videoId}
                title={nextEpisode.video.title}
                countdown={autoplayOn ? NEXT_EPISODE_COUNTDOWN : null}
                onPlay={advance}
                onReplay={replay}
              />
            )}
            {ended && !playerError && endKind === 'suggestions' && <SuggestionsCard videos={endSuggestions} onReplay={replay} />}
            {playerError ? (
              <div role="alert" className="absolute inset-0 z-20 flex items-center justify-center overflow-y-auto bg-black/90 p-3 text-white sm:p-6">
                <div className="max-w-md space-y-2 text-center sm:space-y-3">
                  <CircleAlert aria-hidden="true" className="mx-auto hidden size-9 text-white/70 sm:block" />
                  <h2 className="text-sm font-semibold sm:text-lg">{t('player.errorTitle')}</h2>
                  <p className="text-xs leading-relaxed text-white/70 sm:text-sm">{t(playerError)}</p>
                  <Button size="sm" onClick={streamError ? retryStream : () => setMetaRetry((value) => value + 1)}>
                    <RotateCcw aria-hidden="true" className="size-4" />
                    {t(streamError ? 'player.retry' : 'player.retryDetails')}
                  </Button>
                </div>
              </div>
            ) : (
              <VideoControls
                videoRef={videoRef}
                frameRef={frameRef}
                version={streamVersion}
                quality={displayedQuality}
                qualities={qualities}
                onQualityChange={setQualityOverride}
                audioTracks={audioTracks}
                onAudioChange={setAudioLanguage}
                loading={!streamUrl || manifestLoading}
                chapters={videoMeta?.chapters}
                previewVideoId={videoMeta?.platform === 'youtube' ? nativeId : undefined}
                knownDuration={videoMeta?.duration ?? 0}
                resumePosition={resume.resumePosition}
                onSeek={seekTo}
                onPlaybackIntentChange={autoplay.setPlaybackPaused}
                onNext={seriesId && nextEpisode && !contextError ? () => void openVideo(nextEpisode.video, { series: seriesId, source }) : undefined}
                nextTitle={nextEpisode?.video.title}
                live={videoMeta?.live}
              />
            )}
          </section>
          {canMarkWatched && !privatePlayback && (
            <div className="space-y-2 rounded-xl border bg-card px-4 py-3 text-xs leading-relaxed text-muted-foreground" aria-live="polite">
              {sourceWatch.status === 'waiting' && <p>{t('player.history.waiting')}</p>}
              {sourceWatch.status === 'marking' && <p>{t('player.history.marking')}</p>}
              {sourceWatch.status === 'marked' && <p>{t('player.history.marked')}</p>}
              {sourceWatch.status === 'failed' && (
                <>
                  <p role="alert" className="text-destructive">
                    {t('player.history.failed')} {sourceWatch.error && t(sourceWatch.error)}
                  </p>
                  <Button variant="outline" onClick={() => void sourceWatch.submit(true)}>
                    {t('player.history.retry')}
                  </Button>
                </>
              )}
            </div>
          )}
          {!videoMeta ? (
            <div
              ref={metaError ? undefined : informationRef}
              hidden={!!metaError}
              aria-hidden="true"
              className="flex flex-wrap items-center justify-between gap-x-6 gap-y-3"
            >
              <div className="flex items-center gap-3">
                <Skeleton className="size-12 rounded-full" />
                <div className="space-y-2">
                  <Skeleton className="h-4 w-28" />
                  <Skeleton className="h-3 w-20" />
                </div>
                <Skeleton className="h-8 w-24 rounded-full" />
              </div>
              <Skeleton className="h-10 w-36 rounded-full" />
            </div>
          ) : (
            <div className="space-y-5">
              <div ref={informationRef}>
                <VideoInformation
                  video={videoMeta}
                  channelAvatar={videoMeta.channelThumbnail || channelAvatar}
                  subscriberCount={channelSubscriberCount}
                  viewers={liveNow?.viewers}
                  nativeId={nativeId}
                  subscribed={subscription.subscribed}
                  subscriptionDisabled={subscription.disabled}
                  reaction={reaction}
                  pending={reactionPending || !reactionReady}
                  onSubscribe={() => void subscription.toggle()}
                  onReact={(kind) => void toggleReaction(kind)}
                  onShare={() => setShareOpen(true)}
                />
              </div>
              {shareOpen && (
                <ShareDialog video={videoMeta} series={seriesId} getTime={() => videoRef.current?.currentTime ?? 0} onClose={() => setShareOpen(false)} />
              )}
              {subscription.error && (
                <p role="alert" className="text-sm text-destructive">
                  {t('channel.subscription.failed')}
                </p>
              )}
              {reactionError && (
                <p role="alert" className="text-sm text-destructive">
                  {t('player.reactions.failed')}
                </p>
              )}
            </div>
          )}
        </div>
        {showSide && (
          <div className="stacked:mt-5 beside:flex beside:shrink-0">
            <SidePanel sections={sections} preferred={preferredSection} context={panelContext} loading={!videoMeta} />
          </div>
        )}
      </div>
    </div>
  );
}
