import { ConflictException, ForbiddenException, Inject, Injectable, UnauthorizedException } from '@nestjs/common';
import { hash, verify } from '@node-rs/argon2';
import { eq } from 'drizzle-orm';
import { randomBytes } from 'node:crypto';
import { Session, SessionData } from 'express-session';
import { Request } from 'express';
import { DRIZZLE, Db } from '../db/db.module';
import { users } from '../db/schema';
import { LoginDto } from './dto/login.dto';
import { RegisterDto } from './dto/register.dto';

type ReqSession = Session & Partial<SessionData>; // exactly what express hands us (see @types/express-session)

// Verified against when the email doesn't exist, so a miss costs the same as
// a wrong password (no timing-based user enumeration).
const DUMMY_HASH = hash(randomBytes(32).toString('hex'));

const publicUser = (u: typeof users.$inferSelect) => ({
  id: u.id,
  email: u.email,
  role: u.role,
  status: u.status,
  createdAt: u.createdAt,
});

@Injectable()
export class AuthService {
  constructor(@Inject(DRIZZLE) private readonly db: Db) {}

  async register(dto: RegisterDto) {
    // Hash before the conflict check so duplicate emails cost the same time
    // as new ones (no user-enumeration timing signal).
    const passwordHash = await hash(dto.password);
    const [user] = await this.db
      .insert(users)
      .values({ email: dto.email, passwordHash })
      .onConflictDoNothing({ target: users.email })
      .returning({ id: users.id, email: users.email, role: users.role, status: users.status, createdAt: users.createdAt });
    if (!user) throw new ConflictException('email already registered');
    return user;
  }

  async login(dto: LoginDto, req: Request) {
    const [user] = await this.db.select().from(users).where(eq(users.email, dto.email)).limit(1);
    const passwordOk = await verify(user ? user.passwordHash : await DUMMY_HASH, dto.password);
    if (!user || !passwordOk) throw new UnauthorizedException('invalid email or password');
    if (user.status !== 'active') throw new ForbiddenException('account disabled');
    // Fresh session id on privilege change (plan §7): a pre-login id can't be replayed.
    // MUST write to req.session (not a captured reference): regenerate() reassigns
    // req.session to a brand-new object — mutations on the old one are silently dropped.
    await new Promise<void>((resolve, reject) => req.session.regenerate((err) => (err ? reject(err) : resolve())));
    req.session.uid = user.id;
    req.session.startedAt = Date.now();
    req.session.csrf = randomBytes(32).toString('hex');
    return { ...publicUser(user), csrfToken: req.session.csrf };
  }

  async logout(sess: ReqSession): Promise<void> {
    await new Promise<void>((resolve, reject) => sess.destroy((err) => (err ? reject(err) : resolve())));
  }

  async me(sess: ReqSession) {
    const [user] = await this.db.select().from(users).where(eq(users.id, sess.uid!)).limit(1);
    if (!user || user.status !== 'active') {
      // disabled or deleted since login — drop the session (admin disable also
      // deletes sessions, this is the belt to that suspenders)
      await new Promise<void>((resolve, reject) => sess.destroy((e) => (e ? reject(e) : resolve())));
      throw new UnauthorizedException();
    }
    // self-heal: sessions that predate a deploy may lack a token; the store's
    // shouldSave (content hash) persists the mint on the way out.
    sess.csrf ??= randomBytes(32).toString('hex');
    return { ...publicUser(user), csrfToken: sess.csrf };
  }
}
