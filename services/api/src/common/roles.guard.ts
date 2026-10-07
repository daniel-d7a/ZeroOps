import { CanActivate, ExecutionContext, ForbiddenException, Inject, Injectable } from '@nestjs/common';
import { Reflector } from '@nestjs/core';
import { eq } from 'drizzle-orm';
import { Request } from 'express';
import { DRIZZLE, Db } from '../db/db.module';
import { UserRole, users } from '../db/schema';
import { ROLES_KEY } from './roles.decorator';

/** Role read from the DB (not the session) so role changes apply immediately. Pair with SessionGuard. */
@Injectable()
export class RolesGuard implements CanActivate {
  constructor(
    private readonly reflector: Reflector,
    @Inject(DRIZZLE) private readonly db: Db,
  ) {}

  async canActivate(ctx: ExecutionContext): Promise<boolean> {
    const required = this.reflector.getAllAndOverride<UserRole[] | undefined>(ROLES_KEY, [
      ctx.getHandler(),
      ctx.getClass(),
    ]);
    if (!required?.length) return true;
    const req = ctx.switchToHttp().getRequest<Request>();
    const [user] = await this.db.select({ role: users.role }).from(users).where(eq(users.id, req.session.uid!)).limit(1);
    if (!user || !required.includes(user.role)) throw new ForbiddenException('admin only');
    return true;
  }
}
