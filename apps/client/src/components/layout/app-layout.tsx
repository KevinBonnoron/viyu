import { Eye, EyeOff } from 'lucide-react';
import { useEffect, useState } from 'react';
import { Notifications } from '@/components/notifications';
import { SearchDialog } from '@/components/search-dialog';
import { ThemeToggle } from '@/components/theme-toggle';
import { Button } from '@/components/ui/button';
import { cn } from '@/lib/utils';
import { useI18n } from '@/providers/i18n-provider';
import { PageHeaderProvider } from '@/providers/page-header-provider';
import { usePlaybackPrivacy } from '@/providers/playback-privacy-provider';
import { MobileNavigation } from './mobile-navigation';
import { Sidebar } from './sidebar';

const sidebarKey = 'viyu.sidebar.collapsed';

const privacyFrameStyle = {
  position: 'fixed',
  inset: 2,
  zIndex: 100,
  border: '2px dashed var(--primary)',
  borderRadius: 'var(--radius)',
  pointerEvents: 'none',
} as const;

export function PlaybackPrivacyFrame() {
  return <div aria-hidden="true" data-private-frame="" style={privacyFrameStyle} />;
}

export function AppLayout({ children }: { children: React.ReactNode }) {
  const { t } = useI18n();
  const [titleTarget, setTitleTarget] = useState<HTMLDivElement | null>(null);
  const [compact, setCompact] = useState(false);
  useEffect(() => {
    const update = () => setCompact((previous) => window.scrollY > (previous ? 4 : 48));
    update();
    window.addEventListener('scroll', update, { passive: true });
    return () => window.removeEventListener('scroll', update);
  }, []);
  const { privatePlayback, setPrivatePlayback } = usePlaybackPrivacy();
  const [collapsed, setCollapsed] = useState(() => {
    try {
      return localStorage.getItem(sidebarKey) === 'true';
    } catch {
      return false;
    }
  });
  const toggleSidebar = () => {
    setCollapsed((value) => {
      try {
        localStorage.setItem(sidebarKey, String(!value));
      } catch {
        // The layout remains usable when storage is unavailable.
      }
      return !value;
    });
  };
  return (
    <PageHeaderProvider target={titleTarget} compact={compact}>
      <div className="app-shell min-h-dvh" data-private-playback={privatePlayback}>
        {privatePlayback && <PlaybackPrivacyFrame />}
        <a
          href="#main-content"
          className="sr-only z-50 rounded-lg bg-primary px-4 py-2 text-primary-foreground focus:not-sr-only focus:fixed focus:left-4 focus:top-4"
        >
          {t('ui.shell.skipNavigation')}
        </a>
        <Sidebar collapsed={collapsed} onToggle={toggleSidebar} />
        <main
          id="main-content"
          tabIndex={-1}
          className={cn('ml-0 flex min-h-dvh min-w-0 flex-col px-3 pb-24 pt-3 outline-none md:ml-[4.5rem] md:px-4 md:py-4', !collapsed && 'md:ml-60')}
        >
          <div className="relative flex-1">
            <div
              data-topbar=""
              className={cn(
                'sticky top-0 z-30 -mx-3 mb-6 flex min-h-10 flex-wrap items-start justify-between gap-3 bg-background px-3 py-3 md:-mx-4 md:px-4',
                compact && 'border-b',
              )}
            >
              <div ref={setTitleTarget} className={cn('min-w-0 flex-1 empty:hidden', compact ? 'pt-2' : 'pt-1')} />
              <div className="ml-auto flex shrink-0 items-center gap-2">
                <Button
                  type="button"
                  variant={privatePlayback ? 'default' : 'ghost'}
                  size="icon"
                  aria-label={t('player.privatePlayback')}
                  aria-pressed={privatePlayback}
                  title={`${t(privatePlayback ? 'player.privatePlaybackActive' : 'player.privatePlayback')}\n${t('player.privatePlaybackDescription')}`}
                  onClick={() => setPrivatePlayback(!privatePlayback)}
                  className="rounded-full"
                >
                  {privatePlayback ? <EyeOff aria-hidden="true" className="size-5" /> : <Eye aria-hidden="true" className="size-5" />}
                </Button>
                <ThemeToggle />
                <Notifications />
              </div>
            </div>
            <div className="app-content w-full">{children}</div>
          </div>
        </main>
        <SearchDialog />
        <MobileNavigation />
      </div>
    </PageHeaderProvider>
  );
}
