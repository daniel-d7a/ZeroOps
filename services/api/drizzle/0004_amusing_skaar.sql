CREATE TABLE "workers" (
	"id" varchar(255) PRIMARY KEY NOT NULL,
	"roles" text[] NOT NULL,
	"node_id" varchar(63) NOT NULL,
	"started_at" timestamp with time zone DEFAULT now() NOT NULL,
	"last_seen_at" timestamp with time zone DEFAULT now() NOT NULL
);
