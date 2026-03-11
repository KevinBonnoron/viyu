import { Check, Copy, EyeOff, LoaderCircle, MoreVertical, ThumbsDown, Trash2 } from 'lucide-react';
import { type KeyboardEvent, useEffect, useId, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { api } from '@/lib/api';
import { markNotInterested } from '@/lib/reactions';
import { markVideoWatched, removeVideoHistory } from '@/lib/watch-history';
import { useAuth } from '@/providers/auth-provider';
import { type TranslationKey, useI18n } from '@/providers/i18n-provider';
import { useProfiles } from '@/providers/profiles-provider';
import { useUndo } from '@/providers/undo-provider';

const MENU_WIDTH = 240;
const MENU_GAP = 4;

type MenuProps = {
  /** The `videos` record the menu acts on, and its title for the accessible labels. */
  video: { video: string; title: string };
  watched?: boolean;
  canMarkWatched?: boolean;
  canDismiss?: boolean;
  disabled?: boolean;
  onRemove?: () => void;
};

export function VideoCardMenu(props: MenuProps) {
  const { user } = useAuth();
  const { activeProfile } = useProfiles();
  return (
    <ProfileVideoCardMenu
      key={`${user?.id}:${activeProfile?.id}`}
      {...props}
      userId={user?.id ?? ''}
      profileId={activeProfile?.user === user?.id ? (activeProfile?.id ?? '') : ''}
    />
  );
}

function ProfileVideoCardMenu({
  video,
  watched = false,
  canMarkWatched = false,
  canDismiss = false,
  disabled = false,
  onRemove,
  userId,
  profileId,
}: MenuProps & { userId: string; profileId: string }) {
  const { t } = useI18n();
  const offerUndo = useUndo();
  const menuId = useId();
  const root = useRef<HTMLDivElement>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  const menu = useRef<HTMLDivElement>(null);
  const mounted = useRef(false);
  const [open, setOpen] = useState(false);
  const [position, setPosition] = useState<{ top: number; left: number }>();
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<TranslationKey | false>(false);
  const [copied, setCopied] = useState(false);
  const unavailable = disabled || !userId || !profileId;

  // Virtualized rows are transformed, so each row is its own stacking context: an in-place menu
  // would paint under the following rows. The menu therefore renders in a portal, anchored to the
  // trigger in document coordinates so it scrolls with the page.
  const toggle = () => {
    if (open) {
      close();
      return;
    }
    setError(false);
    setCopied(false);
    const rect = trigger.current?.getBoundingClientRect();
    if (rect) {
      setPosition({ top: rect.bottom + window.scrollY + MENU_GAP, left: Math.max(8, rect.right + window.scrollX - MENU_WIDTH) });
    }
    setOpen(true);
  };

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  useEffect(() => {
    if (!open) {
      return;
    }
    menu.current?.querySelector<HTMLButtonElement>('button:not(:disabled)')?.focus({ preventScroll: true });
    const outside = (event: PointerEvent) => {
      const target = event.target as Node;
      // The menu lives in a portal: "inside" covers both the trigger and the menu itself.
      if (!root.current?.contains(target) && !menu.current?.contains(target)) {
        setOpen(false);
      }
    };
    document.addEventListener('pointerdown', outside);
    return () => document.removeEventListener('pointerdown', outside);
  }, [open]);

  const close = () => {
    setOpen(false);
    trigger.current?.focus({ preventScroll: true });
  };
  const keyboard = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key === 'Escape') {
      event.preventDefault();
      event.stopPropagation();
      close();
      return;
    }
    const items = [...(menu.current?.querySelectorAll<HTMLButtonElement>('[role="menuitem"]:not(:disabled)') ?? [])];
    if (!items.length) {
      return;
    }
    const current = items.indexOf(document.activeElement as HTMLButtonElement);
    const next =
      event.key === 'Home'
        ? 0
        : event.key === 'End'
          ? items.length - 1
          : event.key === 'ArrowDown'
            ? (current + 1) % items.length
            : event.key === 'ArrowUp'
              ? (current - 1 + items.length) % items.length
              : -1;
    if (next >= 0) {
      event.preventDefault();
      items[next]?.focus();
    }
  };
  const toggleWatched = async () => {
    if (pending || unavailable) {
      return;
    }
    setPending(true);
    setError(false);
    try {
      const undo = watched ? await removeVideoHistory(userId, profileId, video.video) : await markVideoWatched(profileId, video.video);
      offerUndo(t(watched ? 'video.markedUnwatched' : 'video.markedWatched', { title: video.title }), undo);
      if (mounted.current) {
        close();
      }
    } catch {
      if (mounted.current) {
        setError('video.historyActionError');
        setOpen(true);
      }
    } finally {
      if (mounted.current) {
        setPending(false);
      }
    }
  };
  const dismiss = async () => {
    if (pending || unavailable) {
      return;
    }
    setPending(true);
    setError(false);
    try {
      const undo = await markNotInterested(profileId, video.video);
      offerUndo(t('video.markedNotInterested', { title: video.title }), undo);
      if (mounted.current) {
        close();
      }
    } catch {
      if (mounted.current) {
        setError('video.historyActionError');
        setOpen(true);
      }
    } finally {
      if (mounted.current) {
        setPending(false);
      }
    }
  };
  const copyOriginalLink = async () => {
    if (pending) {
      return;
    }
    setPending(true);
    setError(false);
    setCopied(false);
    try {
      const { url } = await api.videos.url(video.video);
      await navigator.clipboard.writeText(url);
      if (mounted.current) {
        setCopied(true);
      }
    } catch {
      if (mounted.current) {
        setError('video.originalLinkError');
      }
    } finally {
      if (mounted.current) {
        setPending(false);
      }
    }
  };

  return (
    <div ref={root} className="relative">
      <button
        ref={trigger}
        type="button"
        aria-label={t('video.actions', { title: video.title })}
        title={t('video.actions', { title: video.title })}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-controls={open ? menuId : undefined}
        disabled={pending || unavailable}
        onClick={toggle}
        className="flex size-8 items-center justify-center rounded-full text-muted-foreground transition-colors hover:bg-muted hover:text-foreground disabled:opacity-50"
      >
        {pending ? <LoaderCircle aria-hidden="true" className="size-4 animate-spin" /> : <MoreVertical aria-hidden="true" className="size-4" />}
      </button>
      {open &&
        position &&
        createPortal(
          <div
            ref={menu}
            id={menuId}
            role="menu"
            onKeyDown={keyboard}
            aria-label={t('video.actions', { title: video.title })}
            aria-busy={pending}
            style={{ position: 'absolute', top: position.top, left: position.left }}
            className="z-50 mt-1 w-60 max-w-[calc(100vw-6rem)] rounded-xl border bg-popover p-1.5 text-popover-foreground shadow-xl"
          >
            {canMarkWatched && (
              <button
                type="button"
                role="menuitem"
                disabled={pending || unavailable}
                onClick={() => void toggleWatched()}
                className="flex w-full items-center gap-2 rounded-lg px-3 py-2 text-left text-xs hover:bg-muted focus:bg-muted disabled:opacity-50"
              >
                {watched ? <EyeOff aria-hidden="true" className="size-4 shrink-0" /> : <Check aria-hidden="true" className="size-4 shrink-0" />}
                {t(watched ? 'video.markUnwatched' : 'video.markWatched')}
              </button>
            )}
            {canDismiss && (
              <button
                type="button"
                role="menuitem"
                disabled={pending || unavailable}
                onClick={() => void dismiss()}
                className="flex w-full items-center gap-2 rounded-lg px-3 py-2 text-left text-xs hover:bg-muted focus:bg-muted disabled:opacity-50"
              >
                <ThumbsDown aria-hidden="true" className="size-4 shrink-0" />
                {t('video.notInterested')}
              </button>
            )}
            <button
              type="button"
              role="menuitem"
              disabled={pending}
              onClick={() => void copyOriginalLink()}
              className="flex w-full items-center gap-2 rounded-lg px-3 py-2 text-left text-xs hover:bg-muted focus:bg-muted disabled:opacity-50"
            >
              {copied ? <Check aria-hidden="true" className="size-4 shrink-0 text-success" /> : <Copy aria-hidden="true" className="size-4 shrink-0" />}
              <span aria-live="polite">{t(copied ? 'video.originalLinkCopied' : 'video.copyOriginalLink')}</span>
            </button>
            {onRemove && (
              <button
                type="button"
                role="menuitem"
                onClick={() => {
                  close();
                  onRemove();
                }}
                className="flex w-full items-center gap-2 rounded-lg px-3 py-2 text-left text-xs text-destructive hover:bg-destructive/10 focus:bg-destructive/10"
              >
                <Trash2 aria-hidden="true" className="size-4 shrink-0" />
                {t('history.remove')}
              </button>
            )}
            {error && (
              <p role="alert" className="px-3 py-2 text-xs text-destructive">
                {t(error)}
              </p>
            )}
          </div>,
          document.body,
        )}
    </div>
  );
}
