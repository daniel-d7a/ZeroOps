import { BadRequestException, Inject, Injectable, NotFoundException } from '@nestjs/common';
import { desc, eq, sql } from 'drizzle-orm';
import { DRIZZLE, Db } from '../db/db.module';
import {
  deployments,
  publicProjectColumns,
  publicUserColumns,
  projects,
  sessions,
  users,
  UserStatus,
  workers,
} from '../db/schema';
import { PageQueryDto } from '../common/page-query.dto';
import { liveUrl } from '../common/live-url';

@Injectable()
export class AdminService {
  constructor(@Inject(DRIZZLE) private readonly db: Db) {}

  /** Users page (plan §M6.6): email, role, status, project count, created. */
  async listUsers(query: PageQueryDto) {
    const limit = query.limit ?? 20;
    const offset = query.offset ?? 0;
    const [items, [{ count }]] = await Promise.all([
      this.db
        .select({ ...publicUserColumns, projectCount: sql<number>`count(${projects.id})` })
        .from(users)
        .leftJoin(projects, eq(projects.userId, users.id))
        .groupBy(users.id)
        .orderBy(desc(users.createdAt))
        .limit(limit)
        .offset(offset),
      this.db.select({ count: sql<number>`count(*)` }).from(users),
    ]);
    return { items: items.map((u) => ({ ...u, projectCount: Number(u.projectCount) })), total: Number(count) };
  }

  /** All-projects page (plan §M6.6): owner, status, node, URL, stop action.
   *  `node` = the worker that built the current deployment (worker_id). */
  async listProjects(query: PageQueryDto) {
    const limit = query.limit ?? 20;
    const offset = query.offset ?? 0;
    const [items, [{ count }]] = await Promise.all([
      this.db
        .select({
          ...publicProjectColumns,
          ownerEmail: users.email,
          node: deployments.workerId,
        })
        .from(projects)
        .innerJoin(users, eq(projects.userId, users.id))
        .leftJoin(deployments, eq(deployments.id, projects.currentDeploymentId))
        .orderBy(desc(projects.createdAt))
        .limit(limit)
        .offset(offset),
      this.db.select({ count: sql<number>`count(*)` }).from(projects),
    ]);
    return { items: items.map((p) => ({ ...p, url: liveUrl(p.slug) })), total: Number(count) };
  }

  /** System page (plan §M6.6): workers with last heartbeat + roles, and
   *  deployment counts by status. Read-only — heartbeat upserts are the
   *  worker's (AGENTS state ownership). */
  async system() {
    const [workerRows, counts] = await Promise.all([
      this.db.select().from(workers).orderBy(desc(workers.lastSeenAt)),
      this.db
        .select({ status: deployments.status, count: sql<number>`count(*)` })
        .from(deployments)
        .groupBy(deployments.status),
    ]);
    return {
      workers: workerRows,
      deployments: counts.map((c) => ({ status: c.status, count: Number(c.count) })),
    };
  }

  async updateUserStatus(id: string, status: UserStatus, actorId: string) {
    if (status === 'disabled' && id === actorId) throw new BadRequestException('cannot disable your own account');
    const [user] = await this.db
      .update(users)
      .set({ status })
      .where(eq(users.id, id))
      .returning(publicUserColumns);
    if (!user) throw new NotFoundException('user not found');
    if (status === 'disabled') {
      // live sessions carry the uid inside the jsonb payload
      await this.db.delete(sessions).where(sql`sess->>'uid' = ${id}`);
    }
    return user;
  }
}
