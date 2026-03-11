import { Link } from '@tanstack/react-router';
import { Check, Settings2 } from 'lucide-react';
import { ProfileAvatar } from '@/components/profile-avatar';
import { useI18n } from '@/providers/i18n-provider';
import { useProfiles } from '@/providers/profiles-provider';

export function ProfileSwitcher({ onSelect }: { onSelect: () => void }) {
  const { t } = useI18n();
  const { profiles, activeProfile, isReady, isError, selectProfile, retry } = useProfiles();

  return (
    <section aria-label={t('profiles.label')} className="p-1.5">
      <p className="px-2 pb-1 pt-1.5 text-xs font-medium text-muted-foreground">{t('profiles.label')}</p>
      {!isReady && !isError && <p className="px-2 py-2 text-xs text-muted-foreground">{t('common.loading')}</p>}
      {isError && (
        <button type="button" className="w-full rounded-lg px-2 py-2 text-left text-xs text-destructive hover:bg-muted" onClick={retry}>
          {t('profiles.loadError')} {t('common.retry')}
        </button>
      )}
      <div className="max-h-60 overflow-y-auto">
        {profiles.map((profile) => {
          const selected = activeProfile?.id === profile.id;
          return (
            <button
              key={profile.id}
              type="button"
              aria-pressed={selected}
              onClick={() => {
                selectProfile(profile.id);
                onSelect();
              }}
              className="flex w-full items-center gap-2.5 rounded-lg px-2 py-2 text-left text-sm transition-colors hover:bg-muted"
            >
              <ProfileAvatar profile={profile} className="size-7" />
              <span className="min-w-0 flex-1 truncate">{profile.name}</span>
              {selected && <Check aria-hidden="true" className="size-4 shrink-0 text-primary" />}
            </button>
          );
        })}
      </div>
      <Link
        to="/settings"
        search={{ tab: 'profiles' }}
        onClick={onSelect}
        className="mt-1 flex items-center gap-2.5 rounded-lg border-t px-2 py-2.5 text-sm transition-colors hover:bg-muted"
      >
        <Settings2 aria-hidden="true" className="size-4 text-muted-foreground" />
        {t('profiles.manage')}
      </Link>
    </section>
  );
}
