import { cycleA } from './cycle-a';

export function cycleB(n: number): number {
  return n <= 0 ? 1 : cycleA(n - 1);
}
