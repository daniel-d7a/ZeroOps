CREATE TYPE "public"."desired_state" AS ENUM('running', 'stopped');--> statement-breakpoint
CREATE TYPE "public"."project_status" AS ENUM('new', 'deploying', 'running', 'stopped', 'failed');--> statement-breakpoint
CREATE TYPE "public"."project_type" AS ENUM('node');--> statement-breakpoint
CREATE TABLE "projects" (
	"id" uuid PRIMARY KEY DEFAULT gen_random_uuid() NOT NULL,
	"user_id" uuid NOT NULL,
	"name" varchar(100) NOT NULL,
	"slug" varchar(63) NOT NULL,
	"type" "project_type" DEFAULT 'node' NOT NULL,
	"wants_db" boolean DEFAULT false NOT NULL,
	"node_id" varchar(63) DEFAULT 'node-1' NOT NULL,
	"desired_state" "desired_state" DEFAULT 'running' NOT NULL,
	"status" "project_status" DEFAULT 'new' NOT NULL,
	"current_deployment_id" uuid,
	"created_at" timestamp with time zone DEFAULT now() NOT NULL,
	CONSTRAINT "projects_slug_unique" UNIQUE("slug")
);
--> statement-breakpoint
ALTER TABLE "projects" ADD CONSTRAINT "projects_user_id_users_id_fk" FOREIGN KEY ("user_id") REFERENCES "public"."users"("id") ON DELETE no action ON UPDATE no action;