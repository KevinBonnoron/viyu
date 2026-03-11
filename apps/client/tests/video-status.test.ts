import { buildVideoStatuses } from '../src/lib/video-status';
import { expect, test } from 'bun:test';

const history = (video: string, fields: Partial<{ video_duration: number; watched: boolean; position: number }> = {}) => ({
  video,
  video_duration: 600,
  watched: false,
  position: 0,
  ...fields,
});

test('a video the profile never met has no status', () => {
  expect(buildVideoStatuses([], []).size).toBe(0);
});

test('a watched row is watched and has nothing to resume', () => {
  const status = buildVideoStatuses([history('v1', { watched: true, position: 580 })], []).get('v1');
  expect(status).toEqual({ watched: true, progress: undefined });
});

test('an unfinished video reports the fraction to resume from', () => {
  const status = buildVideoStatuses([history('v1', { position: 150 })], []).get('v1');
  expect(status?.watched).toBe(false);
  expect(status?.progress).toBe(0.25);
});

test('no duration or no position means nothing to resume', () => {
  expect(buildVideoStatuses([history('v1', { video_duration: 0, position: 30 })], []).get('v1')?.progress).toBeUndefined();
  expect(buildVideoStatuses([history('v2', { position: 0 })], []).get('v2')?.progress).toBeUndefined();
});

test('a manual mark is watched without playback', () => {
  expect(buildVideoStatuses([history('v1', { watched: true })], []).get('v1')?.watched).toBe(true);
});

test('a reaction joins the history of the same video, or stands alone', () => {
  const statuses = buildVideoStatuses(
    [history('v1', { position: 150 })],
    [
      { video: 'v1', reaction: 'like' },
      { video: 'v2', reaction: 'not_interested' },
    ],
  );
  expect(statuses.get('v1')).toEqual({ watched: false, progress: 0.25, reaction: 'like' });
  expect(statuses.get('v2')).toEqual({ watched: false, reaction: 'not_interested' });
});
