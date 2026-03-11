import { Camera, Trash2 } from 'lucide-react';
import { useRef, useState } from 'react';
import { AccountAvatar } from '@/components/account-avatar';
import { pb } from '@/lib/pocketbase';
import { useAuth } from '@/providers/auth-provider';
import { type TranslationKey, useI18n } from '@/providers/i18n-provider';

const avatarTypes = ['image/jpeg', 'image/png', 'image/webp', 'image/gif'];
const avatarLimit = 5 * 1024 * 1024;

export function SettingsAvatar({ busy, onBusyChange }: { busy: boolean; onBusyChange: (busy: boolean) => void }) {
  const { user } = useAuth();
  const { t } = useI18n();
  const [error, setError] = useState<TranslationKey | null>(null);
  const [saved, setSaved] = useState(false);
  const inFlight = useRef(false);
  const inputRef = useRef<HTMLInputElement>(null);
  const update = async (file: File | null) => {
    if (!user || busy || inFlight.current) {
      return;
    }
    setError(null);
    setSaved(false);
    if (file && (!avatarTypes.includes(file.type) || file.size === 0 || file.size > avatarLimit)) {
      setError('settings.avatar.invalidFile');
      return;
    }
    inFlight.current = true;
    onBusyChange(true);
    try {
      const data = new FormData();
      data.set('avatar', file ?? '');
      const record = await pb.collection('users').update(user.id, data);
      pb.authStore.save(pb.authStore.token, record);
      setSaved(true);
      window.setTimeout(() => setSaved(false), 2500);
    } catch {
      setError('settings.avatar.saveError');
    } finally {
      inFlight.current = false;
      onBusyChange(false);
    }
  };
  if (!user) {
    return null;
  }
  return (
    <section aria-label={t('settings.avatar.change')} className="flex shrink-0 flex-col items-center gap-2 sm:w-40">
      <div className="relative inline-flex">
        <button
          type="button"
          aria-label={t('settings.avatar.change')}
          aria-describedby="avatar-help"
          title={t('settings.avatar.change')}
          disabled={busy}
          onClick={() => inputRef.current?.click()}
          className="group relative rounded-full outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background disabled:opacity-50"
        >
          <AccountAvatar user={user} className="size-24 text-3xl" />
          {!user.avatar && (
            <span
              aria-hidden="true"
              className="absolute bottom-0 right-0 flex size-8 items-center justify-center rounded-full border border-border bg-card text-foreground shadow-sm group-hover:bg-muted"
            >
              <Camera className="size-4" />
            </span>
          )}
        </button>
        {user.avatar && (
          <button
            type="button"
            aria-label={t('settings.avatar.remove')}
            title={t('settings.avatar.remove')}
            disabled={busy}
            onClick={() => void update(null)}
            className="absolute bottom-0 right-0 flex size-8 items-center justify-center rounded-full border border-border bg-card text-foreground shadow-sm hover:bg-muted focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring disabled:opacity-50"
          >
            <Trash2 aria-hidden="true" className="size-4" />
          </button>
        )}
      </div>
      <input
        ref={inputRef}
        id="account-avatar-file"
        type="file"
        accept={avatarTypes.join(',')}
        disabled={busy}
        aria-label={t('settings.avatar.change')}
        aria-describedby="avatar-help"
        className="sr-only"
        tabIndex={-1}
        onChange={(event) => {
          const file = event.target.files?.[0];
          event.target.value = '';
          if (file) {
            void update(file);
          }
        }}
      />
      <p id="avatar-help" className="text-center text-xs leading-relaxed text-muted-foreground">
        {t('settings.avatar.help')}
      </p>
      <div aria-live="polite" className="text-xs">
        {error && (
          <p role="alert" className="text-destructive">
            {t(error)}
          </p>
        )}
        {saved && <output className="text-primary">{t('settings.avatar.saved')}</output>}
      </div>
    </section>
  );
}
