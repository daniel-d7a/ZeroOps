import { drizzle } from 'drizzle-orm/node-postgres';
import { migrate } from 'drizzle-orm/node-postgres/migrator';
import { Pool } from 'pg';

// One-shot: applies drizzle/ SQL migrations, then exits. Compose runs this
// before api/worker start (depends_on: service_completed_successfully).
async function main(): Promise<void> {
  const url = process.env.PLATFORM_DB_URL;
  if (!url) throw new Error('PLATFORM_DB_URL is not set');
  const pool = new Pool({ connectionString: url });
  try {
    await migrate(drizzle(pool), { migrationsFolder: process.env.MIGRATIONS_DIR ?? './drizzle' });
    console.log('migrate: up to date');
  } finally {
    await pool.end();
  }
}

main().catch((err: unknown) => {
  console.error('migrate failed:', err);
  process.exit(1);
});
