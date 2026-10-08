// Package run implements the run role (plan §8.2 deploy; §8.6 actions —
// stop/start/restart — land with M4).
//
// Where the message came from: the build role finished a deployment and
// published {"type":"deploy"} to run.x, routed to this node's queue
// (run.<node>.q). This handler turns "built" into "live".
//
// M2 fakes the work: lock → claim → mark live (+ supersede + project running).
// M4 swaps in env loading, DB provisioning (§8.5), the replace-container
// routine (§8.3) and Traefik routing (§8.4) — between the claim and MarkLive.
//
// Failure classification is the build role's (plan §6): malformed → dead,
// transient → retry ×3 → failed + dead, permanent detectors arrive M4
// (health check failed, node at capacity, image missing).
package run

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	amqp091 "github.com/rabbitmq/amqp091-go"

	"zeroops/worker/internal/queue"
	"zeroops/worker/internal/store"
)

// Handler processes run jobs. Stateless like the build handler — replicas
// share one queue and the DB holds all state.
type Handler struct {
	Store    *store.Store
	Broker   *queue.Conn
	WorkerID string
	Logger   *slog.Logger
}

// jobMessage is the shared message envelope (plan §6): ids only.
type jobMessage struct {
	Type         string `json:"type"`
	DeploymentID string `json:"deployment_id"`
}

// Handle processes exactly one run delivery. Step order follows plan §8.2:
//
//  1. resolve project → 2. lock project → 3. claim built→starting
//     → 4. do the deploy (M2: fake) → 5. mark live (+ supersede + project) → ack
//
// Return contract: nil → main acks (finished / skipped / retried /
// dead-lettered — DB writes happen before the ack); non-nil → crash, message
// requeues.
func (handler *Handler) Handle(ctx context.Context, delivery amqp091.Delivery) error {
	var job jobMessage
	if err := json.Unmarshal(delivery.Body, &job); err != nil {
		return handler.dead(ctx, delivery, "malformed: "+err.Error())
	}
	if job.Type != "deploy" || job.DeploymentID == "" {
		return handler.dead(ctx, delivery, fmt.Sprintf("unexpected message type %q", job.Type))
	}
	logger := handler.Logger.With("deployment_id", job.DeploymentID)

	// Messages carry ids only — reload everything from the DB (AGENTS).
	projectID, found, err := handler.Store.LoadDeployment(ctx, job.DeploymentID)
	if err != nil {
		return handler.transient(ctx, delivery, job.DeploymentID, "", logger, err)
	}
	if !found {
		// Row gone (earlier permanent failure cleaned it up): nothing to do.
		logger.Warn("deployment not found, skipping")
		return nil
	}
	logger = logger.With("project_id", projectID)

	// --- per-project lock (plan §8) ---------------------------------------
	// Serializes jobs for the same project (a stop must not race a deploy).
	// Lock not acquired = transient (plan §6 error classes): the retry
	// exchange's 30 s TTL is also the natural backoff for lock contention —
	// a naive nack-requeue would hot-loop while the other job holds it.
	unlock, lockAcquired, err := handler.Store.TryProjectLock(ctx, projectID)
	if err != nil {
		return handler.transient(ctx, delivery, job.DeploymentID, "", logger, err)
	}
	if !lockAcquired {
		return handler.transient(ctx, delivery, job.DeploymentID, "", logger,
			fmt.Errorf("project lock busy"))
	}
	defer func() {
		if unlockErr := unlock(); unlockErr != nil {
			logger.Error("advisory unlock failed", "error", unlockErr)
		}
	}()

	// --- claim (atomic; stale-heartbeat steal built in) --------------------
	claimed, currentStatus, err := handler.Store.ClaimRun(ctx, job.DeploymentID, handler.WorkerID)
	if err != nil {
		// See build role: undo a possibly-applied claim so the retry can
		// claim afresh instead of bouncing off our own fresh heartbeat.
		if releaseErr := handler.Store.ReleaseClaim(ctx, job.DeploymentID, handler.WorkerID, "built"); releaseErr != nil {
			logger.Error("release after failed claim did not apply", "error", releaseErr)
		}
		return handler.transient(ctx, delivery, job.DeploymentID, "", logger, err)
	}
	if !claimed {
		// Duplicate or a sibling replica owns it — both are fine: ack, no work.
		logger.Info("deploy skipped", "status", currentStatus)
		return nil
	}
	logger.Info("deploy claimed")

	// --- the actual deploy (M2: fake) --------------------------------------
	// M4 inserts here: env vars + PORT, DB provisioning (§8.5), replace
	// container (§8.3), health probe, Traefik route (§8.4), image prune — all
	// while the claim holds. Permanent failures detected there (health check
	// failed, node at capacity, image missing) → MarkFailed + ack.
	live, err := handler.Store.MarkLive(ctx, job.DeploymentID, projectID, handler.WorkerID)
	if err != nil {
		// Claim held, status still 'starting' (tx rolled back) — release it.
		return handler.transient(ctx, delivery, job.DeploymentID, "built", logger, err)
	}
	if !live {
		logger.Info("claim lost, another worker took the job")
		return nil
	}
	logger.Info("deploy done: live")
	return nil
}

// transient mirrors the build role (plan §6): release the claim if held,
// publish to the retry exchange (or dead at MaxAttempts), record the failure
// in the DB on give-up, then let main ack.
func (handler *Handler) transient(ctx context.Context, delivery amqp091.Delivery, deploymentID, releaseTo string, logger *slog.Logger, cause error) error {
	if releaseTo != "" {
		if err := handler.Store.ReleaseClaim(ctx, deploymentID, handler.WorkerID, releaseTo); err != nil {
			return err
		}
	}
	gaveUp, err := handler.Broker.RetryOrGiveUp(ctx, "run", delivery, cause.Error())
	if err != nil {
		return err
	}
	if gaveUp {
		if err := handler.Store.MarkFailed(ctx, deploymentID, "deploy failed after retries: "+cause.Error()); err != nil {
			return err
		}
		logger.Error("gave up after retries, dead-lettered", "cause", cause)
		return nil
	}
	logger.Warn("transient error, retry scheduled", "cause", cause, "attempt", queue.ReadAttempt(delivery)+1)
	return nil
}

// dead parks an unprocessable message in run.dead.q (plan §6: inspection).
func (handler *Handler) dead(ctx context.Context, delivery amqp091.Delivery, reason string) error {
	if err := handler.Broker.PublishDead(ctx, "run", delivery, reason); err != nil {
		return err
	}
	handler.Logger.Warn("message dead-lettered", "reason", reason)
	return nil
}
