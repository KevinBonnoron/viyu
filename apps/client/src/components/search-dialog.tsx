import { useLocation, useNavigate, useSearch as useRouteSearch } from '@tanstack/react-router';
import { ArrowUpRight, Check, Heart, LoaderCircle, Search, UserCheck, UserPlus, Video, X } from 'lucide-react';
import { Fragment, type RefObject, useEffect, useId, useMemo, useRef, useState } from 'react';
import { ChannelAvatar } from '@/components/channel-avatar';
import { PlatformBadge } from '@/components/platform-badge';
import { PlatformFilters } from '@/components/platform-filters';
import { SourceModes } from '@/components/source-feed';
import { Button } from '@/components/ui/button';
import { Dialog } from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { useThumbnail } from '@/features/video/hooks/use-thumbnail';
import { useVideoMetadata } from '@/features/video/hooks/use-video-metadata';
import { displayTitle, needsEnrichment, thumbnailCandidates } from '@/features/video/utils/video-presentation';
import { youtubeVideoIdFromUrl } from '@/features/video/utils/youtube-url';
import { useLocalSearch } from '@/hooks/useLocalSearch';
import { useSearch } from '@/hooks/useSearch';
import { useChannelSubscription } from '@/hooks/useSubscriptions';
import { useHideSubscriberOnly, useUserSettings } from '@/hooks/useUserSettings';
import { useVideoStatuses } from '@/hooks/useVideoStatuses';
import { requestError } from '@/lib/api-error';
import { useOpenChannel, useOpenLink, useOpenVideo } from '@/lib/links';
import { localVideoResult } from '@/lib/local-search';
import { shortcutModifierLabel } from '@/lib/platform-shortcut';
import { searchPlatformOrAll } from '@/lib/source-activation';
import { cn, formatDuration } from '@/lib/utils';
import { useAuth } from '@/providers/auth-provider';
import { type TranslationKey, useI18n } from '@/providers/i18n-provider';
import { useActiveSource, useSources } from '@/providers/sources-provider';
import { isChannelResult, type VideoResult } from '@/types';

type Choice = { id: string; kind: 'video' | 'channel' | 'link'; platform: string; title: string; subtitle: string; thumbnail: string; video?: VideoResult };

export function SearchDialog() {
  const { t } = useI18n();
  const navigate = useNavigate();
  const openVideo = useOpenVideo();
  const openChannel = useOpenChannel();
  const openLink = useOpenLink();
  const currentSearch = useRouteSearch({ strict: false });
  const currentLocation = useLocation();
  const currentSource = useActiveSource(
    currentLocation.pathname === '/' ||
      currentLocation.pathname === '/history' ||
      currentLocation.pathname === '/search' ||
      currentLocation.pathname.startsWith('/player/')
      ? currentLocation.state.viyuSource
      : undefined,
  );
  const { isEnabled } = useSources();
  // A platform chosen earlier that has been disabled since is not searched: the search goes back to every enabled platform.
  const currentPlatform = searchPlatformOrAll(currentLocation.state.viyuSearchPlatform ?? currentSource ?? 'all', isEnabled);
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState('');
  const [source, setSource] = useState<string>();
  const [platform, setPlatform] = useState('all');
  const [platformFilter, setPlatformFilter] = useState<string>();
  const [linkBusy, setLinkBusy] = useState(false);
  const [linkError, setLinkError] = useState<TranslationKey | null>(null);
  const [subscriptionError, setSubscriptionError] = useState(false);
  const [active, setActive] = useState(-1);
  const input = useRef<HTMLInputElement>(null);
  const [shortcut] = useState(shortcutModifierLabel);
  const { user } = useAuth();
  const { settings, isReady } = useUserSettings();
  const preferOriginal = settings?.use_original_titles ?? true;
  const hideSubscriberOnly = useHideSubscriberOnly();
  const id = useId();
  // A pasted YouTube link is opened directly only while YouTube is enabled; otherwise it is an ordinary query.
  const direct = isEnabled('youtube') && !!youtubeVideoIdFromUrl(query);
  // Typing searches after a pause, never a pasted link: the quick answer is the one page the dialog shows.
  const { results, loading: searching, requestError: searchError, handOff } = useSearch({ query, platform, source, typing: true, enabled: open });
  const loading = searching || linkBusy;
  const error = linkError ?? searchError;
  // The server answers for every platform at once; narrowing to one is done here, on what was received.
  const platforms = [...new Set(results.map((result) => result.platform))].sort();
  const activePlatform = platformFilter && platforms.includes(platformFilter) ? platformFilter : undefined;
  const filteredResults = results.filter(
    (result) => (!activePlatform || result.platform === activePlatform) && (isChannelResult(result) || !(hideSubscriberOnly && result.subscriberOnly)),
  );
  // Local answers come from records Viyu already holds: instant, no debounce and no request, narrowed
  // by the platform filter the user picked (independent of what the server has returned so far).
  const local = useLocalSearch(query, platformFilter);
  const localChoices: Choice[] = direct
    ? []
    : [
        ...local.channels.map((channel) => ({
          id: channel.id,
          kind: 'channel' as const,
          platform: channel.platform,
          title: channel.name,
          subtitle: t('channel.type'),
          thumbnail: channel.thumbnail,
        })),
        ...local.videos.map((match) => {
          const video = localVideoResult(match);
          return {
            id: video.id,
            kind: 'video' as const,
            platform: video.platform,
            title: video.title,
            subtitle: video.channelName,
            thumbnail: video.thumbnail,
            video,
          };
        }),
      ];
  const localIds = new Set(localChoices.map((choice) => `${choice.platform}:${choice.id}`));
  const choices: Choice[] = direct
    ? [{ id: 'link', kind: 'link', platform: 'youtube', title: t('search.quick.openVideo'), subtitle: query, thumbnail: '' }]
    : [
        ...localChoices,
        ...filteredResults
          .filter((result) => !localIds.has(`${result.platform}:${result.id}`))
          .slice(0, 8)
          .map(
            (result): Choice =>
              isChannelResult(result)
                ? { id: result.id, kind: 'channel', platform: result.platform, title: result.name, subtitle: t('channel.type'), thumbnail: result.thumbnail }
                : {
                    id: result.id,
                    kind: 'video',
                    platform: result.platform,
                    title: result.title,
                    subtitle: result.channelName,
                    thumbnail: result.thumbnail,
                    video: { ...result, source },
                  },
          ),
      ];
  // What Viyu knows about the videos shown (watched, resumable, liked): only their rows are read.
  const shownKey = open ? choices.flatMap((choice) => choice.video?.id ?? []).join(',') : '';
  const shownVideoIds = useMemo(() => (shownKey ? shownKey.split(',') : []), [shownKey]);
  const { statuses } = useVideoStatuses(shownVideoIds);

  useEffect(() => {
    const keyboard = (event: KeyboardEvent) => {
      if ((event.ctrlKey || event.metaKey) && !event.altKey && !event.isComposing && event.key.toLowerCase() === 'k') {
        event.preventDefault();
        setOpen((value) => !value);
      }
    };
    window.addEventListener('keydown', keyboard);
    return () => window.removeEventListener('keydown', keyboard);
  }, []);

  useEffect(() => {
    if (open) {
      setQuery(typeof currentSearch.q === 'string' ? currentSearch.q : '');
      setSource(currentSource);
      setPlatform(currentPlatform);
      setActive(-1);
      input.current?.focus();
    }
  }, [open, currentSearch.q, currentSource, currentPlatform]);

  // Whatever the previous query left behind (a failed link, a failed subscription, the highlighted row) does not carry over to the next.
  useEffect(() => {
    setLinkError(null);
    setSubscriptionError(false);
    setActive(-1);
  }, [open, query, platform, source, direct]);

  useEffect(() => {
    if (active >= 0) {
      document.getElementById(`${id}-option-${active}`)?.scrollIntoView({ block: 'nearest' });
    }
  }, [active, id]);

  const choose = (choice: Choice) => {
    if (choice.kind === 'link') {
      // The dialog stays open while the server stores the video, so a failure shows where it was asked.
      setLinkError(null);
      setLinkBusy(true);
      openLink(query.trim(), { source: source === 'youtube' ? source : undefined })
        .then(() => setOpen(false))
        .catch((failure: unknown) => setLinkError(requestError(failure)))
        .finally(() => setLinkBusy(false));
      return;
    }
    setOpen(false);
    if (choice.kind === 'channel') {
      void openChannel({ id: choice.id, platform: choice.platform, source });
    } else {
      void openVideo({ ...(choice.video ?? { id: choice.id }), source: choice.platform === source ? source : undefined });
    }
  };
  const showAll = () => {
    if (!query.trim()) {
      return;
    }
    handOff();
    setOpen(false);
    void navigate({ to: '/search', search: { q: query.trim() }, state: { viyuSource: source, viyuSearchPlatform: platform } });
  };

  return (
    <Dialog
      open={open}
      onClose={() => setOpen(false)}
      closeOnBackdrop
      aria-labelledby={`${id}-title`}
      className="max-w-2xl overflow-hidden bg-popover p-0 text-popover-foreground sm:p-0"
    >
      <h2 id={`${id}-title`} className="sr-only">
        {t('search.quick.title')}
      </h2>
      <div className="flex items-center gap-3 border-b px-4 py-3">
        <Search aria-hidden="true" className="size-5 shrink-0 text-muted-foreground" />
        <Input
          ref={input}
          role="combobox"
          aria-label={t('search.videos')}
          aria-controls={`${id}-results`}
          aria-expanded={open && choices.length > 0}
          aria-autocomplete="list"
          aria-activedescendant={active >= 0 ? `${id}-option-${active}` : undefined}
          placeholder={t('search.placeholder')}
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          onKeyDown={(event) => {
            if (event.nativeEvent.isComposing) {
              return;
            }
            if ((event.key === 'ArrowDown' || event.key === 'ArrowUp') && choices.length) {
              event.preventDefault();
              setActive((previous) => (event.key === 'ArrowDown' ? (previous + 1) % choices.length : previous <= 0 ? choices.length - 1 : previous - 1));
            } else if (event.key === 'Enter') {
              event.preventDefault();
              if (active >= 0 && choices[active]) {
                choose(choices[active]);
              } else if (direct) {
                choose(choices[0]);
              } else {
                showAll();
              }
            }
          }}
          className="h-11 border-0 bg-transparent px-0 shadow-none focus-visible:ring-0"
        />
        <button
          type="button"
          aria-label={t('search.quick.close')}
          onClick={() => setOpen(false)}
          className="rounded-lg p-2 text-muted-foreground hover:bg-muted hover:text-foreground"
        >
          <X aria-hidden="true" className="size-4" />
        </button>
      </div>
      <SourceModes
        className="px-4 pt-3"
        source={source}
        capability="search"
        localLabel={t('search.allTab')}
        onChange={(selected, selectedPlatform) => {
          setSource(selected);
          setPlatform(selectedPlatform ?? 'all');
        }}
      />
      {platforms.length > 1 && (
        <div className="px-4 pt-3">
          <PlatformFilters platforms={platforms} selected={activePlatform} onChange={setPlatformFilter} />
        </div>
      )}
      <div className="max-h-[55dvh] overflow-y-auto p-2">
        <div id={`${id}-results`} role="listbox" aria-label={t('search.quick.results')}>
          {localChoices.length > 0 && (
            <div className="px-3 pb-1 pt-2 text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">{t('search.localSection')}</div>
          )}
          {choices.map((choice, index) => {
            const status = choice.video ? statuses.get(choice.video.id) : undefined;
            const firstResult = localChoices.length > 0 && index === localChoices.length;
            return (
              <Fragment key={`${choice.kind}:${choice.id}`}>
                {firstResult && (
                  <div className="px-3 pb-1 pt-3 text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">{t('search.resultsSection')}</div>
                )}
                <SearchChoiceItem
                  choice={choice}
                  optionId={`${id}-option-${index}`}
                  active={active === index}
                  onActivate={() => setActive(index)}
                  onChoose={() => choose(choice)}
                  onSubscriptionError={() => setSubscriptionError(true)}
                  preferOriginal={preferOriginal}
                  ready={isReady}
                  owner={user?.id ?? ''}
                  local={choice.video ? { watched: status?.watched ?? false, liked: status?.reaction === 'like', progress: status?.progress } : undefined}
                />
              </Fragment>
            );
          })}
        </div>
        {subscriptionError && (
          <p role="alert" className="px-3 pb-2 text-sm text-destructive">
            {t('channel.subscription.failed')}
          </p>
        )}
        {loading && (
          <output className="flex items-center justify-center gap-2 p-7 text-sm text-muted-foreground">
            <LoaderCircle aria-hidden="true" className="size-4 animate-spin" />
            {t('search.loading')}
          </output>
        )}
        {error && (
          <p role="alert" className="p-5 text-sm text-destructive">
            {t(error)}
          </p>
        )}
        {!loading && !error && choices.length === 0 && (
          <p className="p-7 text-center text-sm text-muted-foreground">{t(query.trim().length < 2 ? 'search.quick.hint' : 'search.empty')}</p>
        )}
      </div>
      <div className="flex flex-wrap items-center justify-between gap-3 border-t px-4 py-3 text-xs text-muted-foreground">
        <span>{t('search.quick.keyboard', { shortcut })}</span>
        {!direct && query.trim() && (
          <button type="button" onClick={showAll} className="rounded-sm font-medium text-primary hover:underline">
            {t('search.quick.allResults')}
          </button>
        )}
      </div>
    </Dialog>
  );
}

type RowProps = { choice: Choice; optionId: string; active: boolean; onActivate: () => void; onChoose: () => void; onSubscriptionError: () => void };
type EnrichmentProps = { preferOriginal: boolean; ready: boolean; owner: string };
/** What Viyu already knows about this row's video: shown as chips next to the channel name. */
type LocalRowStatus = { watched: boolean; liked: boolean; progress?: number };

function SearchChoiceItem({ preferOriginal, ready, owner, local, ...row }: RowProps & EnrichmentProps & { local?: LocalRowStatus }) {
  // Only videos are enriched; channels and the pasted-link shortcut have nothing to look up.
  if (row.choice.video) {
    return <SearchVideoChoice {...row} video={row.choice.video} preferOriginal={preferOriginal} ready={ready} owner={owner} local={local} />;
  }
  return <SearchChoiceRow {...row} local={local} title={row.choice.title} thumbnails={thumbnailCandidates(row.choice)} />;
}

function SearchVideoChoice({
  video,
  preferOriginal,
  ready,
  owner,
  local,
  ...row
}: RowProps & EnrichmentProps & { video: VideoResult; local?: LocalRowStatus }) {
  const metadata = useVideoMetadata<HTMLDivElement>(video, ready && needsEnrichment(video, preferOriginal), owner);
  const unproven = preferOriginal && !video.originalTitleVerified;
  return (
    <SearchChoiceRow
      {...row}
      local={local}
      rowRef={metadata.ref}
      title={displayTitle(video, preferOriginal, metadata.title)}
      thumbnails={thumbnailCandidates(video, metadata)}
      busy={ready && unproven && !metadata.title && !metadata.failed}
      failed={unproven && metadata.failed}
    />
  );
}

function SearchChoiceRow({
  choice,
  optionId,
  active,
  onActivate,
  onChoose,
  onSubscriptionError,
  local,
  rowRef,
  title,
  thumbnails,
  busy = false,
  failed = false,
}: RowProps & { local?: LocalRowStatus; rowRef?: RefObject<HTMLDivElement | null>; title: string; thumbnails: string[]; busy?: boolean; failed?: boolean }) {
  const { t } = useI18n();
  const thumbnail = useThumbnail(thumbnails);
  return (
    <div ref={rowRef} role="presentation" onPointerMove={onActivate} className={cn('flex items-center rounded-xl', active ? 'bg-muted' : 'hover:bg-muted/60')}>
      <button
        id={optionId}
        type="button"
        role="option"
        tabIndex={-1}
        aria-selected={active}
        aria-busy={busy}
        onMouseDown={(event) => event.preventDefault()}
        onClick={onChoose}
        className="flex min-w-0 flex-1 items-center gap-3 rounded-xl p-3 text-left"
      >
        {choice.kind === 'channel' ? (
          <ChannelAvatar src={choice.thumbnail} className="size-12" />
        ) : thumbnail.src ? (
          <img src={thumbnail.src} alt="" className="h-12 w-20 shrink-0 rounded-lg bg-muted object-cover" onError={thumbnail.onError} />
        ) : (
          <span className="flex h-12 w-20 shrink-0 items-center justify-center rounded-lg bg-muted">
            {choice.video ? <Video aria-hidden="true" className="size-5 opacity-50" /> : <ArrowUpRight aria-hidden="true" className="size-5" />}
          </span>
        )}
        <span className="min-w-0">
          <span className="block truncate text-sm font-medium">{title}</span>
          <span className="mt-1 flex items-center gap-1.5 text-xs text-muted-foreground">
            <PlatformBadge platform={choice.platform} className="size-5 shadow-none [&_svg]:size-3" />
            <span className="truncate">{choice.subtitle}</span>
            {failed && <span className="shrink-0">· {t('video.originalTitleFailed')}</span>}
            {local?.watched && (
              <span className="inline-flex shrink-0 items-center gap-0.5 rounded bg-primary/10 px-1 py-px text-[10px] font-semibold text-primary">
                <Check aria-hidden="true" className="size-3" />
                {t('video.watched')}
              </span>
            )}
            {local?.liked && (
              <span aria-hidden="true" className="shrink-0 text-red-500">
                <Heart className="size-3.5 fill-current" />
              </span>
            )}
            {local?.progress !== undefined && choice.video && (
              <span className="shrink-0 tabular-nums">
                {t('search.resumeAt', { time: formatDuration(Math.round(local.progress * choice.video.duration)) })}
              </span>
            )}
          </span>
        </span>
      </button>
      {choice.kind === 'channel' && <SearchChannelSubscription choice={choice} onError={onSubscriptionError} />}
    </div>
  );
}

function SearchChannelSubscription({ choice, onError }: { choice: Choice; onError: () => void }) {
  const { t } = useI18n();
  const subscription = useChannelSubscription({ id: choice.id, platform: choice.platform, name: choice.title, thumbnail: choice.thumbnail });

  useEffect(() => {
    if (subscription.error) {
      onError();
    }
  }, [subscription.error, onError]);

  return (
    <Button
      type="button"
      variant={subscription.subscribed ? 'secondary' : 'outline'}
      size="sm"
      className="mr-3 rounded-full"
      disabled={subscription.disabled}
      aria-pressed={subscription.subscribed}
      aria-label={`${t(subscription.subscribed ? 'player.subscribed' : 'player.subscribe')} — ${choice.title}`}
      onMouseDown={(event) => event.preventDefault()}
      onClick={() => void subscription.toggle()}
    >
      {subscription.subscribed ? <UserCheck aria-hidden="true" className="size-4" /> : <UserPlus aria-hidden="true" className="size-4" />}
      {t(subscription.subscribed ? 'player.subscribed' : 'player.subscribe')}
    </Button>
  );
}
