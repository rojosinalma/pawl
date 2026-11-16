type Kind = 'a' | 'b' | 'c';

export function kind(k: Kind): number {
  switch (k) {
    case 'a':
      return 1;
    case 'b':
      return 2;
  }
  return 0;
}

export const wrongType: number = 'not a number';

export function Bad_Name(): number {
  return 1;
}
