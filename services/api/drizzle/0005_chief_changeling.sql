CREATE TABLE "deploy_logs" (
	"id" bigserial PRIMARY KEY NOT NULL,
	"deployment_id" uuid NOT NULL,
	"stream" text NOT NULL,
	"line" text NOT NULL,
	"created_at" timestamp with time zone DEFAULT now() NOT NULL
);
--> statement-breakpoint
ALTER TABLE "deploy_logs" ADD CONSTRAINT "deploy_logs_deployment_id_deployments_id_fk" FOREIGN KEY ("deployment_id") REFERENCES "public"."deployments"("id") ON DELETE no action ON UPDATE no action;--> statement-breakpoint
CREATE INDEX "deploy_logs_deployment_idx" ON "deploy_logs" USING btree ("deployment_id","id");