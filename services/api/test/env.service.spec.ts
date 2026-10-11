import { describe, expect, it } from 'vitest';
import { BadRequestException, NotFoundException } from '@nestjs/common';
import { EnvService } from '../src/projects/env.service';

/** assertOwned only: a chainable select whose .limit(1) resolves [{id}]. */
function ownedDb() {
  const chain = {
    from: () => chain,
    where: () => chain,
    limit: () => Promise.resolve([{ id: 'project-id' }]),
  };
  return { select: () => chain } as never;
}

function service() {
  return new EnvService(ownedDb());
}

describe('env var validation rules (plan §7)', () => {
  // private, exercised via bracket access — no need to mock the whole DB
  const validate = (s: EnvService, key: string, value: string) =>
    (s as unknown as { validate: (k: string, v: string) => void }).validate(key, value);

  it('accepts a well-formed key and value', () => {
    expect(() => validate(service(), 'GREETING', 'hello world')).not.toThrow();
    expect(() => validate(service(), '_PRIVATE2', "with 'quotes'")).not.toThrow();
  });

  it('rejects keys that are not ^[A-Z_][A-Z0-9_]*$', () => {
    for (const key of ['lower', '1ABC', 'A B', 'A-B', 'ÜPPER', '']) {
      expect(() => validate(service(), key, 'x')).toThrow(/key must match/);
    }
  });

  it('rejects keys longer than 128 characters', () => {
    expect(() => validate(service(), 'A'.repeat(128), 'x')).not.toThrow();
    expect(() => validate(service(), 'A'.repeat(129), 'x')).toThrow(/key must match/);
  });

  it('rejects the reserved platform keys', () => {
    expect(() => validate(service(), 'PORT', '9999')).toThrow(/reserved/);
    expect(() => validate(service(), 'DATABASE_URL', 'postgres://x')).toThrow(/reserved/);
  });

  it('rejects empty values (shared rule: single PUT and bulk alike)', () => {
    expect(() => validate(service(), 'KEY', '')).toThrow(/must not be empty/);
  });

  it('enforces 4 KB in BYTES, not characters', () => {
    expect(() => validate(service(), 'KEY', 'a'.repeat(4096))).not.toThrow();
    expect(() => validate(service(), 'KEY', 'a'.repeat(4097))).toThrow(/4 KB/);
    expect(() => validate(service(), 'KEY', '€'.repeat(1365))).not.toThrow(); // 4095 bytes
    expect(() => validate(service(), 'KEY', '€'.repeat(1366))).toThrow(/4 KB/); // 4098 bytes
  });
});

describe('bulk() validation wrapper (names the offending key, writes nothing)', () => {
  it('prefixes key errors with set.<KEY>', async () => {
    await expect(service().bulk('p', 'u', { 'BAD-KEY': 'x' }, undefined)).rejects.toThrow(
      /set.BAD-KEY: key must match/,
    );
    await expect(service().bulk('p', 'u', { PORT: '1' }, undefined)).rejects.toThrow(
      /set.PORT: PORT is reserved/,
    );
    await expect(service().bulk('p', 'u', { KEY: '' }, undefined)).rejects.toThrow(/set.KEY: .*empty/);
    await expect(service().bulk('p', 'u', { KEY: 'x'.repeat(4097) }, undefined)).rejects.toThrow(
      /set.KEY: .*4 KB/,
    );
  });

  it('rejects non-string values before touching the DB beyond ownership', async () => {
    await expect(service().bulk('p', 'u', { KEY: 42 as never }, undefined)).rejects.toThrow(
      /set.KEY: value must be a string/,
    );
  });

  it('still 404s for a project the caller does not own', async () => {
    const foreign = new EnvService({
      select: () => ({ from: () => ({ where: () => ({ limit: () => Promise.resolve([]) }) }) }),
    } as never);
    await expect(foreign.bulk('p', 'u', { KEY: 'v' }, undefined)).rejects.toThrow(NotFoundException);
    await expect(foreign.put('p', 'u', 'KEY', 'v')).rejects.toThrow(NotFoundException);
  });

  it('validates before any quota query — bad input never reaches a write', async () => {
    await expect(service().bulk('p', 'u', { KEY: 'ok', 'BAD-KEY': 'x' }, undefined)).rejects.toThrow(
      BadRequestException,
    );
  });
});
