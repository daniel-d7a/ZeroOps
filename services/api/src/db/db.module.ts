import { Global, Module } from '@nestjs/common';
import { drizzle, NodePgDatabase } from 'drizzle-orm/node-postgres';
import { Pool } from 'pg';
import * as schema from './schema';

export const POOL = 'PG_POOL';
export const DRIZZLE = 'DRIZZLE';
export type Db = NodePgDatabase<typeof schema>;

// Global: inject @Inject(DRIZZLE) db: Db (queries) or @Inject(POOL) pool: Pool
// (main.ts session store). The pool is left open on shutdown:
// enableShutdownHooks force-exits the process, an idle pool has nothing to flush.
@Global()
@Module({
  providers: [
    {
      provide: POOL,
      useFactory: () => {
        const url = process.env.PLATFORM_DB_URL;
        if (!url) throw new Error('PLATFORM_DB_URL is not set');
        return new Pool({ connectionString: url });
      },
    },
    { provide: DRIZZLE, useFactory: (pool: Pool) => drizzle(pool, { schema }), inject: [POOL] },
  ],
  exports: [DRIZZLE, POOL],
})
export class DbModule {}
