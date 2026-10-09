// Package run implements the run role (plan §8.2 deploy; §8.6 actions —
// stop/start/restart — land with M4.2).
//
// Where the message came from: the build role finished a deployment and
// published {"type":"deploy"} to run.x, routed to this node's queue
// (run.<node>.q). This handler turns "built" into "live" for real:
//
//	lock → capacity → claim → network + container (§8.3) → health probe
//	→ Traefik route (§8.4) → old containers removed → MarkLive (§8.2 step 6)
//
// Failure classification is the build role's (plan §6): malformed → dead,
// transient → retry ×3 → failed + dead; permanent here = node at capacity,
// health check failed, image missing (§8.2/§8.6).
package run

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	amqp091 "github.com/rabbitmq/amqp091-go"

	"zeroops/worker/internal/docker"
	"zeroops/worker/internal/queue"
	"zeroops/worker/internal/route"
	"zeroops/worker/internal/store"
)

// Handler processes run jobs. Stateless like the build handler — replicas
// share one queue and the DB holds all state.
type Handler struct {
	Store    *store.Store
	Broker   *queue.Conn
	WorkerID string
	Logger   *slog.Logger

	// Real-run plumbing (plan §8.2-8.4, M4):
	Docker           *docker.Client
	Router           *route.Writer
	TraefikContainer string
	MaxRunning       int
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
	projectID, slug, imageRef, found, err := handler.Store.LoadDeployment(ctx, job.DeploymentID)
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

	// --- capacity (plan §8.2 step 1): permanent, before claiming -----------
	// At the limit nothing this node does can help — a retry would hit the
	// same wall (plan §6 "node at capacity" is permanent).
	if running, err := handler.Docker.RunningStudentContainers(ctx); err != nil {
		return handler.transient(ctx, delivery, job.DeploymentID, "", logger, err)
	} else if running >= handler.MaxRunning {
		return handler.permanent(ctx, job.DeploymentID, logger,
			fmt.Sprintf("node at capacity: %d/%d student containers running", running, handler.MaxRunning))
	}

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

	// --- the real deploy (plan §8.2 steps 3-5) -----------------------------
	if imageRef == "" {
		// A "built" deployment without an image is a broken row; no retry
		// will conjure one (plan §6 permanent).
		return handler.permanent(ctx, job.DeploymentID, logger, "deployment has no image — use Redeploy")
	}
	// Env: user variables arrive with M5 (decrypted) — for now the platform
	// contract is just the port (plan §8.2 step 3).
	env := []string{"PORT=3000"}
	// DATABASE_URL joins here with M5 (plan §8.5), only when wants_db.

	// --- replace-container routine (plan §8.3) -----------------------------
	networkName, err := handler.Docker.EnsureProjectNetwork(ctx, projectID)
	if err != nil {
		return handler.transient(ctx, delivery, job.DeploymentID, "starting", logger, err)
	}
	// Traefik must live on the project network to reach the app by name
	// through the file-provider route (§8.4). Idempotent.
	if err := handler.Docker.ConnectContainer(ctx, networkName, handler.TraefikContainer); err != nil {
		return handler.transient(ctx, delivery, job.DeploymentID, "starting", logger, err)
	}

	// app-<deploymentId> = 40 chars, inside the 63-char DNS label limit —
	// Docker DNS refuses longer names ("no such host"), which broke the probe
	// when the name was app-<projectId>-<deploymentId> (77 chars). Deployment
	// ids are unique, so no project prefix is needed.
	containerName := fmt.Sprintf("app-%s", job.DeploymentID)
	spec := docker.StudentSpec{
		Name:         containerName,
		Image:        imageRef,
		Network:      networkName,
		Env:          env,
		ProjectID:    projectID,
		DeploymentID: job.DeploymentID,
	}
	if err := handler.Docker.RunStudent(ctx, spec); err != nil {
		if errors.Is(err, docker.ErrImageMissing) {
			return handler.permanent(ctx, job.DeploymentID, logger, err.Error())
		}
		return handler.transient(ctx, delivery, job.DeploymentID, "starting", logger, err)
	}

	// Probe from inside the project network (plan §8.3.3): join, dial, leave.
	leave, err := handler.Docker.JoinForProbe(ctx, networkName, handler.WorkerID)
	if err != nil {
		return handler.transient(ctx, delivery, job.DeploymentID, "starting", logger, err)
	}
	probeErr := docker.ProbeTCP(ctx, containerName+":3000")
	leave()
	if probeErr != nil {
		// Plan §8.3.4: remove ONLY the new container; the old container and
		// its route are untouched — a failed deploy never takes the site down.
		if rmErr := handler.Docker.RemoveContainer(ctx, containerName); rmErr != nil {
			logger.Error("cleanup after failed probe did not apply", "error", rmErr)
		}
		return handler.permanent(ctx, job.DeploymentID, logger, "health check failed: "+probeErr.Error())
	}

	// Route moves only now (plan §8.4), then the old containers go (§8.3.5).
	if err := handler.Router.Write(projectID, slug, containerName); err != nil {
		return handler.transient(ctx, delivery, job.DeploymentID, "starting", logger, err)
	}
	if err := handler.Docker.RemoveProjectContainers(ctx, projectID, containerName); err != nil {
		// The app is live and routed; a failed old-container cleanup must not
		// fail the deploy — retry would be worse (it re-probes and re-routes).
		logger.Error("old container cleanup failed", "error", err)
	}

	// --- finish (plan §8.2 step 6) ----------------------------------------
	// TODO(M4.2): prune old images for this project — keep the live one and
	// the one before it.
	live, err := handler.Store.MarkLive(ctx, job.DeploymentID, projectID, handler.WorkerID)
	if err != nil {
		// Claim held, status still 'starting' (tx rolled back) — release it.
		return handler.transient(ctx, delivery, job.DeploymentID, "built", logger, err)
	}
	if !live {
		logger.Info("claim lost, another worker took the job")
		return nil
	}
	logger.Info("deploy done: live", "slug", slug)
	return nil
}

// permanent fails a deploy for good (plan §6): record the cause and ack —
// no retry ladder, no dead message; the deployments row IS the record.
// Mirrors the build role's helper (different package, same 8 lines).
func (handler *Handler) permanent(ctx context.Context, deploymentID string, logger *slog.Logger, cause string) error {
	if err := handler.Store.MarkFailed(ctx, deploymentID, cause); err != nil {
		return err // DB unreachable: crash, the unacked message requeues
	}
	logger.Error("deploy failed permanently", "cause", cause)
	return nil // main acks — finished, nothing to retry
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
