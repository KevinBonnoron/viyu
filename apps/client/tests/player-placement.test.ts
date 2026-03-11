import { decideBeside } from '@/features/video/hooks/use-player-layout';
import { describe, expect, test } from 'bun:test';

describe('decideBeside', () => {
  test('a wide, short area puts the panel beside: the video is limited by the height anyway', () => {
    expect(decideBeside(1700, 800)).toBe(true);
  });

  test('a tall area stacks the panel: beside, the video would shrink', () => {
    expect(decideBeside(1400, 1100)).toBe(false);
  });

  test('a narrow area always stacks', () => {
    expect(decideBeside(900, 400)).toBe(false);
  });
});
