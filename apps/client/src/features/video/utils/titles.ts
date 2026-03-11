import type { VideoRecord } from '@/types';

/** The language of the translated title: the one the server asks the platform to serve. */
export const TRANSLATION_LANGUAGE = 'en';

/**
 * The titles of a `videos` record as the API presents them: `title` is the translation when there is
 * one, else the original; `originalTitle` is the original, falling back to `title`.
 */
export function recordTitles(titles: VideoRecord['titles']): { title: string; originalTitle: string } {
  const title = titles?.[TRANSLATION_LANGUAGE] || titles?.original || '';
  return { title, originalTitle: titles?.original || title };
}
