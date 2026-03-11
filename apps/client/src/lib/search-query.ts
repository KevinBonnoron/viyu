import { pastedLink } from './pasted-link';

/** Pause after the last keystroke before a typed query is searched; Enter searches immediately. */
export const SEARCH_DEBOUNCE_MS = 400;

/** Whether typing alone may launch this query: long enough, and not a pasted video or channel link (which is opened, not searched). */
export function isTypedSearchQuery(value: string): boolean {
  const query = value.trim();
  return query.length >= 2 && !pastedLink(query);
}
