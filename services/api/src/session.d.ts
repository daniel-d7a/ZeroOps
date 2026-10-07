import 'express-session';

declare module 'express-session' {
  interface SessionData {
    /** authenticated user id */
    uid?: string;
    /** login time, ms epoch — enforces the 30d absolute session cap (plan §7) */
    startedAt?: number;
    /** CSRF token echoed in X-CSRF-Token on state-changing requests (plan §7) */
    csrf?: string;
  }
}
