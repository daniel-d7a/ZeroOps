import { CanActivate, ExecutionContext, Injectable, UnauthorizedException } from '@nestjs/common';
import { Request } from 'express';
import { SESSION_ABSOLUTE_MAX_MS } from './session.constants';

/** Requires a live session (set by login). Applied per-route with @UseGuards. */
@Injectable()
export class SessionGuard implements CanActivate {
  async canActivate(ctx: ExecutionContext): Promise<boolean> {
    const req = ctx.switchToHttp().getRequest<Request>();
    const sess = req.session;
    if (!sess?.uid) throw new UnauthorizedException();
    if (!sess.startedAt || Date.now() - sess.startedAt > SESSION_ABSOLUTE_MAX_MS) {
      await new Promise<void>((resolve, reject) => sess.destroy((err) => (err ? reject(err) : resolve())));
      throw new UnauthorizedException('session expired');
    }
    return true;
  }
}
