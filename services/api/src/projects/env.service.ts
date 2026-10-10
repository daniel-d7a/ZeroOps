import { BadRequestException, ForbiddenException, Inject, Injectable, NotFoundException } from '@nestjs/common';
import { and, asc, eq, ne, sql } from 'drizzle-orm';
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
}
