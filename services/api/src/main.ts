import { NestFactory } from '@nestjs/core';
import { AppModule } from './app.module';

async function bootstrap() {
  const app = await NestFactory.create(AppModule);
  app.enableShutdownHooks(); // close the HTTP server on SIGTERM (watch restarts, compose stop)
  app.setGlobalPrefix('api');
  // One hop (Traefik): express trusts its X-Forwarded-For hop only, so the
  // rightmost entry is the real client — required for per-IP rate limiting.
  app.getHttpAdapter().getInstance().set('trust proxy', 1);
  const port = Number(process.env.PORT ?? 3000);
  await app.listen(port, '0.0.0.0');
  // eslint-disable-next-line no-console
  console.log(`api listening on :${port}`);
}
void bootstrap();
