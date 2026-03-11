import { Link, useNavigate } from '@tanstack/react-router';
import type { ComponentPropsWithoutRef } from 'react';
import { useCallback } from 'react';
import { api } from './api';
import { type ChannelTargetInput, channelTarget, type VideoTargetInput, videoTarget } from './link-targets';
import { seedVideoMeta } from './records';

type AnchorProps = Omit<ComponentPropsWithoutRef<'a'>, 'href'>;

/** A link to a video: its live page while it is a stream in progress, its record page otherwise (see `link-targets.ts`). */
export function VideoLink({ video, series, source, ...anchor }: AnchorProps & { video: VideoTargetInput; series?: string; source?: string }) {
  const target = videoTarget(video, { series, source });
  if (target.kind === 'live') {
    return <Link to="/live/$channelId" params={{ channelId: target.channelId }} {...anchor} />;
  }
  return (
    <Link to="/player/$videoId" params={{ videoId: target.videoId }} search={{ series: target.series }} state={{ viyuSource: target.source }} {...anchor} />
  );
}

/** A link to a channel page, optionally on its series tab. */
export function ChannelLink({ channel, tab, ...anchor }: AnchorProps & { channel: ChannelTargetInput; tab?: 'videos' | 'series' }) {
  const target = channelTarget(channel, tab);
  return (
    <Link to="/channel/$channelId" params={{ channelId: target.channelId }} search={{ tab: target.tab }} state={{ viyuSource: target.source }} {...anchor} />
  );
}

/** Imperative counterpart of `VideoLink`. */
export function useOpenVideo() {
  const navigate = useNavigate();
  return useCallback(
    (video: VideoTargetInput, options: { series?: string; source?: string; t?: number; replace?: boolean } = {}) => {
      const target = videoTarget(video, options);
      if (target.kind === 'live') {
        return navigate({ to: '/live/$channelId', params: { channelId: target.channelId }, replace: options.replace });
      }
      return navigate({
        to: '/player/$videoId',
        params: { videoId: target.videoId },
        search: { series: target.series, t: options.t },
        state: { viyuSource: target.source },
        replace: options.replace,
      });
    },
    [navigate],
  );
}

/** Opens a pasted external video link: the server stores the video and the player opens on its record, with the details already at hand. */
export function useOpenLink() {
  const openVideo = useOpenVideo();
  return useCallback(
    async (url: string, options: { source?: string; signal?: AbortSignal } = {}) => {
      const video = await api.videos.resolve(url, { signal: options.signal });
      if (options.signal?.aborted) {
        return;
      }
      seedVideoMeta(video);
      await openVideo(video, { source: options.source });
    },
    [openVideo],
  );
}

/** Imperative counterpart of `ChannelLink`. */
export function useOpenChannel() {
  const navigate = useNavigate();
  return useCallback(
    (channel: ChannelTargetInput, options: { tab?: 'videos' | 'series'; replace?: boolean } = {}) => {
      const target = channelTarget(channel, options.tab);
      return navigate({
        to: '/channel/$channelId',
        params: { channelId: target.channelId },
        search: { tab: target.tab },
        state: { viyuSource: target.source },
        replace: options.replace,
      });
    },
    [navigate],
  );
}
