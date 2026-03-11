import { createFileRoute } from '@tanstack/react-router';
import { Languages, Palette, Search, SlidersHorizontal, UserRound, Users } from 'lucide-react';
import { type KeyboardEvent, useEffect, useRef, useState } from 'react';
import { settingCollection } from '@/collections/setting.collection';
import { PageTitle } from '@/components/page-title';
import { SettingsAccount } from '@/components/settings-account';
import { SettingsProfiles } from '@/components/settings-profiles';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { useUserSettings } from '@/hooks/useUserSettings';
import { SYSTEM_FIELDS } from '@/lib/collection-writes';
import { foldSearch } from '@/lib/text';
import { useAuth } from '@/providers/auth-provider';
import { type TranslationKey, useI18n } from '@/providers/i18n-provider';
import { useTheme } from '@/providers/theme-provider';
import { type PlaybackQuality, playbackQualities, type SettingsRecord } from '@/types';

export const Route = createFileRoute('/_app/settings')({
  validateSearch: (search: Record<string, unknown>): { tab?: Tab } => ({ tab: tabs.some((tab) => tab.id === search.tab) ? (search.tab as Tab) : undefined }),
  component: SettingsPage,
});

const tabs = [
  { id: 'account', label: 'settings.tabs.account', icon: UserRound },
  { id: 'profiles', label: 'profiles.label', icon: Users },
  { id: 'appearance', label: 'settings.tabs.appearance', icon: Palette },
  { id: 'language', label: 'settings.tabs.language', icon: Languages },
  { id: 'playback', label: 'settings.tabs.playback', icon: SlidersHorizontal },
] as const;
type Tab = (typeof tabs)[number]['id'];
type PlaybackField = 'hide_shorts' | 'hide_watched' | 'hide_subscriber_only' | 'use_original_titles' | 'playback_quality' | 'auto_skip_sponsors' | 'autoplay';

const searchableSettings: { tab: Tab; label: TranslationKey; description: TranslationKey; target: string }[] = [
  { tab: 'account', label: 'settings.account.displayName', description: 'settings.account.profile', target: 'account-name' },
  { tab: 'account', label: 'settings.avatar.change', description: 'settings.avatar.help', target: 'avatar-heading' },
  { tab: 'account', label: 'settings.account.changePassword', description: 'ui.settings.passwordHint', target: 'account-new-password' },
  { tab: 'account', label: 'settings.account.data.title', description: 'settings.account.data.description', target: 'account-data-heading' },
  { tab: 'profiles', label: 'profiles.label', description: 'profiles.description', target: 'settings-panel-profiles' },
  { tab: 'appearance', label: 'settings.appearance.theme', description: 'settings.appearance.description', target: 'settings-theme' },
  { tab: 'language', label: 'settings.language.displayLanguage', description: 'settings.language.description', target: 'settings-language' },
  {
    tab: 'playback',
    label: 'settings.playback.defaultQuality',
    description: 'settings.playback.defaultQualityDescription',
    target: 'default-playback-quality',
  },
  { tab: 'playback', label: 'settings.playback.autoplay', description: 'settings.playback.autoplayDescription', target: 'setting-autoplay' },
  {
    tab: 'playback',
    label: 'settings.playback.autoSkipSponsors',
    description: 'settings.playback.autoSkipSponsorsDescription',
    target: 'setting-auto_skip_sponsors',
  },
  { tab: 'playback', label: 'settings.playback.hideShorts', description: 'settings.playback.hideShortsDescription', target: 'setting-hide_shorts' },
  { tab: 'playback', label: 'settings.playback.hideWatched', description: 'settings.playback.hideWatchedDescription', target: 'setting-hide_watched' },
  {
    tab: 'playback',
    label: 'settings.playback.hideSubscriberOnly',
    description: 'settings.playback.hideSubscriberOnlyDescription',
    target: 'setting-hide_subscriber_only',
  },
  {
    tab: 'playback',
    label: 'settings.playback.originalTitles',
    description: 'settings.playback.originalTitlesDescription',
    target: 'setting-use_original_titles',
  },
];

function SettingsPage() {
  const { t, language, setLanguage } = useI18n();
  const { theme, setTheme } = useTheme();
  const { tab } = Route.useSearch();
  const navigate = Route.useNavigate();
  const selectedTab = tab ?? 'account';
  const setSelectedTab = (next: Tab) => void navigate({ search: { tab: next }, replace: true });
  const tabRefs = useRef<Array<HTMLButtonElement | null>>([]);
  const [query, setQuery] = useState('');
  const [searchTarget, setSearchTarget] = useState<string>();
  const terms = foldSearch(query).trim().split(/\s+/).filter(Boolean);
  const searching = terms.length > 0;
  const results = searching
    ? searchableSettings.filter((setting) => {
        const text = foldSearch(`${t(setting.label)} ${t(setting.description)} ${t(tabs.find((tab) => tab.id === setting.tab)?.label ?? 'settings.title')}`);
        return terms.every((term) => text.includes(term));
      })
    : [];

  useEffect(() => {
    if (!searchTarget || searching) {
      return;
    }
    const target = document.getElementById(searchTarget);
    if (target) {
      if (!target.matches('input,button,select,textarea,[tabindex]')) {
        target.tabIndex = -1;
      }
      target.scrollIntoView({ block: 'center' });
      target.focus({ preventScroll: true });
    }
    setSearchTarget(undefined);
  }, [searchTarget, selectedTab, searching]);

  const navigateTabs = (event: KeyboardEvent<HTMLButtonElement>, index: number) => {
    let next = index;
    if (event.key === 'ArrowDown') {
      next = (index + 1) % tabs.length;
    } else if (event.key === 'ArrowUp') {
      next = (index - 1 + tabs.length) % tabs.length;
    } else if (event.key === 'Home') {
      next = 0;
    } else if (event.key === 'End') {
      next = tabs.length - 1;
    } else {
      return;
    }
    event.preventDefault();
    setSelectedTab(tabs[next].id);
    tabRefs.current[next]?.focus();
  };

  return (
    <div className="w-full space-y-7">
      <PageTitle description={t('ui.settings.description')}>{t('settings.title')}</PageTitle>
      <div className="relative max-w-xl">
        <Search aria-hidden="true" className="pointer-events-none absolute left-3 top-3 size-4 text-muted-foreground" />
        <Input
          type="search"
          aria-label={t('settings.search.label')}
          placeholder={t('settings.search.label')}
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          className="pl-9"
        />
      </div>
      {searching && (
        <section aria-label={t('settings.search.results')} className="space-y-3">
          <output className="block text-sm text-muted-foreground">{t('settings.search.count', { count: results.length })}</output>
          {results.length === 0 && <p className="rounded-xl border bg-card p-5 text-sm text-muted-foreground">{t('settings.search.empty')}</p>}
          <div className="grid gap-3 lg:grid-cols-2">
            {results.map((setting) => (
              <button
                key={setting.target}
                type="button"
                onClick={async () => {
                  await navigate({ search: { tab: setting.tab }, replace: true });
                  setQuery('');
                  setSearchTarget(setting.target);
                }}
                className="space-y-1 rounded-xl border bg-card p-4 text-left hover:bg-muted focus-visible:ring-2 focus-visible:ring-ring"
              >
                <span className="block text-xs text-muted-foreground">{t(tabs.find((tab) => tab.id === setting.tab)?.label ?? 'settings.title')}</span>
                <span className="block font-medium">{t(setting.label)}</span>
                <span className="block text-sm text-muted-foreground">{t(setting.description)}</span>
              </button>
            ))}
          </div>
        </section>
      )}
      <div className={searching ? 'hidden' : 'grid items-start gap-6 sm:grid-cols-[11rem_minmax(0,1fr)] lg:gap-8'}>
        <div className="sm:sticky sm:top-8">
          <div role="tablist" aria-label={t('settings.sections')} aria-orientation="vertical" className="flex flex-col gap-1">
            {tabs.map((tab, index) => (
              <button
                key={tab.id}
                ref={(element) => {
                  tabRefs.current[index] = element;
                }}
                id={`settings-tab-${tab.id}`}
                type="button"
                role="tab"
                aria-selected={selectedTab === tab.id}
                aria-controls={`settings-panel-${tab.id}`}
                tabIndex={selectedTab === tab.id ? 0 : -1}
                onClick={() => setSelectedTab(tab.id)}
                onKeyDown={(event) => navigateTabs(event, index)}
                className={`flex items-center gap-2.5 rounded-lg border px-3 py-3 text-left text-sm font-medium transition-colors outline-none focus-visible:ring-2 focus-visible:ring-ring ${selectedTab === tab.id ? 'border-border bg-card text-primary shadow-xs' : 'border-transparent text-muted-foreground hover:bg-muted hover:text-foreground'}`}
              >
                <tab.icon aria-hidden="true" className="size-4" />
                {t(tab.label)}
              </button>
            ))}
          </div>
        </div>
        <div className="min-w-0">
          <section
            id="settings-panel-account"
            role="tabpanel"
            aria-labelledby="settings-tab-account"
            hidden={selectedTab !== 'account'}
            className="outline-none focus-visible:ring-2 focus-visible:ring-ring rounded-xl"
          >
            <SettingsAccount />
          </section>
          <section
            id="settings-panel-profiles"
            role="tabpanel"
            aria-labelledby="settings-tab-profiles"
            hidden={selectedTab !== 'profiles'}
            className="outline-none focus-visible:ring-2 focus-visible:ring-ring rounded-xl"
          >
            <SettingsProfiles />
          </section>
          <section
            id="settings-panel-appearance"
            role="tabpanel"
            aria-labelledby="settings-tab-appearance"
            hidden={selectedTab !== 'appearance'}
            className="space-y-6 rounded-2xl border bg-card p-5 outline-none focus-visible:ring-2 focus-visible:ring-ring sm:p-6"
          >
            <div>
              <h2 className="text-base font-semibold">{t('settings.tabs.appearance')}</h2>
              <p className="mt-1 text-sm text-muted-foreground">{t('settings.appearance.description')}</p>
            </div>
            <fieldset id="settings-theme" className="space-y-3">
              <legend className="mb-3 font-medium">{t('settings.appearance.theme')}</legend>
              <div className="grid gap-3 lg:grid-cols-3">
                {(
                  [
                    { value: 'light', label: 'settings.appearance.light', description: 'settings.appearance.lightDescription' },
                    { value: 'dark', label: 'settings.appearance.dark', description: 'settings.appearance.darkDescription' },
                    { value: 'system', label: 'settings.appearance.system', description: 'settings.appearance.systemDescription' },
                  ] as const
                ).map((option) => (
                  <label
                    key={option.value}
                    className={`relative block cursor-pointer rounded-xl border p-3 transition-colors has-[:focus-visible]:ring-2 has-[:focus-visible]:ring-ring ${theme === option.value ? 'border-primary bg-accent/25' : 'hover:bg-muted/50'}`}
                  >
                    <input
                      type="radio"
                      name="theme"
                      value={option.value}
                      checked={theme === option.value}
                      onChange={() => setTheme(option.value)}
                      className="absolute right-3 top-3 size-3.5 accent-primary"
                    />
                    <span
                      aria-hidden="true"
                      className={`mb-3 flex h-20 overflow-hidden rounded-lg border ${option.value === 'dark' ? 'border-zinc-700 bg-zinc-950' : option.value === 'light' ? 'border-zinc-200 bg-white' : 'border-zinc-500 bg-[linear-gradient(90deg,#fff_50%,#18181b_50%)]'}`}
                    >
                      <span className={`w-5 border-r ${option.value === 'dark' ? 'border-zinc-700 bg-zinc-900' : 'border-zinc-200 bg-zinc-100'}`} />
                      <span className="flex-1 space-y-2 p-3">
                        <span className={`block h-1.5 w-1/2 rounded-full ${option.value === 'dark' ? 'bg-zinc-600' : 'bg-zinc-300'}`} />
                        <span className="grid grid-cols-2 gap-1.5">
                          <span className={`h-6 rounded ${option.value === 'dark' ? 'bg-zinc-800' : 'bg-zinc-200'}`} />
                          <span className={`h-6 rounded ${option.value === 'light' ? 'bg-zinc-200' : 'bg-zinc-800'}`} />
                        </span>
                      </span>
                    </span>
                    <span>
                      <span className="block text-sm font-medium">{t(option.label)}</span>
                      <span className="mt-1 block text-xs leading-relaxed text-muted-foreground">{t(option.description)}</span>
                    </span>
                  </label>
                ))}
              </div>
            </fieldset>
          </section>
          <section
            id="settings-panel-language"
            role="tabpanel"
            aria-labelledby="settings-tab-language"
            hidden={selectedTab !== 'language'}
            className="space-y-6 rounded-2xl border bg-card p-5 outline-none focus-visible:ring-2 focus-visible:ring-ring sm:p-6"
          >
            <div>
              <h2 className="text-base font-semibold">{t('settings.tabs.language')}</h2>
              <p className="mt-1 text-sm text-muted-foreground">{t('settings.language.description')}</p>
            </div>
            <fieldset id="settings-language" className="space-y-3">
              <legend className="mb-3 font-medium">{t('settings.language.displayLanguage')}</legend>
              <div className="grid gap-3 lg:grid-cols-2">
                {(
                  [
                    { value: 'en', label: 'English' },
                    { value: 'fr', label: 'Français' },
                  ] as const
                ).map((option) => (
                  <label
                    key={option.value}
                    className={`flex cursor-pointer items-center gap-3 rounded-xl border p-4 transition-colors ${language === option.value ? 'border-primary bg-accent/25' : 'hover:bg-muted/50'}`}
                  >
                    <input
                      type="radio"
                      name="language"
                      value={option.value}
                      checked={language === option.value}
                      onChange={() => setLanguage(option.value)}
                      className="size-4 accent-primary"
                    />
                    <span
                      aria-hidden="true"
                      className="flex size-9 items-center justify-center rounded-lg border bg-background text-xs font-semibold uppercase text-muted-foreground"
                    >
                      {option.value}
                    </span>
                    <span lang={option.value} className="font-medium">
                      {option.label}
                    </span>
                  </label>
                ))}
              </div>
            </fieldset>
          </section>
          <section
            id="settings-panel-playback"
            role="tabpanel"
            aria-labelledby="settings-tab-playback"
            hidden={selectedTab !== 'playback'}
            className="outline-none focus-visible:ring-2 focus-visible:ring-ring rounded-xl"
          >
            <PlaybackSettings />
          </section>
        </div>
      </div>
    </div>
  );
}

function PlaybackSettings() {
  const { user } = useAuth();
  const { t } = useI18n();
  const { settings: savedSettings, isReady, isError } = useUserSettings();
  const [pending, setPending] = useState<PlaybackField | null>(null);
  const [feedback, setFeedback] = useState<{ error?: TranslationKey; success?: TranslationKey }>({});

  const updateSetting = async <Field extends PlaybackField>(field: Field, value: SettingsRecord[Field]) => {
    if (!user || pending || !isReady) {
      return;
    }
    setPending(field);
    setFeedback({});
    try {
      // The collection is loaded whole (the server rule keeps it to this user's row), so a missing row really is missing: no duplicate can be created.
      if (savedSettings) {
        await settingCollection.update(savedSettings.id, (draft) => {
          Object.assign(draft, { [field]: value });
        }).isPersisted.promise;
      } else {
        const created: SettingsRecord = {
          id: settingCollection.utils.newId(),
          ...SYSTEM_FIELDS,
          user: user.id,
          hide_shorts: false,
          hide_watched: false,
          hide_subscriber_only: false,
          use_original_titles: true,
          playback_quality: 'auto',
          auto_skip_sponsors: false,
          autoplay: true,
        };
        Object.assign(created, { [field]: value });
        await settingCollection.insert(created).isPersisted.promise;
      }
      setFeedback({ success: 'settings.playback.saved' });
    } catch {
      setFeedback({ error: 'settings.playback.saveError' });
    } finally {
      setPending(null);
    }
  };

  return (
    <div className="space-y-6 rounded-2xl border bg-card p-5 sm:p-6">
      <div>
        <h2 className="text-base font-semibold">{t('settings.tabs.playback')}</h2>
        <p className="mt-1 text-sm text-muted-foreground">{t('settings.playback.description')}</p>
      </div>
      <div className="space-y-6" aria-busy={!!pending}>
        {(
          [
            {
              label: 'settings.playback.playerGroup',
              quality: true,
              options: [
                { field: 'autoplay', label: 'settings.playback.autoplay', description: 'settings.playback.autoplayDescription' },
                { field: 'auto_skip_sponsors', label: 'settings.playback.autoSkipSponsors', description: 'settings.playback.autoSkipSponsorsDescription' },
              ],
            },
            {
              label: 'settings.playback.catalogGroup',
              quality: false,
              options: [
                { field: 'hide_shorts', label: 'settings.playback.hideShorts', description: 'settings.playback.hideShortsDescription' },
                { field: 'hide_watched', label: 'settings.playback.hideWatched', description: 'settings.playback.hideWatchedDescription' },
                {
                  field: 'hide_subscriber_only',
                  label: 'settings.playback.hideSubscriberOnly',
                  description: 'settings.playback.hideSubscriberOnlyDescription',
                },
                { field: 'use_original_titles', label: 'settings.playback.originalTitles', description: 'settings.playback.originalTitlesDescription' },
              ],
            },
          ] as const
        ).map((group) => (
          <fieldset key={group.label} className="min-w-0">
            <legend className="mb-3 text-sm font-semibold">{t(group.label)}</legend>
            <div className="divide-y rounded-xl border">
              {group.quality && (
                <div className="flex flex-col gap-4 p-4 lg:flex-row lg:items-center lg:justify-between lg:gap-8">
                  <div className="min-w-0 space-y-1">
                    <label htmlFor="default-playback-quality" className="block font-medium">
                      {t('settings.playback.defaultQuality')}
                    </label>
                    <p id="default-playback-quality-description" className="text-sm text-muted-foreground">
                      {t('settings.playback.defaultQualityDescription')}
                    </p>
                  </div>
                  <Select
                    value={savedSettings?.playback_quality || 'auto'}
                    disabled={!!pending || !isReady}
                    onValueChange={(value) => void updateSetting('playback_quality', value as PlaybackQuality)}
                  >
                    <SelectTrigger id="default-playback-quality" aria-describedby="default-playback-quality-description" className="shrink-0 lg:w-56">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {playbackQualities.map((quality) => (
                        <SelectItem key={quality} value={quality}>
                          {quality === 'auto' ? t('player.quality.auto') : `${quality}p`}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
              )}
              {group.options.map((option) => {
                const checked =
                  option.field === 'use_original_titles' || option.field === 'autoplay'
                    ? (savedSettings?.[option.field] ?? true)
                    : (savedSettings?.[option.field] ?? false);
                return (
                  <div key={option.field} className="flex items-center justify-between gap-5 p-4">
                    <div>
                      <p id={`${option.field}-label`} className="font-medium">
                        {t(option.label)}
                      </p>
                      <p id={`${option.field}-description`} className="mt-1 text-sm text-muted-foreground">
                        {t(option.description)}
                      </p>
                    </div>
                    <button
                      id={`setting-${option.field}`}
                      type="button"
                      role="switch"
                      aria-checked={checked}
                      aria-labelledby={`${option.field}-label`}
                      aria-describedby={`${option.field}-description`}
                      disabled={!!pending || !isReady}
                      onClick={() => updateSetting(option.field, !checked)}
                      className={`relative inline-flex h-6 w-11 shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:cursor-wait disabled:opacity-50 ${checked ? 'bg-primary' : 'bg-input'}`}
                    >
                      <span
                        className={`pointer-events-none inline-block size-5 rounded-full bg-background shadow-lg transition-transform ${checked ? 'translate-x-5' : 'translate-x-0'}`}
                      />
                    </button>
                  </div>
                );
              })}
            </div>
          </fieldset>
        ))}
        {!isReady && (
          <p role={isError ? 'alert' : 'status'} className={isError ? 'text-sm text-destructive' : 'text-sm text-muted-foreground'}>
            {isError ? t('settings.playback.loadError') : t('settings.playback.loading')}
          </p>
        )}
      </div>
      <div aria-live="polite" aria-atomic="true" className="text-sm">
        {pending && <output className="text-muted-foreground">{t('common.saving')}</output>}
        {feedback.error && (
          <div className="space-y-2">
            <p role="alert" className="text-destructive">
              {t(feedback.error)}
            </p>
            <Button type="button" variant="outline" onClick={() => setFeedback({})}>
              {t('common.dismiss')}
            </Button>
          </div>
        )}
        {feedback.success && <output className="text-muted-foreground">{t(feedback.success)}</output>}
      </div>
    </div>
  );
}
