export function classify(a: number, b: number, c: number): string {
  let out = '';
  for (let i = 0; i < a; i++) {
    if (i % 2 === 0) {
      if (b > 0) {
        for (let j = 0; j < b; j++) {
          if (j % 3 === 0 || (c > 1 && j > 2)) {
            if (c > 2) {
              out += 'x';
            } else if (c > 1) {
              out += 'y';
            } else {
              while (c < 0) {
                if (out.length > 10 && b > 1) {
                  break;
                }
                out += 'z';
              }
            }
          }
        }
      } else if (b < -1 && c > 0) {
        out += 'w';
      }
    }
  }
  return out;
}
