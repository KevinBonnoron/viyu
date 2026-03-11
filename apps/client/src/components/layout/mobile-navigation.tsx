import { Link, useRouterState } from '@tanstack/react-router';
import { BarChart3, Heart, LogOut, MoreHorizontal, Server, Settings, Settings2, X } from 'lucide-react';
import { useState } from 'react';
import { ProfileSwitcher } from '@/components/profile-switcher';
import { Dialog } from '@/components/ui/dialog';
import { changeServer, isStandaloneClient } from '@/lib/server';
import { cn } from '@/lib/utils';
import { useAuth } from '@/providers/auth-provider';
import { useI18n } from '@/providers/i18n-provider';
import { useSubscriptions } from '@/providers/subscriptions-provider';
import { navigation } from './sidebar';

const primaryItems = [navigation[0].items[0], navigation[0].items[1], navigation[1].items[0], navigation[1].items[1]];

export function MobileNavigation() {
  const { t } = useI18n();
  const currentPath = useRouterState({ select: (state) => state.location.pathname });
  const { user, logout, isAdmin } = useAuth();
  const { data: subscriptions = [], isReady: subscriptionsReady } = useSubscriptions();
  const [open, setOpen] = useState(false);
  const [serverError, setServerError] = useState(false);
  const moreActive = ['/likes', '/stats', '/sources', '/settings', '/admin'].some((path) => currentPath.startsWith(path));

  const close = () => setOpen(false);
  return (
    <>
      <nav
        aria-label={t('ui.shell.navigation')}
        className="fixed inset-x-0 bottom-0 z-40 border-t border-sidebar-border bg-sidebar pb-[env(safe-area-inset-bottom)] md:hidden"
      >
        <div className="mx-auto grid max-w-lg grid-cols-5 px-1">
          {primaryItems.map(({ label, icon: Icon, to }) => {
            const active = to === '/' ? currentPath === '/' : currentPath.startsWith(to) || (to === '/subscriptions' && currentPath.startsWith('/series'));
            return (
              <Link
                key={to}
                to={to}
                title={t(label)}
                aria-label={t(label)}
                aria-current={active ? 'page' : undefined}
                className={cn(
                  'relative flex min-h-16 flex-col items-center justify-center gap-1 rounded-lg px-1 text-[11px] font-medium',
                  active ? 'text-primary' : 'text-muted-foreground',
                )}
              >
                <Icon aria-hidden="true" className="size-5" strokeWidth={active ? 2.2 : 1.8} />
                <span>{t(label)}</span>
                {to === '/subscriptions' && subscriptionsReady && subscriptions.length > 0 && (
                  <span className="absolute right-2 top-1 rounded-full bg-primary px-1 text-[9px] leading-4 text-primary-foreground">
                    {subscriptions.length}
                  </span>
                )}
              </Link>
            );
          })}
          <button
            type="button"
            aria-label={t('ui.shell.more')}
            aria-expanded={open}
            aria-controls="mobile-more-dialog"
            onClick={() => setOpen(true)}
            className={cn(
              'flex min-h-16 flex-col items-center justify-center gap-1 rounded-lg px-1 text-[11px] font-medium',
              moreActive ? 'text-primary' : 'text-muted-foreground',
            )}
          >
            <MoreHorizontal aria-hidden="true" className="size-5" />
            <span>{t('ui.shell.more')}</span>
          </button>
        </div>
      </nav>
      <Dialog
        open={open}
        id="mobile-more-dialog"
        aria-label={t('ui.shell.more')}
        onClose={close}
        closeOnBackdrop
        className="m-0 mt-auto max-h-[85dvh] w-full max-w-none overflow-y-auto rounded-b-none rounded-t-2xl border-x-0 border-b-0 bg-popover p-0 text-popover-foreground shadow-2xl backdrop:backdrop-blur-none sm:p-0"
      >
        <div className="mx-auto max-w-lg p-4 pb-[max(1rem,env(safe-area-inset-bottom))]">
          <div className="mb-4 flex items-center justify-between">
            <h2 className="text-lg font-semibold">{t('ui.shell.more')}</h2>
            <button type="button" aria-label={t('common.dismiss')} onClick={close} className="rounded-lg p-2 text-muted-foreground hover:bg-muted">
              <X aria-hidden="true" className="size-5" />
            </button>
          </div>
          <div className="grid grid-cols-2 gap-2">
            <Link to="/likes" onClick={close} className="flex items-center gap-2 rounded-xl border p-3 text-sm hover:bg-muted">
              <Heart aria-hidden="true" className="size-4" />
              {t('nav.likes')}
            </Link>
            <Link to="/stats" onClick={close} className="flex items-center gap-2 rounded-xl border p-3 text-sm hover:bg-muted">
              <BarChart3 aria-hidden="true" className="size-4" />
              {t('nav.stats')}
            </Link>
            <Link to="/sources" onClick={close} className="flex items-center gap-2 rounded-xl border p-3 text-sm hover:bg-muted">
              <Settings aria-hidden="true" className="size-4" />
              {t('nav.sources')}
            </Link>
            <Link to="/settings" onClick={close} className="flex items-center gap-2 rounded-xl border p-3 text-sm hover:bg-muted">
              <Settings2 aria-hidden="true" className="size-4" />
              {t('nav.settings')}
            </Link>
            {isAdmin && (
              <Link to="/admin" search={{ tab: 'settings' }} onClick={close} className="flex items-center gap-2 rounded-xl border p-3 text-sm hover:bg-muted">
                <Settings2 aria-hidden="true" className="size-4" />
                {t('admin.settings')}
              </Link>
            )}
          </div>
          <section className="mt-5 border-t pt-4">
            <h3 className="mb-2 text-xs font-semibold uppercase tracking-wide text-muted-foreground">{t('profiles.label')}</h3>
            <ProfileSwitcher onSelect={close} />
          </section>
          {isStandaloneClient && (
            <button
              type="button"
              onClick={() => {
                setServerError(false);
                void changeServer().catch(() => setServerError(true));
              }}
              className="mt-4 flex w-full items-center gap-2 rounded-xl border p-3 text-sm hover:bg-muted"
            >
              <Server aria-hidden="true" className="size-4" />
              {t('server.change')}
            </button>
          )}
          {serverError && (
            <p role="alert" className="mt-2 text-xs text-destructive">
              {t('server.errors.storage')}
            </p>
          )}
          {user && (
            <button
              type="button"
              onClick={() => {
                close();
                logout();
              }}
              className="mt-2 flex w-full items-center gap-2 rounded-xl border p-3 text-sm text-destructive hover:bg-muted"
            >
              <LogOut aria-hidden="true" className="size-4" />
              {t('nav.logout')}
            </button>
          )}
        </div>
      </Dialog>
    </>
  );
}
