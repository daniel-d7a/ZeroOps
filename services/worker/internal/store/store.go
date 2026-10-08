// Package store owns ALL of the worker's SQL.
//
// AGENTS invariant: every query the worker runs lives in this package and
// nowhere else — that keeps the state machine auditable in one file and stops
// the API and the worker from writing the same transitions twice. The API
// writes only while handling requests (create, upload); the worker writes
// only while processing a job (claims, built/live, failures).
//
// Two patterns repeat through this file:
//
//   - Claim = an atomic UPDATE that doubles as a lock. The WHERE clause only
//     matches the state we're allowed to move from, so two replicas racing for
//     the same job can never both win (Postgres serializes the UPDATEs).
//     Claiming also stamps worker_id + heartbeat_at, so a dead replica's claim
//     can be stolen after the heartbeat goes stale (2 minutes).
//
//   - CAS (compare-and-swap) = the finish step repeats the same guard
//     (WHERE status='building' AND worker_id=$us). If someone else stole the
//     job while we worked, 0 rows match and we write nothing — stale work
//     never overwrites fresh state.
package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store wraps a pgx connection pool for the platform DB. pgxpool hands out
// pooled connections per query, so nothing here holds a connection open
// except TryProjectLock (which must — see below).
type Store struct {
	pool *pgxpool.Pool
}

// Open connects and pings so a broken DB URL fails at startup, not on the
// first job.
func Open(ctx context.Context, databaseURL string) (*Store, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("connect platform db: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping platform db: %w", err)
	}
	return &Store{pool: pool}, nil
}

// Close shuts the pool down (called on graceful shutdown).
func (s *Store) Close() {
	s.pool.Close()
}

// CheckSchema refuses to run against a schema older than this worker expects
// (plan §2, AGENTS invariant). The API owns migrations (drizzle); each applied
// migration is a row in drizzle.__drizzle_migrations, so counting rows is a
// cheap "how new is this schema" check. A too-old schema means the API hasn't
// migrated yet — running anyway would crash mid-job on a missing column.
func (s *Store) CheckSchema(ctx context.Context, requiredMigrations int) error {
	var appliedMigrations int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM drizzle.__drizzle_migrations`).Scan(&appliedMigrations); err != nil {
		return fmt.Errorf("read migration ledger: %w", err)
	}
	if appliedMigrations < requiredMigrations {
		return fmt.Errorf("schema too old: %d migrations applied, worker needs %d (run migrate)", appliedMigrations, requiredMigrations)
	}
	return nil
}

// ClaimBuild moves a deployment queued → building (plan §8.1), or steals a
// build whose heartbeat went stale (the previous worker died mid-job).
//
// Return values:
//   - claimed=true: we own the job now, do the work.
//   - claimed=false: someone else beat us or the job already moved on;
//     status carries the deployment's current status ("" if the row is gone)
//     so the caller can run the "already built → the deploy publish may have
//     been lost, republish it" repair path.
func (s *Store) ClaimBuild(ctx context.Context, deploymentID, workerID string) (claimed bool, status string, err error) {
	// COALESCE: started_at records when the deployment first started work and
	// must never be overwritten by a later steal.
	result, err := s.pool.Exec(ctx, `
		UPDATE deployments
		SET status = 'building', worker_id = $2, heartbeat_at = now(),
		    started_at = COALESCE(started_at, now())
		WHERE id = $1
		  AND (status = 'queued'
		       OR (status = 'building' AND heartbeat_at < now() - interval '2 minutes'))`,
		deploymentID, workerID)
	if err != nil {
		return false, "", fmt.Errorf("claim build: %w", err)
	}
	if result.RowsAffected() == 1 {
		return true, "", nil
	}
	// Claim failed — read why (needed to distinguish "duplicate delivery,
	// nothing to do" from "already built, republish the deploy job").
	if err := s.pool.QueryRow(ctx, `SELECT status FROM deployments WHERE id = $1`, deploymentID).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, "", nil
		}
		return false, "", fmt.Errorf("read deployment status: %w", err)
	}
	return false, status, nil
}

// MarkBuilt finalizes the build phase (status → built). The WHERE clause is
// the CAS: it only matches if we still hold the claim. ok=false means the
// claim was stolen (our heartbeat went stale) and a different worker owns the
// job now — report that instead of writing over their state; the caller acks
// and skips (the stealer is doing the work).
func (s *Store) MarkBuilt(ctx context.Context, deploymentID, workerID string) (ok bool, err error) {
	result, err := s.pool.Exec(ctx, `
		UPDATE deployments SET status = 'built'
		WHERE id = $1 AND status = 'building' AND worker_id = $2`,
		deploymentID, workerID)
	if err != nil {
		return false, fmt.Errorf("mark built: %w", err)
	}
	return result.RowsAffected() == 1, nil
}

// NodeID returns the owning project's node id — the routing key for run.x
// (each node has its own run queue, so jobs only go where they can run).
// found=false means the deployment row is gone.
func (s *Store) NodeID(ctx context.Context, deploymentID string) (string, bool, error) {
	var nodeID string
	err := s.pool.QueryRow(ctx, `
		SELECT p.node_id FROM deployments d
		JOIN projects p ON p.id = d.project_id
		WHERE d.id = $1`, deploymentID).Scan(&nodeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read node id: %w", err)
	}
	return nodeID, true, nil
}

// LoadDeployment returns the project a deployment belongs to — the run role
// needs the project id *before* it can take the per-project lock, and it must
// reload it from the DB (messages carry ids only, AGENTS invariant).
// found=false means the deployment row is gone.
func (s *Store) LoadDeployment(ctx context.Context, deploymentID string) (projectID string, found bool, err error) {
	err = s.pool.QueryRow(ctx, `SELECT project_id FROM deployments WHERE id = $1`, deploymentID).Scan(&projectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read deployment: %w", err)
	}
	return projectID, true, nil
}

// TryProjectLock takes the per-project advisory lock (plan §8) so two jobs for
// the same project (e.g. stop + deploy) can never run at the same time.
//
// How it works: pg_try_advisory_lock is a Postgres lock that lives on the
// *session* (the connection), not on a statement. Two consequences:
//
//  1. We must pin one pooled connection for the whole critical section —
//     hence pool.Acquire here and conn.Release inside the returned unlock.
//     Returning the connection while still holding the lock would leak the
//     lock into an unrelated future query.
//  2. ok=false means another job holds it. That's transient by design: the
//     caller requeues the message instead of failing (a short busy-wait beats
//     serializing the whole project queue behind one lock).
//
// The unlock runs on a background context on purpose: the lock must be
// released even when our work context was canceled by shutdown, otherwise the
// connection returns to the pool still holding it.
// ponytail: hashtext gives a 32-bit key — a collision merely serializes two
// unrelated projects (harmless); upgrade to a two-int key only if it shows.
func (s *Store) TryProjectLock(ctx context.Context, projectID string) (unlock func() error, ok bool, err error) {
	lockedConn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("acquire conn for lock: %w", err)
	}
	var lockAcquired bool
	if err := lockedConn.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtext($1)::bigint)`, projectID).Scan(&lockAcquired); err != nil {
		lockedConn.Release()
		return nil, false, fmt.Errorf("advisory lock: %w", err)
	}
	if !lockAcquired {
		// Busy: give the connection straight back, signal "retry later".
		lockedConn.Release()
		return nil, false, nil
	}
	unlock = func() error {
		defer lockedConn.Release() // release the pooled conn only after unlocking
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := lockedConn.Exec(unlockCtx, `SELECT pg_advisory_unlock(hashtext($1)::bigint)`, projectID)
		return err
	}
	return unlock, true, nil
}

// ClaimRun moves a deploy built → starting (plan §8.2), same claim pattern as
// ClaimBuild: atomic UPDATE, steal allowed only when the heartbeat is stale.
func (s *Store) ClaimRun(ctx context.Context, deploymentID, workerID string) (claimed bool, status string, err error) {
	result, err := s.pool.Exec(ctx, `
		UPDATE deployments
		SET status = 'starting', worker_id = $2, heartbeat_at = now(),
		    started_at = COALESCE(started_at, now())
		WHERE id = $1
		  AND (status = 'built'
		       OR (status = 'starting' AND heartbeat_at < now() - interval '2 minutes'))`,
		deploymentID, workerID)
	if err != nil {
		return false, "", fmt.Errorf("claim deploy: %w", err)
	}
	if result.RowsAffected() == 1 {
		return true, "", nil
	}
	if err := s.pool.QueryRow(ctx, `SELECT status FROM deployments WHERE id = $1`, deploymentID).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, "", nil
		}
		return false, "", fmt.Errorf("read deployment status: %w", err)
	}
	return false, status, nil
}

// ReleaseClaim hands a claim back after a transient failure: status returns
// to its pre-claim value, owner cleared, so the retried message can claim
// afresh. Without this, a retry arriving 30 s later would bounce off our own
// still-fresh heartbeat (claims steal only after 2 min) and be acked as
// "not mine" — the job would wedge.
//
// CAS on worker_id: only the current owner releases, and only from an
// in-progress state. releaseTo is caller-supplied ('queued' from the build
// claim, 'built' from the run claim) and validated against typos.
// ponytail: if this write itself fails (DB down), the retry path may still
// wedge for up to 2 min until the heartbeat goes stale — the §14 sweeper is
// the designed backstop for that residual window.
func (s *Store) ReleaseClaim(ctx context.Context, deploymentID, workerID, releaseTo string) error {
	if releaseTo != "queued" && releaseTo != "built" {
		return fmt.Errorf("release claim: unexpected target state %q", releaseTo)
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE deployments SET status = $3, worker_id = NULL, heartbeat_at = NULL
		WHERE id = $1 AND worker_id = $2 AND status IN ('building', 'starting')`,
		deploymentID, workerID, releaseTo)
	if err != nil {
		return fmt.Errorf("release claim: %w", err)
	}
	// 0 rows = we never held it (or already released): nothing to undo.
	return nil
}

// MarkFailed records the final failure (plan §6: permanent errors and
// exhausted retries) in one transaction — same shape as the API's publish
// compensation: the deployment fails with the cause, and the project only if
// it is still 'deploying' (CAS, so a failed redeploy cannot flip a project
// that a later deploy already moved to running).
// The guard is "any open state": correct because only the owning worker calls
// this while holding a fresh claim, and open states are exactly what a claim
// can be in. Idempotent on 0 rows (already terminal).
func (s *Store) MarkFailed(ctx context.Context, deploymentID, failure string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin mark failed: %w", err)
	}
	defer tx.Rollback(ctx)

	result, err := tx.Exec(ctx, `
		UPDATE deployments SET status = 'failed', error = $2, finished_at = now()
		WHERE id = $1 AND status IN ('queued', 'building', 'built', 'starting')`,
		deploymentID, failure)
	if err != nil {
		return fmt.Errorf("mark failed: %w", err)
	}
	if result.RowsAffected() == 0 {
		return nil // already terminal (live/superseded/failed)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE projects SET status = 'failed'
		WHERE id = (SELECT project_id FROM deployments WHERE id = $1) AND status = 'deploying'`,
		deploymentID); err != nil {
		return fmt.Errorf("fail project: %w", err)
	}
	return tx.Commit(ctx)
}

// MarkLive finishes a deploy in ONE transaction (plan §8.2 step 6). The three
// writes must be atomic because readers (API, console polling) would otherwise
// catch a half-applied switch — e.g. the new deployment live while the project
// still points at the old one:
//
//  1. our deployment starting → live          (CAS: only if we still hold the claim)
//  2. previous live deployment → superseded   (at most one live per project)
//  3. project → current_deployment_id + status/desired_state 'running'
//
// ok=false means the claim was stolen under us — the deferred Rollback undoes
// the empty work; the caller acks and skips (the stealer owns the job now).
// Rollback after Commit is a no-op, so one defer covers every error path.
func (s *Store) MarkLive(ctx context.Context, deploymentID, projectID, workerID string) (ok bool, err error) {
	transaction, err := s.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin mark live: %w", err)
	}
	defer transaction.Rollback(ctx)

	result, err := transaction.Exec(ctx, `
		UPDATE deployments SET status = 'live', finished_at = now()
		WHERE id = $1 AND status = 'starting' AND worker_id = $2`,
		deploymentID, workerID)
	if err != nil {
		return false, fmt.Errorf("mark live: %w", err)
	}
	if result.RowsAffected() == 0 {
		return false, nil // claim lost — tx still empty, rolls back
	}
	if _, err := transaction.Exec(ctx, `
		UPDATE deployments SET status = 'superseded'
		WHERE project_id = $1 AND status = 'live' AND id <> $2`,
		projectID, deploymentID); err != nil {
		return false, fmt.Errorf("supersede previous live: %w", err)
	}
	if _, err := transaction.Exec(ctx, `
		UPDATE projects SET current_deployment_id = $1, status = 'running', desired_state = 'running'
		WHERE id = $2`,
		deploymentID, projectID); err != nil {
		return false, fmt.Errorf("update project: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit mark live: %w", err)
	}
	return true, nil
}

// UpsertWorker refreshes this replica's row in workers (plan §8.7): it
// appears on first tick, then last_seen_at moves every 15 seconds.
// processStartedAt is the process start time in unix seconds — the same value
// on every tick, so a container restart correctly resets started_at instead of
// accumulating ticks. ON CONFLICT keeps it a single round trip.
func (s *Store) UpsertWorker(ctx context.Context, workerID string, roles []string, nodeID string, processStartedAt int64) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO workers (id, roles, node_id, started_at, last_seen_at)
		VALUES ($1, $2, $3, to_timestamp($4), now())
		ON CONFLICT (id) DO UPDATE
		SET roles = EXCLUDED.roles, node_id = EXCLUDED.node_id,
		    started_at = EXCLUDED.started_at, last_seen_at = now()`,
		workerID, roles, nodeID, processStartedAt)
	if err != nil {
		return fmt.Errorf("worker heartbeat: %w", err)
	}
	return nil
}
