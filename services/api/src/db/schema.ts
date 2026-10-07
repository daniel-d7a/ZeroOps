import { sql } from 'drizzle-orm';
import { boolean, index, integer, jsonb, pgEnum, pgTable, text, timestamp, uniqueIndex, uuid, varchar, type AnyPgColumn } from 'drizzle-orm/pg-core';

export const userRole = pgEnum('user_role', ['user', 'admin']);
export const userStatus = pgEnum('user_status', ['active', 'disabled']);
export const projectType = pgEnum('project_type', ['node']);
export const desiredState = pgEnum('desired_state', ['running', 'stopped']);
export const projectStatus = pgEnum('project_status', ['new', 'deploying', 'running', 'stopped', 'failed']);
export const deploymentStatus = pgEnum('deployment_status', [
  'queued',
  'building',
  'built',
  'starting',
  'live',
  'failed',
  'superseded',
]);
export const deploymentKind = pgEnum('deployment_kind', ['upload', 'redeploy']);
export const actionType = pgEnum('action_type', ['stop', 'start', 'restart']);
export const actionStatus = pgEnum('action_status', ['queued', 'running', 'done', 'failed']);

export type UserRole = (typeof userRole.enumValues)[number];
export type UserStatus = (typeof userStatus.enumValues)[number];
export type ProjectStatus = (typeof projectStatus.enumValues)[number];
export type DesiredState = (typeof desiredState.enumValues)[number];
export type DeploymentStatus = (typeof deploymentStatus.enumValues)[number];
export type DeploymentKind = (typeof deploymentKind.enumValues)[number];
export type ActionType = (typeof actionType.enumValues)[number];
export type ActionStatus = (typeof actionStatus.enumValues)[number];

export const users = pgTable('users', {
  id: uuid('id').defaultRandom().primaryKey(),
  email: varchar('email', { length: 254 }).notNull().unique(),
  passwordHash: text('password_hash').notNull(),
  role: userRole('role').notNull().default('user'),
  status: userStatus('status').notNull().default('active'),
  createdAt: timestamp('created_at', { withTimezone: true }).notNull().defaultNow(),
});

/** Shared projection: the columns a response may expose (never password_hash). */
export const publicUserColumns = {
  id: users.id,
  email: users.email,
  role: users.role,
  status: users.status,
  createdAt: users.createdAt,
};

// type is a single-value enum = the future project-type seam.
export const projects = pgTable('projects', {
  id: uuid('id').defaultRandom().primaryKey(),
  userId: uuid('user_id')
    .notNull()
    .references(() => users.id),
  name: varchar('name', { length: 100 }).notNull(),
  slug: varchar('slug', { length: 63 }).notNull().unique(),
  type: projectType('type').notNull().default('node'),
  wantsDb: boolean('wants_db').notNull().default(false),
  nodeId: varchar('node_id', { length: 63 }).notNull().default('node-1'),
  desiredState: desiredState('desired_state').notNull().default('running'),
  status: projectStatus('status').notNull().default('new'),
  currentDeploymentId: uuid('current_deployment_id').references((): AnyPgColumn => deployments.id),
  createdAt: timestamp('created_at', { withTimezone: true }).notNull().defaultNow(),
});

/** Shared projection for project responses (url is appended by the service). */
export const publicProjectColumns = {
  id: projects.id,
  name: projects.name,
  slug: projects.slug,
  type: projects.type,
  wantsDb: projects.wantsDb,
  desiredState: projects.desiredState,
  status: projects.status,
  currentDeploymentId: projects.currentDeploymentId,
  createdAt: projects.createdAt,
};

/** Saved zip uploads (plan §5). `path` is internal — never exposed. */
export const sources = pgTable('sources', {
  id: uuid('id').defaultRandom().primaryKey(),
  projectId: uuid('project_id')
    .notNull()
    .references(() => projects.id),
  path: text('path').notNull(),
  sizeBytes: integer('size_bytes').notNull(),
  sha256: varchar('sha256', { length: 64 }).notNull(),
  originalFilename: varchar('original_filename', { length: 255 }).notNull(),
  createdAt: timestamp('created_at', { withTimezone: true }).notNull().defaultNow(),
  deletedAt: timestamp('deleted_at', { withTimezone: true }),
});

export const publicSourceColumns = {
  id: sources.id,
  projectId: sources.projectId,
  sizeBytes: sources.sizeBytes,
  sha256: sources.sha256,
  originalFilename: sources.originalFilename,
  createdAt: sources.createdAt,
  deletedAt: sources.deletedAt,
};

/**
 * Deployment lifecycle: queued → building → built → starting → live,
 * or failed; a replaced live deployment becomes superseded.
 */
export const deployments = pgTable(
  'deployments',
  {
    id: uuid('id').defaultRandom().primaryKey(),
    projectId: uuid('project_id')
      .notNull()
      .references(() => projects.id),
    sourceId: uuid('source_id')
      .notNull()
      .references(() => sources.id),
    kind: deploymentKind('kind').notNull().default('upload'),
    status: deploymentStatus('status').notNull().default('queued'),
    imageRef: varchar('image_ref', { length: 255 }),
    containerName: varchar('container_name', { length: 128 }),
    error: text('error'),
    attempt: integer('attempt').notNull().default(0),
    workerId: varchar('worker_id', { length: 255 }),
    heartbeatAt: timestamp('heartbeat_at', { withTimezone: true }),
    triggeredBy: uuid('triggered_by'),
    createdAt: timestamp('created_at', { withTimezone: true }).notNull().defaultNow(),
    startedAt: timestamp('started_at', { withTimezone: true }),
    finishedAt: timestamp('finished_at', { withTimezone: true }),
  },
  (t) => [index('deployments_project_idx').on(t.projectId)],
);

export const publicDeploymentColumns = {
  id: deployments.id,
  projectId: deployments.projectId,
  sourceId: deployments.sourceId,
  kind: deployments.kind,
  status: deployments.status,
  error: deployments.error,
  attempt: deployments.attempt,
  triggeredBy: deployments.triggeredBy,
  createdAt: deployments.createdAt,
  startedAt: deployments.startedAt,
  finishedAt: deployments.finishedAt,
};

/** Stop/start/restart requests; at most one open action per (project, type). */
export const projectActions = pgTable(
  'project_actions',
  {
    id: uuid('id').defaultRandom().primaryKey(),
    projectId: uuid('project_id')
      .notNull()
      .references(() => projects.id),
    type: actionType('type').notNull(),
    status: actionStatus('status').notNull().default('queued'),
    error: text('error'),
    requestedBy: uuid('requested_by'),
    createdAt: timestamp('created_at', { withTimezone: true }).notNull().defaultNow(),
    finishedAt: timestamp('finished_at', { withTimezone: true }),
  },
  (t) => [
    index('project_actions_project_idx').on(t.projectId),
    uniqueIndex('project_actions_open_idx')
      .on(t.projectId, t.type)
      .where(sql`status in ('queued', 'running')`),
  ],
);

// express-session store (connect-pg-simple): sid/sess/expire column names are
// its API — do not rename.
export const sessions = pgTable(
  'sessions',
  {
    sid: varchar('sid', { length: 191 }).primaryKey(),
    sess: jsonb('sess').notNull(),
    expire: timestamp('expire', { withTimezone: true }).notNull(),
  },
  (t) => [index('sessions_expire_idx').on(t.expire)],
);
