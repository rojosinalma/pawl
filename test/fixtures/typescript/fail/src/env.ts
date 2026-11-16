export function apiBase(): string {
  return process.env.ACME_API_BASE ?? 'https://api.example.com';
}
