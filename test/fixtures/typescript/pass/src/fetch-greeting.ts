export interface Clock {
  now(): Date;
}

export async function fetchGreeting(url: string, timeoutMs: number): Promise<string> {
  const response = await fetch(url, { signal: AbortSignal.timeout(timeoutMs) });
  if (!response.ok) {
    throw new Error(`greeting service answered ${response.status}`);
  }
  return response.text();
}

export function dayPart(clock: Clock): 'morning' | 'day' {
  return clock.now().getHours() < 12 ? 'morning' : 'day';
}
