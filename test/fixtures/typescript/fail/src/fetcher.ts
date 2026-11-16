export async function download(url: string): Promise<string> {
  const response = await fetch(url);
  return response.text();
}
