import { CanActivate, ExecutionContext, ForbiddenException, Injectable } from '@nestjs/common';
import { timingSafeEqual } from 'node:crypto';
import { Request } from 'express';

const SAFE_METHODS = new Set(['GET', 'HEAD', 'OPTIONS']);

/**
 * Global (plan §7): every state-changing request backed by a cookie session
 * must echo the session's CSRF token in X-CSRF-Token. Anonymous requests have
 * nothing cookie-authenticated to protect — route guards 401 them if needed.
 * The attacker can attach the cookie but can neither read the token (same-origin
 * policy) nor set a custom header cross-origin (we serve no CORS).
 */
@Injectable()
export class CsrfGuard implements CanActivate {
  canActivate(ctx: ExecutionContext): boolean {
    const req = ctx.switchToHttp().getRequest<Request>();
    if (SAFE_METHODS.has(req.method)) return true;
    const sess = req.session;
    if (!sess?.uid) return true;
    const expected = sess.csrf;
    if (!expected) throw new ForbiddenException('CSRF token missing from session');
    const got = Buffer.from(req.get('x-csrf-token') ?? '');
    const want = Buffer.from(expected);
    if (got.length !== want.length || !timingSafeEqual(got, want)) {
      throw new ForbiddenException('invalid CSRF token');
    }
    return true;
  }
}
