import { followSkipNotice, SKIP_NOTICE_LINGER_MS, skipSegment } from '../src/features/video/hooks/use-skip-notice';
import { afterEach, beforeEach, expect, jest, test } from 'bun:test';

beforeEach(() => jest.useFakeTimers());
afterEach(() => jest.useRealTimers());

class Video extends EventTarget {
  public seeking = false;
  public paused = false;
  public readyState = 4;
  public duration = 100;
  /** Every position the player asked for, in order. */
  public seeks: number[] = [];
  private position = 0;

  /** Setting the position is a seek: it starts, and the position is the one asked for. */
  public get currentTime() {
    return this.position;
  }

  public set currentTime(time: number) {
    this.position = time;
    this.seeks.push(time);
    this.seeking = true;
  }

  /** A skip: the seek starts, and the data for the new position may not be there yet. */
  public seek(readyState: number) {
    this.seeking = true;
    this.readyState = readyState;
  }

  public emit(event: string, state: Partial<Pick<Video, 'seeking' | 'readyState' | 'paused'>> = {}) {
    Object.assign(this, state);
    this.dispatchEvent(new Event(event));
  }
}

function follow(video: Video) {
  const changes: boolean[] = [];
  const notice = followSkipNotice(video, (visible) => changes.push(visible));
  return { notice, changes, visible: () => changes.at(-1) === true };
}

test('the notice outlasts a seek that keeps loading, then leaves shortly after playback resumes', () => {
  const video = new Video();
  const { notice, visible } = follow(video);
  video.seek(1);
  notice.show();
  expect(visible()).toBe(true);

  jest.advanceTimersByTime(SKIP_NOTICE_LINGER_MS * 10);
  expect(visible()).toBe(true);
  video.emit('seeked', { seeking: false });
  jest.advanceTimersByTime(SKIP_NOTICE_LINGER_MS * 10);
  expect(visible()).toBe(true);
  video.emit('canplay', { readyState: 3 });
  video.emit('playing');
  jest.advanceTimersByTime(SKIP_NOTICE_LINGER_MS - 1);
  expect(visible()).toBe(true);
  jest.advanceTimersByTime(1);
  expect(visible()).toBe(false);
  notice.dispose();
});

test('events of a seek still under way do not start the countdown', () => {
  const video = new Video();
  const { notice, visible } = follow(video);
  video.seek(1);
  notice.show();
  video.emit('playing');
  video.emit('canplay', { readyState: 4 });
  jest.advanceTimersByTime(SKIP_NOTICE_LINGER_MS * 10);
  expect(visible()).toBe(true);
  notice.dispose();
});

test('a playing video that is short of data after the seek keeps the notice until it can carry on', () => {
  const video = new Video();
  const { notice, visible } = follow(video);
  video.seek(1);
  notice.show();
  video.emit('seeked', { seeking: false, readyState: 2 });
  jest.advanceTimersByTime(SKIP_NOTICE_LINGER_MS * 10);
  expect(visible()).toBe(true);
  video.emit('canplay', { readyState: 4 });
  jest.advanceTimersByTime(SKIP_NOTICE_LINGER_MS);
  expect(visible()).toBe(false);
  notice.dispose();
});

test('a paused video needs only its frame, so a paused seek does not keep the notice forever', () => {
  const video = new Video();
  video.paused = true;
  const { notice, visible } = follow(video);
  video.seek(1);
  notice.show();
  video.emit('seeked', { seeking: false, readyState: 2 });
  expect(visible()).toBe(true);
  jest.advanceTimersByTime(SKIP_NOTICE_LINGER_MS);
  expect(visible()).toBe(false);
  notice.dispose();
});

test('pausing after the seek settles lets the notice leave once the current frame is available', () => {
  const video = new Video();
  const { notice, visible } = follow(video);
  video.seek(1);
  notice.show();
  video.emit('seeked', { seeking: false, readyState: 2 });
  jest.advanceTimersByTime(SKIP_NOTICE_LINGER_MS * 2);
  expect(visible()).toBe(true);

  video.emit('pause', { paused: true });
  jest.advanceTimersByTime(SKIP_NOTICE_LINGER_MS);
  expect(visible()).toBe(false);
  notice.dispose();
});

test('a second skip starts the lifetime again', () => {
  const video = new Video();
  const { notice, visible } = follow(video);
  video.seek(4);
  notice.show();
  video.emit('seeked', { seeking: false });
  jest.advanceTimersByTime(SKIP_NOTICE_LINGER_MS - 1);
  video.seek(1);
  notice.show();
  jest.advanceTimersByTime(SKIP_NOTICE_LINGER_MS * 10);
  expect(visible()).toBe(true);
  video.emit('seeked', { seeking: false, readyState: 4 });
  jest.advanceTimersByTime(SKIP_NOTICE_LINGER_MS - 1);
  expect(visible()).toBe(true);
  jest.advanceTimersByTime(1);
  expect(visible()).toBe(false);
  notice.dispose();
});

test('replacing the media clears the notice, and a disposed follower neither shows nor hides anything', () => {
  const video = new Video();
  const { notice, changes, visible } = follow(video);
  video.seek(1);
  notice.show();
  video.emit('emptied');
  expect(visible()).toBe(false);

  notice.show();
  notice.dispose();
  video.emit('seeked', { seeking: false });
  jest.advanceTimersByTime(SKIP_NOTICE_LINGER_MS * 10);
  expect(changes).toEqual([true, false, true]);
});

test('nothing shows without a skip', () => {
  const video = new Video();
  const { notice, changes } = follow(video);
  video.emit('seeked');
  video.emit('playing');
  jest.advanceTimersByTime(SKIP_NOTICE_LINGER_MS * 2);
  expect(changes).toEqual([]);
  notice.dispose();
});

/** The player's side of a skip: the pending ref, the active segment and the follower, around a media at a position. */
function player(duration: number, position: number) {
  const video = new Video();
  video.duration = duration;
  video.currentTime = position;
  video.seeks = [];
  video.seeking = false;
  const { notice, changes } = follow(video);
  const pending = { current: false };
  let active: object | null = {};
  const skip = (end: number) => skipSegment(video, { end }, pending, () => (active = null), notice);
  /** What the browser does after a seek: the position is reached, `seeked` fires and the player clears `pending`. */
  const settle = () => {
    video.emit('seeked', { seeking: false });
    pending.current = false;
  };
  return { video, notice, changes, pending, skip, settle, isActive: () => active !== null };
}

test('a skip seeks past the segment, clears it and raises the notice', () => {
  const { video, notice, changes, pending, skip, isActive } = player(100, 10);
  expect(skip(30)).toBe(true);
  expect(video.currentTime).toBe(30);
  expect(video.seeks).toEqual([30]);
  expect(pending.current).toBe(true);
  expect(isActive()).toBe(false);
  expect(changes).toEqual([true]);
  notice.dispose();
});

test('a skip already pending does nothing more', () => {
  const { video, notice, changes, skip } = player(100, 10);
  skip(30);
  expect(skip(40)).toBe(false);
  expect(video.seeks).toEqual([30]);
  expect(changes).toEqual([true]);
  notice.dispose();
});

test('a segment running past the end is capped to the duration, and may still be skipped from before it', () => {
  const { video, notice, skip } = player(100, 95);
  expect(skip(120)).toBe(true);
  expect(video.seeks).toEqual([100]);
  notice.dispose();
});

test('a segment running past the end does not seek again once the playhead is at the end', () => {
  const { video, notice, changes, pending, skip, settle, isActive } = player(100, 95);
  skip(120);
  settle();
  expect(video.currentTime).toBe(100);
  jest.advanceTimersByTime(SKIP_NOTICE_LINGER_MS * 2);

  for (let i = 0; i < 3; i++) {
    expect(skip(120)).toBe(false);
    settle();
  }
  expect(video.seeks).toEqual([100]);
  expect(changes).toEqual([true, false]);
  expect(video.seeking).toBe(false);
  expect(pending.current).toBe(false);
  expect(isActive()).toBe(false);
  notice.dispose();
});

test('a target at or behind the playhead changes nothing', () => {
  for (const end of [50, 40]) {
    const { video, notice, changes, pending, skip, isActive } = player(100, 50);
    expect(skip(end)).toBe(false);
    expect(video.seeks).toEqual([]);
    expect(pending.current).toBe(false);
    expect(isActive()).toBe(true);
    expect(changes).toEqual([]);
    notice.dispose();
  }
});

test('a stream of unknown duration is skipped to the segment end', () => {
  for (const duration of [Number.NaN, Number.POSITIVE_INFINITY]) {
    const { video, notice, skip } = player(duration, 10);
    expect(skip(30)).toBe(true);
    expect(video.seeks).toEqual([30]);
    notice.dispose();
  }
});
