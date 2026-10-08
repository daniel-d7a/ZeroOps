import {
  BadGatewayException,
  BadRequestException,
  ConflictException,
  Inject,
  Injectable,
  NotFoundException,
  PayloadTooLargeException,
} from '@nestjs/common';
import { and, desc, eq, inArray, isNull, sql } from 'drizzle-orm';
import { createHash, randomUUID } from 'node:crypto';
import { createReadStream } from 'node:fs';
import { mkdir, rename, unlink } from 'node:fs/promises';
import path from 'node:path';
import { fromFile } from 'file-type';
import { DRIZZLE, Db } from '../db/db.module';
import { deployments, publicDeploymentColumns, publicSourceColumns, projects, sources } from '../db/schema';
import { PageQueryDto } from '../common/page-query.dto';
import { PublisherService } from '../queue/publisher.service';

export const MAX_UPLOAD_BYTES = 50 * 1024 * 1024; // plan §7: 50 MB
const uploadsRoot = (): string => process.env.UPLOADS_DIR ?? '/data/uploads';
const ACTIVE_STATUSES = ['queued', 'building', 'built', 'starting'] as const;

async function sha256File(filePath: string): Promise<string> {
  const hash = createHash('sha256');
  for await (const chunk of createReadStream(filePath)) hash.update(chunk);
  return hash.digest('hex');
}

@Injectable()
export class DeploymentsService {
  constructor(
    @Inject(DRIZZLE) private readonly db: Db,
    private readonly publisher: PublisherService,
  ) {}

  /** 404 (not 403) when the project belongs to someone else or doesn't exist (AGENTS). */
  private async assertOwned(projectId: string, userId: string) {
    const [project] = await this.db
      .select({ id: projects.id, status: projects.status })
      .from(projects)
      .where(and(eq(projects.id, projectId), eq(projects.userId, userId)))
      .limit(1);
    if (!project) throw new NotFoundException('project not found');
    return project;
  }

  /** Multipart zip upload → source + queued deployment + build job (plan §7). */
  async uploadZip(projectId: string, userId: string, file?: Express.Multer.File) {
    const tmp = file?.path;
    if (!file || !tmp) throw new BadRequestException('zip file required (multipart field "file")');
    try {
      const project = await this.assertOwned(projectId, userId);

      const [active] = await this.db
        .select({ id: deployments.id })
        .from(deployments)
        .where(and(eq(deployments.projectId, projectId), inArray(deployments.status, [...ACTIVE_STATUSES])))
        .limit(1);
      if (active) throw new ConflictException('a deployment is already queued or in progress');

      if (file.size > MAX_UPLOAD_BYTES) throw new PayloadTooLargeException('zip exceeds 50 MB');

      const type = await fromFile(tmp); // content sniff (zip → application/zip), not the filename
      if (type?.mime !== 'application/zip') throw new BadRequestException('not a zip file');

      const sha256 = await sha256File(tmp);
      const sourceId = randomUUID();
      const relPath = path.posix.join(projectId, `${sourceId}.zip`);
      const finalPath = path.join(uploadsRoot(), relPath);
      await mkdir(path.dirname(finalPath), { recursive: true });
      await rename(tmp, finalPath); // file first: a crash here leaves an unreferenced file, never a dangling row

      // source + deployment + project status are one unit (a crash must not
      // leave an orphan sources row or a queued deployment on a stale project)
      const deployment = await this.db.transaction(async (tx) => {
        await tx.insert(sources).values({
          id: sourceId,
          projectId,
          path: relPath, // relative to UPLOADS_DIR (worker joins the same root, read-only)
          sizeBytes: file.size,
          sha256,
          originalFilename: file.originalname.slice(0, 255),
        });
        const [dep] = await tx
          .insert(deployments)
          .values({ projectId, sourceId, kind: 'upload', triggeredBy: userId })
          .returning(publicDeploymentColumns);
        await tx.update(projects).set({ status: 'deploying' }).where(eq(projects.id, projectId));
        return dep;
      });

      try {
        await this.publisher.publishBuild(deployment.id);
      } catch (err) {
        // compensating write: the commit above is durable, so undo both sides
        const message = err instanceof Error ? err.message : String(err);
        await this.db.transaction(async (tx) => {
          await tx
            .update(deployments)
            .set({ status: 'failed', error: `publish failed: ${message}`, finishedAt: new Date() })
            .where(eq(deployments.id, deployment.id));
          await tx
            .update(projects)
            .set({ status: project.status })
            .where(and(eq(projects.id, projectId), eq(projects.status, 'deploying')));
        });
        throw new BadGatewayException('could not queue the build job');
      }

      return deployment;
    } finally {
      await unlink(tmp).catch(() => {}); // no-op once renamed away
    }
  }

  async listDeployments(projectId: string, userId: string, query: PageQueryDto) {
    await this.assertOwned(projectId, userId);
    const limit = query.limit ?? 20;
    const offset = query.offset ?? 0;
    const [items, [{ count }]] = await Promise.all([
      this.db
        .select(publicDeploymentColumns)
        .from(deployments)
        .where(eq(deployments.projectId, projectId))
        .orderBy(desc(deployments.createdAt))
        .limit(limit)
        .offset(offset),
      this.db.select({ count: sql<number>`count(*)` }).from(deployments).where(eq(deployments.projectId, projectId)),
    ]);
    return { items, total: Number(count) };
  }

  async getDeployment(userId: string, id: string) {
    const [row] = await this.db
      .select(publicDeploymentColumns)
      .from(deployments)
      .innerJoin(projects, eq(deployments.projectId, projects.id))
      .where(and(eq(deployments.id, id), eq(projects.userId, userId)))
      .limit(1);
    if (!row) throw new NotFoundException('deployment not found');
    return row;
  }

  async listSources(projectId: string, userId: string, query: PageQueryDto) {
    await this.assertOwned(projectId, userId);
    const limit = query.limit ?? 20;
    const offset = query.offset ?? 0;
    const live = and(eq(sources.projectId, projectId), isNull(sources.deletedAt));
    const [items, [{ count }]] = await Promise.all([
      this.db
        .select(publicSourceColumns)
        .from(sources)
        .where(live)
        .orderBy(desc(sources.createdAt))
        .limit(limit)
        .offset(offset),
      this.db.select({ count: sql<number>`count(*)` }).from(sources).where(live),
    ]);
    return { items, total: Number(count) };
  }

  /** Soft-delete a saved upload; refuses while a live/in-flight deployment needs the file. */
  async deleteSource(projectId: string, userId: string, sourceId: string) {
    await this.assertOwned(projectId, userId);
    const [source] = await this.db
      .select({ id: sources.id })
      .from(sources)
      .where(and(eq(sources.id, sourceId), eq(sources.projectId, projectId), isNull(sources.deletedAt)))
      .limit(1);
    if (!source) throw new NotFoundException('source not found');

    // plan §7: never the source of the live deployment; also block in-flight
    // ones (M3+ would unzip a file the platform is about to read)
    const [inUse] = await this.db
      .select({ id: deployments.id })
      .from(deployments)
      .where(
        and(
          eq(deployments.sourceId, sourceId),
          inArray(deployments.status, [...ACTIVE_STATUSES, 'live']),
        ),
      )
      .limit(1);
    if (inUse) throw new ConflictException('source is in use by a live or in-flight deployment');

    await this.db.update(sources).set({ deletedAt: new Date() }).where(eq(sources.id, sourceId));
  }
}
