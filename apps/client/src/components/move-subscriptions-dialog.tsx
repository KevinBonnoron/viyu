import { eq, useLiveQuery } from '@tanstack/react-db';
import { type FormEvent, useId, useMemo, useState } from 'react';
import { subscriptionCollection } from '@/collections/subscription.collection';
import { ChannelAvatar } from '@/components/channel-avatar';
import { PlatformBadge } from '@/components/platform-badge';
import { Button } from '@/components/ui/button';
import { Dialog } from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { api } from '@/lib/api';
import type { GroupableSubscription } from '@/lib/channel-groups';
import { buildMoveRows, filterMoveRows, selectedSubscriptionIds } from '@/lib/subscription-move';
import { useI18n } from '@/providers/i18n-provider';
import { useProfiles } from '@/providers/profiles-provider';
import { useUndo } from '@/providers/undo-provider';
import type { MoveSubscriptionsResult, ProfileRecord } from '@/types';

/** The select value that stands for "a profile to create". */
const NEW_PROFILE = '__new__';

/** The subscriptions collection follows realtime; this only makes sure a move that realtime missed still shows. */
async function refreshSubscriptions() {
  try {
    await subscriptionCollection.utils.refetch();
  } catch {
    // Realtime keeps the collection current; nothing is wrong when no query has started.
  }
}

/**
 * Hands subscriptions of `source` to another profile of the account (an existing one, or one created on the spot).
 * A creator group is one row and moves whole. Once done the dialog tells what happened and, when closed, the app's
 * undo snackbar offers to move them back.
 */
export function MoveSubscriptionsDialog({ source, onClose }: { source: ProfileRecord; onClose: () => void }) {
  const { t, language } = useI18n();
  const { profiles, createProfile } = useProfiles();
  const offerUndo = useUndo();
  const id = useId();
  const others = profiles.filter((profile) => profile.id !== source.id);
  const [target, setTarget] = useState(others.length === 1 ? others[0].id : others.length === 0 ? NEW_PROFILE : '');
  const [newName, setNewName] = useState('');
  const [search, setSearch] = useState('');
  const [selected, setSelected] = useState<ReadonlySet<string>>(new Set());
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<'move' | 'create'>();
  const [done, setDone] = useState<{ result: MoveSubscriptionsResult; targetId: string; targetName: string }>();

  const query = useLiveQuery({
    query: (q) => q.from({ subscriptions: subscriptionCollection }).where(({ subscriptions }) => eq(subscriptions.profile, source.id)),
  });
  const rows = useMemo(
    () =>
      buildMoveRows(
        (query.data ?? []).map((subscription): GroupableSubscription => ({ ...subscription, channelInfo: subscription.expand?.channel })),
        language,
      ),
    [query.data, language],
  );
  const visible = useMemo(() => filterMoveRows(rows, search), [rows, search]);
  const chosen = useMemo(() => selectedSubscriptionIds(rows, selected), [rows, selected]);
  const total = useMemo(() => rows.reduce((sum, row) => sum + row.subscriptionIds.length, 0), [rows]);
  const creating = target === NEW_PROFILE;
  const canSubmit = !pending && chosen.length > 0 && (creating ? newName.trim().length > 0 : target !== '');

  const toggle = (key: string, checked: boolean) =>
    setSelected((current) => {
      const next = new Set(current);
      if (checked) {
        next.add(key);
      } else {
        next.delete(key);
      }
      return next;
    });
  const selectVisible = (checked: boolean) =>
    setSelected((current) => {
      const next = new Set(current);
      for (const row of visible) {
        if (checked) {
          next.add(row.key);
        } else {
          next.delete(row.key);
        }
      }
      return next;
    });

  const close = () => {
    if (pending) {
      return;
    }
    if (done && done.result.moved + done.result.merged > 0) {
      const { result, targetId, targetName } = done;
      offerUndo(t(result.moved === 1 ? 'profiles.move.resultOne' : 'profiles.move.resultOther', { count: result.moved, name: targetName }), async () => {
        // The rows that moved go back as they are; the ones the target already had are followed again by copying them.
        if (result.movedIds.length > 0) {
          await api.subscriptions.move(targetId, source.id, result.movedIds);
        }
        if (result.mergedIds.length > 0) {
          await api.subscriptions.move(targetId, source.id, result.mergedIds, { copy: true });
        }
        await refreshSubscriptions();
      });
    }
    onClose();
  };

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (!canSubmit) {
      return;
    }
    setPending(true);
    setError(undefined);
    let targetId = target;
    let targetName = others.find((profile) => profile.id === target)?.name ?? '';
    try {
      if (creating) {
        // The new profile is not made the active one: the viewer is in Settings, moving things around.
        const created = await createProfile({ name: newName }, { select: false });
        targetId = created.id;
        targetName = created.name;
        setTarget(created.id);
      }
    } catch {
      setError('create');
      setPending(false);
      return;
    }
    try {
      const result = await api.subscriptions.move(source.id, targetId, chosen);
      await refreshSubscriptions();
      setDone({ result, targetId, targetName });
    } catch {
      setError('move');
    } finally {
      setPending(false);
    }
  };

  if (done) {
    const { result, targetName } = done;
    return (
      <Dialog aria-labelledby={`${id}-title`} aria-describedby={`${id}-result`} closeOnBackdrop onClose={close} className="max-w-lg space-y-5">
        <h2 id={`${id}-title`} className="text-lg font-semibold tracking-tight">
          {t('profiles.move.doneTitle')}
        </h2>
        <div id={`${id}-result`} role="status" className="space-y-1 text-sm">
          <p>{t(result.moved === 1 ? 'profiles.move.resultOne' : 'profiles.move.resultOther', { count: result.moved, name: targetName })}</p>
          {result.merged > 0 && <p className="text-muted-foreground">{t('profiles.move.merged', { count: result.merged, name: targetName })}</p>}
        </div>
        <div className="flex justify-end">
          <Button type="button" autoFocus onClick={close}>
            {t('profiles.move.done')}
          </Button>
        </div>
      </Dialog>
    );
  }

  return (
    <Dialog
      aria-labelledby={`${id}-title`}
      aria-describedby={`${id}-description`}
      busy={pending}
      closeOnBackdrop
      onClose={close}
      className="max-w-xl p-0 sm:p-0"
    >
      <form onSubmit={(event) => void submit(event)} className="space-y-4 p-5 sm:p-6">
        <div className="space-y-1">
          <h2 id={`${id}-title`} className="text-lg font-semibold tracking-tight">
            {t('profiles.move.title', { name: source.name })}
          </h2>
          <p id={`${id}-description`} className="text-sm text-muted-foreground">
            {t('profiles.move.description', { name: source.name })}
          </p>
        </div>
        <div className="space-y-2">
          <label htmlFor={`${id}-target`} className="text-sm font-medium">
            {t('profiles.move.target')}
          </label>
          <Select value={target || undefined} disabled={pending} onValueChange={setTarget}>
            <SelectTrigger id={`${id}-target`}>
              <SelectValue placeholder={t('profiles.move.targetPlaceholder')} />
            </SelectTrigger>
            <SelectContent>
              {others.map((profile) => (
                <SelectItem key={profile.id} value={profile.id}>
                  {profile.name}
                </SelectItem>
              ))}
              <SelectItem value={NEW_PROFILE}>{t('profiles.move.newProfile')}</SelectItem>
            </SelectContent>
          </Select>
          {creating && (
            <Input
              aria-label={t('profiles.move.newName')}
              placeholder={t('profiles.move.newName')}
              value={newName}
              onChange={(event) => setNewName(event.target.value)}
              maxLength={40}
              required
              disabled={pending}
            />
          )}
        </div>
        {!query.isReady && !query.isError && <p className="py-6 text-center text-sm text-muted-foreground">{t('common.loading')}</p>}
        {query.isError && (
          <p role="alert" className="py-6 text-center text-sm text-destructive">
            {t('profiles.move.loadError')}
          </p>
        )}
        {query.isReady && rows.length === 0 && (
          <p role="status" className="rounded-lg border border-dashed px-4 py-8 text-center text-sm text-muted-foreground">
            {t('profiles.move.empty', { name: source.name })}
          </p>
        )}
        {rows.length > 0 && (
          <>
            <Input
              type="search"
              aria-label={t('profiles.move.search')}
              placeholder={t('profiles.move.search')}
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              disabled={pending}
            />
            <div className="flex flex-wrap items-center gap-2">
              <Button type="button" size="sm" variant="outline" disabled={pending || visible.length === 0} onClick={() => selectVisible(true)}>
                {t('profiles.move.selectAll')}
              </Button>
              <Button type="button" size="sm" variant="outline" disabled={pending || visible.length === 0} onClick={() => selectVisible(false)}>
                {t('profiles.move.selectNone')}
              </Button>
              <span role="status" aria-live="polite" className="ml-auto text-sm tabular-nums text-muted-foreground">
                {t('profiles.move.count', { selected: chosen.length, total })}
              </span>
            </div>
            {visible.length > 0 ? (
              <ul aria-label={t('profiles.move.list', { name: source.name })} className="max-h-80 space-y-1 overflow-y-auto">
                {visible.map((row) => (
                  <li key={row.key}>
                    <label className="flex cursor-pointer items-center gap-3 rounded-lg px-2 py-2 text-sm transition-colors hover:bg-muted has-[:focus-visible]:outline-2 has-[:focus-visible]:outline-ring">
                      <input
                        type="checkbox"
                        checked={selected.has(row.key)}
                        disabled={pending}
                        onChange={(event) => toggle(row.key, event.target.checked)}
                        className="size-4 shrink-0 accent-primary"
                      />
                      <ChannelAvatar src={row.thumbnail} className="size-10" />
                      <span className="min-w-0 flex-1">
                        <span className="block truncate font-medium">{row.name}</span>
                        {row.platforms.length > 1 && (
                          <span className="block truncate text-xs text-muted-foreground">
                            {t('profiles.move.groupMembers', { count: row.platforms.length })}
                          </span>
                        )}
                      </span>
                      <span className="flex shrink-0 items-center gap-1">
                        {row.platforms.map((platform) => (
                          <PlatformBadge key={platform} platform={platform} />
                        ))}
                      </span>
                    </label>
                  </li>
                ))}
              </ul>
            ) : (
              <p role="status" className="py-6 text-center text-sm text-muted-foreground">
                {t('profiles.move.noMatch')}
              </p>
            )}
          </>
        )}
        {error && (
          <p role="alert" className="text-sm text-destructive">
            {t(error === 'create' ? 'profiles.createError' : 'profiles.move.error')}
          </p>
        )}
        <div className="flex items-center justify-end gap-2">
          <Button type="button" variant="outline" disabled={pending} onClick={close}>
            {t('common.cancel')}
          </Button>
          <Button type="submit" disabled={!canSubmit}>
            {pending ? t('profiles.move.pending') : t('profiles.move.confirm', { count: chosen.length })}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
