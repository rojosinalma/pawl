const seen = new Map<string, number>();

export function remember(key: string): number {
  const count = (seen.get(key) ?? 0) + 1;
  seen.set(key, count);
  return count;
}
