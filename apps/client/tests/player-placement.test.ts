import { decideBeside } from '@/features/video/hooks/use-player-layout';
import { describe, expect, test } from 'bun:test';

describe('player side-panel placement', () => {
  test('stacks the panel below the video just under the available-width cutoff', () => {
    expect(decideBeside(959)).toBe(false);
  });

  test('keeps the panel beside the video at the available-width cutoff', () => {
    expect(decideBeside(960)).toBe(true);
  });
});
