import { and, eq, queryOnce } from '@tanstack/react-db';
import { videoReactionCollection } from '@/collections/video-reaction.collection';
import { isValidationFailure, SYSTEM_FIELDS } from '@/lib/collection-writes';
import type { VideoReaction, VideoReactionRecord } from '@/types';

/** The reaction of a profile to a video, read from the server through the collection; there is at most one (`(profile, video)` is unique). */
async function findReaction(profileId: string, videoId: string): Promise<VideoReactionRecord | undefined> {
  const [row] = await queryOnce((q) =>
    q.from({ reactions: videoReactionCollection }).where(({ reactions }) => and(eq(reactions.profile, profileId), eq(reactions.video, videoId))),
  );
  return row;
}

async function replaceReaction(existing: VideoReactionRecord, reaction: VideoReaction | null): Promise<void> {
  if (reaction === null) {
    await videoReactionCollection.delete(existing.id).isPersisted.promise;
  } else {
    await videoReactionCollection.update(existing.id, (draft) => {
      draft.reaction = reaction;
    }).isPersisted.promise;
  }
}

/**
 * Sets the one reaction a profile has to a video, replacing a previous one; `null` takes it back. Another device may have
 * reacted first (the `(profile, video)` index then refuses the insert): its reaction is replaced instead.
 */
export async function setReaction(profileId: string, videoId: string, reaction: VideoReaction | null): Promise<void> {
  const existing = await findReaction(profileId, videoId);
  if (existing) {
    await replaceReaction(existing, reaction);
    return;
  }
  if (reaction === null) {
    return;
  }
  try {
    await videoReactionCollection.insert({ id: videoReactionCollection.utils.newId(), ...SYSTEM_FIELDS, profile: profileId, video: videoId, reaction })
      .isPersisted.promise;
  } catch (failure) {
    const raced = isValidationFailure(failure) ? await findReaction(profileId, videoId) : undefined;
    if (!raced) {
      throw failure;
    }
    await replaceReaction(raced, reaction);
  }
}

/**
 * Tells a profile's feeds that a video does not interest it: the profile's one reaction to the video, replacing a like or a
 * dislike. It returns how to take it back (the previous reaction, or none).
 */
export async function markNotInterested(profileId: string, videoId: string): Promise<() => Promise<void>> {
  const previous = (await findReaction(profileId, videoId))?.reaction ?? null;
  await setReaction(profileId, videoId, 'not_interested');
  return () => setReaction(profileId, videoId, previous);
}
