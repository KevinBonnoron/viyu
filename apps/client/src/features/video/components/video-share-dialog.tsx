import { useRouter } from '@tanstack/react-router';
import { Check, Copy } from 'lucide-react';
import { useEffect, useMemo, useRef, useState } from 'react';
import { Button } from '@/components/ui/button';
import { Dialog } from '@/components/ui/dialog';
import { formatTimecode } from '@/features/video/utils/timecode';
import { videoTarget } from '@/lib/link-targets';
import { serverBase } from '@/lib/pocketbase';
import { formatDuration } from '@/lib/utils';
import { useI18n } from '@/providers/i18n-provider';
import type { VideoResult } from '@/types';

/** The page address a recipient can open, like `VideoLink` builds it: a record page, or the channel's live page for a stream in progress. A connected source is the sharer's own, so it is never part of the link. The link points at the server: a native shell's own origin is not an address anyone can open. */
function useShareHref(video: VideoResult, series?: string): string {
  const router = useRouter();
  return useMemo(() => {
    const target = videoTarget({ id: video.id, channelId: video.channelId, live: video.live }, { series });
    const location =
      target.kind === 'live'
        ? router.buildLocation({ to: '/live/$channelId', params: { channelId: target.channelId } })
        : router.buildLocation({ to: '/player/$videoId', params: { videoId: target.videoId }, search: { series: target.series } });
    return serverBase() + location.href;
  }, [router, video.id, video.channelId, video.live, series]);
}

/**
 * Offers the video's link, optionally carrying the position it is watched at. A stream in progress has no
 * timecode to share: the checkbox does not exist there.
 */
export function ShareDialog({ video, series, getTime, onClose }: { video: VideoResult; series?: string; getTime: () => number; onClose: () => void }) {
  const { t } = useI18n();
  const base = useShareHref(video, series);
  const live = !!video.live;
  // The position is captured when the dialog opens, not on every render of the ticking clock.
  const [seconds] = useState(() => Math.floor(getTime()));
  const [withTime, setWithTime] = useState(!live && seconds > 0);
  const [copied, setCopied] = useState(false);
  const [copyError, setCopyError] = useState(false);
  const mounted = useRef(false);

  const url = !live && withTime && seconds > 0 ? `${base}${base.includes('?') ? '&' : '?'}t=${formatTimecode(seconds)}` : base;

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(url);
      if (mounted.current) {
        setCopied(true);
        setCopyError(false);
      }
    } catch {
      if (mounted.current) {
        setCopyError(true);
      }
    }
  };

  return (
    <Dialog onClose={onClose} aria-labelledby="share-title" className="space-y-5">
      <h2 id="share-title" className="text-lg font-semibold">
        {t('player.share.title')}
      </h2>
      <div className="flex gap-2">
        <input
          readOnly
          value={url}
          onFocus={(event) => event.target.select()}
          aria-label={t('player.share.link')}
          className="min-w-0 flex-1 rounded-lg border bg-background px-3 py-2 text-sm text-muted-foreground"
        />
        <Button type="button" variant="secondary" onClick={() => void copy()} aria-live="polite">
          {copied ? <Check aria-hidden="true" className="size-4 text-success" /> : <Copy aria-hidden="true" className="size-4" />}
          {copied ? t('player.share.copied') : t('player.share.copy')}
        </Button>
      </div>
      {!live && seconds > 0 && (
        <label className="flex items-center gap-2 text-sm">
          <input type="checkbox" checked={withTime} onChange={(event) => setWithTime(event.target.checked)} className="size-4 accent-primary" />
          {t('player.share.includeTime', { time: formatDuration(seconds) })}
        </label>
      )}
      {copyError && (
        <p role="alert" className="text-sm text-destructive">
          {t('player.share.copyError')}
        </p>
      )}
    </Dialog>
  );
}
