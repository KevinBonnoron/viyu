import { createFileRoute } from '@tanstack/react-router';
import { AdminGlobalSettings } from '@/components/admin/admin-global-settings';
import { AdminUsers } from '@/components/admin/admin-users';
import { PageTitle } from '@/components/page-title';
import { Button } from '@/components/ui/button';
import { useAuth } from '@/providers/auth-provider';
import { useI18n } from '@/providers/i18n-provider';

export const Route = createFileRoute('/_app/admin')({
  validateSearch: (search: Record<string, unknown>): { tab?: 'settings' | 'users' } => ({
    tab: search.tab === 'users' ? 'users' : undefined,
  }),
  component: AdminPage,
});

function AdminPage() {
  const { user, isAdmin, adminReady, adminError, retryAdmin } = useAuth();
  const { tab } = Route.useSearch();
  const { t } = useI18n();
  const users = tab === 'users';

  return (
    <div className="w-full space-y-7">
      <PageTitle description={t(users ? 'admin.usersDescription' : 'admin.settingsDescription')}>{t(users ? 'admin.users' : 'admin.settings')}</PageTitle>
      {adminError ? (
        <section role="alert" className="space-y-3 rounded-2xl border bg-card p-5">
          <p className="text-sm text-destructive">{t('admin.capabilityError')}</p>
          <Button type="button" variant="outline" onClick={retryAdmin}>
            {t('admin.retry')}
          </Button>
        </section>
      ) : !adminReady ? (
        <output className="block text-sm text-muted-foreground">{t('admin.checkingAccess')}</output>
      ) : !isAdmin || !user ? (
        <section role="alert" className="space-y-3 rounded-2xl border bg-card p-5">
          <h2 className="font-semibold">{t('admin.forbidden')}</h2>
          <p className="text-sm text-muted-foreground">{t('admin.forbiddenDescription')}</p>
          <Button type="button" variant="outline" onClick={retryAdmin}>
            {t('admin.retry')}
          </Button>
        </section>
      ) : users ? (
        <AdminUsers key={user.id} currentUserId={user.id} retryAdmin={retryAdmin} />
      ) : (
        <AdminGlobalSettings key={user.id} retryAdmin={retryAdmin} />
      )}
    </div>
  );
}
