/** Canonical live-URL builder (plan §5) — shared by user and admin lists. */
export function liveUrl(slug: string): string {
  const domain = process.env.BASE_DOMAIN ?? 'localhost';
  const port = process.env.PUBLIC_PORT ? `:${process.env.PUBLIC_PORT}` : '';
  return `http://${slug}.${domain}${port}`;
}
