import {
  Check,
  ChevronLeft,
  ChevronRight,
  EyeOff,
  ListVideo,
  LoaderCircle,
  Maximize,
  Minimize,
  Pause,
  Play,
  Settings2,
  SkipForward,
  Volume2,
  VolumeX,
} from 'lucide-react';
import { type KeyboardEvent, type PointerEvent as ReactPointerEvent, type RefObject, useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type { AudioOption } from '@/features/video/utils/audio-tracks';
import { currentChapter, timelineChapters } from '@/features/video/utils/chapters';
import { keyFocus, OPEN_OVERLAY, playerKeyAction } from '@/features/video/utils/player-keys';
import { cn, formatDuration } from '@/lib/utils';
import { useI18n } from '@/providers/i18n-provider';
import { usePlaybackPrivacy } from '@/providers/playback-privacy-provider';
import type { VideoChapter } from '@/types';

type SettingsSection = 'quality' | 'audio' | 'speed';
const playbackRates = [0.25, 0.5, 0.75, 1, 1.25, 1.5, 1.75, 2] as const;
/** How far behind the live edge playback can be before it counts as "not live" and offers a way back, in seconds. hls.js itself starts a stream 10 to 20 seconds behind its edge. */
const LIVE_EDGE_TOLERANCE = 25;

/** The subtitle and caption tracks the video offers. */
function captionTracks(video: HTMLVideoElement): TextTrack[] {
  return Array.from(video.textTracks).filter((track) => track.kind === 'subtitles' || track.kind === 'captions');
}

/** Hides the captions when one is showing; else shows the one in the page's language, or the first. */
function toggleCaptions(video: HTMLVideoElement, language: string) {
  const tracks = captionTracks(video);
  if (tracks.some((track) => track.mode === 'showing')) {
    for (const track of tracks) {
      track.mode = 'hidden';
    }
    return;
  }
  const chosen = tracks.find((track) => track.language.toLowerCase().startsWith(language.toLowerCase())) ?? tracks[0];
  if (chosen) {
    chosen.mode = 'showing';
  }
}

function changeVolume(video: HTMLVideoElement, delta: number) {
  if (delta > 0 && video.muted) {
    video.muted = false;
  }
  video.volume = Math.min(1, Math.max(0, Math.round((video.volume + delta) * 100) / 100));
}

export function VideoControls({
  videoRef,
  frameRef,
  version,
  quality,
  qualities,
  onQualityChange,
  audioTracks,
  onAudioChange,
  loading,
  chapters,
  previewVideoId,
  knownDuration,
  resumePosition,
  onSeek,
  onPlaybackIntentChange,
  onNext,
  nextTitle,
  live,
}: {
  videoRef: RefObject<HTMLVideoElement | null>;
  frameRef: RefObject<HTMLElement | null>;
  version: number;
  quality: string;
  qualities: number[];
  onQualityChange: (quality: string) => void;
  audioTracks: AudioOption[];
  onAudioChange: (language: string) => void;
  loading: boolean;
  chapters?: VideoChapter[];
  previewVideoId?: string;
  knownDuration: number;
  resumePosition: number;
  onSeek: (seconds: number) => void;
  onPlaybackIntentChange: (paused: boolean) => void;
  /** Plays the next item of the series or playlist being watched; the button only exists when there is one. */
  onNext?: () => void;
  nextTitle?: string;
  /** A stream in progress: the time is replaced by a live indicator, and a button returns to the live edge when playback is behind it. */
  live?: boolean;
}) {
  const { t, language } = useI18n();
  const languageNames = useMemo(() => new Intl.DisplayNames([language], { type: 'language', languageDisplay: 'standard' }), [language]);
  const audioName = (track: AudioOption) => {
    if (track.language.startsWith('track-')) {
      return !track.name || track.name.toLowerCase() === 'default' ? t('player.audio.default') : track.name;
    }
    try {
      return languageNames.of(track.language) || track.name;
    } catch {
      return track.name;
    }
  };
  const [media, setMedia] = useState({
    time: resumePosition,
    duration: knownDuration,
    paused: true,
    muted: false,
    volume: 1,
    rate: 1,
    buffering: true,
    buffered: [] as Array<{ start: number; end: number }>,
  });
  const [seekPreview, setSeekPreview] = useState<number | null>(null);
  const seekDrag = useRef<{ pointerId: number | null; changed: boolean }>({ pointerId: null, changed: false });
  const timelineTime = seekPreview ?? media.time;
  const [hover, setHover] = useState<{ fraction: number } | null>(null);
  const [hoverPreviews] = useState(() => window.matchMedia('(min-width: 768px) and (hover: hover) and (pointer: fine)'));
  const [hoverPointer] = useState(() => window.matchMedia('(hover: hover) and (pointer: fine)'));
  const [failedFrames, setFailedFrames] = useState<Record<number, boolean>>({});
  const [fullscreen, setFullscreen] = useState(false);
  const [visible, setVisible] = useState(false);
  const visibleRef = useRef(false);
  const inputType = useRef('mouse');
  const scrubbing = useRef(false);
  const [controlsFocused, setControlsFocused] = useState(false);
  const [menuOpen, setMenuOpen] = useState(false);
  const [chaptersOpen, setChaptersOpen] = useState(false);
  const [menuPage, setMenuPage] = useState<'root' | SettingsSection>('root');
  const previousSection = useRef<SettingsSection>('quality');
  const playbackRate = useRef(1);
  const [actionError, setActionError] = useState(false);
  const { privatePlayback, setPrivatePlayback } = usePlaybackPrivacy();
  /** The confirmation shown briefly after the viewer switches private playback from the controls. */
  const [privacyNotice, setPrivacyNotice] = useState<boolean | null>(null);
  useEffect(() => {
    if (privacyNotice === null) {
      return;
    }
    const timer = window.setTimeout(() => setPrivacyNotice(null), 2000);
    return () => window.clearTimeout(timer);
  }, [privacyNotice]);
  const frameHovered = useRef(false);
  const controlsHover = useRef(false);
  const hideTimer = useRef<number | undefined>(undefined);
  const reveal = useCallback(() => {
    visibleRef.current = true;
    setVisible(true);
    clearTimeout(hideTimer.current);
    if (!controlsHover.current && !scrubbing.current) {
      hideTimer.current = window.setTimeout(
        () => {
          visibleRef.current = false;
          setVisible(false);
        },
        inputType.current === 'touch' ? 3000 : 1200,
      );
    }
  }, []);
  const hide = useCallback(() => {
    frameHovered.current = false;
    controlsHover.current = false;
    clearTimeout(hideTimer.current);
    visibleRef.current = false;
    setVisible(false);
    setHover(null);
    setMenuOpen(false);
    setChaptersOpen(false);
  }, []);
  const menuRef = useRef<HTMLDivElement>(null);
  const chaptersRef = useRef<HTMLElement>(null);
  const chaptersButtonRef = useRef<HTMLButtonElement>(null);
  const gearRef = useRef<HTMLButtonElement>(null);
  const nextRef = useRef(onNext);
  const spaceTaken = useRef(false);
  useEffect(() => {
    nextRef.current = onNext;
  }, [onNext]);
  const playPause = useCallback(() => {
    const video = videoRef.current;
    if (!video) {
      return;
    }
    setActionError(false);
    onPlaybackIntentChange(!video.paused);
    if (video.paused) {
      if (video.ended) {
        video.currentTime = 0;
      }
      void video.play().catch((error: unknown) => {
        if (!(error instanceof DOMException && error.name === 'AbortError')) {
          setActionError(true);
        }
      });
    } else {
      video.pause();
    }
  }, [videoRef, onPlaybackIntentChange]);

  const toggleMute = useCallback(() => {
    const video = videoRef.current;
    if (!video) {
      return;
    }
    if (video.muted || video.volume === 0) {
      video.muted = false;
      if (video.volume === 0) {
        video.volume = 0.5;
      }
    } else {
      video.muted = true;
    }
  }, [videoRef]);

  // A stream's live edge is the end of what can be sought.
  const liveEdge = live && videoRef.current && videoRef.current.seekable.length > 0 ? videoRef.current.seekable.end(videoRef.current.seekable.length - 1) : 0;
  const behindLive = !!live && liveEdge > 0 && liveEdge - media.time > LIVE_EDGE_TOLERANCE;
  const goLive = useCallback(() => {
    const video = videoRef.current;
    if (!video || video.seekable.length === 0) {
      return;
    }
    onSeek(Math.max(0, video.seekable.end(video.seekable.length - 1) - 2));
    if (video.paused) {
      void video.play().catch(() => undefined);
    }
  }, [videoRef, onSeek]);

  const toggleFullscreen = useCallback(() => {
    setActionError(false);
    const action = document.fullscreenElement ? document.exitFullscreen() : frameRef.current?.requestFullscreen({ navigationUI: 'hide' });
    if (action) {
      void action.catch(() => setActionError(true));
    }
  }, [frameRef]);

  useEffect(() => {
    const video = videoRef.current;
    const frame = frameRef.current;
    if (!video || !frame) {
      return;
    }
    video.playbackRate = playbackRate.current;
    const synchronize = () => {
      const duration =
        Number.isFinite(video.duration) && video.duration > 0 ? video.duration : Number.isFinite(knownDuration) && knownDuration > 0 ? knownDuration : 0;
      const time = video.readyState > 0 ? video.currentTime : resumePosition;
      playbackRate.current = video.playbackRate;
      setMedia({
        time: Math.max(0, duration > 0 ? Math.min(time, duration) : time),
        duration,
        paused: video.paused,
        muted: video.muted,
        volume: video.volume,
        rate: video.playbackRate,
        buffering: !video.paused && video.readyState < 3,
        buffered: Array.from({ length: video.buffered.length }, (_, index) => ({ start: video.buffered.start(index), end: video.buffered.end(index) })),
      });
    };
    const enter = (event: PointerEvent) => {
      if (event.pointerType === 'touch' || !hoverPointer.matches) {
        return;
      }
      inputType.current = event.pointerType;
      frameHovered.current = true;
      reveal();
    };
    const pointerDown = (event: PointerEvent) => {
      inputType.current = event.pointerType;
      setControlsFocused(false);
      if (event.pointerType !== 'touch') {
        enter(event);
      } else {
        frameHovered.current = false;
        controlsHover.current = false;
        if (event.target !== video) {
          reveal();
        }
      }
    };
    const leave = (event: PointerEvent) => {
      if (event.pointerType !== 'touch' && hoverPointer.matches && !scrubbing.current) {
        hide();
      }
    };
    const clickFrame = (event: MouseEvent) => {
      if (event.target instanceof Element && event.target.closest('button, input, a, [role="menu"], [data-chapters]')) {
        return;
      }
      if (inputType.current === 'touch') {
        if (visibleRef.current) {
          hide();
        } else {
          reveal();
        }
      } else {
        playPause();
      }
    };
    const focusInside = (event: FocusEvent) => {
      if (event.target instanceof HTMLElement && event.target.matches(':focus-visible') && inputType.current !== 'touch') {
        reveal();
      }
    };
    const revealInside = () => {
      if (frameHovered.current || frame.contains(document.activeElement)) {
        reveal();
      }
    };
    // One handler for the whole page: the player answers its keys wherever the focus is, unless the focus belongs to something else.
    const keyboard = (event: globalThis.KeyboardEvent) => {
      inputType.current = 'keyboard';
      const target = event.target instanceof Element ? event.target : null;
      const action = playerKeyAction(
        event,
        keyFocus(target, { inPlayer: !!target && frame.contains(target), overlayOpen: document.querySelector(OPEN_OVERLAY) !== null }),
        { hasNext: !!nextRef.current, hasCaptions: captionTracks(video).length > 0 },
      );
      if (!action) {
        return;
      }
      event.preventDefault();
      spaceTaken.current = event.key === ' ' || event.key === 'Spacebar';
      switch (action.type) {
        case 'toggle':
          playPause();
          break;
        case 'mute':
          toggleMute();
          break;
        case 'fullscreen':
          toggleFullscreen();
          break;
        case 'next':
          nextRef.current?.();
          break;
        case 'captions':
          toggleCaptions(video, language);
          break;
        case 'volume':
          changeVolume(video, action.delta);
          break;
        case 'seek': {
          const duration = Number.isFinite(video.duration) && video.duration > 0 ? video.duration : knownDuration;
          if (duration > 0) {
            const time = video.readyState > 0 ? video.currentTime : resumePosition;
            onSeek(Math.max(0, Math.min(duration, time + action.seconds)));
            synchronize();
          }
          break;
        }
        default:
      }
      reveal();
    };
    // Space activates a button when it is released: a press the player took must not click the focused button as well.
    const releaseSpace = (event: globalThis.KeyboardEvent) => {
      if ((event.key === ' ' || event.key === 'Spacebar') && spaceTaken.current) {
        spaceTaken.current = false;
        event.preventDefault();
      }
    };
    const events = [
      'timeupdate',
      'durationchange',
      'loadedmetadata',
      'play',
      'pause',
      'ended',
      'volumechange',
      'ratechange',
      'progress',
      'waiting',
      'playing',
      'seeked',
    ];
    for (const event of events) {
      video.addEventListener(event, synchronize);
    }
    frame.addEventListener('click', clickFrame);
    video.addEventListener('play', revealInside);
    video.addEventListener('playing', revealInside);
    frame.addEventListener('pointerenter', enter);
    frame.addEventListener('pointermove', enter);
    frame.addEventListener('pointerdown', pointerDown);
    frame.addEventListener('pointerleave', leave);
    frame.addEventListener('focusin', focusInside);
    window.addEventListener('keydown', keyboard, true);
    window.addEventListener('keyup', releaseSpace, true);
    synchronize();
    frameHovered.current = window.matchMedia('(hover: hover)').matches && frame.matches(':hover');
    if (frameHovered.current) {
      reveal();
    }
    return () => {
      clearTimeout(hideTimer.current);
      for (const event of events) {
        video.removeEventListener(event, synchronize);
      }
      frame.removeEventListener('click', clickFrame);
      video.removeEventListener('play', revealInside);
      video.removeEventListener('playing', revealInside);
      frame.removeEventListener('pointerenter', enter);
      frame.removeEventListener('pointermove', enter);
      frame.removeEventListener('pointerdown', pointerDown);
      frame.removeEventListener('pointerleave', leave);
      frame.removeEventListener('focusin', focusInside);
      window.removeEventListener('keydown', keyboard, true);
      window.removeEventListener('keyup', releaseSpace, true);
    };
  }, [videoRef, frameRef, version, playPause, toggleMute, toggleFullscreen, reveal, hide, knownDuration, resumePosition, onSeek, language, hoverPointer]);

  useEffect(() => {
    const update = () => setFullscreen(document.fullscreenElement === frameRef.current);
    document.addEventListener('fullscreenchange', update);
    return () => document.removeEventListener('fullscreenchange', update);
  }, [frameRef]);

  useEffect(() => {
    if (!menuOpen) {
      return;
    }
    const outside = (event: PointerEvent) => {
      if (event.target instanceof Node && !menuRef.current?.contains(event.target) && !gearRef.current?.contains(event.target)) {
        setMenuOpen(false);
      }
    };
    document.addEventListener('pointerdown', outside);
    return () => document.removeEventListener('pointerdown', outside);
  }, [menuOpen]);

  useEffect(() => {
    if (!chaptersOpen) {
      return;
    }
    const outside = (event: PointerEvent) => {
      if (event.target instanceof Node && !chaptersRef.current?.contains(event.target) && !chaptersButtonRef.current?.contains(event.target)) {
        setChaptersOpen(false);
      }
    };
    document.addEventListener('pointerdown', outside);
    return () => document.removeEventListener('pointerdown', outside);
  }, [chaptersOpen]);

  useEffect(() => {
    if (!menuOpen) {
      return;
    }
    const selected = menuPage === 'root' ? `[data-settings-section="${previousSection.current}"]:not(:disabled)` : '[aria-checked="true"]';
    const target =
      menuRef.current?.querySelector<HTMLButtonElement>(selected) ??
      menuRef.current?.querySelector<HTMLButtonElement>('[role="menuitem"]:not(:disabled),[role="menuitemradio"]');
    target?.focus({ preventScroll: true });
  }, [menuOpen, menuPage]);

  const menuKeyboard = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key === 'Escape' || (event.key === 'ArrowLeft' && menuPage !== 'root')) {
      event.preventDefault();
      event.stopPropagation();
      if (menuPage !== 'root') {
        setMenuPage('root');
      } else {
        setMenuOpen(false);
        gearRef.current?.focus({ preventScroll: true });
      }
      return;
    }
    if (event.key === 'ArrowRight' && menuPage === 'root') {
      event.preventDefault();
      event.stopPropagation();
      if (document.activeElement instanceof HTMLButtonElement && document.activeElement.dataset.settingsSection) {
        document.activeElement.click();
      }
      return;
    }
    const items = [...event.currentTarget.querySelectorAll<HTMLButtonElement>('[role="menuitem"]:not(:disabled),[role="menuitemradio"]')];
    const focused = document.activeElement;
    const current = focused instanceof HTMLButtonElement ? items.indexOf(focused) : -1;
    const next =
      event.key === 'Home'
        ? 0
        : event.key === 'End'
          ? items.length - 1
          : event.key === 'ArrowDown'
            ? (current + 1) % items.length
            : event.key === 'ArrowUp'
              ? (current - 1 + items.length) % items.length
              : -1;
    if (next >= 0) {
      event.preventDefault();
      event.stopPropagation();
      items[next]?.focus();
    }
  };
  const chapterList = useMemo(() => timelineChapters(chapters, media.duration), [chapters, media.duration]);
  const activeChapter = currentChapter(chapterList, timelineTime);
  const chapterSegments = useMemo(() => {
    if (!(media.duration > 0)) {
      return [];
    }
    const segments: Array<{ chapter: VideoChapter; start: number; end: number }> = [];
    for (let index = 0; index < chapterList.length; index++) {
      const chapter = chapterList[index];
      if (chapter.start_time >= media.duration) {
        continue;
      }
      const next = chapterList[index + 1];
      segments.push({ chapter, start: chapter.start_time, end: Math.min(next ? next.start_time : media.duration, media.duration) });
    }
    return segments;
  }, [chapterList, media.duration]);
  const hoverTime = hover !== null && media.duration > 0 ? hover.fraction * media.duration : null;
  const hoveredChapterIndex = hoverTime === null ? -1 : chapterSegments.findIndex((segment) => hoverTime >= segment.start && hoverTime < segment.end);
  const hoveredChapter = hoveredChapterIndex >= 0 ? chapterSegments[hoveredChapterIndex].chapter : null;
  const show = visible || media.paused || menuOpen || chaptersOpen || controlsFocused;
  const holdControls = () => {
    scrubbing.current = true;
    clearTimeout(hideTimer.current);
  };
  const releaseControls = () => {
    scrubbing.current = false;
    reveal();
  };
  const commitSeek = (seconds: number) => {
    onSeek(seconds);
    setMedia((previous) => ({ ...previous, time: seconds }));
    setSeekPreview(null);
  };
  const cancelSeek = (event: ReactPointerEvent<HTMLInputElement>) => {
    if (seekDrag.current.pointerId !== event.pointerId) {
      return;
    }
    seekDrag.current.pointerId = null;
    seekDrag.current.changed = false;
    setSeekPreview(null);
    releaseControls();
  };
  const progress = media.duration > 0 ? (timelineTime / media.duration) * 100 : 0;
  const buttonClass =
    'flex size-11 shrink-0 touch-manipulation items-center justify-center rounded-lg text-white/90 transition-colors hover:bg-white/15 hover:text-white focus-visible:ring-white focus-visible:ring-offset-0 disabled:opacity-40 sm:size-9';
  const selectedAudio = audioTracks.find((track) => track.selected);
  const rateLabel = (rate: number) => (rate === 1 ? t('player.speed.normal') : `${rate.toLocaleString(language, { maximumFractionDigits: 2 })}×`);
  const settingsEntries = [
    { id: 'quality', label: t('player.quality.label'), value: quality === 'auto' ? t('player.quality.auto') : `${quality}p`, disabled: qualities.length === 0 },
    {
      id: 'audio',
      label: t('player.audio.label'),
      value: selectedAudio ? `${audioName(selectedAudio)}${selectedAudio.original ? ` · ${t('player.audio.original')}` : ''}` : t('player.audio.default'),
      disabled: audioTracks.length === 0,
    },
    { id: 'speed', label: t('player.speed.label'), value: rateLabel(media.rate), disabled: false },
  ] as const;

  return (
    <div
      className="pointer-events-none absolute inset-0 text-white"
      onFocusCapture={(event) => {
        if (inputType.current === 'keyboard' && event.target.matches(':focus-visible')) {
          setControlsFocused(true);
        }
      }}
      onBlurCapture={(event) => {
        if (!(event.relatedTarget instanceof Node) || !event.currentTarget.contains(event.relatedTarget)) {
          setControlsFocused(false);
        }
      }}
      onKeyDownCapture={() => setControlsFocused(true)}
      onPointerEnter={(event) => {
        if (event.pointerType !== 'touch' && hoverPointer.matches) {
          controlsHover.current = true;
          reveal();
        }
      }}
      onPointerLeave={(event) => {
        if (event.pointerType === 'touch' || !hoverPointer.matches) {
          return;
        }
        controlsHover.current = false;
        if (event.relatedTarget instanceof Node && frameRef.current?.contains(event.relatedTarget)) {
          reveal();
        } else {
          hide();
        }
      }}
    >
      {(loading || media.buffering) && (
        <div className="absolute inset-0 flex items-center justify-center">
          <LoaderCircle aria-label={t('player.preparing')} className="size-9 animate-spin text-white/90" />
        </div>
      )}
      {actionError && (
        <p role="alert" className="absolute left-4 top-4 rounded-lg bg-black/80 px-3 py-2 text-xs">
          {t('player.controls.actionError')}
        </p>
      )}
      <p
        aria-live="polite"
        className={cn('absolute left-1/2 top-4 -translate-x-1/2 rounded-full bg-black/80 px-3 py-1.5 text-xs font-medium', privacyNotice === null && 'sr-only')}
      >
        {privacyNotice !== null && (
          <span className="inline-flex items-center gap-2">
            <EyeOff aria-hidden="true" className="size-4" />
            {t(privacyNotice ? 'player.privatePlaybackActive' : 'player.privatePlaybackInactive')}
          </span>
        )}
      </p>
      {chaptersOpen && chapterList.length > 0 && (
        <section
          ref={chaptersRef}
          data-chapters
          aria-label={t('player.controls.chapters')}
          onKeyDown={(event) => {
            if (event.key === 'Escape') {
              event.stopPropagation();
              setChaptersOpen(false);
              chaptersButtonRef.current?.focus();
            }
          }}
          className="pointer-events-auto absolute bottom-20 right-3 z-20 max-h-[calc(100%-6rem)] w-80 max-w-[calc(100%-1.5rem)] overflow-y-auto rounded-xl border border-white/15 bg-zinc-950/95 p-1.5 shadow-xl"
        >
          {chapterList.map((chapter) => (
            <button
              key={`${chapter.start_time}:${chapter.end_time}:${chapter.title}`}
              type="button"
              aria-current={chapter === activeChapter ? 'true' : undefined}
              onClick={() => {
                onSeek(chapter.start_time);
                setChaptersOpen(false);
                chaptersButtonRef.current?.focus();
              }}
              className="flex w-full items-start gap-3 rounded-lg px-3 py-2 text-left text-sm hover:bg-white/10 focus-visible:bg-white/10"
            >
              <span className="shrink-0 tabular-nums text-white/60">{formatDuration(Math.floor(chapter.start_time))}</span>
              <span className={cn('min-w-0 break-words', chapter === activeChapter && 'text-primary')}>{chapter.title}</span>
            </button>
          ))}
        </section>
      )}
      {menuOpen && (
        <div
          ref={menuRef}
          id="video-settings-menu"
          role="menu"
          aria-label={menuPage === 'root' ? t('player.controls.settings') : settingsEntries.find((entry) => entry.id === menuPage)?.label}
          onKeyDown={menuKeyboard}
          className="pointer-events-auto absolute bottom-16 right-3 z-20 max-h-[calc(100%-5rem)] w-72 max-w-[calc(100%-1.5rem)] overflow-y-auto rounded-xl border border-white/15 bg-zinc-950/95 p-1.5 shadow-xl backdrop-blur-md"
        >
          {menuPage === 'root' ? (
            settingsEntries.map((entry) => (
              <button
                key={entry.id}
                type="button"
                role="menuitem"
                aria-haspopup="menu"
                data-settings-section={entry.id}
                disabled={entry.disabled}
                onClick={() => {
                  previousSection.current = entry.id;
                  setMenuPage(entry.id);
                }}
                className="flex w-full items-center gap-3 rounded-lg px-3 py-2.5 text-left text-sm hover:bg-white/10 focus-visible:bg-white/10 disabled:opacity-40"
              >
                <span className="shrink-0">{entry.label}</span>
                <span className="ml-auto min-w-0 truncate text-xs text-white/60" title={entry.value}>
                  {entry.value}
                </span>
                <ChevronRight aria-hidden="true" className="size-4 shrink-0 text-white/60" />
              </button>
            ))
          ) : (
            <>
              <button
                type="button"
                role="menuitem"
                aria-label={t('player.controls.back')}
                onClick={() => setMenuPage('root')}
                className="sticky top-0 mb-1 flex w-full items-center gap-3 border-b border-white/10 bg-zinc-950 px-3 py-2.5 text-left text-sm hover:bg-white/10 focus-visible:bg-white/10"
              >
                <ChevronLeft aria-hidden="true" className="size-4 shrink-0" />
                {settingsEntries.find((entry) => entry.id === menuPage)?.label}
              </button>
              {menuPage === 'quality' &&
                ['auto', ...qualities.map(String)].map((option) => (
                  <button
                    key={option}
                    type="button"
                    role="menuitemradio"
                    aria-checked={quality === option}
                    onClick={() => {
                      onQualityChange(option);
                      setMenuPage('root');
                    }}
                    className="flex w-full items-center gap-3 rounded-lg px-3 py-2 text-left text-sm hover:bg-white/10 focus-visible:bg-white/10"
                  >
                    <span className="size-4 shrink-0">{quality === option && <Check aria-hidden="true" className="size-4 text-primary" />}</span>
                    {option === 'auto' ? t('player.quality.auto') : `${option}p`}
                  </button>
                ))}
              {menuPage === 'audio' &&
                audioTracks.map((track) => (
                  <button
                    key={track.language}
                    type="button"
                    role="menuitemradio"
                    aria-checked={track.selected}
                    onClick={() => {
                      onAudioChange(track.language);
                      setMenuPage('root');
                    }}
                    className="flex w-full items-center gap-3 rounded-lg px-3 py-2 text-left text-sm hover:bg-white/10 focus-visible:bg-white/10"
                  >
                    <span className="size-4 shrink-0">{track.selected && <Check aria-hidden="true" className="size-4 text-primary" />}</span>
                    <span>
                      {audioName(track)}
                      {track.original && <span className="ml-1 text-white/50">· {t('player.audio.original')}</span>}
                    </span>
                  </button>
                ))}
              {menuPage === 'speed' &&
                playbackRates.map((rate) => (
                  <button
                    key={rate}
                    type="button"
                    role="menuitemradio"
                    aria-checked={media.rate === rate}
                    onClick={() => {
                      const video = videoRef.current;
                      if (video) {
                        video.playbackRate = rate;
                      }
                      setMenuPage('root');
                    }}
                    className="flex w-full items-center gap-3 rounded-lg px-3 py-2 text-left text-sm hover:bg-white/10 focus-visible:bg-white/10"
                  >
                    <span className="size-4 shrink-0">{media.rate === rate && <Check aria-hidden="true" className="size-4 text-primary" />}</span>
                    {rateLabel(rate)}
                  </button>
                ))}
            </>
          )}
        </div>
      )}
      <div
        data-player-controls=""
        inert={!show}
        className={cn(
          'absolute inset-x-0 bottom-0 bg-gradient-to-t from-black/90 via-black/65 to-transparent px-3 pb-2 pt-6 sm:px-4 sm:pb-3 sm:pt-9',
          show ? 'pointer-events-auto opacity-100' : 'pointer-events-none opacity-0',
        )}
      >
        <div
          className="relative mb-1 flex h-8 items-center sm:h-5"
          onPointerMove={(event) => {
            if (event.pointerType === 'touch' || !hoverPreviews.matches) {
              setHover(null);
              return;
            }
            const rect = event.currentTarget.getBoundingClientRect();
            if (rect.width > 0) {
              setHover({ fraction: Math.min(1, Math.max(0, (event.clientX - rect.left) / rect.width)) });
            }
          }}
          onPointerLeave={() => setHover(null)}
        >
          {hover !== null &&
            media.duration > 0 &&
            (previewVideoId || hoveredChapter) &&
            (() => {
              const frame = Math.min(3, Math.floor(hover.fraction * 4));
              return (
                <div
                  aria-hidden="true"
                  className="player-hover-preview pointer-events-none absolute bottom-full z-10 mb-2 min-w-40 max-w-64 -translate-x-1/2 overflow-hidden rounded-md border border-white/20 bg-black shadow-lg"
                  style={{ left: `${hover.fraction * 100}%` }}
                >
                  {hoveredChapter && <p className="px-2 py-1 text-center text-xs font-medium text-white">{hoveredChapter.title}</p>}
                  {previewVideoId && !failedFrames[frame] ? (
                    <img
                      key={frame}
                      src={`https://i.ytimg.com/vi/${previewVideoId}/${frame}.jpg`}
                      alt=""
                      className="aspect-video w-full object-cover"
                      onError={() => setFailedFrames((failed) => ({ ...failed, [frame]: true }))}
                    />
                  ) : null}
                  <p className="bg-black/80 px-1.5 py-0.5 text-center text-[11px] tabular-nums text-white">
                    {formatDuration(Math.floor(hover.fraction * media.duration))}
                  </p>
                </div>
              );
            })()}
          <div aria-hidden="true" className="absolute inset-x-0 h-1 overflow-hidden rounded-full bg-white/25">
            {media.duration > 0 &&
              media.buffered.map((range) => (
                <span
                  key={range.start}
                  className="absolute h-full bg-white/30"
                  style={{ left: `${(range.start / media.duration) * 100}%`, width: `${((range.end - range.start) / media.duration) * 100}%` }}
                />
              ))}
            <span className="absolute inset-y-0 left-0 bg-primary" style={{ width: `${progress}%` }} />
          </div>
          {chapterSegments.map((segment, index) => (
            <span
              key={`${segment.start}:${segment.end}:${segment.chapter.title}`}
              className={cn(
                'pointer-events-none absolute top-1/2 -translate-y-1/2 rounded-full transition-[height,background-color]',
                hoveredChapterIndex === index ? 'h-2 bg-white/40' : 'h-1 bg-transparent',
              )}
              style={{ left: `${(segment.start / media.duration) * 100}%`, width: `${((segment.end - segment.start) / media.duration) * 100}%` }}
            />
          ))}
          <input
            type="range"
            data-player-seek
            aria-label={t('player.controls.seek')}
            aria-valuetext={`${formatDuration(Math.floor(timelineTime))} / ${formatDuration(Math.floor(media.duration))}${activeChapter ? ` · ${activeChapter.title}` : ''}`}
            min={0}
            max={media.duration || 1}
            step={0.1}
            value={timelineTime}
            disabled={media.duration <= 0}
            onPointerDown={(event) => {
              if (!event.isPrimary || event.button !== 0 || seekDrag.current.pointerId !== null) {
                return;
              }
              seekDrag.current.pointerId = event.pointerId;
              seekDrag.current.changed = false;
              setSeekPreview(Number(event.currentTarget.value));
              event.currentTarget.setPointerCapture(event.pointerId);
              holdControls();
            }}
            onPointerUp={(event) => {
              if (seekDrag.current.pointerId !== event.pointerId) {
                return;
              }
              seekDrag.current.pointerId = null;
              if (seekDrag.current.changed) {
                commitSeek(Number(event.currentTarget.value));
              } else {
                setSeekPreview(null);
              }
              seekDrag.current.changed = false;
              releaseControls();
            }}
            onPointerCancel={cancelSeek}
            onLostPointerCapture={cancelSeek}
            onChange={(event) => {
              const seconds = Number(event.target.value);
              if (seekDrag.current.pointerId !== null) {
                seekDrag.current.changed = true;
                setSeekPreview(seconds);
              } else {
                commitSeek(seconds);
              }
            }}
            className="player-range relative h-8 w-full cursor-pointer appearance-none bg-transparent text-white outline-none focus-visible:ring-2 focus-visible:ring-white disabled:cursor-wait sm:h-5"
          />
        </div>
        <div className="flex items-center gap-0 sm:gap-2">
          <button
            type="button"
            aria-label={t(media.paused ? 'player.controls.play' : 'player.controls.pause')}
            title={t(media.paused ? 'player.controls.play' : 'player.controls.pause')}
            data-shortcut="k"
            aria-keyshortcuts="k Space"
            onClick={playPause}
            className={buttonClass}
          >
            {media.paused ? <Play aria-hidden="true" className="size-5 fill-current" /> : <Pause aria-hidden="true" className="size-5 fill-current" />}
          </button>
          {onNext && (
            <button
              type="button"
              aria-label={t('player.controls.next')}
              title={nextTitle ? `${t('player.controls.next')} · ${nextTitle}` : t('player.controls.next')}
              data-shortcut="n"
              aria-keyshortcuts="n"
              onClick={onNext}
              className={buttonClass}
            >
              <SkipForward aria-hidden="true" className="size-5 fill-current" />
            </button>
          )}
          {live ? (
            behindLive ? (
              <button
                type="button"
                aria-label={t('player.controls.goLive')}
                title={`${t('player.controls.goLive')} (−${formatDuration(Math.round(liveEdge - media.time))})`}
                onClick={goLive}
                className="inline-flex items-center gap-1.5 rounded-full bg-red-600 px-2.5 py-1 text-[11px] font-bold uppercase tracking-wide text-white hover:bg-red-500 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-white"
              >
                <SkipForward aria-hidden="true" className="size-3.5 fill-current" />
                {t('player.controls.goLive')}
              </button>
            ) : (
              <span className="inline-flex items-center gap-1.5 text-[11px] font-bold uppercase tracking-wide text-red-500 sm:text-xs">
                <span aria-hidden="true" className="size-2 rounded-full bg-red-500" />
                {t('channel.live')}
              </span>
            )
          ) : (
            <span className="whitespace-nowrap text-[11px] tabular-nums sm:text-xs">
              {formatDuration(Math.floor(timelineTime))}
              <span className="hidden min-[360px]:inline">
                <span className="px-1 text-white/50">/</span>
                {formatDuration(Math.floor(media.duration))}
              </span>
            </span>
          )}
          <button
            type="button"
            aria-label={t(media.muted || media.volume === 0 ? 'player.controls.unmute' : 'player.controls.mute')}
            title={t(media.muted || media.volume === 0 ? 'player.controls.unmute' : 'player.controls.mute')}
            data-shortcut="m"
            aria-keyshortcuts="m"
            onClick={toggleMute}
            className={cn(buttonClass, 'hidden sm:flex')}
          >
            {media.muted || media.volume === 0 ? <VolumeX aria-hidden="true" className="size-5" /> : <Volume2 aria-hidden="true" className="size-5" />}
          </button>
          <input
            type="range"
            aria-label={t('player.controls.volume')}
            min={0}
            max={1}
            step={0.01}
            value={media.muted ? 0 : media.volume}
            onPointerDown={holdControls}
            onPointerUp={releaseControls}
            onPointerCancel={releaseControls}
            onChange={(event) => {
              if (videoRef.current) {
                videoRef.current.volume = Number(event.target.value);
                videoRef.current.muted = false;
              }
            }}
            className="hidden h-1 w-16 cursor-pointer accent-white sm:block"
          />
          {chapterList.length > 0 && (
            <button
              ref={chaptersButtonRef}
              type="button"
              data-chapters
              aria-label={t('player.controls.chapters')}
              aria-expanded={chaptersOpen}
              title={activeChapter?.title ?? t('player.controls.chapters')}
              onKeyDown={(event) => {
                if (event.key === 'Escape' && chaptersOpen) {
                  event.stopPropagation();
                  setChaptersOpen(false);
                }
              }}
              onClick={() => {
                setMenuOpen(false);
                setChaptersOpen((open) => !open);
              }}
              className="flex size-11 shrink-0 items-center justify-center rounded-lg text-xs text-white/90 hover:bg-white/15 focus-visible:ring-2 focus-visible:ring-white sm:h-9 sm:w-auto sm:min-w-0 sm:max-w-48 sm:px-2"
            >
              <ListVideo aria-hidden="true" className="size-5 sm:hidden" />
              <span className="hidden truncate sm:inline">{activeChapter?.title ?? t('player.controls.chapters')}</span>
            </button>
          )}
          <div className="ml-auto" />
          <button
            type="button"
            aria-label={t('player.privatePlayback')}
            aria-pressed={privatePlayback}
            title={`${t(privatePlayback ? 'player.privatePlaybackActive' : 'player.privatePlayback')}\n${t('player.privatePlaybackDescription')}`}
            onClick={() => {
              setPrivatePlayback(!privatePlayback);
              setPrivacyNotice(!privatePlayback);
            }}
            className={cn(buttonClass, privatePlayback && 'bg-white text-black hover:bg-white/90 hover:text-black')}
          >
            <EyeOff aria-hidden="true" className="size-5" />
          </button>
          <button
            ref={gearRef}
            type="button"
            aria-label={t('player.controls.settings')}
            title={t('player.controls.settings')}
            aria-haspopup="menu"
            aria-expanded={menuOpen}
            aria-controls="video-settings-menu"
            onClick={() => {
              setMenuPage('root');
              setChaptersOpen(false);
              setMenuOpen((open) => !open);
            }}
            className={buttonClass}
          >
            <Settings2 aria-hidden="true" className="size-5" />
          </button>
          <button
            type="button"
            aria-label={t(fullscreen ? 'player.controls.exitFullscreen' : 'player.controls.fullscreen')}
            title={t(fullscreen ? 'player.controls.exitFullscreen' : 'player.controls.fullscreen')}
            data-shortcut="f"
            aria-keyshortcuts="f"
            onClick={toggleFullscreen}
            className={buttonClass}
          >
            {fullscreen ? <Minimize aria-hidden="true" className="size-5" /> : <Maximize aria-hidden="true" className="size-5" />}
          </button>
        </div>
      </div>
    </div>
  );
}
