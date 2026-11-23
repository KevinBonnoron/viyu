import type { ChannelPage } from '@/lib/api';
import type { VideoResult } from '@/types';

export interface CatalogPrefix {
  page: number;
  videos: VideoResult[];
  revision?: string;
}

export function catalogPolling(needsConfirmation: boolean, sawSync: boolean, active: boolean) {
  const confirm = !active && (needsConfirmation || sawSync);
  return { crawling: active || confirm, needsConfirmation: active };
}

export async function loadCatalogPrefix(
  page: number,
  previous: CatalogPrefix | null,
  fetchPage: (page: number) => Promise<ChannelPage>,
): Promise<{ prefix: CatalogPrefix; last: ChannelPage; sawSync: boolean } | null> {
  let sawSync = false;
  let append = previous !== null && previous.page < page ? previous : null;
  for (let attempt = 0; attempt < 2; attempt++) {
    const videos = new Map<string, VideoResult>(append?.videos.map((video) => [`${video.platform}:${video.id}`, video] as const));
    let revision = append?.revision;
    let last: ChannelPage | undefined;
    let changed = false;
    for (let current = append ? append.page + 1 : 1; current <= page; current++) {
      const result = await fetchPage(current);
      sawSync ||= result.sync?.status === 'pending' || result.sync?.status === 'running';
      if (result.revision === '' || (revision !== undefined && result.revision !== revision)) {
        changed = true;
        break;
      }
      revision = result.revision;
      for (const video of result.videos) {
        videos.set(`${video.platform}:${video.id}`, video);
      }
      last = result;
      if (!result.hasMore) {
        break;
      }
    }
    if (!changed && last) {
      return { prefix: { page: last.page, videos: Array.from(videos.values()), revision }, last, sawSync };
    }
    if (!append) {
      return null;
    }
    append = null;
  }
  return null;
}
