import { BadRequestException, ConflictException, ForbiddenException, Inject, Injectable, NotFoundException } from '@nestjs/common';
import { and, desc, eq, sql } from 'drizzle-orm';
import { DRIZZLE, Db } from '../db/db.module';
import { publicProjectColumns, projectDatabases, projects } from '../db/schema';
import { PageQueryDto } from '../common/page-query.dto';
import { CreateProjectDto } from './dto/create-project.dto';
import { isReservedSlug, isValidSlug, slugify } from './slug';

@Injectable()
export class ProjectsService {
  constructor(@Inject(DRIZZLE) private readonly db: Db) {}

  private liveUrl(slug: string): string {
    const domain = process.env.BASE_DOMAIN ?? 'localhost';
    const port = process.env.PUBLIC_PORT ? `:${process.env.PUBLIC_PORT}` : '';
    return `http://${slug}.${domain}${port}`;
  }

  async create(userId: string, dto: CreateProjectDto) {
    const slug = dto.slug ?? slugify(dto.name);
    if (!slug) throw new BadRequestException('cannot derive a slug from that name — pass an explicit slug');
    if (!isValidSlug(slug)) throw new BadRequestException('invalid slug');
    if (isReservedSlug(slug)) throw new BadRequestException(`slug '${slug}' is reserved`);

    const max = Number(process.env.MAX_PROJECTS_PER_USER) || 3;
    const [{ count }] = await this.db
      .select({ count: sql<number>`count(*)` })
      .from(projects)
      .where(eq(projects.userId, userId));
    if (Number(count) >= max) throw new ForbiddenException('project quota reached');

    const [row] = await this.db
      .insert(projects)
      .values({ userId, name: dto.name, slug, wantsDb: dto.wantsDb ?? false })
      .onConflictDoNothing({ target: projects.slug })
      .returning(publicProjectColumns);
    if (!row) throw new ConflictException('slug already in use');
    return { ...row, url: this.liveUrl(row.slug) };
  }

  async list(userId: string, query: PageQueryDto) {
    const limit = query.limit ?? 20;
    const offset = query.offset ?? 0;
    const owned = eq(projects.userId, userId);
    const [items, [{ count }]] = await Promise.all([
      this.db
        .select(publicProjectColumns)
        .from(projects)
        .where(owned)
        .orderBy(desc(projects.createdAt))
        .limit(limit)
        .offset(offset),
      this.db.select({ count: sql<number>`count(*)` }).from(projects).where(owned),
    ]);
    return { items: items.map((p) => ({ ...p, url: this.liveUrl(p.slug) })), total: Number(count) };
  }

  async get(userId: string, id: string) {
    const [row] = await this.db
      .select(publicProjectColumns)
      .from(projects)
      .where(and(eq(projects.id, id), eq(projects.userId, userId)))
      .limit(1);
    // 404 (not 403) for other users' projects: they must not learn it exists (AGENTS)
    if (!row) throw new NotFoundException('project not found');
    return { ...row, url: this.liveUrl(row.slug) };
  }

  /**
   * Provisioning status for the Database tab (M6.5). Owner-checked via the
   * project first, password never leaves the DB — status only; the worker is
   * the sole writer of transitions (AGENTS state-ownership invariant).
   */
  async getDatabase(userId: string, projectId: string) {
    await this.get(userId, projectId); // 404s for foreign/unknown ids
    const [row] = await this.db
      .select({
        status: projectDatabases.status,
        dbName: projectDatabases.dbName,
        dbUser: projectDatabases.dbUser,
        createdAt: projectDatabases.createdAt,
      })
      .from(projectDatabases)
      .where(eq(projectDatabases.projectId, projectId))
      .limit(1);
    if (!row) throw new NotFoundException('database not provisioned yet');
    return row;
  }
}
