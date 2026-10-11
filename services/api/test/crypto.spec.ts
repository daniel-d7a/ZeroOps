import { beforeEach, describe, expect, it, vi } from 'vitest';

const KEY = 'ab'.repeat(32); // 64 hex chars

/** Fresh module per case: the key is cached after first use (src/crypto.ts). */
async function load() {
  vi.resetModules();
  return import('../src/common/crypto');
}

beforeEach(() => {
  vi.resetModules();
});

describe('AES-256-GCM env values (plan §7)', () => {
  it('round-trips unicode plaintext', async () => {
    process.env.ENCRYPTION_KEY = KEY;
    const { encrypt, decrypt } = await load();
    const value = 'héllo wörld — 🔐 newline\nand "quotes"';
    expect(decrypt(encrypt(value))).toBe(value);
  });

  it('uses the wire layout base64(iv(12) || tag(16) || ct) shared with Go', async () => {
    process.env.ENCRYPTION_KEY = KEY;
    const { encrypt } = await load();
    const buf = Buffer.from(encrypt('hello'), 'base64');
    expect(buf.length).toBe(12 + 16 + 'hello'.length);
  });

  it('randomizes the IV — same plaintext, different ciphertext', async () => {
    process.env.ENCRYPTION_KEY = KEY;
    const { encrypt } = await load();
    expect(encrypt('same')).not.toBe(encrypt('same'));
  });

  it('rejects tampered ciphertext (GCM auth tag)', async () => {
    process.env.ENCRYPTION_KEY = KEY;
    const { encrypt, decrypt } = await load();
    const buf = Buffer.from(encrypt('secret'), 'base64');
    buf[buf.length - 1] ^= 0xff;
    expect(() => decrypt(buf.toString('base64'))).toThrow();
  });

  it('rejects truncated ciphertext before touching the cipher', async () => {
    process.env.ENCRYPTION_KEY = KEY;
    const { decrypt } = await load();
    expect(() => decrypt(Buffer.from('short').toString('base64'))).toThrow('ciphertext too short');
  });

  it('fails fast on a missing or malformed key', async () => {
    delete process.env.ENCRYPTION_KEY;
    const bad = await load();
    expect(() => bad.encrypt('x')).toThrow('ENCRYPTION_KEY must be 64 hex characters');

    process.env.ENCRYPTION_KEY = 'nothex';
    const worse = await load();
    expect(() => worse.encrypt('x')).toThrow('ENCRYPTION_KEY must be 64 hex characters');
  });

  it('fails on decrypt when the key differs from the one that encrypted', async () => {
    process.env.ENCRYPTION_KEY = KEY;
    const enc = await load();
    const blob = enc.encrypt('secret');

    process.env.ENCRYPTION_KEY = 'cd'.repeat(32);
    const dec = await load();
    expect(() => dec.decrypt(blob)).toThrow();
  });
});
