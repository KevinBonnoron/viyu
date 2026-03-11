import { ArrowRightLeft, Check, Pencil, Plus, Trash2 } from 'lucide-react';
import { useState } from 'react';
import { ErrorSnackbar } from '@/components/error-snackbar';
import { MoveSubscriptionsDialog } from '@/components/move-subscriptions-dialog';
import { ProfileAvatar } from '@/components/profile-avatar';
import { ProfileDialog } from '@/components/profile-dialog';
import { Button } from '@/components/ui/button';
import { useI18n } from '@/providers/i18n-provider';
import { useProfiles } from '@/providers/profiles-provider';
import type { ProfileRecord } from '@/types';

export function SettingsProfiles() {
  const { t } = useI18n();
  const { profiles, activeProfile, isReady, isError, selectProfile, deleteProfile, retry } = useProfiles();
  // A profile means editing it; an empty object means creating one.
  const [dialog, setDialog] = useState<{ profile?: ProfileRecord }>();
  const [deletingId, setDeletingId] = useState<string>();
  const [pending, setPending] = useState(false);
  const [moving, setMoving] = useState<ProfileRecord>();
  const [deleteFailed, setDeleteFailed] = useState(false);

  const remove = async (id: string) => {
    if (pending) {
      return;
    }
    setPending(true);
    setDeleteFailed(false);
    try {
      await deleteProfile(id);
      setDeletingId(undefined);
    } catch {
      setDeleteFailed(true);
    } finally {
      setPending(false);
    }
  };

  return (
    <div className="w-full overflow-hidden rounded-2xl border bg-card">
      <header className="flex flex-col gap-4 border-b p-5 sm:flex-row sm:items-center sm:justify-between sm:p-6">
        <div>
          <h2 className="text-base font-semibold">{t('profiles.label')}</h2>
          <p className="mt-1 text-sm text-muted-foreground">{t('profiles.description')}</p>
        </div>
        <Button
          type="button"
          onClick={() => {
            setDeletingId(undefined);
            setDialog({});
          }}
        >
          <Plus aria-hidden="true" className="size-4" />
          {t('profiles.new')}
        </Button>
      </header>
      <section aria-label={t('profiles.label')}>
        {!isReady && !isError && <p className="p-5 text-sm text-muted-foreground">{t('common.loading')}</p>}
        {isError && <ErrorSnackbar message={t('profiles.loadError')} action={t('common.retry')} onAction={retry} />}
        <div className="divide-y">
          {profiles.map((profile) => {
            const selected = activeProfile?.id === profile.id;
            if (deletingId === profile.id) {
              return (
                <div key={profile.id} className="space-y-3 bg-muted/30 p-4">
                  <p className="text-sm">{t('profiles.deleteConfirm', { name: profile.name })}</p>
                  <div className="flex gap-2">
                    <Button type="button" size="sm" variant="destructive" disabled={pending} onClick={() => void remove(profile.id)}>
                      {t('common.delete')}
                    </Button>
                    <Button type="button" size="sm" variant="ghost" disabled={pending} onClick={() => setDeletingId(undefined)}>
                      {t('common.cancel')}
                    </Button>
                  </div>
                </div>
              );
            }
            return (
              <div key={profile.id} className="flex items-center gap-2 p-2 pl-4">
                <button
                  type="button"
                  aria-pressed={selected}
                  disabled={pending}
                  onClick={() => selectProfile(profile.id)}
                  className="flex min-w-0 flex-1 items-center gap-3 rounded-lg px-2 py-2 text-left transition-colors hover:bg-muted"
                >
                  <ProfileAvatar profile={profile} />
                  <span className="min-w-0 flex-1 truncate text-sm font-medium">{profile.name}</span>
                  {selected && (
                    <span className="flex items-center gap-1.5 text-xs text-primary">
                      <Check aria-hidden="true" className="size-4" />
                      {t('profiles.active')}
                    </span>
                  )}
                </button>
                <Button
                  type="button"
                  size="sm"
                  variant="ghost"
                  disabled={pending}
                  aria-label={t('profiles.edit', { name: profile.name })}
                  title={t('profiles.edit', { name: profile.name })}
                  onClick={() => setDialog({ profile })}
                >
                  <Pencil aria-hidden="true" className="size-4" />
                </Button>
                <Button
                  type="button"
                  size="sm"
                  variant="ghost"
                  disabled={pending}
                  aria-label={t('profiles.move.action', { name: profile.name })}
                  title={t('profiles.move.action', { name: profile.name })}
                  onClick={() => setMoving(profile)}
                >
                  <ArrowRightLeft aria-hidden="true" className="size-4" />
                </Button>
                <Button
                  type="button"
                  size="sm"
                  variant="ghost"
                  disabled={pending || profiles.length === 1}
                  aria-label={t('profiles.delete', { name: profile.name })}
                  title={profiles.length === 1 ? t('profiles.lastError') : t('profiles.delete', { name: profile.name })}
                  onClick={() => {
                    setDeletingId(profile.id);
                    setDeleteFailed(false);
                  }}
                  className="hover:text-destructive"
                >
                  <Trash2 aria-hidden="true" className="size-4" />
                </Button>
              </div>
            );
          })}
        </div>
        {deleteFailed && <ErrorSnackbar message={t('profiles.deleteError')} action={t('common.dismiss')} onAction={() => setDeleteFailed(false)} />}
      </section>
      {dialog && <ProfileDialog profile={dialog.profile} onClose={() => setDialog(undefined)} />}
      {moving && <MoveSubscriptionsDialog source={moving} onClose={() => setMoving(undefined)} />}
    </div>
  );
}
