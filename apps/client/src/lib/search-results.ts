import { isChannelResult, type SearchResult } from '@/types';

/** What makes two search results the same one: a video and a channel never collide, nor do two platforms. */
export function resultIdentity(result: SearchResult): string {
  return `${isChannelResult(result) ? 'channel' : 'video'}:${result.platform}:${result.id}`;
}

/** The results of the pages received so far followed by those of a new page, a result already shown (pages overlap) kept where it was first seen. */
export function mergeResults(previous: readonly SearchResult[], incoming: readonly SearchResult[]): SearchResult[] {
  const seen = new Set(previous.map(resultIdentity));
  const merged = [...previous];
  for (const result of incoming) {
    const key = resultIdentity(result);
    if (!seen.has(key)) {
      seen.add(key);
      merged.push(result);
    }
  }
  return merged;
}
