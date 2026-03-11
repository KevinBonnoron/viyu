import { Check } from 'lucide-react';
import { type FormEvent, useId, useState } from 'react';
import { ProfileAvatar } from '@/components/profile-avatar';
import { Button } from '@/components/ui/button';
import { Dialog } from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { isProfileIcon, PROFILE_ICON_KEYS, PROFILE_ICONS, profileTheme } from '@/lib/profile-appearance';
import { cn } from '@/lib/utils';
import { useI18n } from '@/providers/i18n-provider';
import { useProfiles } from '@/providers/profiles-provider';
import { ACCENTS, DEFAULT_ACCENT } from '@/providers/theme-provider';
import type { ProfileRecord } from '@/types';

/** Creates a profile, or edits one when given: name, icon shown in the application bar, and colour palette. */
export function ProfileDialog({ profile, onClose }: { profile?: ProfileRecord; onClose: () => void }) {
  const { t } = useI18n();
  const { createProfile, updateProfile } = useProfiles();
  const id = useId();
  const [name, setName] = useState(profile?.name ?? '');
  const [icon, setIcon] = useState(isProfileIcon(profile?.icon) ? profile.icon : '');
  const [theme, setTheme] = useState(profileTheme(profile?.theme) ?? '');
  const [pending, setPending] = useState(false);
  const [error, setError] = useState(false);

  const close = () => {
    if (!pending) {
      onClose();
    }
  };

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (!name.trim() || pending) {
      return;
    }
    setPending(true);
    setError(false);
    try {
      if (profile) {
        await updateProfile(profile.id, { name, icon, theme });
      } else {
        await createProfile({ name, icon, theme });
      }
      onClose();
    } catch {
      setError(true);
      setPending(false);
    }
  };

  return (
    <Dialog aria-labelledby={`${id}-title`} busy={pending} closeOnBackdrop onClose={close} className="max-w-lg p-0 text-card-foreground sm:p-0">
      {/* The chosen palette scopes the form, so the preview, selection marks and Save button show it. */}
      <form onSubmit={(event) => void submit(event)} data-accent={theme || DEFAULT_ACCENT} className="space-y-6 p-6">
        <div className="flex items-center gap-4">
          <ProfileAvatar profile={{ icon, theme }} className="size-14" />
          <h2 id={`${id}-title`} className="text-lg font-semibold tracking-tight">
            {t(profile ? 'profiles.editTitle' : 'profiles.newTitle')}
          </h2>
        </div>
        <div className="space-y-2">
          <label htmlFor={`${id}-name`} className="text-sm font-medium">
            {t('profiles.name')}
          </label>
          <Input id={`${id}-name`} value={name} onChange={(event) => setName(event.target.value)} maxLength={40} required autoFocus disabled={pending} />
        </div>
        <fieldset className="space-y-2">
          <legend className="mb-2 text-sm font-medium">{t('profiles.icon')}</legend>
          <div className="grid grid-cols-5 gap-2 sm:grid-cols-10">
            {PROFILE_ICON_KEYS.map((key) => {
              const Icon = PROFILE_ICONS[key];
              const selected = icon === key;
              return (
                <button
                  key={key}
                  type="button"
                  aria-pressed={selected}
                  aria-label={t(`profiles.icons.${key}`)}
                  title={t(`profiles.icons.${key}`)}
                  disabled={pending}
                  onClick={() => setIcon(selected ? '' : key)}
                  className={cn(
                    'grid size-10 place-items-center rounded-xl border text-muted-foreground transition-colors hover:bg-muted',
                    selected && 'border-primary bg-primary/15 text-primary',
                  )}
                >
                  <Icon aria-hidden="true" className="size-5" />
                </button>
              );
            })}
          </div>
        </fieldset>
        <fieldset className="space-y-2">
          <legend className="text-sm font-medium">{t('profiles.theme')}</legend>
          <p className="mb-2 text-xs text-muted-foreground">{t('profiles.themeDescription')}</p>
          <div className="flex flex-wrap items-center gap-3">
            <Button
              type="button"
              variant="outline"
              size="sm"
              aria-pressed={theme === ''}
              disabled={pending}
              onClick={() => setTheme('')}
              className="rounded-full aria-pressed:border-primary aria-pressed:bg-primary/15"
            >
              {t('profiles.themeDefault')}
            </Button>
            {ACCENTS.map((value) => (
              <button
                key={value}
                type="button"
                data-accent={value}
                aria-label={t(`settings.appearance.accents.${value}`)}
                title={t(`settings.appearance.accents.${value}`)}
                aria-pressed={theme === value}
                disabled={pending}
                onClick={() => setTheme(value)}
                className={cn(
                  'grid size-9 place-items-center rounded-full bg-primary text-primary-foreground transition-transform hover:scale-105',
                  theme === value && 'ring-2 ring-foreground ring-offset-2 ring-offset-card',
                )}
              >
                {theme === value && <Check aria-hidden="true" className="size-4" />}
              </button>
            ))}
          </div>
        </fieldset>
        {error && (
          <p role="alert" className="text-sm text-destructive">
            {t(profile ? 'profiles.updateError' : 'profiles.createError')}
          </p>
        )}
        <div className="flex justify-end gap-2">
          <Button type="button" variant="outline" disabled={pending} onClick={close}>
            {t('common.cancel')}
          </Button>
          <Button type="submit" disabled={pending || !name.trim()}>
            {t(pending ? 'common.saving' : profile ? 'common.save' : 'profiles.create')}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
