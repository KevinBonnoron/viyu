import { youtubeVideoIdFromUrl } from '../src/features/video/utils/youtube-url';
import { expect, test } from 'bun:test';

const id = 'I_efxjQV4xc';
test('standard, shared, mobile, music, shorts, live and embed URLs identify the same video', () => {
  for (const link of [
    `https://www.youtube.com/watch?v=${id}&list=PLabc`,
    `https://youtu.be/${id}?si=share`,
    `youtube.com/watch?v=${id}`,
    `https://m.youtube.com/watch?v=${id}`,
    `https://music.youtube.com/watch?v=${id}`,
    `https://youtube.com/shorts/${id}`,
    `https://youtube.com/live/${id}`,
    `https://www.youtube-nocookie.com/embed/${id}`,
  ]) {
    expect(youtubeVideoIdFromUrl(link)).toBe(id);
  }
});

test('search terms, malformed IDs, non-video links and lookalike domains are not treated as videos', () => {
  for (const value of [
    'defakator',
    'https://youtube.com/playlist?list=PL123',
    'https://youtube.com/watch?v=short',
    `https://youtube.com.evil.test/watch?v=${id}`,
    `https://youtube.com@evil.test/watch?v=${id}`,
    'javascript:alert(1)',
    `https://example.test/${id}`,
  ]) {
    expect(youtubeVideoIdFromUrl(value)).toBeNull();
  }
});
