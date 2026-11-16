import { describe, expect, test } from 'bun:test';
import { cycleA } from './index';

describe('cycle', () => {
  test.only('focused', () => {
    expect(cycleA(1)).toBe(1);
  });

  test.skip('skipped', () => {
    expect(cycleA(2)).toBe(0);
  });
});
