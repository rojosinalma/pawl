export function ignoreFailure(fn: () => void): void {
  try {
    fn();
  } catch {}
}
