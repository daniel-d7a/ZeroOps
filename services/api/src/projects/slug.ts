/** Reserved project slugs (plan §5) — collide with console/platform hostnames. */
export const RESERVED_SLUGS = new Set(['console', 'api', 'www', 'admin', 'traefik', 'rabbitmq']);

/** DNS label: 1-63 chars, [a-z0-9-], no leading/trailing hyphen. */
export const SLUG_PATTERN = /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$/;

export const isValidSlug = (slug: string): boolean => SLUG_PATTERN.test(slug);
export const isReservedSlug = (slug: string): boolean => RESERVED_SLUGS.has(slug);

/** Derive a slug from a project name; '' when nothing survives the sanitize. */
export function slugify(name: string): string {
  return name
    .toLowerCase()
    .normalize('NFKD') // decompose accents: e + combining mark
    .replace(/\p{M}+/gu, '') // strip the marks the decomposition exposed
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 63)
    .replace(/-+$/g, '');
}
