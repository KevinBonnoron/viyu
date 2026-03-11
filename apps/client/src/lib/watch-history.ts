import { and, eq, queryOnce } from '@tanstack/react-db';
import { watchHistoryCollection } from '@/collections/watch-history.collection';
import { playbackPositionKey } from '@/features/video/utils/playback-position';
import { isValidationFailure, SYSTEM_FIELDS } from '@/lib/collection-writes';
import type { WatchHistoryRecord } from '@/types';

/** The history row of a video for a profile, read from the server through the collection; there is at most one (`(profile, video)` is unique). */
export async function findHistory(profileId: string, videoId: string): Promise<WatchHistoryRecord | undefined> {
  const [row] = await queryOnce((q) =>
    q.from({ history: watchHistoryCollection }).where(({ history }) => and(eq(history.profile, profileId), eq(history.video, videoId))),
  );
  return row;
}

/** Creates a history row; when the unique `(profile, video)` index refuses it (another tab or device wrote first), the existing row is returned for the caller to update instead. */
export async function insertHistory(
  fields: Omit<WatchHistoryRecord, 'id' | 'collectionId' | 'collectionName' | 'created' | 'updated' | 'expand'>,
): Promise<{ created: string } | { existing: WatchHistoryRecord }> {
  const id = watchHistoryCollection.utils.newId();
  try {
    await watchHistoryCollection.insert({ id, ...SYSTEM_FIELDS, ...fields }).isPersisted.promise;
    return { created: id };
  } catch (failure) {
    const existing = isValidationFailure(failure) ? await findHistory(fields.profile, fields.video) : undefined;
    if (!existing) {
      throw failure;
    }
    return { existing };
  }
}

/** Takes an action back; it resolves once the server has the previous state again. */
export type Undo = () => Promise<void>;

/** Marks a video watched by hand. It never invents played seconds and keeps the resume position. It returns how to take it back. */
export async function markVideoWatched(profileId: string, videoId: string): Promise<Undo> {
  const watchedAt = new Date().toISOString();
  const existing = await findHistory(profileId, videoId);
  if (existing) {
    const previous = { watched: existing.watched, watched_at: existing.watched_at };
    await watchHistoryCollection.update(existing.id, (draft) => {
      draft.watched = true;
      draft.watched_at = watchedAt;
    }).isPersisted.promise;
    return () => restoreHistory(existing.id, previous);
  }
  const inserted = await insertHistory({
    profile: profileId,
    video: videoId,
    watched_at: watchedAt,
    duration_watched: 0,
    video_duration: 0,
    position: 0,
    watched: true,
  });
  if ('existing' in inserted) {
    const previous = { watched: inserted.existing.watched, watched_at: inserted.existing.watched_at };
    await watchHistoryCollection.update(inserted.existing.id, (draft) => {
      draft.watched = true;
      draft.watched_at = watchedAt;
    }).isPersisted.promise;
    return () => restoreHistory(inserted.existing.id, previous);
  }
  return async () => {
    await watchHistoryCollection.delete(inserted.created).isPersisted.promise;
  };
}

async function restoreHistory(id: string, previous: Pick<WatchHistoryRecord, 'watched' | 'watched_at'>): Promise<void> {
  await watchHistoryCollection.update(id, (draft) => {
    draft.watched = previous.watched;
    draft.watched_at = previous.watched_at;
  }).isPersisted.promise;
}

/**
 * Forgets a video's history for a profile, along with the resume position this device kept for it. It returns how to
 * take it back: the row is written again with the same values, and the local position restored.
 */
export async function removeVideoHistory(userId: string, profileId: string, videoId: string): Promise<Undo> {
  const existing = await findHistory(profileId, videoId);
  const key = playbackPositionKey(userId, profileId, videoId);
  let position: string | null = null;
  try {
    position = localStorage.getItem(key);
  } catch {
    position = null;
  }
  if (existing) {
    await watchHistoryCollection.delete(existing.id).isPersisted.promise;
  }
  try {
    localStorage.removeItem(key);
  } catch {
    // Restricted local storage must not turn a successful server deletion into a failure.
  }
  return async () => {
    if (existing) {
      const {
        id: _id,
        collectionId: _collectionId,
        collectionName: _collectionName,
        created: _created,
        updated: _updated,
        expand: _expand,
        ...fields
      } = existing;
      await insertHistory(fields);
    }
    if (position !== null) {
      try {
        localStorage.setItem(key, position);
      } catch {
        // The server row is back; the local position is a convenience.
      }
    }
  };
}
