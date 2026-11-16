import { describe, expect, test } from 'bun:test';
import { dayPart, greet } from './index';

describe('greet', () => {
  test('greets plainly by default', () => {
    expect(greet(' Ada ')).toBe('Hello, Ada.');
  });

  test('greets warmly', () => {
    expect(greet('Ada', 'warm')).toBe('Hello, Ada! Good to see you.');
  });

  test('rejects an empty name', () => {
    expect(() => greet('  ')).toThrow('name must not be empty');
  });
});

describe('dayPart', () => {
  test('reads the injected clock', () => {
    expect(dayPart({ now: () => new Date(2026, 0, 1, 9) })).toBe('morning');
    expect(dayPart({ now: () => new Date(2026, 0, 1, 15) })).toBe('day');
  });
});
