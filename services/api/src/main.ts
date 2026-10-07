import { NestFactory } from '@nestjs/core';
import connectPgSimple from 'connect-pg-simple';
import session from 'express-session';
import { AppModule } from './app.module';
import { POOL } from './db/db.module';

async function bootstrap() {
  const app = await NestFactory.create(AppModule);
  app.enableShutdownHooks(); // close the HTTP server on SIGTERM (watch restarts, compose stop)
  app.setGlobalPrefix('api');
  // One hop (Traefik): express trusts its X-Forwarded-For hop only, so the
  // rightmost entry is the real client — required for per-IP rate limiting.
  app.getHttpAdapter().getInstance().set('trust proxy', 1);

  const secret = process.env.SESSION_SECRET;
  if (!secret) throw new Error('SESSION_SECRET is not set');
  const PgSession = connectPgSimple(session);
  app.use(
    session({
      store: new PgSession({ pool: app.get(POOL), tableName: 'sessions' }),
      secret,
      resave: false,
      saveUninitialized: false,
      rolling: true, // 7d idle timeout refreshes per request (plan §7); absolute 30d cap in SessionGuard
      cookie: { httpOnly: true, sameSite: 'lax', secure: 'auto', maxAge: 7 * 24 * 60 * 60 * 1000 },
    }),
  );

  const port = Number(process.env.PORT ?? 3000);
  await app.listen(port, '0.0.0.0');
  // eslint-disable-next-line no-console
  console.log(`api listening on :${port}`);
}
void bootstrap();
