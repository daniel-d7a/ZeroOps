import { ConflictException, Inject, Injectable } from '@nestjs/common';
import { hash } from '@node-rs/argon2';
import { DRIZZLE, Db } from '../db/db.module';
import { users } from '../db/schema';
import { RegisterDto } from './dto/register.dto';

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
}
