import { useState } from 'react';
import { NotificationsProvider } from '@/components/notifications';
import { SearchDialog } from '@/components/search-dialog';
import { cn } from '@/lib/utils';
import { useI18n } from '@/providers/i18n-provider';
import { MobileNavigation } from './mobile-navigation';
import { Sidebar } from './sidebar';

const sidebarKey = 'viyu.sidebar.collapsed';

export function AppLayout({ children }: { children: React.ReactNode }) {
  const { t } = useI18n();
  const [quickMode, setQuickMode] = useState<'search' | 'commands' | null>(null);
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
  const openCommands = () => setQuickMode('commands');
  return (
    <NotificationsProvider>
      <div className="app-shell min-h-dvh">
        <a
          href="#main-content"
          className="sr-only z-50 rounded-lg bg-primary px-4 py-2 text-primary-foreground focus:not-sr-only focus:fixed focus:left-4 focus:top-4"
        >
          {t('ui.shell.skipNavigation')}
        </a>
        <Sidebar collapsed={collapsed} onToggle={toggleSidebar} onOpenCommands={openCommands} />
        <main
          id="main-content"
          tabIndex={-1}
          className={cn(
            'app-main ml-0 flex min-h-dvh min-w-0 flex-col pl-[max(0.75rem,var(--safe-area-inset-left,env(safe-area-inset-left,0px)))] pr-[max(0.75rem,var(--safe-area-inset-right,env(safe-area-inset-right,0px)))] pb-24 outline-none desktop:ml-[4.5rem] desktop:px-4 desktop:pb-4',
            !collapsed && 'desktop:ml-60',
          )}
        >
          <div className="app-content relative w-full flex-1">{children}</div>
        </main>
        <SearchDialog mode={quickMode} onModeChange={setQuickMode} onToggleSidebar={toggleSidebar} sidebarCollapsed={collapsed} />
        <MobileNavigation onOpenCommands={openCommands} />
      </div>
    </NotificationsProvider>
  );
}
