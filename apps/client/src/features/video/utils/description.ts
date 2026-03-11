import { safeHref, trimUrl, URL_SOURCE } from '../../../lib/text-links';

export type DescriptionPart =
  | { kind: 'text'; text: string; offset: number }
  | { kind: 'url'; text: string; offset: number; href: string }
  | { kind: 'time'; text: string; offset: number; seconds: number };

export function parseVideoDescription(description: string, duration: number): DescriptionPart[] {
  const parts: DescriptionPart[] = [];
  const pattern = new RegExp(String.raw`${URL_SOURCE}|(?<![\p{L}\p{N}:])(?:\d{1,3}:)?\d{1,2}:\d{2}(?![\p{L}\p{N}:])`, 'giu');
  let offset = 0;
  for (const match of description.matchAll(pattern)) {
    const start = match.index;
    const raw = match[0];
    if (start > offset) {
      parts.push({ kind: 'text', text: description.slice(offset, start), offset });
    }
    if (/^https?:/iu.test(raw)) {
      const text = trimUrl(raw);
      const href = safeHref(text);
      if (href) {
        parts.push({ kind: 'url', text, href, offset: start });
        if (text.length < raw.length) {
          parts.push({ kind: 'text', text: raw.slice(text.length), offset: start + text.length });
        }
      } else {
        parts.push({ kind: 'text', text: raw, offset: start });
      }
    } else {
      const values = raw.split(':').map(Number);
      const seconds = values.reduce((total, value) => total * 60 + value, 0);
      if (values[values.length - 1] < 60 && (values.length === 2 || values[1] < 60) && (duration <= 0 || seconds <= duration)) {
        parts.push({ kind: 'time', text: raw, seconds, offset: start });
      } else {
        parts.push({ kind: 'text', text: raw, offset: start });
      }
    }
    offset = start + raw.length;
  }
  if (offset < description.length) {
    parts.push({ kind: 'text', text: description.slice(offset), offset });
  }
  return parts;
}
