CREATE TYPE "public"."database_status" AS ENUM('provisioning', 'ready');--> statement-breakpoint
CREATE TABLE "env_vars" (
	"id" uuid PRIMARY KEY DEFAULT gen_random_uuid() NOT NULL,
	"project_id" uuid NOT NULL,
	"key" varchar(128) NOT NULL,
	"value_encrypted" text NOT NULL,
	"created_at" timestamp with time zone DEFAULT now() NOT NULL,
	"updated_at" timestamp with time zone DEFAULT now() NOT NULL
);
--> statement-breakpoint
CREATE TABLE "project_databases" (
	"id" uuid PRIMARY KEY DEFAULT gen_random_uuid() NOT NULL,
	"project_id" uuid NOT NULL,
	"db_name" varchar(63) NOT NULL,
	"db_user" varchar(63) NOT NULL,
	"password_encrypted" text NOT NULL,
	"status" "database_status" DEFAULT 'provisioning' NOT NULL,
	"created_at" timestamp with time zone DEFAULT now() NOT NULL,
	CONSTRAINT "project_databases_project_id_unique" UNIQUE("project_id")
);
--> statement-breakpoint
ALTER TABLE "env_vars" ADD CONSTRAINT "env_vars_project_id_projects_id_fk" FOREIGN KEY ("project_id") REFERENCES "public"."projects"("id") ON DELETE no action ON UPDATE no action;--> statement-breakpoint
ALTER TABLE "project_databases" ADD CONSTRAINT "project_databases_project_id_projects_id_fk" FOREIGN KEY ("project_id") REFERENCES "public"."projects"("id") ON DELETE no action ON UPDATE no action;--> statement-breakpoint
CREATE UNIQUE INDEX "env_vars_project_key_idx" ON "env_vars" USING btree ("project_id","key");