import { expect, mock, test } from 'bun:test';

const keepAwake = mock((): Promise<void> => Promise.resolve());
const allowSleep = mock((): Promise<void> => Promise.resolve());
mock.module('@capacitor-community/keep-awake', () => ({ KeepAwake: { keepAwake, allowSleep } }));
// The hook must load after the Capacitor module is replaced for this test.
const { holdScreenWhilePlaying } = await import('../src/features/video/hooks/use-keep-awake');

class Video extends EventTarget {
  public paused = true;
  public ended = false;

  public play() {
    this.paused = false;
    this.dispatchEvent(new Event('playing'));
  }

  public pause() {
    this.paused = true;
    this.dispatchEvent(new Event('pause'));
  }
}

// Each queued request has a then/catch pair; drain microtasks for the two-request chains below.
async function settle() {
  for (let i = 0; i < 20; i++) {
    await Promise.resolve();
  }
}

async function withDocument(run: (document: EventTarget) => Promise<void>) {
  const previous = Object.getOwnPropertyDescriptor(globalThis, 'document');
  const document = Object.assign(new EventTarget(), { visibilityState: 'visible' });
  Object.defineProperty(globalThis, 'document', { configurable: true, value: document });
  try {
    await run(document);
  } finally {
    if (previous) {
      Object.defineProperty(globalThis, 'document', previous);
    } else {
      Reflect.deleteProperty(globalThis, 'document');
    }
    keepAwake.mockReset();
    allowSleep.mockReset();
  }
}

for (const release of ['pause', 'dispose'] as const) {
  test(`a stale acquisition failure cannot prevent release on ${release}`, async () => {
    await withDocument(async () => {
      const calls: string[] = [];
      const first = Promise.withResolvers<void>();
      let acquisitions = 0;
      keepAwake.mockImplementation(() => {
        calls.push('keep');
        return ++acquisitions === 1 ? first.promise : Promise.resolve();
      });
      allowSleep.mockImplementation(() => {
        calls.push('allow');
        return Promise.resolve();
      });
      const video = new Video();
      const dispose = holdScreenWhilePlaying(video as unknown as HTMLVideoElement);

      video.play();
      await settle(); // A has started and remains pending.
      video.pause();
      video.play(); // B is queued behind A and its release.
      first.reject(new Error('acquisition failed'));
      await settle();
      expect(calls).toEqual(['keep', 'allow', 'keep']);

      if (release === 'pause') {
        video.pause();
      } else {
        dispose();
      }
      await settle();
      expect(calls).toEqual(['keep', 'allow', 'keep', 'allow']);
      dispose();
    });
  });
}

test('a current acquisition failure permits a visibility retry', async () => {
  await withDocument(async (document) => {
    const calls: string[] = [];
    keepAwake.mockImplementation(() => {
      calls.push('keep');
      return calls.length === 1 ? Promise.reject(new Error('acquisition failed')) : Promise.resolve();
    });
    allowSleep.mockImplementation(() => {
      calls.push('allow');
      return Promise.resolve();
    });
    const video = new Video();
    const dispose = holdScreenWhilePlaying(video as unknown as HTMLVideoElement);

    video.play();
    await settle();
    document.dispatchEvent(new Event('visibilitychange'));
    await settle();
    expect(calls).toEqual(['keep', 'keep']);
    dispose();
    await settle();
    expect(calls).toEqual(['keep', 'keep', 'allow']);
  });
});
