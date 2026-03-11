import { currentChapter, timelineChapters } from '../src/features/video/utils/chapters';
import type { VideoChapter } from '../src/types/platform.type';
import { expect, test } from 'bun:test';

const chapters: VideoChapter[] = [
  { title: 'Second', start_time: 30.5, end_time: 50 },
  { title: 'Introduction', start_time: 5, end_time: 20 },
];

test('timeline uses native chapter boundaries and titles without filling gaps', () => {
  const timeline = timelineChapters(chapters, 60);
  expect(timeline.map(({ title, start_time }) => [title, start_time])).toEqual([
    ['Introduction', 5],
    ['Second', 30.5],
  ]);
  expect(currentChapter(timeline, 4.9)).toBeUndefined();
  expect(currentChapter(timeline, 5)?.title).toBe('Introduction');
  expect(currentChapter(timeline, 20)).toBeUndefined();
  expect(currentChapter(timeline, 30.5)?.title).toBe('Second');
  expect(currentChapter(timeline, 50)).toBeUndefined();
});

test('missing, invalid and out-of-video chapters cannot create timeline entries', () => {
  expect(timelineChapters(undefined, 60)).toEqual([]);
  expect(timelineChapters(chapters, 0)).toEqual([]);
  expect(
    timelineChapters(
      [
        { title: '', start_time: 0, end_time: 5 },
        { title: 'Invalid', start_time: -1, end_time: 5 },
        { title: 'Invalid', start_time: 5, end_time: 5 },
        { title: 'Outside', start_time: 60, end_time: 70 },
        { title: 'Invalid', start_time: Number.NaN, end_time: 5 },
      ],
      60,
    ),
  ).toEqual([]);
});

test('replacing video metadata replaces seek targets and current title', () => {
  const first = timelineChapters(chapters, 60);
  const next = timelineChapters([{ title: 'New video', start_time: 2, end_time: 8 }], 10);
  expect(currentChapter(first, 5)?.title).toBe('Introduction');
  expect(currentChapter(next, 5)?.title).toBe('New video');
  expect(next.map((chapter) => chapter.start_time)).toEqual([2]);
  expect(timelineChapters(undefined, 10)).toEqual([]);
});
