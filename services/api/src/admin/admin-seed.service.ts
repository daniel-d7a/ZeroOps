import { Inject, Injectable, OnApplicationBootstrap } from '@nestjs/common';
import { hash } from '@node-rs/argon2';
import { isEmail } from 'class-validator';
import { eq } from 'drizzle-orm';
import { DRIZZLE, Db } from '../db/db.module';
import { users } from '../db/schema';

/**
 * First admin from ADMIN_EMAIL/ADMIN_PASSWORD if no admin exists (plan §7).
 * Runs on every boot, idempotent: an existing admin email is left alone
 * (registering first must never escalate to admin).
 */
@Injectable()
export class AdminSeedService implements OnApplicationBootstrap {
  constructor(@Inject(DRIZZLE) private readonly db: Db) {}

  async onApplicationBootstrap(): Promise<void> {
    const [existing] = await this.db.select({ id: users.id }).from(users).where(eq(users.role, 'admin')).limit(1);
    if (existing) return;
    const email = process.env.ADMIN_EMAIL?.trim().toLowerCase();
    const password = process.env.ADMIN_PASSWORD;
    if (!email || !password) {
      console.warn('admin seed: no admin exists and ADMIN_EMAIL/ADMIN_PASSWORD not set — skipping');
      return;
    }
    // A set-but-invalid email is a config error: fail boot loudly instead of
    // shipping a platform with no way to get an admin.
    if (!isEmail(email)) throw new Error(`admin seed: ADMIN_EMAIL is not a valid email: ${email}`);
    const passwordHash = await hash(password);
    const [admin] = await this.db
      .insert(users)
      .values({ email, passwordHash, role: 'admin' })
      .onConflictDoNothing({ target: users.email })
      .returning({ id: users.id, email: users.email });
    if (admin) console.log(`admin seed: created ${admin.email}`);
    else console.warn(`admin seed: ${email} already registered without admin role — not escalating`);
  }
}
