import Hls, { type ErrorData, type Level } from 'hls.js';
import { type RefObject, useEffect, useRef, useState } from 'react';
import { type AudioOption, audioTrackIndex, audioTrackOptions, originalAudioTrackIndex } from '@/features/video/utils/audio-tracks';
import { type ApiError, apiErrorFrom } from '@/lib/api-error';
import { apiUrl, pb } from '@/lib/pocketbase';

/** The server's answer to a stream request hls.js gave up on (`{code, message}` with its status), when there is one. */
function streamFailure(data: ErrorData): ApiError | undefined {
  const status = data.response?.code;
  if (!status || status < 400) {
    return undefined;
  }
  const details: unknown = data.networkDetails;
  const text = details instanceof XMLHttpRequest && typeof details.responseText === 'string' ? details.responseText : '';
  let body: unknown = null;
  try {
    body = text ? JSON.parse(text) : null;
  } catch {
    body = null;
  }
  return apiErrorFrom(status, body);
}

/** Whether a request of hls.js goes to the server's own stream routes (never a third-party URL): only those carry the session token. */
function isStreamRequest(url: string): boolean {
  return url.startsWith(apiUrl('/api/stream/'));
}

function resolution(level: Level): number {
  return level.width > 0 ? Math.min(level.width, level.height) : level.height;
}

function playbackLevel(levels: readonly Level[], quality: string): number {
  const limit = quality === 'auto' ? Number.POSITIVE_INFINITY : Number(quality);
  const available = levels.map((level, index) => ({ level, index, height: resolution(level) })).filter(({ height }) => height > 0);
  const belowLimit = available.filter(({ height }) => height <= limit);
  const candidates = belowLimit.length ? belowLimit : available.filter(({ height }) => height === Math.min(...available.map((item) => item.height)));
  candidates.sort((a, b) => b.height - a.height || b.level.frameRate - a.level.frameRate || b.level.bitrate - a.level.bitrate);
  return candidates[0]?.index ?? -1;
}

export function useHlsPlayback({
  videoRef,
  url,
  version,
  quality,
  audioLanguage,
  originalLanguage,
  resumeAt,
  onFatal,
}: {
  videoRef: RefObject<HTMLVideoElement | null>;
  url: string | undefined;
  version: number;
  quality: string;
  audioLanguage: string;
  originalLanguage: string;
  resumeAt: RefObject<number>;
  /** Called when playback cannot go on; `failure` is the server's answer when a request of the stream failed. */
  onFatal: (failure?: ApiError) => void;
}) {
  const engine = useRef<Hls | null>(null);
  const selectedQuality = useRef(quality);
  const selectedAudio = useRef(audioLanguage);
  const original = useRef(originalLanguage);
  const fatal = useRef(onFatal);
  const [qualities, setQualities] = useState<number[]>([]);
  const [audioTracks, setAudioTracks] = useState<AudioOption[]>([]);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    fatal.current = onFatal;
  }, [onFatal]);
  useEffect(() => {
    selectedQuality.current = quality;
    const hls = engine.current;
    if (hls?.levels.length) {
      const level = playbackLevel(hls.levels, quality);
      if (level >= 0) {
        hls.currentLevel = level;
      }
    }
  }, [quality]);
  useEffect(() => {
    selectedAudio.current = audioLanguage;
    original.current = originalLanguage;
    const hls = engine.current;
    if (hls) {
      const index = audioLanguage === 'original' ? originalAudioTrackIndex(hls.audioTracks, originalLanguage) : audioTrackIndex(hls.audioTracks, audioLanguage);
      if (index >= 0 && index !== hls.audioTrack) {
        hls.audioTrack = index;
      }
      setAudioTracks(audioTrackOptions(hls.audioTracks, originalLanguage, hls.audioTrack));
    }
  }, [audioLanguage, originalLanguage]);

  useEffect(() => {
    const video = videoRef.current;
    if (!url || !video) {
      return;
    }
    let active = true;
    setQualities([]);
    setAudioTracks([]);
    setLoading(true);
    // hls.js (MSE) is the only path: a native HLS player (Safari) loads its manifest and segments without any way to set an
    // `Authorization` header, and the stream routes accept nothing else. A browser without MSE cannot play the stream.
    if (!Hls.isSupported()) {
      setLoading(false);
      fatal.current();
      return;
    }
    // The session token is read when each request is made, so a token refreshed mid-playback is the one used; only requests to the server's own stream routes carry it.
    const authorize = (requestUrl: string, headers: Headers) => {
      if (isStreamRequest(requestUrl)) {
        headers.set('Authorization', `Bearer ${pb.authStore.token}`);
      }
      return headers;
    };
    // Auto is explicitly the maximum resolution, not bandwidth adaptation.
    const hls = new Hls({
      autoStartLoad: false,
      capLevelToPlayerSize: false,
      audioPreference: original.current ? { lang: original.current } : undefined,
      xhrSetup: (xhr, requestUrl) => {
        if (isStreamRequest(requestUrl)) {
          xhr.setRequestHeader('Authorization', `Bearer ${pb.authStore.token}`);
        }
      },
      fetchSetup: (context, init) => new Request(context.url, { ...init, headers: authorize(context.url, new Headers(init?.headers)) }),
    });
    engine.current = hls;
    const selectAudio = () => {
      if (!active) {
        return;
      }
      const index =
        selectedAudio.current === 'original'
          ? originalAudioTrackIndex(hls.audioTracks, original.current)
          : audioTrackIndex(hls.audioTracks, selectedAudio.current);
      if (index >= 0 && hls.audioTrack !== index) {
        hls.audioTrack = index;
      }
      setAudioTracks(audioTrackOptions(hls.audioTracks, original.current, hls.audioTrack));
    };
    hls.on(Hls.Events.AUDIO_TRACKS_UPDATED, selectAudio);
    hls.on(Hls.Events.AUDIO_TRACK_SWITCHED, () => {
      if (active) {
        setAudioTracks(audioTrackOptions(hls.audioTracks, original.current, hls.audioTrack));
      }
    });
    hls.on(Hls.Events.MANIFEST_PARSED, () => {
      if (!active) {
        return;
      }
      setQualities([...new Set(hls.levels.map(resolution).filter((height) => height > 0))].sort((a, b) => b - a));
      const level = playbackLevel(hls.levels, selectedQuality.current);
      if (level < 0) {
        setLoading(false);
        fatal.current();
        return;
      }
      hls.startLevel = level;
      hls.currentLevel = level;
      selectAudio();
      // With no position to resume, let hls.js choose: the start of a VOD, the live edge of a stream.
      hls.startLoad(resumeAt.current > 0 ? resumeAt.current : -1);
      setLoading(false);
    });
    // A fatal error is recovered from once per kind before giving up; playback progressing again re-arms the recovery.
    const recovery = { media: false, network: false, at: -1 };
    const recovered = () => {
      recovery.at = video.currentTime;
    };
    const rearm = () => {
      if (recovery.at >= 0 && video.currentTime - recovery.at >= 1) {
        recovery.media = false;
        recovery.network = false;
        recovery.at = -1;
      }
    };
    video.addEventListener('timeupdate', rearm);
    hls.on(Hls.Events.ERROR, (_, data) => {
      if (!active || !data.fatal) {
        return;
      }
      if (data.type === Hls.ErrorTypes.MEDIA_ERROR && !recovery.media) {
        recovery.media = true;
        recovered();
        hls.recoverMediaError();
        return;
      }
      if (data.type === Hls.ErrorTypes.NETWORK_ERROR && !recovery.network) {
        recovery.network = true;
        recovered();
        // Before the manifest is known there is no fragment loading to resume: ask for the manifest again.
        if (hls.levels.length === 0) {
          hls.loadSource(url);
        } else {
          hls.startLoad();
        }
        return;
      }
      setLoading(false);
      video.pause();
      fatal.current(streamFailure(data));
    });
    hls.attachMedia(video);
    hls.loadSource(url);
    return () => {
      active = false;
      video.removeEventListener('timeupdate', rearm);
      if (engine.current === hls) {
        engine.current = null;
      }
      hls.destroy();
    };
  }, [url, version, videoRef, resumeAt]);

  return { qualities, audioTracks, loading };
}
