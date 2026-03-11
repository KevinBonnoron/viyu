/** Where an address starts and runs, in free text. The end is refined by `trimUrl`. */
export const URL_SOURCE = String.raw`https?:\/\/[^\s<>"']+`;

/**
 * Takes off what ends a sentence rather than an address: trailing punctuation, and a closing bracket the address never
 * opened (so `(see https://a.example/x)` links `https://a.example/x`, while a Wikipedia address keeps its own bracket).
 */
export function trimUrl(raw: string): string {
  let text = raw;
  const pairs: Record<string, string> = { ')': '(', ']': '[', '}': '{' };
  while (text) {
    const last = text[text.length - 1];
    if (/[.,;:!?]/u.test(last)) {
      text = text.slice(0, -1);
      continue;
    }
    const opening = pairs[last];
    if (opening) {
      let balance = 0;
      for (const character of text) {
        if (character === opening) {
          balance++;
        }
        if (character === last) {
          balance--;
        }
      }
      if (balance < 0) {
        text = text.slice(0, -1);
        continue;
      }
    }
    break;
  }
  return text;
}

/** The address to open for a text that looks like one, or null: only http and https are ever opened, whatever the text says. */
export function safeHref(text: string): string | null {
  try {
    const address = new URL(text);
    return address.protocol === 'http:' || address.protocol === 'https:' ? address.href : null;
  } catch {
    return null;
  }
}

/** A run of text, and the address it opens when it is a link. */
export interface LinkPart {
  text: string;
  offset: number;
  href?: string;
}

/** Splits a text into plain runs and links, in order and without losing a character. */
export function splitLinks(text: string): LinkPart[] {
  const parts: LinkPart[] = [];
  let cursor = 0;
  for (const match of text.matchAll(new RegExp(URL_SOURCE, 'giu'))) {
    const start = match.index;
    const linked = trimUrl(match[0]);
    const href = safeHref(linked);
    if (!href) {
      continue;
    }
    if (start > cursor) {
      parts.push({ text: text.slice(cursor, start), offset: cursor });
    }
    parts.push({ text: linked, offset: start, href });
    cursor = start + linked.length;
  }
  if (cursor < text.length) {
    parts.push({ text: text.slice(cursor), offset: cursor });
  }
  return parts;
}
