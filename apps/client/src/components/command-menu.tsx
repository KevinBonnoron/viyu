import { useNavigate } from '@tanstack/react-router';
import type { LucideIcon } from 'lucide-react';
import {
  ArrowUpRight,
  Check,
  Command,
  Eye,
  EyeOff,
  Languages,
  LoaderCircle,
  LogOut,
  Monitor,
  Moon,
  PanelLeftClose,
  PanelLeftOpen,
  Plug,
  Settings,
  Sun,
  Users,
  X,
} from 'lucide-react';
import { Fragment, type RefObject, useEffect, useId, useState } from 'react';
import { navigation } from '@/components/layout/sidebar';
import { Input } from '@/components/ui/input';
import { foldSearch } from '@/lib/text';
import { cn } from '@/lib/utils';
import { useAuth } from '@/providers/auth-provider';
import { useI18n } from '@/providers/i18n-provider';
import { usePlaybackPrivacy } from '@/providers/playback-privacy-provider';
import { useProfiles } from '@/providers/profiles-provider';
import { useTheme } from '@/providers/theme-provider';

type CommandChoice = { id: string; title: string; group: string; icon: LucideIcon; current?: boolean; run: (() => void) | (() => Promise<unknown>) };

/** Application actions only: typing here never starts a content search or changes a setting. */
export function CommandMenu({
  onClose,
  onToggleSidebar,
  sidebarCollapsed,
  inputRef,
}: {
  onClose: () => void;
  onToggleSidebar: () => void;
  sidebarCollapsed: boolean;
  inputRef: RefObject<HTMLInputElement | null>;
}) {
  const { t, language, setLanguage } = useI18n();
  const { theme, setTheme } = useTheme();
  const { privatePlayback, setPrivatePlayback } = usePlaybackPrivacy();
  const { profiles, activeProfile, selectProfile } = useProfiles();
  const { isAdmin, logout } = useAuth();
  const navigate = useNavigate();
  const id = useId();
  const [query, setQuery] = useState('');
  const [selected, setSelected] = useState<string>();
  const [pending, setPending] = useState(false);
  const [failed, setFailed] = useState(false);
  const navigationGroup = t('commands.navigation');
  const settingsGroup = t('nav.settings');
  const appearanceGroup = t('settings.tabs.appearance');
  const profileGroup = t('profiles.label');
  const commands: CommandChoice[] = [
    ...navigation.flatMap((group) =>
      group.items.map((item) => ({ id: item.to, title: t(item.label), group: navigationGroup, icon: item.icon, run: () => navigate({ to: item.to }) })),
    ),
    { id: 'sources', title: t('nav.sources'), group: navigationGroup, icon: Plug, run: () => navigate({ to: '/sources' }) },
    { id: 'series', title: t('nav.series'), group: navigationGroup, icon: Users, run: () => navigate({ to: '/subscriptions', search: { tab: 'series' } }) },
    ...(['account', 'profiles', 'appearance', 'language', 'playback'] as const).map((tab) => ({
      id: `settings:${tab}`,
      title: t(tab === 'profiles' ? 'profiles.manage' : `settings.tabs.${tab}`),
      group: settingsGroup,
      icon: Settings,
      run: () => navigate({ to: '/settings', search: { tab } }),
    })),
    ...(isAdmin
      ? ([
          {
            id: 'admin:settings',
            title: t('admin.settings'),
            group: t('admin.title'),
            icon: Settings,
            run: () => navigate({ to: '/admin', search: { tab: 'settings' } }),
          },
          { id: 'admin:users', title: t('admin.users'), group: t('admin.title'), icon: Users, run: () => navigate({ to: '/admin', search: { tab: 'users' } }) },
        ] satisfies CommandChoice[])
      : []),
    ...(['light', 'dark', 'system'] as const).map((value) => ({
      id: `theme:${value}`,
      title: t('commands.theme', { theme: t(`settings.appearance.${value}`) }),
      group: appearanceGroup,
      icon: value === 'light' ? Sun : value === 'dark' ? Moon : Monitor,
      current: theme === value,
      run: () => setTheme(value),
    })),
    {
      id: 'sidebar',
      title: t(sidebarCollapsed ? 'ui.shell.expandSidebar' : 'ui.shell.collapseSidebar'),
      group: appearanceGroup,
      icon: sidebarCollapsed ? PanelLeftOpen : PanelLeftClose,
      run: onToggleSidebar,
    },
    ...profiles.map((profile) => ({
      id: `profile:${profile.id}`,
      title: t('commands.profile', { name: profile.name }),
      group: profileGroup,
      icon: Users,
      current: activeProfile?.id === profile.id,
      run: () => selectProfile(profile.id),
    })),
    ...(['fr', 'en'] as const).map((value) => ({
      id: `language:${value}`,
      title: t('commands.language', { language: value === 'fr' ? 'Français' : 'English' }),
      group: t('settings.tabs.language'),
      icon: Languages,
      current: language === value,
      run: () => setLanguage(value),
    })),
    {
      id: 'privacy',
      title: t(privatePlayback ? 'commands.privateOff' : 'commands.privateOn'),
      group: t('ui.shell.account'),
      icon: privatePlayback ? Eye : EyeOff,
      run: () => setPrivatePlayback(!privatePlayback),
    },
    { id: 'logout', title: t('nav.logout'), group: t('ui.shell.account'), icon: LogOut, run: logout },
  ];
  const terms = foldSearch(query).trim().split(/\s+/).filter(Boolean);
  const choices = commands.filter((command) => {
    const text = foldSearch(`${command.group} ${command.title}`);
    return terms.every((term) => text.includes(term));
  });
  const active = Math.max(
    0,
    choices.findIndex((choice) => choice.id === selected),
  );
  const activeId = choices[active]?.id;

  useEffect(() => {
    inputRef.current?.focus();
  }, [inputRef]);
  useEffect(() => {
    if (activeId) {
      document.getElementById(`${id}-${activeId}`)?.scrollIntoView({ block: 'nearest' });
    }
  }, [id, activeId]);

  const choose = async (choice: CommandChoice) => {
    if (pending) {
      return;
    }
    setPending(true);
    setFailed(false);
    try {
      await choice.run();
      onClose();
    } catch {
      setFailed(true);
    } finally {
      setPending(false);
    }
  };

  return (
    <>
      <div className="flex items-center gap-3 border-b px-4 py-3">
        <Command aria-hidden="true" className="size-5 shrink-0 text-muted-foreground" />
        <Input
          ref={inputRef}
          role="combobox"
          aria-label={t('commands.placeholder')}
          aria-controls={`${id}-results`}
          aria-expanded={choices.length > 0}
          aria-autocomplete="list"
          aria-activedescendant={activeId ? `${id}-${activeId}` : undefined}
          placeholder={t('commands.placeholder')}
          value={query}
          disabled={pending}
          onChange={(event) => {
            setQuery(event.target.value);
            setSelected(undefined);
            setFailed(false);
          }}
          onKeyDown={(event) => {
            if (event.nativeEvent.isComposing || pending) {
              return;
            }
            if ((event.key === 'ArrowDown' || event.key === 'ArrowUp') && choices.length) {
              event.preventDefault();
              setSelected(choices[(active + (event.key === 'ArrowDown' ? 1 : choices.length - 1)) % choices.length].id);
            } else if (event.key === 'Home' || event.key === 'End') {
              if (!choices.length) {
                return;
              }
              event.preventDefault();
              setSelected(choices[event.key === 'Home' ? 0 : choices.length - 1].id);
            } else if (event.key === 'Enter' && choices[active]) {
              event.preventDefault();
              void choose(choices[active]);
            }
          }}
          className="h-11 border-0 bg-transparent px-0 shadow-none focus-visible:ring-0"
        />
        <button
          type="button"
          aria-label={t('commands.close')}
          onClick={onClose}
          className="rounded-lg p-2 text-muted-foreground hover:bg-muted hover:text-foreground"
        >
          <X aria-hidden="true" className="size-4" />
        </button>
      </div>
      <div className="max-h-[55dvh] overflow-y-auto p-2">
        <div id={`${id}-results`} role="listbox" aria-label={t('commands.title')} aria-busy={pending}>
          {choices.map((choice, index) => (
            <Fragment key={choice.id}>
              {(index === 0 || choices[index - 1].group !== choice.group) && (
                <div role="presentation" className="px-3 pb-1 pt-3 text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">
                  {choice.group}
                </div>
              )}
              <button
                type="button"
                tabIndex={-1}
                id={`${id}-${choice.id}`}
                role="option"
                aria-selected={index === active}
                disabled={pending}
                onPointerMove={() => {
                  if (!pending) {
                    setSelected(choice.id);
                  }
                }}
                onMouseDown={(event) => event.preventDefault()}
                onClick={() => void choose(choice)}
                className={cn(
                  'flex w-full cursor-pointer items-center gap-3 rounded-lg px-3 py-3 text-left text-sm',
                  index === active ? 'bg-accent text-accent-foreground' : 'hover:bg-muted',
                )}
              >
                <choice.icon aria-hidden="true" className="size-4 shrink-0 text-muted-foreground" />
                <span className="min-w-0 flex-1 break-words">{choice.title}</span>
                {choice.current ? (
                  <Check aria-hidden="true" className="size-4 text-primary" />
                ) : (
                  <ArrowUpRight aria-hidden="true" className="size-4 text-muted-foreground" />
                )}
                {choice.current && <span className="sr-only">{t('commands.current')}</span>}
              </button>
            </Fragment>
          ))}
        </div>
        {choices.length === 0 && (
          <p role="status" className="p-7 text-center text-sm text-muted-foreground">
            {t('commands.empty')}
          </p>
        )}
        {pending && <LoaderCircle aria-label={t('search.loading')} className="mx-auto my-3 size-4 animate-spin" />}
        {failed && (
          <p role="alert" className="p-3 text-sm text-destructive">
            {t('commands.failed')}
          </p>
        )}
      </div>
      <div className="border-t px-4 py-3 text-xs text-muted-foreground">{t('commands.keyboard')}</div>
    </>
  );
}
