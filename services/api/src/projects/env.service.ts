import { BadRequestException, ForbiddenException, Inject, Injectable, NotFoundException } from '@nestjs/common';
import { and, asc, eq, inArray, ne, sql } from 'drizzle-orm';
import { DRIZZLE, Db } from '../db/db.module';
import { envVars, projects } from '../db/schema';
import { decrypt, encrypt } from '../common/crypto';

// Plan §7: key ^[A-Z_][A-Z0-9_]*$ (≤128), value ≤4 KB, reserved keys
// cannot be set (the platform injects PORT and DATABASE_URL), max 50 vars.
const KEY_PATTERN = /^[A-Z_][A-Z0-9_]*$/;
const RESERVED_KEYS = new Set(['PORT', 'DATABASE_URL']);
const MAX_VARS = 50;
const MAX_VALUE_BYTES = 4 * 1024;

@Injectable()
export class EnvService {
  constructor(@Inject(DRIZZLE) private readonly db: Db) {}

  /** 404 (not 403) when the project belongs to someone else (AGENTS). */
  private async assertOwned(projectId: string, userId: string) {
    const [project] = await this.db
      .select({ id: projects.id })
      .from(projects)
      .where(and(eq(projects.id, projectId), eq(projects.userId, userId)))
      .limit(1);
    if (!project) throw new NotFoundException('project not found');
  }

  private validate(key: string, value: string) {
    if (key.length > 128 || !KEY_PATTERN.test(key)) {
      throw new BadRequestException('key must match ^[A-Z_][A-Z0-9_]*$ (max 128 chars)');
    }
    if (RESERVED_KEYS.has(key)) throw new BadRequestException(`${key} is reserved and cannot be set`);
    if (value.length === 0) throw new BadRequestException('value must not be empty');
    if (Buffer.byteLength(value, 'utf8') > MAX_VALUE_BYTES) {
      throw new BadRequestException('value exceeds 4 KB');
    }
  }

  /** Owner-only listing: values are decrypted here and nowhere else logged. */
  async list(projectId: string, userId: string) {
    await this.assertOwned(projectId, userId);
    const rows = await this.db
      .select({ key: envVars.key, valueEncrypted: envVars.valueEncrypted, updatedAt: envVars.updatedAt })
      .from(envVars)
      .where(eq(envVars.projectId, projectId))
      .orderBy(asc(envVars.key));
    return {
      items: rows.map((row) => ({ key: row.key, value: decrypt(row.valueEncrypted), updatedAt: row.updatedAt })),
      total: rows.length,
    };
  }

  /** Create or update one var (PUT upserts on the (project, key) index). */
  async put(projectId: string, userId: string, key: string, value: string) {
    await this.assertOwned(projectId, userId);
    this.validate(key, value);

    // Quota applies to new keys only — updating an existing one never 409s.
    const [{ count }] = await this.db
      .select({ count: sql<number>`count(*)` })
      .from(envVars)
      .where(and(eq(envVars.projectId, projectId), ne(envVars.key, key)));
    if (Number(count) >= MAX_VARS) throw new ForbiddenException('env var quota reached (50 per project)');

    const [row] = await this.db
      .insert(envVars)
      .values({ projectId, key, valueEncrypted: encrypt(value) })
      .onConflictDoUpdate({
        target: [envVars.projectId, envVars.key],
        set: { valueEncrypted: encrypt(value), updatedAt: new Date() },
      })
      .returning({ key: envVars.key, updatedAt: envVars.updatedAt });
    return { key: row.key, value, updatedAt: row.updatedAt };
  }

  async remove(projectId: string, userId: string, key: string) {
    await this.assertOwned(projectId, userId);
    const deleted = await this.db
      .delete(envVars)
      .where(and(eq(envVars.projectId, projectId), eq(envVars.key, key)))
      .returning({ id: envVars.id });
    if (deleted.length === 0) throw new NotFoundException('env var not found');
  }

  /**
   * Bulk apply (plan §M6.5): the `.env` editor's diff lands in ONE request.
   * Everything is validated up-front (400 names the offending key, nothing
   * written), quota is checked against the FINAL state, and the writes run in
   * a single transaction — all-or-nothing, no partial-apply recovery.
   * A key present in both maps is saved, not deleted (the pasted file wins).
   */
  async bulk(projectId: string, userId: string, set: Record<string, string> | undefined, remove: string[] | undefined) {
    await this.assertOwned(projectId, userId);

    const sets = Object.entries(set ?? {});
    for (const [key, value] of sets) {
      if (typeof value !== 'string') throw new BadRequestException(`set.${key}: value must be a string`);
      try {
        this.validate(key, value);
      } catch (e) {
        throw new BadRequestException(`set.${key}: ${(e as Error).message}`);
      }
    }
    const dels = [...new Set(remove ?? [])].filter((k) => !(k in (set ?? {})));

    const existingRows = await this.db
      .select({ key: envVars.key })
      .from(envVars)
      .where(eq(envVars.projectId, projectId));
    const existing = new Set(existingRows.map((r) => r.key));
    const newKeys = sets.filter(([k]) => !existing.has(k)).length;
    const removedExisting = dels.filter((k) => existing.has(k)).length;
    // existingRows is the full set (quota caps it at 50) — no count(*) needed.
    if (existingRows.length - removedExisting + newKeys > MAX_VARS) {
      throw new ForbiddenException('env var quota reached (50 per project)');
    }

    const deleted = await this.db.transaction(async (tx) => {
      if (sets.length > 0) {
        // One multi-row upsert instead of a query per key; excluded.* refers
        // to the proposed row, so updates overwrite in the same statement.
        await tx
          .insert(envVars)
          .values(sets.map(([key, value]) => ({ projectId, key, valueEncrypted: encrypt(value) })))
          .onConflictDoUpdate({
            target: [envVars.projectId, envVars.key],
            set: { valueEncrypted: sql`excluded.value_encrypted`, updatedAt: new Date() },
          });
      }
      if (dels.length === 0) return 0;
      const rows = await tx
        .delete(envVars)
        .where(and(eq(envVars.projectId, projectId), inArray(envVars.key, dels)))
        .returning({ id: envVars.id });
      return rows.length;
    });

    return { saved: sets.length, deleted };
  }
}
