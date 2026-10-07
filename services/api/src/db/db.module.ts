import { Global, Module } from '@nestjs/common';
import { drizzle, NodePgDatabase } from 'drizzle-orm/node-postgres';
import { Pool } from 'pg';
import * as schema from './schema';

export const DRIZZLE = 'DRIZZLE';
export type Db = NodePgDatabase<typeof schema>;

// Global: inject with @Inject(DRIZZLE) db: Db anywhere.
// The pg pool is left open on shutdown: enableShutdownHooks force-exits the
// process, and an idle pool has nothing worth flushing.
@Global()
@Module({
  providers: [
    {
      provide: DRIZZLE,
      useFactory: () => {
        const url = process.env.PLATFORM_DB_URL;
        if (!url) throw new Error('PLATFORM_DB_URL is not set');
        return drizzle(new Pool({ connectionString: url }), { schema });
      },
    },
  ],
  exports: [DRIZZLE],
})
export class DbModule {}
