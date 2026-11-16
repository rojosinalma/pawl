import { cycleB } from './cycle-b';

export function cycleA(n: number): number {
  return n <= 0 ? 0 : cycleB(n - 1);
}
