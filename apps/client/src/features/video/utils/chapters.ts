import type { VideoChapter } from '@/types';

export function timelineChapters(chapters: VideoChapter[] | undefined, duration: number): VideoChapter[] {
  if (!Array.isArray(chapters) || !Number.isFinite(duration) || duration <= 0) {
    return [];
  }
  return chapters
    .filter(
      (chapter) =>
        chapter &&
        typeof chapter.title === 'string' &&
        chapter.title.trim() !== '' &&
        Number.isFinite(chapter.start_time) &&
        Number.isFinite(chapter.end_time) &&
        chapter.start_time >= 0 &&
        chapter.start_time < duration &&
        chapter.end_time > chapter.start_time,
    )
    .slice()
    .sort((left, right) => left.start_time - right.start_time);
}

export function currentChapter(chapters: VideoChapter[], time: number): VideoChapter | undefined {
  for (let index = chapters.length - 1; index >= 0; index--) {
    const chapter = chapters[index];
    if (time >= chapter.start_time && time < chapter.end_time) {
      return chapter;
    }
  }
  return undefined;
}
