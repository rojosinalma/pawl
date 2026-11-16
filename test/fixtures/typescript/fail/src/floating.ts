async function save(value: string): Promise<string> {
  return value;
}

export function load(value: string): void {
  save(value);
}
