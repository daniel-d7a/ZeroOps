import { BadGatewayException, ConflictException, Inject, Injectable, NotFoundException } from '@nestjs/common';
import { and, desc, eq, inArray, sql } from 'drizzle-orm';
import { DRIZZLE, Db } from '../db/db.module';
import { ActionType, deployments, projectActions, projects } from '../db/schema';
import { PageQueryDto } from '../common/page-query.dto';
import { PublisherService } from '../queue/publisher.service';

// Same set upload refuses with 409 (plan §5): see deployments.service.
const ACTIVE_DEPLOYMENT_STATUSES = ['queued', 'building', 'built', 'starting'] as const;

/**
 * Stop / start / restart (plan §5 endpoints, §8.6 semantics on the worker).
 * The API only records the request and queues the job — all container work
 * happens in the worker's run role.
 */
@Injectable()
export class ActionsService {
  constructor(
    @Inject(DRIZZLE) private readonly db: Db,
    private readonly publisher: PublisherService,
  ) {}

  /** 404 (not 403) for other users' projects — same rule as the project routes (AGENTS). */
  private async ownedProject(projectId: string, userId: string) {
    const [row] = await this.db
      .select({ id: projects.id, nodeId: projects.nodeId })
      .from(projects)
      .where(and(eq(projects.id, projectId), eq(projects.userId, userId)))
      .limit(1);
    if (!row) throw new NotFoundException('project not found');
    return row;
  }

  async request(projectId: string, userId: string, type: ActionType) {
    const project = await this.ownedProject(projectId, userId);
    return this.queue(project, userId, type);
  }

  /** Plan §5: `POST /admin/projects/:id/stop` — stop any project (roles guard checked the admin). */
  async adminStop(projectId: string, adminId: string) {
    const [project] = await this.db
      .select({ id: projects.id, nodeId: projects.nodeId })
      .from(projects)
      .where(eq(projects.id, projectId))
      .limit(1);
    if (!project) throw new NotFoundException('project not found');
    return this.queue(project, adminId, 'stop');
  }

  private async queue(project: { id: string; nodeId: string }, requestedBy: string, type: ActionType) {
    // A stop racing a deploy would only wait on the worker's project lock and
    // then bounce through the retry ladder — refuse at the door instead (409,
    // same rule as upload).
    const [active] = await this.db
      .select({ id: deployments.id })
      .from(deployments)
      .where(
        and(eq(deployments.projectId, project.id), inArray(deployments.status, [...ACTIVE_DEPLOYMENT_STATUSES])),
      )
      .limit(1);
    if (active) throw new ConflictException('a deployment is already queued or in progress');

    // One open action per (project, type): the partial unique index in the DB
    // is the arbiter — onConflictDoNothing turns its violation into "no row".
    const [action] = await this.db
      .insert(projectActions)
      .values({ projectId: project.id, type, requestedBy })
      .onConflictDoNothing()
      .returning();
    if (!action) throw new ConflictException(`a ${type} action is already queued`);

    try {
      // Messages carry ids only; routing key = the project's node (plan §8).
      await this.publisher.publishAction(action.id, project.nodeId);
    } catch (err) {
      // Compensating write (same pattern as upload): the row above is durable,
      // so record the queue failure before telling the client.
      const message = err instanceof Error ? err.message : String(err);
      await this.db
        .update(projectActions)
        .set({ status: 'failed', error: `publish failed: ${message}`, finishedAt: new Date() })
        .where(eq(projectActions.id, action.id));
      throw new BadGatewayException(`could not queue the ${type} job`);
    }
    return action;
  }

  async list(projectId: string, userId: string, query: PageQueryDto) {
    await this.ownedProject(projectId, userId);
    const limit = query.limit ?? 20;
    const offset = query.offset ?? 0;
    const where = eq(projectActions.projectId, projectId);
    const [items, [{ count }]] = await Promise.all([
      this.db
        .select()
        .from(projectActions)
        .where(where)
        .orderBy(desc(projectActions.createdAt), desc(projectActions.id)) // id: deterministic tie-break
        .limit(limit)
        .offset(offset),
      this.db.select({ count: sql<number>`count(*)` }).from(projectActions).where(where),
    ]);
    return { items, total: Number(count) };
  }
}
