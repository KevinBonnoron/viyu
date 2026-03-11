import type { ChatFragment, ChatMessage, ChatReply } from '../types/chat.type';

/** How many messages a chat list keeps: older ones scroll out of the buffer for good. */
export const CHAT_MESSAGE_LIMIT = 200;

/** Adds messages at the end, ignoring ids already present, and keeps only the last `limit`. */
export function appendMessages(current: ChatMessage[], incoming: ChatMessage[], limit = CHAT_MESSAGE_LIMIT): ChatMessage[] {
  const known = new Set(current.map((message) => message.id));
  const fresh = incoming.filter((message) => {
    if (known.has(message.id)) {
      return false;
    }
    known.add(message.id);
    return true;
  });
  if (fresh.length === 0) {
    return current;
  }
  const next = current.concat(fresh);
  return next.length > limit ? next.slice(next.length - limit) : next;
}

function parseFragments(value: unknown): ChatFragment[] | null {
  if (!Array.isArray(value)) {
    return null;
  }
  const fragments: ChatFragment[] = [];
  for (const item of value) {
    if (!item || typeof item !== 'object' || !('text' in item) || typeof item.text !== 'string') {
      return null;
    }
    fragments.push('emote' in item && typeof item.emote === 'string' && item.emote ? { text: item.text, emote: item.emote } : { text: item.text });
  }
  return fragments;
}

/** A chat message received from the server, or null when the data is not one (it is dropped, never shown). */
export function parseChatMessage(data: string): ChatMessage | null {
  let value: unknown;
  try {
    value = JSON.parse(data);
  } catch {
    return null;
  }
  if (
    !value ||
    typeof value !== 'object' ||
    !('id' in value) ||
    typeof value.id !== 'string' ||
    !value.id ||
    !('user' in value) ||
    typeof value.user !== 'string'
  ) {
    return null;
  }
  const fragments = 'fragments' in value ? parseFragments(value.fragments) : null;
  if (!fragments) {
    return null;
  }
  return {
    id: value.id,
    user: value.user,
    fragments,
    ...('color' in value && typeof value.color === 'string' && /^#[0-9a-f]{6}$/i.test(value.color) ? { color: value.color } : {}),
    ...('at' in value && typeof value.at === 'number' && Number.isFinite(value.at) ? { at: value.at } : {}),
    ...('reply' in value ? replyOf(value.reply) : {}),
  };
}

function replyOf(value: unknown): { reply: ChatReply } | Record<string, never> {
  if (
    !value ||
    typeof value !== 'object' ||
    !('id' in value) ||
    typeof value.id !== 'string' ||
    !value.id ||
    !('user' in value) ||
    typeof value.user !== 'string' ||
    !value.user
  ) {
    return {};
  }
  return { reply: { id: value.id, user: value.user, text: 'text' in value && typeof value.text === 'string' ? value.text : '' } };
}

const MENTION = /@[A-Za-z0-9_]{2,25}/g;

/** A run of text, and the name it mentions when it is an `@name`. */
export interface TextPart {
  text: string;
  /** The mentioned user's name, without the `@`. */
  mention?: string;
}

/** Splits a text into plain runs and `@name` mentions, in order and without losing a character. */
export function splitMentions(text: string): TextPart[] {
  const parts: TextPart[] = [];
  let cursor = 0;
  for (const match of text.matchAll(MENTION)) {
    const start = match.index ?? 0;
    // An `@` inside a word (an address, say) is not a mention.
    if (start > 0 && /[A-Za-z0-9_]/.test(text[start - 1] ?? '')) {
      continue;
    }
    if (start > cursor) {
      parts.push({ text: text.slice(cursor, start) });
    }
    parts.push({ text: match[0], mention: match[0].slice(1) });
    cursor = start + match[0].length;
  }
  if (cursor < text.length) {
    parts.push({ text: text.slice(cursor) });
  }
  return parts;
}

/**
 * Twitch starts the text of a reply with the `@name` of the author it answers. The answered message is shown above the
 * reply, so that mention only repeats it: it is dropped (with the space after it) when it names the answered author.
 */
export function withoutReplyMention(fragments: ChatFragment[], reply: ChatReply | undefined): ChatFragment[] {
  const [head, ...rest] = fragments;
  if (!reply || !head || head.emote) {
    return fragments;
  }
  const mention = `@${reply.user}`;
  if (!head.text.toLowerCase().startsWith(mention.toLowerCase())) {
    return fragments;
  }
  const after = head.text.slice(mention.length);
  if (after !== '' && !/^\s/.test(after)) {
    return fragments;
  }
  const text = after.replace(/^\s+/, '');
  return text === '' && rest.length === 0 ? fragments : text === '' ? rest : [{ text }, ...rest];
}

export function emoteUrl(id: string): string {
  return `https://static-cdn.jtvnw.net/emoticons/v2/${encodeURIComponent(id)}/default/dark/1.0`;
}

/** How many of `messages` arrived after the one with id `seenId`; all of them when it is unknown or has scrolled out of the buffer. */
export function countUnread(messages: ChatMessage[], seenId: string | undefined): number {
  if (seenId === undefined) {
    return messages.length;
  }
  for (let index = messages.length - 1; index >= 0; index--) {
    if (messages[index].id === seenId) {
      return messages.length - index - 1;
    }
  }
  return messages.length;
}

/** Relative luminances of the card surface in the light and in the dark theme. */
const LIGHT_SURFACE = 0.99;
const DARK_SURFACE = 0.012;

function luminance(color: string): number {
  const [red, green, blue] = [1, 3, 5].map((start) => {
    const value = Number.parseInt(color.slice(start, start + 2), 16) / 255;
    return value <= 0.03928 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4;
  });
  return 0.2126 * (red ?? 0) + 0.7152 * (green ?? 0) + 0.0722 * (blue ?? 0);
}

/** WCAG contrast ratio of a `#RRGGBB` colour against a surface luminance. */
export function contrastRatio(color: string, surface: number): number {
  const text = luminance(color);
  return (Math.max(text, surface) + 0.05) / (Math.min(text, surface) + 0.05);
}

/** A user's chosen colour when it can be read on the theme's surface, otherwise undefined (use the foreground colour). */
export function readableNameColor(color: string | undefined, dark: boolean, minimum = 3): string | undefined {
  if (!color || !/^#[0-9a-f]{6}$/i.test(color)) {
    return undefined;
  }
  return contrastRatio(color, dark ? DARK_SURFACE : LIGHT_SURFACE) >= minimum ? color : undefined;
}
