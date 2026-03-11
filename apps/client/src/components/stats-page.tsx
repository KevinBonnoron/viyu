import { Link } from '@tanstack/react-router';
import { BarChart3, CircleAlert, Clock, Film, RotateCcw, Users } from 'lucide-react';
import { type ReactNode, useEffect, useId, useState } from 'react';
import { ChannelAvatar } from '@/components/channel-avatar';
import { EmptyState } from '@/components/empty-state';
import { PageTitle } from '@/components/page-title';
import { PlatformBadge } from '@/components/platform-badge';
import { ProfilesError } from '@/components/profiles-error';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { api } from '@/lib/api';
import { ChannelLink } from '@/lib/links';
import { platformName, platformPresentation } from '@/lib/platforms';
import { barPercent, cn, formatMonth, formatWatchTime } from '@/lib/utils';
import { type TranslationKey, useI18n } from '@/providers/i18n-provider';
import { useProfiles } from '@/providers/profiles-provider';
import type { Stats, StatsChannel, StatsEntry, StatsMonth, StatsPeriod, StatsPlatform } from '@/types';

const PERIODS: readonly StatsPeriod[] = ['all', '30d', '90d', '365d'];
const PERIOD_LABELS: Record<StatsPeriod, TranslationKey> = {
  all: 'stats.period.all',
  '30d': 'stats.period.30d',
  '90d': 'stats.period.90d',
  '365d': 'stats.period.365d',
};
const TONE_BARS = { red: 'bg-red-500', violet: 'bg-violet-500', orange: 'bg-orange-500', neutral: 'bg-primary' } as const;

export function StatsPage() {
  const { t } = useI18n();
  const { activeProfile, isError, retry } = useProfiles();
  const [period, setPeriod] = useState<StatsPeriod>('all');
  return (
    <div className="flex flex-col gap-6">
      <PageTitle description={t('stats.description')}>{t('stats.title')}</PageTitle>
      <PeriodSelector period={period} onChange={setPeriod} />
      {isError ? <ProfilesError onRetry={retry} /> : <StatsContent key={activeProfile?.id ?? ''} profileId={activeProfile?.id} period={period} />}
    </div>
  );
}

function PeriodSelector({ period, onChange }: { period: StatsPeriod; onChange: (period: StatsPeriod) => void }) {
  const { t } = useI18n();
  return (
    <fieldset className="m-0 inline-flex min-w-0 max-w-full overflow-x-auto rounded-lg border bg-muted p-0.5">
      <legend className="sr-only">{t('stats.period.label')}</legend>
      {PERIODS.map((value) => (
        <Button
          key={value}
          type="button"
          variant="ghost"
          size="sm"
          aria-pressed={period === value}
          onClick={() => onChange(value)}
          className={cn(
            'h-8 rounded-md px-3',
            period === value ? 'bg-card text-foreground shadow-xs hover:bg-card' : 'text-muted-foreground hover:bg-transparent hover:text-foreground',
          )}
        >
          {t(PERIOD_LABELS[value])}
        </Button>
      ))}
    </fieldset>
  );
}

function StatsContent({ profileId, period }: { profileId?: string; period: StatsPeriod }) {
  const { t } = useI18n();
  const [stats, setStats] = useState<Stats>();
  const [loading, setLoading] = useState(true);
  const [failed, setFailed] = useState(false);
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    if (!profileId) {
      return;
    }
    const controller = new AbortController();
    setLoading(true);
    setFailed(false);
    api
      .stats(profileId, period, { signal: controller.signal })
      .then((result) => {
        if (!controller.signal.aborted) {
          setStats(result);
          setLoading(false);
        }
      })
      .catch(() => {
        if (!controller.signal.aborted) {
          setFailed(true);
          setLoading(false);
        }
      });
    return () => controller.abort();
  }, [profileId, period, attempt]);

  if (failed) {
    return (
      <div role="alert" className="flex flex-wrap items-center gap-3 rounded-xl border border-destructive/25 bg-card px-4 py-3 text-sm">
        <CircleAlert aria-hidden="true" className="size-4 shrink-0 text-destructive" />
        <span>{t('stats.error')}</span>
        <Button type="button" variant="outline" size="sm" onClick={() => setAttempt((value) => value + 1)}>
          <RotateCcw aria-hidden="true" />
          {t('common.retry')}
        </Button>
      </div>
    );
  }
  if (!stats) {
    return <StatsSkeleton />;
  }
  if (stats.totals.videos === 0) {
    return <EmptyState icon={BarChart3} title={t('stats.emptyTitle')} description={t(period === 'all' ? 'stats.emptyDescription' : 'stats.emptyPeriod')} />;
  }
  return (
    <div aria-busy={loading} className={cn('space-y-6 transition-opacity', loading && 'opacity-60')}>
      <Summary stats={stats} />
      <MonthlyChart months={stats.byMonth} />
      <div className="grid gap-6 lg:grid-cols-2">
        <TopicList title={t('stats.tags.title')} entries={stats.topTags} />
        <TopicList title={t('stats.categories.title')} entries={stats.topCategories} />
        <ChannelList channels={stats.topChannels} />
        <PlatformSplit platforms={stats.byPlatform} />
      </div>
    </div>
  );
}

function StatsSkeleton() {
  const { t } = useI18n();
  return (
    <div aria-busy="true" aria-live="polite" className="space-y-6">
      <span className="sr-only">{t('stats.loading')}</span>
      <div aria-hidden="true" className="grid gap-4 sm:grid-cols-3">
        {['watchTime', 'videos', 'channels'].map((card) => (
          <Skeleton key={card} className="h-[4.5rem] rounded-xl" />
        ))}
      </div>
      <Skeleton aria-hidden="true" className="h-64 rounded-xl" />
      <div aria-hidden="true" className="grid gap-6 lg:grid-cols-2">
        {['tags', 'categories', 'channels', 'platforms'].map((section) => (
          <Skeleton key={section} className="h-64 rounded-xl" />
        ))}
      </div>
    </div>
  );
}

function Summary({ stats }: { stats: Stats }) {
  const { t, language } = useI18n();
  return (
    <div className="grid gap-4 sm:grid-cols-3">
      <SummaryCard icon={Clock} label={t('stats.card.watchTime')} value={formatWatchTime(stats.totals.seconds, language)} />
      <SummaryCard icon={Film} label={t('stats.card.videos')} value={stats.totals.videos.toLocaleString(language)} />
      <SummaryCard icon={Users} label={t('stats.card.channels')} value={stats.totals.channels.toLocaleString(language)} />
    </div>
  );
}

function SummaryCard({ icon: Icon, label, value }: { icon: typeof Clock; label: string; value: string }) {
  return (
    <div className="flex items-center gap-3 rounded-xl border bg-card p-4">
      <span className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-accent text-primary">
        <Icon aria-hidden="true" className="size-5" />
      </span>
      <div className="min-w-0">
        <p className="text-xs text-muted-foreground">{label}</p>
        <p className="truncate text-xl font-semibold tabular-nums">{value}</p>
      </div>
    </div>
  );
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  const id = useId();
  return (
    <section aria-labelledby={id} className="min-w-0 rounded-xl border bg-card p-4">
      <h2 id={id} className="mb-4 text-sm font-semibold">
        {title}
      </h2>
      {children}
    </section>
  );
}

function SectionEmpty() {
  const { t } = useI18n();
  return <p className="text-sm text-muted-foreground">{t('stats.section.empty')}</p>;
}

/** The time and the number of videos of a row, as one line of text. */
function useUsage() {
  const { t, language } = useI18n();
  return (seconds: number, count: number) =>
    `${formatWatchTime(seconds, language)} · ${t(count === 1 ? 'stats.videoCount.one' : 'stats.videoCount.other', { count: count.toLocaleString(language) })}`;
}

/** A horizontal bar whose width is relative to the top entry. The numbers are in the row's text, so the bar itself is decoration. */
function Bar({ percent }: { percent: number }) {
  return (
    <div aria-hidden="true" className="mt-1.5 h-1.5 overflow-hidden rounded-full bg-muted">
      <div className="h-full rounded-full bg-primary" style={{ width: `${percent > 0 ? Math.max(percent, 2) : 0}%` }} />
    </div>
  );
}

const rowClass = 'block rounded-lg px-2 py-1.5 transition-colors hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40';

function TopicList({ title, entries }: { title: string; entries: StatsEntry[] }) {
  const usage = useUsage();
  // The entries are ranked by watch time; bars of an all-zero list fall back to the video count.
  const byTime = entries.some((entry) => entry.seconds > 0);
  const max = Math.max(...entries.map((entry) => (byTime ? entry.seconds : entry.count)), 0);
  return (
    <Section title={title}>
      {entries.length === 0 ? (
        <SectionEmpty />
      ) : (
        <ul className="-mx-2 space-y-1">
          {entries.map((entry) => (
            <li key={entry.key}>
              <Link to="/history" className={rowClass}>
                <span className="flex items-baseline justify-between gap-3">
                  <span className="min-w-0 truncate text-sm font-medium">{entry.name}</span>
                  <span className="shrink-0 text-xs tabular-nums text-muted-foreground">{usage(entry.seconds, entry.count)}</span>
                </span>
                <Bar percent={barPercent(byTime ? entry.seconds : entry.count, max)} />
              </Link>
            </li>
          ))}
        </ul>
      )}
    </Section>
  );
}

function ChannelList({ channels }: { channels: StatsChannel[] }) {
  const { t } = useI18n();
  const usage = useUsage();
  const byTime = channels.some((channel) => channel.seconds > 0);
  const max = Math.max(...channels.map((channel) => (byTime ? channel.seconds : channel.count)), 0);
  return (
    <Section title={t('stats.channels.title')}>
      {channels.length === 0 ? (
        <SectionEmpty />
      ) : (
        <ul className="-mx-2 space-y-1">
          {channels.map((channel) => (
            <li key={`${channel.platform}:${channel.channelId}`}>
              <ChannelLink channel={{ id: channel.recordId, platform: channel.platform }} className={cn(rowClass, 'flex items-center gap-3')}>
                <ChannelAvatar src={channel.thumbnail} className="size-9" />
                <span className="min-w-0 flex-1">
                  <span className="flex items-center justify-between gap-3">
                    <span className="flex min-w-0 items-center gap-1.5">
                      <span className="truncate text-sm font-medium">{channel.name}</span>
                      <PlatformBadge platform={channel.platform} className="size-5 border-0 bg-transparent shadow-none" />
                    </span>
                    <span className="shrink-0 text-xs tabular-nums text-muted-foreground">{usage(channel.seconds, channel.count)}</span>
                  </span>
                  <Bar percent={barPercent(byTime ? channel.seconds : channel.count, max)} />
                </span>
              </ChannelLink>
            </li>
          ))}
        </ul>
      )}
    </Section>
  );
}

function MonthlyChart({ months }: { months: StatsMonth[] }) {
  const { t, language } = useI18n();
  const usage = useUsage();
  const max = Math.max(...months.map((month) => month.seconds), 0);
  return (
    <Section title={t('stats.months.title')}>
      {months.length === 0 ? (
        <SectionEmpty />
      ) : (
        <>
          <div role="img" aria-label={t('stats.months.chartLabel')} className="flex h-48 items-stretch gap-1.5 sm:gap-2">
            {months.map((month) => (
              <div
                key={month.month}
                title={`${formatMonth(month.month, language)}: ${usage(month.seconds, month.count)}`}
                className="flex min-w-0 flex-1 flex-col items-center gap-1.5"
              >
                <div className="flex w-full flex-1 items-end">
                  <div
                    className="w-full rounded-t bg-primary"
                    style={{ height: `${month.seconds > 0 ? Math.max(barPercent(month.seconds, max), 2) : 0}%`, minHeight: 2 }}
                  />
                </div>
                <span className="w-full truncate text-center text-[10px] text-muted-foreground">{formatMonth(month.month, language, 'short')}</span>
              </div>
            ))}
          </div>
          <table className="sr-only">
            <caption>{t('stats.months.chartLabel')}</caption>
            <thead>
              <tr>
                <th scope="col">{t('stats.months.month')}</th>
                <th scope="col">{t('stats.card.watchTime')}</th>
                <th scope="col">{t('stats.card.videos')}</th>
              </tr>
            </thead>
            <tbody>
              {months.map((month) => (
                <tr key={month.month}>
                  <th scope="row">{formatMonth(month.month, language)}</th>
                  <td>{formatWatchTime(month.seconds, language)}</td>
                  <td>{month.count.toLocaleString(language)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </>
      )}
    </Section>
  );
}

function PlatformSplit({ platforms }: { platforms: StatsPlatform[] }) {
  const { t, language } = useI18n();
  const usage = useUsage();
  const byTime = platforms.some((platform) => platform.seconds > 0);
  const share = (platform: StatsPlatform) => (byTime ? platform.seconds : platform.count);
  const total = platforms.reduce((sum, platform) => sum + share(platform), 0);
  return (
    <Section title={t('stats.platforms.title')}>
      {platforms.length === 0 ? (
        <SectionEmpty />
      ) : (
        <>
          <div aria-hidden="true" className="flex h-3 overflow-hidden rounded-full bg-muted">
            {platforms.map((platform) => (
              <div
                key={platform.platform}
                className={cn('h-full border-r border-card last:border-r-0', TONE_BARS[platformPresentation(platform.platform).tone])}
                style={{ width: `${barPercent(share(platform), total)}%` }}
              />
            ))}
          </div>
          <ul className="mt-4 space-y-2">
            {platforms.map((platform) => (
              <li key={platform.platform} className="flex items-center gap-3 text-sm">
                <PlatformBadge platform={platform.platform} className="size-6" />
                <span className="min-w-0 flex-1 truncate font-medium">{platformName(platform.platform)}</span>
                <span className="shrink-0 text-xs tabular-nums text-muted-foreground">
                  {(barPercent(share(platform), total) / 100).toLocaleString(language, { style: 'percent', maximumFractionDigits: 0 })} ·{' '}
                  {usage(platform.seconds, platform.count)}
                </span>
              </li>
            ))}
          </ul>
        </>
      )}
    </Section>
  );
}
