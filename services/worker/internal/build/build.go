// Package build implements the build role (plan §8.1): it consumes build.q
// and turns an uploaded zip into a runnable image.
//
// Message flow for one deployment (M2 fakes the middle of it):
//
//	API (upload) ──build.x──▶ build.q ──▶ this handler ──run.x──▶ run.<node>.q
//	                              │                              │
//	                 queued → building → built            starting → live
//
// M2 fakes the work (claim → mark built → publish the run job); M3 swaps in
// source loading, safe unzip and railpack — the claim/finish skeleton stays.
//
// Idempotency note: RabbitMQ gives at-least-once delivery, so this handler
// WILL see duplicates (redeliveries after a crash, republished repair jobs).
// That's fine because the claim only matches the expected state — a duplicate
// finds nothing to claim, gets acked and does no work.
package build

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	amqp091 "github.com/rabbitmq/amqp091-go"

	"zeroops/worker/internal/queue"
	"zeroops/worker/internal/store"
)

// Handler processes build jobs. It is stateless — all state lives in the DB
// (deployments) and the broker (unacked messages), so any replica can pick up
// any job.
type Handler struct {
	Store    *store.Store
	Broker   *queue.Conn
	WorkerID string
	Logger   *slog.Logger
}

// jobMessage is the build/run envelope (plan §6): messages carry ids only —
// never payloads — and the worker always reloads the real state from the DB.
type jobMessage struct {
	Type         string `json:"type"`
	DeploymentID string `json:"deployment_id"`
}

// Handle processes exactly one build delivery.
//
// Outcomes (see deliveryHandler in cmd/worker/main.go for the full contract):
//   - (ack=true, requeue=false, nil): finished or safely skipped.
//   - (ack=false, requeue=true, nil): transient — unused by the build role
//     until step 21 adds the retry exchange.
//   - (…, err!=nil): infrastructure failure — the process crashes, the unacked
//     message requeues, compose restarts us. Step 21 replaces this crash with
//     attempt classification (retry ×3 → dead queue).
func (handler *Handler) Handle(ctx context.Context, delivery amqp091.Delivery) (ack, requeue bool, err error) {
	var job jobMessage
	if err := json.Unmarshal(delivery.Body, &job); err != nil {
		// A message we can't parse will never parse: ack it (skip) or it
		// would be redelivered forever. Malformed input is not transient.
		handler.Logger.Warn("malformed message, skipping", "error", err)
		return true, false, nil
	}
	logger := handler.Logger.With("deployment_id", job.DeploymentID)
	if job.Type != "build" || job.DeploymentID == "" {
		logger.Warn("unexpected message, skipping", "type", job.Type)
		return true, false, nil
	}

	// --- claim (atomic: exactly one replica can win) -----------------------
	claimed, currentStatus, err := handler.Store.ClaimBuild(ctx, job.DeploymentID, handler.WorkerID)
	if err != nil {
		return false, false, err
	}
	if !claimed {
		if currentStatus == "built" {
			// Repair path (plan §8.1): we crashed *after* marking built but
			// *before* the run job was confirmed. The status says "done" — so
			// just republish the deploy message instead of rebuilding.
			if err := handler.publishDeployJob(ctx, logger, job.DeploymentID); err != nil {
				return false, false, err
			}
			logger.Info("already built, republished deploy job")
		} else {
			// Duplicate delivery or another replica already owns it: nothing
			// to do — ack so it is never redelivered.
			logger.Info("build skipped", "status", currentStatus)
		}
		return true, false, nil
	}

	logger.Info("build claimed")

	// --- the actual build (M2: fake) --------------------------------------
	// M3: load the source zip, safe-unzip, run railpack, write image_ref and
	// stream logs into deploy_logs — right here, still guarded by the claim.
	if err := handler.Store.MarkBuilt(ctx, job.DeploymentID, handler.WorkerID); err != nil {
		return false, false, err
	}
	// Publish only AFTER the final DB write: if the publish fails we crash
	// with status='built', and the next delivery (or the sweeper) takes the
	// republish path above. The reverse order would leave status='building'
	// with a live run job — a split brain.
	if err := handler.publishDeployJob(ctx, logger, job.DeploymentID); err != nil {
		return false, false, err
	}
	logger.Info("build done, deploy job published")
	return true, false, nil
}

// publishDeployJob routes the deploy job to the owning node's run queue.
func (handler *Handler) publishDeployJob(ctx context.Context, logger *slog.Logger, deploymentID string) error {
	nodeID, found, err := handler.Store.NodeID(ctx, deploymentID)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("deployment %s not found", deploymentID)
	}
	return handler.Broker.PublishDeploy(ctx, nodeID, deploymentID)
}
