export type Tone = 'plain' | 'warm';

export function greet(name: string, tone: Tone = 'plain'): string {
  const trimmed = name.trim();
  if (trimmed === '') {
    throw new Error('name must not be empty');
  }
  switch (tone) {
    case 'plain':
      return `Hello, ${trimmed}.`;
    case 'warm':
      return `Hello, ${trimmed}! Good to see you.`;
  }
}
