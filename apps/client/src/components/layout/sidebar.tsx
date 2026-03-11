import { Link, useRouterState } from '@tanstack/react-router';
import {
  BarChart3,
  ChevronDown,
  ChevronsUpDown,
  Clock,
  Heart,
  Home,
  LogOut,
  PanelLeftClose,
  PanelLeftOpen,
  Plug,
  Search,
  Server,
  Settings,
  Settings2,
  Users,
} from 'lucide-react';
import { useEffect, useRef, useState } from 'react';
import { AccountAvatar, accountDisplayName } from '@/components/account-avatar';
import { Brand } from '@/components/brand';
import { ProfileAvatar } from '@/components/profile-avatar';
import { ProfileSwitcher } from '@/components/profile-switcher';
import { shortcutModifierLabel } from '@/lib/platform-shortcut';
import { changeServer, isStandaloneClient } from '@/lib/server';
import { isSourceActive } from '@/lib/source-activation';
import { cn } from '@/lib/utils';
import { useAuth } from '@/providers/auth-provider';
import { useI18n } from '@/providers/i18n-provider';
import { useProfiles } from '@/providers/profiles-provider';
import { useSources } from '@/providers/sources-provider';
import { useSubscriptions } from '@/providers/subscriptions-provider';

export const navigation = [
  {
    label: 'ui.shell.discover',
    items: [
      { label: 'nav.feed', icon: Home, to: '/' },
      { label: 'nav.search', icon: Search, to: '/search' },
    ],
  },
  {
    label: 'ui.shell.library',
    items: [
      { label: 'nav.subscriptions', icon: Users, to: '/subscriptions' },
      { label: 'nav.history', icon: Clock, to: '/history' },
      { label: 'nav.likes', icon: Heart, to: '/likes' },
      { label: 'nav.stats', icon: BarChart3, to: '/stats' },
    ],
  },
] as const;

export function Sidebar({ collapsed, onToggle }: { collapsed: boolean; onToggle: () => void }) {
  const currentPath = useRouterState({ select: (state) => state.location.pathname });
  const currentSearch = useRouterState({ select: (state) => state.location.searchStr });
  const { user, logout, isAdmin } = useAuth();
  const { t } = useI18n();
  const { sources } = useSources();
  const { activeProfile } = useProfiles();
  const { data: subscriptions = [], isReady: subscriptionsReady } = useSubscriptions();
  const connected = sources.filter(isSourceActive).length;
  const [showMenu, setShowMenu] = useState(false);
  const [serverError, setServerError] = useState(false);
  const [showProfiles, setShowProfiles] = useState(false);
  const menuRef = useRef<HTMLDivElement>(null);
  const accountRef = useRef<HTMLButtonElement>(null);
  const profilesRef = useRef<HTMLDivElement>(null);
  const profileButtonRef = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    if (!showMenu) {
      return;
    }
    const outside = (event: MouseEvent) => {
      if (!menuRef.current?.contains(event.target as Node)) {
        setShowMenu(false);
      }
    };
    const handleEscape = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        setShowMenu(false);
        accountRef.current?.focus();
      }
    };
    document.addEventListener('mousedown', outside);
    document.addEventListener('keydown', handleEscape);
    return () => {
      document.removeEventListener('mousedown', outside);
      document.removeEventListener('keydown', handleEscape);
    };
  }, [showMenu]);

  useEffect(() => {
    if (!showProfiles) {
      return;
    }
    const outside = (event: MouseEvent) => {
      if (!profilesRef.current?.contains(event.target as Node)) {
        setShowProfiles(false);
      }
    };
    const handleEscape = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        setShowProfiles(false);
        profileButtonRef.current?.focus();
      }
    };
    document.addEventListener('mousedown', outside);
    document.addEventListener('keydown', handleEscape);
    return () => {
      document.removeEventListener('mousedown', outside);
      document.removeEventListener('keydown', handleEscape);
    };
  }, [showProfiles]);

  const navClass = cn('flex h-10 items-center justify-center gap-3 rounded-lg px-3 text-sm font-medium transition-colors', !collapsed && 'md:justify-start');
  const showLabels = !collapsed;

  return (
    <aside className={cn('fixed inset-y-0 left-0 z-30 hidden w-[4.5rem] flex-col border-r border-sidebar-border bg-sidebar md:flex', !collapsed && 'md:w-60')}>
      <div className={cn('flex h-16 shrink-0 items-center justify-center px-2', !collapsed && 'md:justify-start md:px-4')}>
        <Link to="/" aria-label="Viyu" className="rounded-xl">
          <Brand className={cn('[&_.brand-name]:hidden', showLabels && 'md:[&_.brand-name]:inline')} />
        </Link>
      </div>
      {user && (
        <div className="relative shrink-0 px-3 pb-3" ref={profilesRef}>
          <button
            ref={profileButtonRef}
            type="button"
            aria-label={t('profiles.label')}
            aria-expanded={showProfiles}
            aria-controls="profile-popover"
            title={activeProfile?.name ?? t('profiles.label')}
            onClick={() => {
              setShowProfiles((open) => !open);
              setShowMenu(false);
            }}
            className={cn(
              'flex h-10 w-full items-center justify-center gap-3 rounded-lg px-3 text-left text-sm font-medium transition-colors',
              showLabels && 'md:justify-start',
              showProfiles ? 'bg-sidebar-accent text-sidebar-accent-foreground' : 'text-muted-foreground hover:bg-muted hover:text-foreground',
            )}
          >
            <ProfileAvatar profile={activeProfile} className="size-7" />
            <span className={cn('hidden min-w-0 flex-1 truncate', showLabels && 'md:block')}>{activeProfile?.name ?? t('profiles.label')}</span>
            <ChevronDown
              aria-hidden="true"
              className={cn('hidden size-4 shrink-0 transition-transform', showLabels && 'md:block', showProfiles && 'rotate-180')}
            />
          </button>
          {showProfiles && (
            <div id="profile-popover" className="absolute left-3 top-full z-50 w-60 rounded-xl border bg-popover p-1.5 text-popover-foreground shadow-xl">
              <ProfileSwitcher onSelect={() => setShowProfiles(false)} />
            </div>
          )}
        </div>
      )}
      <nav aria-label={t('ui.shell.navigation')} className="min-h-0 flex-1 space-y-7 overflow-y-auto px-3">
        {navigation.map((group) => (
          <div key={group.label}>
            <p className={cn('mb-2 hidden px-3 text-[11px] font-semibold uppercase tracking-[0.1em] text-muted-foreground/75', showLabels && 'md:block')}>
              {t(group.label)}
            </p>
            <div className="space-y-1">
              {group.items.map(({ label, icon: Icon, to }) => {
                const active = to === '/' ? currentPath === '/' : currentPath.startsWith(to) || (to === '/subscriptions' && currentPath.startsWith('/series'));
                return (
                  <Link
                    key={to}
                    to={to}
                    title={t(label)}
                    aria-label={t(label)}
                    aria-current={active ? 'page' : undefined}
                    className={cn(
                      navClass,
                      'relative',
                      active ? 'bg-sidebar-accent text-sidebar-accent-foreground' : 'text-muted-foreground hover:bg-muted hover:text-foreground',
                    )}
                  >
                    <Icon aria-hidden="true" className="size-[18px] shrink-0" strokeWidth={1.8} />
                    <span className={cn('hidden', showLabels && 'md:inline')}>{t(label)}</span>
                    {to === '/subscriptions' && subscriptionsReady && subscriptions.length > 0 && (
                      <span
                        className={cn(
                          'min-w-5 rounded-md bg-background px-1.5 py-0.5 text-center text-[10px] tabular-nums text-muted-foreground',
                          showLabels
                            ? 'ml-auto hidden md:inline'
                            : 'absolute bottom-0 right-0 rounded-full border border-sidebar bg-sidebar px-1 py-0 leading-3 text-sidebar-foreground',
                        )}
                      >
                        {subscriptions.length}
                      </span>
                    )}
                    {to === '/search' && showLabels && (
                      <kbd className="ml-auto hidden rounded border px-1 py-0.5 text-[9px] font-normal text-muted-foreground lg:inline">
                        {shortcutModifierLabel()} K
                      </kbd>
                    )}
                  </Link>
                );
              })}
            </div>
          </div>
        ))}
        {isAdmin && (
          <div>
            <p className={cn('mb-2 hidden px-3 text-[11px] font-semibold uppercase tracking-[0.1em] text-muted-foreground/75', showLabels && 'md:block')}>
              {t('admin.title')}
            </p>
            <div className="space-y-1">
              <Link
                to="/admin"
                search={{ tab: 'settings' }}
                title={t('admin.settings')}
                aria-label={t('admin.settings')}
                aria-current={currentPath === '/admin' && !currentSearch.includes('tab=users') ? 'page' : undefined}
                className={cn(
                  navClass,
                  currentPath === '/admin' && !currentSearch.includes('tab=users')
                    ? 'bg-sidebar-accent text-sidebar-accent-foreground'
                    : 'text-muted-foreground hover:bg-muted hover:text-foreground',
                )}
              >
                <Settings2 aria-hidden="true" className="size-[18px] shrink-0" strokeWidth={1.8} />
                <span className={cn('hidden', showLabels && 'md:inline')}>{t('admin.settings')}</span>
              </Link>
              <Link
                to="/admin"
                search={{ tab: 'users' }}
                title={t('admin.users')}
                aria-label={t('admin.users')}
                aria-current={currentPath === '/admin' && currentSearch.includes('tab=users') ? 'page' : undefined}
                className={cn(
                  navClass,
                  currentPath === '/admin' && currentSearch.includes('tab=users')
                    ? 'bg-sidebar-accent text-sidebar-accent-foreground'
                    : 'text-muted-foreground hover:bg-muted hover:text-foreground',
                )}
              >
                <Users aria-hidden="true" className="size-[18px] shrink-0" strokeWidth={1.8} />
                <span className={cn('hidden', showLabels && 'md:inline')}>{t('admin.users')}</span>
              </Link>
            </div>
          </div>
        )}
        <div className="border-t pt-4">
          <Link
            to="/sources"
            title={t('nav.sources')}
            aria-label={t('nav.sources')}
            aria-current={currentPath === '/sources' ? 'page' : undefined}
            className={cn(
              navClass,
              currentPath === '/sources' ? 'bg-sidebar-accent text-sidebar-accent-foreground' : 'text-muted-foreground hover:bg-muted hover:text-foreground',
            )}
          >
            <Plug aria-hidden="true" className="size-[18px] shrink-0" strokeWidth={1.8} />
            <span className={cn('hidden', showLabels && 'md:inline')}>{t('nav.sources')}</span>
            {connected > 0 && showLabels && (
              <span className="ml-auto hidden min-w-5 rounded-md bg-background px-1.5 py-0.5 text-center text-[10px] tabular-nums text-muted-foreground md:inline">
                {connected}
              </span>
            )}
          </Link>
        </div>
      </nav>
      <div className="shrink-0 px-3 pb-3 pt-5">
        <div className="mb-2 hidden justify-end md:flex">
          <button
            type="button"
            aria-label={t(collapsed ? 'ui.shell.expandSidebar' : 'ui.shell.collapseSidebar')}
            title={t(collapsed ? 'ui.shell.expandSidebar' : 'ui.shell.collapseSidebar')}
            onClick={onToggle}
            className="rounded-lg p-2 text-muted-foreground hover:bg-muted hover:text-foreground"
          >
            {collapsed ? <PanelLeftOpen aria-hidden="true" className="size-4" /> : <PanelLeftClose aria-hidden="true" className="size-4" />}
          </button>
        </div>
        {user && (
          <div className="relative border-t pt-3" ref={menuRef}>
            <button
              ref={accountRef}
              type="button"
              aria-expanded={showMenu}
              aria-controls="account-popover"
              aria-label={t('nav.accountOptions')}
              onClick={() => {
                setShowMenu((open) => !open);
                setShowProfiles(false);
                setServerError(false);
              }}
              className={cn(
                'flex w-full items-center justify-center gap-2.5 rounded-xl p-2 text-left transition-colors',
                showLabels && 'md:justify-start',
                showMenu || currentPath === '/settings' || currentPath === '/admin' ? 'bg-muted' : 'hover:bg-muted',
              )}
            >
              <AccountAvatar user={user} className="size-8 text-xs" />
              <span className={cn('hidden min-w-0 flex-1', showLabels && 'md:block')}>
                <span className="block truncate text-xs font-semibold">{accountDisplayName(user)}</span>
                <span className="mt-0.5 block truncate text-[11px] text-muted-foreground">
                  {String(user.name ?? '').trim() ? user.email : t('ui.shell.account')}
                </span>
              </span>
              <ChevronsUpDown aria-hidden="true" className={cn('hidden size-3.5 text-muted-foreground', showLabels && 'md:block')} />
            </button>
            {showMenu && (
              <div
                id="account-popover"
                className="absolute bottom-full left-0 z-50 mb-2 w-60 rounded-xl border bg-popover p-1.5 text-popover-foreground shadow-xl"
              >
                <div className="mb-1 border-b px-2.5 py-3">
                  <p className="truncate text-sm font-medium">{accountDisplayName(user)}</p>
                  <p className="mt-0.5 truncate text-xs text-muted-foreground">{user.email}</p>
                </div>
                <Link to="/settings" onClick={() => setShowMenu(false)} className="flex items-center gap-2.5 rounded-lg px-2.5 py-2.5 text-sm hover:bg-muted">
                  <Settings aria-hidden="true" className="size-4 text-muted-foreground" />
                  {t('nav.settings')}
                </Link>
                {isStandaloneClient && (
                  <button
                    type="button"
                    onClick={() => {
                      setServerError(false);
                      void changeServer().catch(() => setServerError(true));
                    }}
                    className="flex w-full items-center gap-2.5 rounded-lg px-2.5 py-2.5 text-sm hover:bg-muted"
                  >
                    <Server aria-hidden="true" className="size-4 text-muted-foreground" />
                    {t('server.change')}
                  </button>
                )}
                {serverError && (
                  <p role="alert" className="px-2.5 py-2 text-xs text-destructive">
                    {t('server.errors.storage')}
                  </p>
                )}
                <button
                  type="button"
                  onClick={() => {
                    setShowMenu(false);
                    logout();
                  }}
                  className="flex w-full items-center gap-2.5 rounded-lg px-2.5 py-2.5 text-sm hover:bg-muted"
                >
                  <LogOut aria-hidden="true" className="size-4 text-muted-foreground" />
                  {t('nav.logout')}
                </button>
              </div>
            )}
          </div>
        )}
      </div>
    </aside>
  );
}
