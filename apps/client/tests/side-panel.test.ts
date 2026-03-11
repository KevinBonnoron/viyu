import { COMMENTS_SECTION, commentsMode, isLiveChat, selectSection } from '../src/lib/side-panel';
import { expect, test } from 'bun:test';

test('nothing is open until the video suggests a section or the user picks one', () => {
  expect(selectSection(null, ['description', 'comments'])).toBeUndefined();
  expect(selectSection(null, ['series', 'description'], 'series')).toBe('series');
  expect(selectSection(null, ['description'], 'comments')).toBeUndefined();
});

test('the user’s last choice wins when this video has that section', () => {
  expect(selectSection('comments', ['description', 'comments'], 'series')).toBe('comments');
  expect(selectSection('description', ['series', 'description'], 'series')).toBe('description');
});

test('closing the panel stays closed, even on a stream or a series', () => {
  expect(selectSection('', ['comments', 'description'], 'comments')).toBeUndefined();
});

test('a remembered section the video lacks falls back to what suits the video', () => {
  expect(selectSection('series', ['description', 'comments'])).toBeUndefined();
  expect(selectSection('series', ['description'], 'comments')).toBeUndefined();
  expect(selectSection('series', ['comments', 'description'], 'comments')).toBe('comments');
});

test('the section id a stream used to store for its chat is stale and falls back to the comments', () => {
  expect(selectSection('chat', ['description', 'comments'])).toBeUndefined();
  expect(selectSection('chat', ['description', COMMENTS_SECTION], COMMENTS_SECTION)).toBe('comments');
  expect(selectSection('chat', ['series', 'description'], 'series')).toBe('series');
});

test('the comments section depends on what the server says the video supports, not on its platform', () => {
  expect(commentsMode({ comments: 'list' })).toBe('list');
  expect(commentsMode({ comments: 'chat' })).toBe('chat');
  expect(commentsMode({})).toBeUndefined();
  expect(commentsMode({ comments: 'none' })).toBeUndefined();
  expect(commentsMode(null)).toBeUndefined();
  expect(commentsMode(undefined)).toBeUndefined();
});

test('only a chat of a stream in progress opens by itself', () => {
  expect(isLiveChat({ comments: 'chat', live: true })).toBe(true);
  expect(isLiveChat({ comments: 'chat', live: false })).toBe(false);
  expect(isLiveChat({ comments: 'chat' })).toBe(false);
  expect(isLiveChat({ comments: 'list', live: true })).toBe(false);
  expect(isLiveChat({ live: true })).toBe(false);
  expect(isLiveChat(null)).toBe(false);
});
