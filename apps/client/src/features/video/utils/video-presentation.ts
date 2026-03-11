import type { VideoResult } from '@/types';

type Artwork = { thumbnail?: string; thumbnails?: string[] } | undefined;

/** Every usable thumbnail URL, in priority order: the listing's, then the enriched metadata's. */
export function thumbnailCandidates(...sources: Artwork[]): string[] {
  const urls: string[] = [];
  for (const source of sources) {
    for (const value of [source?.thumbnail, ...(source?.thumbnails ?? [])]) {
      const url = value?.trim();
      if (url && !urls.includes(url)) {
        urls.push(url);
      }
    }
  }
  return urls;
}

/**
 * The title to show. With the original-title preference off the listing title is final.
 * Otherwise the original is only used once proven: by the listing itself or by enriched metadata.
 */
export function displayTitle(
  video: Pick<VideoResult, 'title' | 'originalTitle' | 'originalTitleVerified'>,
  preferOriginal: boolean,
  enrichedTitle?: string,
): string {
  if (!preferOriginal) {
    return video.title;
  }
  return video.originalTitleVerified ? video.originalTitle || video.title : enrichedTitle || video.title;
}

/** Enrichment is only worth a request for an unproven original title (when preferred) or missing artwork. */
export function needsEnrichment(video: Pick<VideoResult, 'originalTitleVerified' | 'thumbnail' | 'thumbnails'>, preferOriginal: boolean): boolean {
  return (preferOriginal && !video.originalTitleVerified) || thumbnailCandidates(video).length === 0;
}
