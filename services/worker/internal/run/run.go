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
package run

import (
	"context"
	"encoding/json"
	"log/slog"

	amqp091 "github.com/rabbitmq/amqp091-go"

	"zeroops/worker/internal/store"
)

// Handler processes run jobs. Stateless like the build handler — replicas
// share one queue and the DB holds all state.
type Handler struct {
	Store    *store.Store
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
//  1. resolve project   → 2. lock project   → 3. claim built→starting
//     → 4. do the deploy (M2: fake) → 5. mark live (+ supersede + project)
//     → 6. ack
//
// Outcomes (contract in cmd/worker/main.go):
//   - (ack, no requeue): finished or safely skipped (duplicate/missing job).
//   - (no ack, requeue): transient — the project lock is busy; redeliver later.
//     Step 21 routes this through the retry exchange (TTL) instead of a hot
//     nack loop.
//   - (err): infrastructure failure — crash; unacked message requeues.
//
// ponytail: no in-job heartbeat ticker yet — the fake deploy takes <1s; add
// one together with the real 60s health probe (M4), plus the
// MAX_RUNNING_PER_NODE capacity check (§8.2 step 1).
func (handler *Handler) Handle(ctx context.Context, delivery amqp091.Delivery) (ack, requeue bool, err error) {
	var job jobMessage
	if err := json.Unmarshal(delivery.Body, &job); err != nil {
		handler.Logger.Warn("malformed message, skipping", "error", err)
		return true, false, nil
	}
	logger := handler.Logger.With("deployment_id", job.DeploymentID)
	if job.Type != "deploy" || job.DeploymentID == "" {
		logger.Warn("unexpected message, skipping", "type", job.Type)
		return true, false, nil
	}

	// Messages carry ids only — reload everything from the DB (AGENTS).
	projectID, found, err := handler.Store.LoadDeployment(ctx, job.DeploymentID)
	if err != nil {
		return false, false, err
	}
	if !found {
		// Deployment row gone (pruned/failed earlier): nothing to deploy.
		logger.Warn("deployment not found, skipping")
		return true, false, nil
	}
	logger = logger.With("project_id", projectID)

	// --- per-project lock (plan §8) ---------------------------------------
	// Serializes jobs for the same project (a stop must not race a deploy).
	// Lock is taken BEFORE the claim so a losing job requeues without having
	// touched any state.
	unlock, lockAcquired, err := handler.Store.TryProjectLock(ctx, projectID)
	if err != nil {
		return false, false, err
	}
	if !lockAcquired {
		logger.Info("project lock busy, requeueing")
		return false, true, nil // transient → redeliver later
	}
	defer func() {
		if unlockErr := unlock(); unlockErr != nil {
			logger.Error("advisory unlock failed", "error", unlockErr)
		}
	}()

	// --- claim (atomic; stale-heartbeat steal built in) --------------------
	claimed, currentStatus, err := handler.Store.ClaimRun(ctx, job.DeploymentID, handler.WorkerID)
	if err != nil {
		return false, false, err
	}
	if !claimed {
		// Duplicate or a sibling replica owns it — both are fine: ack, no work.
		logger.Info("deploy skipped", "status", currentStatus)
		return true, false, nil
	}
	logger.Info("deploy claimed")

	// --- the actual deploy (M2: fake) --------------------------------------
	// M4 inserts here: env vars + PORT, DB provisioning (§8.5), replace
	// container (§8.3), health probe, Traefik route (§8.4), image prune — all
	// while the claim holds, heartbeat refreshed every 15s.
	if err := handler.Store.MarkLive(ctx, job.DeploymentID, projectID, handler.WorkerID); err != nil {
		return false, false, err
	}
	logger.Info("deploy done: live")
	return true, false, nil
}
