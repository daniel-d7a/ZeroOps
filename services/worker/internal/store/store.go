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
	"strings"
	"time"
	"unicode/utf8"

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

// MarkBuilt finalizes the build phase (status → built) and records the image
// reference the run role will start (plan §8.1 step 4: image_ref is written
// only here, by the worker that built it — AGENTS invariant). The WHERE clause
// is the CAS: it only matches if we still hold the claim. ok=false means the
// claim was stolen (our heartbeat went stale) and a different worker owns the
// job now — report that instead of writing over their state; the caller acks
// and skips (the stealer is doing the work).
func (s *Store) MarkBuilt(ctx context.Context, deploymentID, workerID, imageRef string) (ok bool, err error) {
	result, err := s.pool.Exec(ctx, `
		UPDATE deployments SET status = 'built', image_ref = $3
		WHERE id = $1 AND status = 'building' AND worker_id = $2`,
		deploymentID, workerID, imageRef)
	if err != nil {
		return false, fmt.Errorf("mark built: %w", err)
	}
	if result.RowsAffected() == 1 {
		// Job end: cap the retained log tail (FIXLIST 1.2). Best-effort —
		// PruneLogs documents why a prune failure must not fail the build.
		_ = s.PruneLogs(ctx, deploymentID)
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

// LoadDeployment returns everything the run role needs to place a deployment
// (plan §8.2): the project id, the project's slug (builds the public URL,
// plan §8.4), wants_db (whether to provision/inject a database, §8.5) and the
// image reference the build role stored — image_ref is read only from here
// (AGENTS invariant). found=false means the row is gone.
func (s *Store) LoadDeployment(ctx context.Context, deploymentID string) (projectID, slug, imageRef string, wantsDb bool, found bool, err error) {
	err = s.pool.QueryRow(ctx, `
		SELECT d.project_id, p.slug, p.wants_db, COALESCE(d.image_ref, '')
		FROM deployments d
		JOIN projects p ON p.id = d.project_id
		WHERE d.id = $1`, deploymentID).Scan(&projectID, &slug, &wantsDb, &imageRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", "", false, false, nil
	}
	if err != nil {
		return "", "", "", false, false, fmt.Errorf("read deployment: %w", err)
	}
	return projectID, slug, imageRef, wantsDb, true, nil
}

// SourceForDeployment returns the owning project id plus the uploaded zip's
// storage path (relative to UPLOADS_DIR) and sha256 — plan §8.1 step 2.
// ok=false means the deployment has no source row or the source was pruned by
// retention; the build treats that as permanent (nothing to rebuild from).
func (s *Store) SourceForDeployment(ctx context.Context, deploymentID string) (projectID, zipPath, checksum string, ok bool, err error) {
	err = s.pool.QueryRow(ctx, `
		SELECT d.project_id, src.path, src.sha256
		FROM deployments d
		JOIN sources src ON src.id = d.source_id
		WHERE d.id = $1 AND src.deleted_at IS NULL`, deploymentID).Scan(&projectID, &zipPath, &checksum)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", "", false, nil
	}
	if err != nil {
		return "", "", "", false, fmt.Errorf("read source: %w", err)
	}
	return projectID, zipPath, checksum, true, nil
}

// AppendLogs inserts one batch of log lines for a deployment (plan §8.1 step
// 4 streams railpack output into deploy_logs, batched ~once per second —
// batching keeps a chatty build from turning into thousands of round trips).
// lines must be non-empty; an empty batch is a no-op.
//
// Curation (FIXLIST 1.2, provider-style): every line is clamped here — this
// is the single funnel for all log inserts, so a pathological multi-MB railpack
// line can never reach the DB. The retained tail is pruned at job end
// (PruneLogs via MarkBuilt/MarkFailed); during the build the table may hold
// more, bounded by build length (max 10 min, plan §8.1).
func (s *Store) AppendLogs(ctx context.Context, deploymentID, stream string, lines []string) error {
	if len(lines) == 0 {
		return nil
	}
	clamped := make([]string, len(lines))
	for i, line := range lines {
		if len(line) > maxLogLineBytes {
			// Clamping on a byte boundary can split a UTF-8 rune; ToValidUTF8
			// replaces the stray bytes, so the INSERT never rejects bad text.
			clamped[i] = strings.ToValidUTF8(line[:maxLogLineBytes], string(utf8.RuneError))
		} else {
			clamped[i] = line
		}
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO deploy_logs (deployment_id, stream, line)
		SELECT $1, $2, unnest($3::text[])`, deploymentID, stream, clamped)
	if err != nil {
		return fmt.Errorf("append logs: %w", err)
	}
	return nil
}

// maxLogLineBytes bounds one deploy_logs line: 2 KB covers every sane build
// line (npm diagnostics, stack traces) and caps DB growth per line.
const maxLogLineBytes = 2048

// maxLogLines is the retained tail per deployment (FIXLIST 1.2): anything
// older is dropped at job end — never read by anything (the API cursor reads
// forward from what the UI saw), so pruning only ever shrinks dead weight.
const maxLogLines = 2000

// PruneLogs keeps only the newest maxLogLines lines of one deployment
// (id is a monotonic bigserial PK — see schema.ts). Best-effort by design:
// call sites run at job end where the status write already committed, and a
// log-prune failure must never fail a build. Idempotent.
func (s *Store) PruneLogs(ctx context.Context, deploymentID string) error {
	_, err := s.pool.Exec(ctx, `
		DELETE FROM deploy_logs d
		WHERE d.deployment_id = $1
		  AND d.id NOT IN (
			  SELECT id FROM deploy_logs
			  WHERE deployment_id = $1
			  ORDER BY id DESC
			  LIMIT $2)`, deploymentID, maxLogLines)
	if err != nil {
		return fmt.Errorf("prune logs: %w", err)
	}
	return nil
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
// can be in. Idempotent on 0 rows (already terminal). At job end the retained
// log tail is capped (best-effort, FIXLIST 1.2).
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
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit mark failed: %w", err)
	}
	// Prune after the final commit, best-effort (FIXLIST 1.2): the row is
	// already terminal, so a prune failure has no state to corrupt.
	_ = s.PruneLogs(ctx, deploymentID)
	return nil
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
//
// supersededImage is the previous live deployment's image_ref ("" on a first
// deploy) — the caller needs it to prune old images keeping live + one before
// (plan §8.2 step 6).
func (s *Store) MarkLive(ctx context.Context, deploymentID, projectID, workerID string) (ok bool, supersededImage string, err error) {
	transaction, err := s.pool.Begin(ctx)
	if err != nil {
		return false, "", fmt.Errorf("begin mark live: %w", err)
	}
	defer transaction.Rollback(ctx)

	result, err := transaction.Exec(ctx, `
		UPDATE deployments SET status = 'live', finished_at = now()
		WHERE id = $1 AND status = 'starting' AND worker_id = $2`,
		deploymentID, workerID)
	if err != nil {
		return false, "", fmt.Errorf("mark live: %w", err)
	}
	if result.RowsAffected() == 0 {
		return false, "", nil // claim lost — tx still empty, rolls back
	}
	// UPDATE ... RETURNING: 0 rows (first deploy) reads as ErrNoRows.
	err = transaction.QueryRow(ctx, `
		UPDATE deployments SET status = 'superseded'
		WHERE project_id = $1 AND status = 'live' AND id <> $2
		RETURNING image_ref`,
		projectID, deploymentID).Scan(&supersededImage)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, "", fmt.Errorf("supersede previous live: %w", err)
	}
	if _, err := transaction.Exec(ctx, `
		UPDATE projects SET current_deployment_id = $1, status = 'running', desired_state = 'running'
		WHERE id = $2`,
		deploymentID, projectID); err != nil {
		return false, "", fmt.Errorf("update project: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return false, "", fmt.Errorf("commit mark live: %w", err)
	}
	return true, supersededImage, nil
}

// --- project actions (plan §8.6: stop / start / restart) ---------------------

// ActionInfo is everything the run handler needs to execute one action: the
// message carries only the action id, so the slug, wants_db flag and the
// current deployment's image ride along here — start/restart can rewrite the
// route, rebuild a missing container and re-join student-db without further
// queries.
type ActionInfo struct {
	ID           string
	ProjectID    string
	Type         string // stop | start | restart
	Status       string // queued | running | done | failed
	Slug         string
	WantsDB      bool
	DeploymentID string // projects.current_deployment_id ("" if never deployed)
	ImageRef     string // its image ("" if none)
}

// LoadAction loads the action plus its project context. found=false: the row
// is gone (deleted project), nothing to do.
func (s *Store) LoadAction(ctx context.Context, actionID string) (ActionInfo, bool, error) {
	var info ActionInfo
	err := s.pool.QueryRow(ctx, `
		SELECT a.id, a.project_id, a.type, a.status, p.slug, p.wants_db,
		       COALESCE(p.current_deployment_id::text, ''),
		       COALESCE(d.image_ref, '')
		FROM project_actions a
		JOIN projects p ON p.id = a.project_id
		LEFT JOIN deployments d ON d.id = p.current_deployment_id
		WHERE a.id = $1`, actionID).Scan(
		&info.ID, &info.ProjectID, &info.Type, &info.Status, &info.Slug, &info.WantsDB,
		&info.DeploymentID, &info.ImageRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return ActionInfo{}, false, nil
	}
	if err != nil {
		return ActionInfo{}, false, fmt.Errorf("load action: %w", err)
	}
	return info, true, nil
}

// ClaimAction moves queued → running. Unlike deployments, actions have no
// worker/heartbeat columns (plan §8.6): they are short-lived, so a claim
// lost to a duplicate delivery is simply skipped — the owner finishes it.
// The one rescue for an orphaned claim lives in the run role: a redelivered
// (requeued-after-death) message may re-execute a `running` action, so the
// row can always reach a terminal state.
func (s *Store) ClaimAction(ctx context.Context, actionID string) (claimed bool, status string, err error) {
	result, err := s.pool.Exec(ctx, `
		UPDATE project_actions SET status = 'running'
		WHERE id = $1 AND status = 'queued'`, actionID)
	if err != nil {
		return false, "", fmt.Errorf("claim action: %w", err)
	}
	if result.RowsAffected() == 1 {
		return true, "", nil
	}
	if err := s.pool.QueryRow(ctx, `SELECT status FROM project_actions WHERE id = $1`, actionID).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, "", nil
		}
		return false, "", fmt.Errorf("read action status: %w", err)
	}
	return false, status, nil
}

// ReleaseAction hands a claim back after a transient failure (running →
// queued) so the retried message can re-claim — same wedge-prevention idea
// as ReleaseClaim, without the worker column.
func (s *Store) ReleaseAction(ctx context.Context, actionID string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE project_actions SET status = 'queued'
		WHERE id = $1 AND status = 'running'`, actionID)
	if err != nil {
		return fmt.Errorf("release action: %w", err)
	}
	return nil
}

// FinishAction records the outcome (plan §8.6 last step). CAS on "open"
// states: the write only applies while nobody else has terminalized the row,
// and is a no-op when it is already done/failed. queued is included so that
// a give-up before any claim (DB was down for the whole ladder) can still
// fail the row instead of leaving it queued forever.
func (s *Store) FinishAction(ctx context.Context, actionID, status, failure string) error {
	if status != "done" && status != "failed" {
		return fmt.Errorf("finish action: unexpected status %q", status)
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE project_actions SET status = $2, error = NULLIF($3, ''), finished_at = now()
		WHERE id = $1 AND status IN ('queued', 'running')`, actionID, status, failure)
	if err != nil {
		return fmt.Errorf("finish action: %w", err)
	}
	return nil
}

// SetProjectState applies the plan §8.6 bookkeeping after a successful
// stop/start/restart: desired_state is what the user asked for, status is
// what the project is now.
func (s *Store) SetProjectState(ctx context.Context, projectID, desired, status string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE projects SET desired_state = $2, status = $3 WHERE id = $1`,
		projectID, desired, status)
	if err != nil {
		return fmt.Errorf("set project state: %w", err)
	}
	return nil
}

// --- startup reconcile (plan §8.7) -----------------------------------------

// ReconcileCandidate is one project this node should check after a restart:
// the project may still want to run, but its container may be gone.
// OpenAction is true when any queued/running action already exists — the
// reconciler must not add another one.
type ReconcileCandidate struct {
	ProjectID    string
	DeploymentID string // current_deployment_id ("" if never deployed)
	OpenAction   bool
}

// ReconcileCandidates lists the desired-running projects assigned to this
// node. The caller checks Docker state; SQL only answers "what should run?".
func (s *Store) ReconcileCandidates(ctx context.Context, nodeID string) ([]ReconcileCandidate, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT p.id::text,
		       COALESCE(p.current_deployment_id::text, ''),
		       EXISTS (
		           SELECT 1 FROM project_actions a
		           WHERE a.project_id = p.id AND a.status IN ('queued', 'running'))
		FROM projects p
		WHERE p.node_id = $1 AND p.desired_state = 'running'
		ORDER BY p.created_at, p.id`, nodeID)
	if err != nil {
		return nil, fmt.Errorf("list reconcile candidates: %w", err)
	}
	defer rows.Close()
	var out []ReconcileCandidate
	for rows.Next() {
		var c ReconcileCandidate
		if err := rows.Scan(&c.ProjectID, &c.DeploymentID, &c.OpenAction); err != nil {
			return nil, fmt.Errorf("scan reconcile candidate: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// QueueStartAction inserts the start action for a project unless an open
// action already exists (plan §8.7: skipped if an open one exists). The
// partial unique index also blocks a duplicate open start under a race, so
// ON CONFLICT is the second guard. created=false means another request won.
func (s *Store) QueueStartAction(ctx context.Context, projectID string) (actionID string, created bool, err error) {
	err = s.pool.QueryRow(ctx, `
		INSERT INTO project_actions (project_id, type, status)
		SELECT $1, 'start', 'queued'
		WHERE NOT EXISTS (
		    SELECT 1 FROM project_actions
		    WHERE project_id = $1 AND status IN ('queued', 'running'))
		ON CONFLICT DO NOTHING
		RETURNING id::text`, projectID).Scan(&actionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("queue start action: %w", err)
	}
	return actionID, true, nil
}

// EnvVar is one env_vars row with the value still encrypted — decryption is
// the run role's job (plan §8.2 step 3); this package never sees plaintext.
type EnvVar struct {
	Key            string
	ValueEncrypted string
}

// EnvVars returns the project's environment variables (plan §8.2 step 3),
// key-ordered for deterministic env slices.
func (s *Store) EnvVars(ctx context.Context, projectID string) ([]EnvVar, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT key, value_encrypted FROM env_vars
		WHERE project_id = $1 ORDER BY key`, projectID)
	if err != nil {
		return nil, fmt.Errorf("load env vars: %w", err)
	}
	defer rows.Close()
	var out []EnvVar
	for rows.Next() {
		var v EnvVar
		if err := rows.Scan(&v.Key, &v.ValueEncrypted); err != nil {
			return nil, fmt.Errorf("scan env var: %w", err)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// --- project databases (plan §8.5) -----------------------------------------

// DatabaseInfo is one project_databases row — everything the provision step
// needs. Status gates the flow: 'provisioning' means DDL still to run (a
// previous attempt died mid-way, or we just inserted it), 'ready' means the
// role and database exist and later deploys skip the DDL entirely.
type DatabaseInfo struct {
	ProjectID         string
	DbName            string // validated p_<project uuid no dashes>
	DbUser            string
	PasswordEncrypted string // AES-256-GCM, decrypt with ENCRYPTION_KEY
	Status            string
}

// EnsureDatabaseRow inserts the provisioning row if absent (plan §8.5 step 1:
// the row with an encrypted password exists BEFORE anything touches
// student-db) and always returns the stored row — ON CONFLICT DO NOTHING
// handles the race, RETURNING handles the fresh insert, so both paths end in
// one query. The caller then uses row.PasswordEncrypted, never its own
// freshly generated password: if the insert lost the race, the winner's
// password is the one student-db will end up with.
func (s *Store) EnsureDatabaseRow(ctx context.Context, projectID, dbName, dbUser, passwordEncrypted string) (DatabaseInfo, error) {
	var status string
	err := s.pool.QueryRow(ctx, `
		INSERT INTO project_databases (project_id, db_name, db_user, password_encrypted)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (project_id) DO NOTHING
		RETURNING db_name, db_user, password_encrypted, status`,
		projectID, dbName, dbUser, passwordEncrypted).Scan(
		&dbName, &dbUser, &passwordEncrypted, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		// Row already exists (prior attempt): read it back — its password is
		// the one stored in student-db, so re-running DDL must use it.
		return s.LoadDatabase(ctx, projectID)
	}
	if err != nil {
		return DatabaseInfo{}, fmt.Errorf("ensure database row: %w", err)
	}
	return DatabaseInfo{ProjectID: projectID, DbName: dbName, DbUser: dbUser, PasswordEncrypted: passwordEncrypted, Status: status}, nil
}

// LoadDatabase reads the project's database row — the run role needs it to
// build DATABASE_URL (plan §8.2 step 4).
func (s *Store) LoadDatabase(ctx context.Context, projectID string) (DatabaseInfo, error) {
	var info DatabaseInfo
	err := s.pool.QueryRow(ctx, `
		SELECT project_id::text, db_name, db_user, password_encrypted, status
		FROM project_databases WHERE project_id = $1`, projectID).Scan(
		&info.ProjectID, &info.DbName, &info.DbUser, &info.PasswordEncrypted, &info.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return DatabaseInfo{}, fmt.Errorf("database row disappeared: %s", projectID)
	}
	if err != nil {
		return DatabaseInfo{}, fmt.Errorf("load database row: %w", err)
	}
	return info, nil
}

// MarkDatabaseReady flips provisioning → ready after the DDL succeeded
// (plan §8.5 last step). CAS on 'provisioning': a ready row stays ready.
func (s *Store) MarkDatabaseReady(ctx context.Context, projectID string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE project_databases SET status = 'ready'
		WHERE project_id = $1 AND status = 'provisioning'`, projectID)
	if err != nil {
		return fmt.Errorf("mark database ready: %w", err)
	}
	return nil
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
