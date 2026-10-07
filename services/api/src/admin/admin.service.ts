import { BadRequestException, Inject, Injectable, NotFoundException } from '@nestjs/common';
import { desc, eq, sql } from 'drizzle-orm';
import { DRIZZLE, Db } from '../db/db.module';
import { publicProjectColumns, publicUserColumns, projects, sessions, users, UserStatus } from '../db/schema';
import { PageQueryDto } from '../common/page-query.dto';

@Injectable()
export class AdminService {
  constructor(@Inject(DRIZZLE) private readonly db: Db) {}

  async listUsers(query: PageQueryDto) {
    const limit = query.limit ?? 20;
    const offset = query.offset ?? 0;
    const [items, [{ count }]] = await Promise.all([
      this.db.select(publicUserColumns).from(users).orderBy(desc(users.createdAt)).limit(limit).offset(offset),
      this.db.select({ count: sql<number>`count(*)` }).from(users),
    ]);
    return { items, total: Number(count) };
  }

  async listProjects(query: PageQueryDto) {
    const limit = query.limit ?? 20;
    const offset = query.offset ?? 0;
    const [items, [{ count }]] = await Promise.all([
      this.db
        .select({ ...publicProjectColumns, ownerEmail: users.email })
        .from(projects)
        .innerJoin(users, eq(projects.userId, users.id))
        .orderBy(desc(projects.createdAt))
        .limit(limit)
        .offset(offset),
      this.db.select({ count: sql<number>`count(*)` }).from(projects),
    ]);
    return { items, total: Number(count) };
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
