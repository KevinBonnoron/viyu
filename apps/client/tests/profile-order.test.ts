import { moveId, profileOrderChanges } from '../src/lib/profile-order';
import { describe, expect, test } from 'bun:test';

describe('profile order', () => {
  test('moving a profile down or up keeps every other in its relative place', () => {
    expect(moveId(['a', 'b', 'c', 'd'], 0, 2)).toEqual(['b', 'c', 'a', 'd']);
    expect(moveId(['a', 'b', 'c', 'd'], 3, 1)).toEqual(['a', 'd', 'b', 'c']);
  });

  test('profiles never reordered (no position yet) all get one, and unchanged ones are not rewritten', () => {
    const never = [{ id: 'a' }, { id: 'b' }, { id: 'c' }];
    expect(profileOrderChanges(never, ['b', 'a', 'c'])).toEqual([
      { id: 'b', position: 0 },
      { id: 'a', position: 1 },
      { id: 'c', position: 2 },
    ]);
    const ordered = [
      { id: 'a', position: 0 },
      { id: 'b', position: 1 },
      { id: 'c', position: 2 },
    ];
    expect(profileOrderChanges(ordered, ['a', 'c', 'b'])).toEqual([
      { id: 'c', position: 1 },
      { id: 'b', position: 2 },
    ]);
    expect(profileOrderChanges(ordered, ['a', 'b', 'c'])).toEqual([]);
  });
});
