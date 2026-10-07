CREATE TYPE "public"."action_status" AS ENUM('queued', 'running', 'done', 'failed');--> statement-breakpoint
CREATE TYPE "public"."action_type" AS ENUM('stop', 'start', 'restart');--> statement-breakpoint
CREATE TYPE "public"."deployment_kind" AS ENUM('upload', 'redeploy');--> statement-breakpoint
CREATE TYPE "public"."deployment_status" AS ENUM('queued', 'building', 'built', 'starting', 'live', 'failed', 'superseded');--> statement-breakpoint
CREATE TABLE "deployments" (
	"id" uuid PRIMARY KEY DEFAULT gen_random_uuid() NOT NULL,
	"project_id" uuid NOT NULL,
	"source_id" uuid NOT NULL,
	"kind" "deployment_kind" DEFAULT 'upload' NOT NULL,
	"status" "deployment_status" DEFAULT 'queued' NOT NULL,
	"image_ref" varchar(255),
	"container_name" varchar(128),
	"error" text,
	"attempt" integer DEFAULT 0 NOT NULL,
	"worker_id" varchar(255),
	"heartbeat_at" timestamp with time zone,
	"triggered_by" uuid,
	"created_at" timestamp with time zone DEFAULT now() NOT NULL,
	"started_at" timestamp with time zone,
	"finished_at" timestamp with time zone
);
--> statement-breakpoint
CREATE TABLE "project_actions" (
	"id" uuid PRIMARY KEY DEFAULT gen_random_uuid() NOT NULL,
	"project_id" uuid NOT NULL,
	"type" "action_type" NOT NULL,
	"status" "action_status" DEFAULT 'queued' NOT NULL,
	"error" text,
	"requested_by" uuid,
	"created_at" timestamp with time zone DEFAULT now() NOT NULL,
	"finished_at" timestamp with time zone
);
--> statement-breakpoint
CREATE TABLE "sources" (
	"id" uuid PRIMARY KEY DEFAULT gen_random_uuid() NOT NULL,
	"project_id" uuid NOT NULL,
	"path" text NOT NULL,
	"size_bytes" integer NOT NULL,
	"sha256" varchar(64) NOT NULL,
	"original_filename" varchar(255) NOT NULL,
	"created_at" timestamp with time zone DEFAULT now() NOT NULL,
	"deleted_at" timestamp with time zone
);
--> statement-breakpoint
ALTER TABLE "deployments" ADD CONSTRAINT "deployments_project_id_projects_id_fk" FOREIGN KEY ("project_id") REFERENCES "public"."projects"("id") ON DELETE no action ON UPDATE no action;--> statement-breakpoint
ALTER TABLE "deployments" ADD CONSTRAINT "deployments_source_id_sources_id_fk" FOREIGN KEY ("source_id") REFERENCES "public"."sources"("id") ON DELETE no action ON UPDATE no action;--> statement-breakpoint
ALTER TABLE "project_actions" ADD CONSTRAINT "project_actions_project_id_projects_id_fk" FOREIGN KEY ("project_id") REFERENCES "public"."projects"("id") ON DELETE no action ON UPDATE no action;--> statement-breakpoint
ALTER TABLE "sources" ADD CONSTRAINT "sources_project_id_projects_id_fk" FOREIGN KEY ("project_id") REFERENCES "public"."projects"("id") ON DELETE no action ON UPDATE no action;--> statement-breakpoint
CREATE INDEX "deployments_project_idx" ON "deployments" USING btree ("project_id");--> statement-breakpoint
CREATE INDEX "project_actions_project_idx" ON "project_actions" USING btree ("project_id");--> statement-breakpoint
CREATE UNIQUE INDEX "project_actions_open_idx" ON "project_actions" USING btree ("project_id","type") WHERE status in ('queued', 'running');--> statement-breakpoint
ALTER TABLE "projects" ADD CONSTRAINT "projects_current_deployment_id_deployments_id_fk" FOREIGN KEY ("current_deployment_id") REFERENCES "public"."deployments"("id") ON DELETE no action ON UPDATE no action;