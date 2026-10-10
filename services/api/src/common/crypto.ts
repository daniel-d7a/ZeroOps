import { createCipheriv, createDecipheriv, randomBytes } from 'node:crypto';

/**
 * AES-256-GCM for values at rest (plan §7). ENCRYPTION_KEY is 64 hex chars
 * (32 bytes). One format, shared with the Go worker in M5.4:
 *
 *	base64( iv(12) || authTag(16) || ciphertext )
 *
 * Fail fast on a bad key — never encrypt with a default or truncated one.
 * The key is read lazily and cached so tests can set the env first.
 */
let cachedKey: Buffer | null = null;
function key(): Buffer {
  if (!cachedKey) {
    const hex = process.env.ENCRYPTION_KEY;
    if (!hex || !/^[0-9a-fA-F]{64}$/.test(hex)) {
      throw new Error('ENCRYPTION_KEY must be 64 hex characters (32 bytes)');
    }
    cachedKey = Buffer.from(hex, 'hex');
  }
  return cachedKey;
}

export function encrypt(plaintext: string): string {
  const iv = randomBytes(12);
  const cipher = createCipheriv('aes-256-gcm', key(), iv);
  const ct = Buffer.concat([cipher.update(plaintext, 'utf8'), cipher.final()]);
  return Buffer.concat([iv, cipher.getAuthTag(), ct]).toString('base64');
}

export function decrypt(encoded: string): string {
  const buf = Buffer.from(encoded, 'base64');
  if (buf.length < 28) throw new Error('ciphertext too short');
  const decipher = createDecipheriv('aes-256-gcm', key(), buf.subarray(0, 12));
  decipher.setAuthTag(buf.subarray(12, 28));
  return Buffer.concat([decipher.update(buf.subarray(28)), decipher.final()]).toString('utf8');
}
