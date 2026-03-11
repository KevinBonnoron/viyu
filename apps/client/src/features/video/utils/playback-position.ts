/** Prefix of every locally remembered resume position; the whole family is dropped when the session or a profile's history ends. */
export const PLAYBACK_POSITION_PREFIX = 'viyu.playback:';

export function playbackPositionKey(userId: string, profileId: string, videoId: string): string {
  return `${PLAYBACK_POSITION_PREFIX}${userId}:${profileId}:${videoId}`;
}

function removePositions(prefix: string): void {
  try {
    for (const key of Object.keys(localStorage)) {
      if (key.startsWith(prefix)) {
        localStorage.removeItem(key);
      }
    }
  } catch {
    // Restricted local storage must not turn a successful server operation into a failure.
  }
}

/** Forgets every resume position kept on this device (logout, server change, expired session). */
export function clearPlaybackPositions(): void {
  removePositions(PLAYBACK_POSITION_PREFIX);
}

/** Forgets the resume positions of one profile (its history was cleared). */
export function clearProfilePlaybackPositions(userId: string, profileId: string): void {
  removePositions(`${PLAYBACK_POSITION_PREFIX}${userId}:${profileId}:`);
}
