import { boolean, index, jsonb, pgEnum, pgTable, text, timestamp, uuid, varchar } from 'drizzle-orm/pg-core';

export const userRole = pgEnum('user_role', ['user', 'admin']);
export const userStatus = pgEnum('user_status', ['active', 'disabled']);
export const projectType = pgEnum('project_type', ['node']);
export const desiredState = pgEnum('desired_state', ['running', 'stopped']);
export const projectStatus = pgEnum('project_status', ['new', 'deploying', 'running', 'stopped', 'failed']);

export type UserRole = (typeof userRole.enumValues)[number];
export type UserStatus = (typeof userStatus.enumValues)[number];
export type ProjectStatus = (typeof projectStatus.enumValues)[number];
export type DesiredState = (typeof desiredState.enumValues)[number];

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

// current_deployment_id: plain uuid for now — deployments table arrives with M2
// (add the FK there). type is a single-value enum = the future project-type seam.
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
  currentDeploymentId: uuid('current_deployment_id'),
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
