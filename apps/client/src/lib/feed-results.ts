import type { VideoResult } from '@/types';

/** API ranking wins the position; a connected copy wins the card's session attribution. Remote-only
 * videos follow in source/page order. A platform's repeated pages never duplicate a card. */
export function mergeFeedVideos(ranked: readonly VideoResult[], feeds: readonly { videos: readonly VideoResult[] }[]): VideoResult[] {
  const connected = new Map<string, VideoResult>();
  for (const feed of feeds) {
    for (const video of feed.videos) {
      const key = `${video.platform}:${video.id}`;
      if (!connected.has(key)) {
        connected.set(key, video);
      }
    }
  }
  const seen = new Set<string>();
  const merged: VideoResult[] = [];
  for (const video of ranked) {
    const key = `${video.platform}:${video.id}`;
    if (!seen.has(key)) {
      seen.add(key);
      merged.push(connected.get(key) ?? video);
    }
  }
  for (const [key, video] of connected) {
    if (!seen.has(key)) {
      merged.push(video);
    }
  }
  return merged;
}
