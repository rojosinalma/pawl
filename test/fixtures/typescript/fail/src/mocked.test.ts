import { expect, mock, test } from 'bun:test';

mock.module('./debug', () => ({ debugValue: () => 0 }));

test('mocks its own module', async () => {
  const { debugValue } = await import('./debug');
  expect(debugValue(1)).toBe(0);
});
