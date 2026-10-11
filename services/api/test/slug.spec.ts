import { describe, expect, it } from 'vitest';
import { isReservedSlug, isValidSlug, slugify } from '../src/projects/slug';

describe('isValidSlug (DNS label, plan §5)', () => {
  it('accepts lowercase labels without edge hyphens', () => {
    for (const s of ['a', 'my-app', 'app-42', '0', 'a-b-c']) expect(isValidSlug(s)).toBe(true);
  });

  it('rejects anything that is not a DNS label', () => {
    for (const s of ['', '-a', 'a-', 'A', 'a_b', 'a b', 'a.b', 'café', '.']) {
      expect(isValidSlug(s)).toBe(false);
    }
  });

  it('rejects labels longer than 63 characters', () => {
    expect(isValidSlug('a'.repeat(63))).toBe(true);
    expect(isValidSlug('a'.repeat(64))).toBe(false);
  });
});

describe('isReservedSlug (reserved hostnames, plan §5)', () => {
  it('reserves platform slugs', () => {
    for (const s of ['console', 'api', 'www', 'admin', 'traefik', 'rabbitmq']) {
      expect(isReservedSlug(s)).toBe(true);
    }
  });

  it('leaves normal names free', () => {
    expect(isReservedSlug('my-app')).toBe(false);
    expect(isReservedSlug('console-2')).toBe(false);
  });
});

describe('slugify', () => {
  it('derives a clean slug from a display name', () => {
    expect(slugify('My First App')).toBe('my-first-app');
    expect(slugify('  Hello   World  ')).toBe('hello-world');
    expect(slugify('2024 Report')).toBe('2024-report');
  });

  it('strips accents via NFKD decomposition', () => {
    expect(slugify('Café Déjà Vu')).toBe('cafe-deja-vu');
  });

  it('collapses junk and returns empty when nothing survives', () => {
    expect(slugify('!!! *** !!!')).toBe('');
    expect(slugify('---')).toBe('');
  });

  it('never ends up longer than 63 chars or with a trailing hyphen', () => {
    const slug = slugify('x'.repeat(60) + ' words that push past the limit');
    expect(slug.length).toBeLessThanOrEqual(63);
    expect(slug.endsWith('-')).toBe(false);
    expect(isValidSlug(slug)).toBe(true);
  });
});
