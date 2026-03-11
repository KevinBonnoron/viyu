import { Fragment, useEffect, useRef } from 'react';
import { ErrorSnackbar } from '@/components/error-snackbar';
import { VideoLink } from '@/lib/links';
import { type TranslationKey, useI18n } from '@/providers/i18n-provider';
import type { Series, SeriesItem, SeriesSync } from '@/types/series.type';

export function SeriesSyncStatus({ sync, error, onRetry }: { sync: SeriesSync | null; error: TranslationKey | null; onRetry: () => void }) {
  const { t } = useI18n();
  const synchronizing = !error && (sync?.status === 'pending' || sync?.status === 'running');
  // Partial or failed background analysis is not reported: the stored series stay visible and the server retries on its own.
  // Only a failure of the page's own request is an error.
  if (!synchronizing && !error) {
    return null;
  }
  return (
    <>
      {synchronizing && (
        <div className="rounded-xl border bg-card p-4 text-sm" aria-live="polite">
          <output className="block text-muted-foreground">{t('series.syncing', { count: sync.loaded })}</output>
        </div>
      )}
      {error && <ErrorSnackbar message={t(error)} action={t('common.retry')} onAction={onRetry} />}
    </>
  );
}

export function SeriesItemLabel({ item, inSeason = false, pendingDate = false }: { item: SeriesItem; inSeason?: boolean; pendingDate?: boolean }) {
  const { t, language } = useI18n();
  const date = Date.parse(item.video.published);
  const known = Number.isFinite(date) && date > 0;
  return (
    <span>
      {item.episode !== null && (
        <>{t(item.season === null || inSeason ? 'series.episode' : 'series.seasonEpisode', { episode: item.episode, season: item.season ?? '' })} · </>
      )}
      {known ? (
        <time dateTime={item.video.published}>{new Intl.DateTimeFormat(language, { dateStyle: 'medium', timeZone: 'UTC' }).format(date)}</time>
      ) : pendingDate ? (
        <span className="inline-block h-4 w-24 animate-pulse rounded bg-muted align-middle">
          <span className="sr-only">{t('video.publicationDateLoading')}</span>
        </span>
      ) : (
        t('series.unknownDate')
      )}
    </span>
  );
}

/** The episodes of the series being played, the current one centred. It is the panel section's own scroller. */
export function SeriesEpisodeList({ series, index, source }: { series: Series; index: number; source?: string }) {
  const { t } = useI18n();
  const list = useRef<HTMLOListElement>(null);
  const current = useRef<HTMLLIElement>(null);
  // Bring the episode being played to the middle of the list, scrolling the list and never the page.
  useEffect(() => {
    const container = list.current;
    const item = current.current;
    if (container && item) {
      const offset = item.getBoundingClientRect().top - container.getBoundingClientRect().top + container.scrollTop;
      container.scrollTop = Math.max(0, offset - container.clientHeight / 2 + item.clientHeight / 2);
    }
  }, [index]);
  // The season the previous row belonged to, to mark each season's first row and count the season's rows.
  let previousSeason: number | null = null;
  let seasonIndex = 0;
  return (
    <ol ref={list} aria-label={t('series.playback')} className="min-h-0 flex-1 divide-y overflow-y-auto">
      {series.items.map((item, itemIndex) => {
        const header = item.season !== null && item.season !== previousSeason;
        if (header) {
          seasonIndex = 0;
        }
        if (item.season !== null) {
          seasonIndex += 1;
        }
        previousSeason = item.season;
        // Numbered rows show the creator's number; a row without one (a pilot) shows its place in the season.
        const number = item.season === null ? itemIndex + 1 : (item.episode ?? seasonIndex);
        return (
          <Fragment key={`${item.video.platform}:${item.video.id}`}>
            {header && item.season !== null && (
              <li
                aria-hidden="true"
                className="sticky top-0 z-10 -mt-px bg-background px-4 pb-1 pt-2.5 text-[11px] font-semibold uppercase tracking-[0.1em] text-muted-foreground"
              >
                {t('series.season', { season: item.season })}
              </li>
            )}
            <li ref={itemIndex === index ? current : undefined}>
              <VideoLink
                video={item.video}
                series={series.id}
                source={source}
                aria-current={itemIndex === index ? 'page' : undefined}
                className={`flex items-start gap-3 px-4 py-3 text-sm hover:bg-muted/60 ${itemIndex === index ? 'bg-primary/5 text-primary' : ''}`}
              >
                <span className="w-6 shrink-0 pt-px text-center text-xs tabular-nums text-muted-foreground">{number}</span>
                <span className="min-w-0 flex-1">
                  <span className="block break-words">{item.video.title}</span>
                  <span className="mt-0.5 block text-xs text-muted-foreground">
                    <SeriesItemLabel item={item} />
                  </span>
                </span>
              </VideoLink>
            </li>
          </Fragment>
        );
      })}
    </ol>
  );
}
