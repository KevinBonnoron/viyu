/** The id of the side-panel section holding the conversation about a video: comments, whatever the platform and whether it is a list or a live chat. */
export const COMMENTS_SECTION = 'comments';

/** What the comments section shows, by what the server says the video supports; `undefined` when the video has none (the section is not offered). */
export function commentsMode(video: { comments?: string } | null | undefined): 'list' | 'chat' | undefined {
  return video?.comments === 'list' || video?.comments === 'chat' ? video.comments : undefined;
}

/** The chat of a stream in progress, which the side panel opens by itself. */
export function isLiveChat(video: { comments?: string; live?: boolean } | null | undefined): boolean {
  return commentsMode(video) === 'chat' && !!video?.live;
}

/** Where the choice of the player's side panel is remembered on this device. A stream, a series episode and a plain video each remember their own, so closing the chat of a stream does not hide the episode list of a series. */
export function sidePanelStorageKey(context: string): string {
  return `viyu:side-panel:${context}`;
}

/**
 * The section of the player's side panel that is open, if any. `stored` is the user's last choice on this device:
 * a section id, `''` for "closed", or `null` when they never chose. A stored section this video does not have
 * (the comments of a video that has none) falls back to `preferred`, the section that suits the video (the chat of a stream,
 * the episode list of a series), which is only used when it exists.
 */
export function selectSection(stored: string | null, available: string[], preferred?: string): string | undefined {
  if (stored === '') {
    return undefined;
  }
  if (stored !== null && available.includes(stored)) {
    return stored;
  }
  return preferred !== undefined && available.includes(preferred) ? preferred : undefined;
}
