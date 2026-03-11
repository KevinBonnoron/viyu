import { pastedLink } from '../src/lib/pasted-link';
import { expect, test } from 'bun:test';

const id = 'I_efxjQV4xc';

test('standard, shared, mobile, music, shorts, live and embed URLs are YouTube videos', () => {
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
    expect(pastedLink(link)).toEqual({ platform: 'youtube', kind: 'video' });
  }
});

test('handle and channel-id pages, on a tab or not, are YouTube channels', () => {
  for (const link of [
    'https://www.youtube.com/@ZeratoR_Replay',
    'youtube.com/@ZeratoR_Replay/videos',
    'https://www.youtube.com/@Z%C3%A9rator.fr',
    'https://www.youtube.com/channel/UCuAXFkgsw1L7xaCfnd5JJOw',
    'https://m.youtube.com/channel/UCuAXFkgsw1L7xaCfnd5JJOw/streams',
  ]) {
    expect(pastedLink(link)).toEqual({ platform: 'youtube', kind: 'channel' });
  }
});

test('Twitch VODs and channel pages are told apart from the site pages', () => {
  expect(pastedLink('https://www.twitch.tv/videos/2919341981')).toEqual({ platform: 'twitch', kind: 'video' });
  for (const link of ['https://www.twitch.tv/zerator', 'twitch.tv/ZeratoR/videos', 'https://m.twitch.tv/zerator/about']) {
    expect(pastedLink(link)).toEqual({ platform: 'twitch', kind: 'channel' });
  }
  for (const link of [
    'https://www.twitch.tv/directory',
    'https://www.twitch.tv/settings',
    'https://www.twitch.tv/zerator/clip/abc',
    'https://www.twitch.tv/videos/0123',
  ]) {
    expect(pastedLink(link)).toBeNull();
  }
});

test('search terms, malformed IDs, other pages and lookalike domains are searched', () => {
  for (const value of [
    'defakator',
    '@ZeratoR_Replay',
    'https://youtube.com/playlist?list=PL123',
    'https://youtube.com/watch?v=short',
    'https://www.youtube.com/channel/UCshort',
    'https://www.youtube.com/c/ZeratoR',
    'https://www.youtube.com/@name/videos/extra',
    `https://youtube.com.evil.test/watch?v=${id}`,
    `https://youtube.com@evil.test/watch?v=${id}`,
    'https://twitch.tv.evil.test/zerator',
    'javascript:alert(1)',
    `https://example.test/${id}`,
  ]) {
    expect(pastedLink(value)).toBeNull();
  }
});
