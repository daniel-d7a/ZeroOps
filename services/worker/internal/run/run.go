// Package run implements the run role (plan §8.2 deploy, §8.6 actions).
//
// Two message types share this queue (run.<node>.q), dispatched on `type`:
//
//	{"type":"deploy","deployment_id":...}  — published by the build role:
//	lock → capacity → claim → replace-container (§8.3) → probe → route (§8.4)
//	→ switch → MarkLive (§8.2)
//	{"type":"action","action_id":...}      — published by the API (stop /
//	start / restart): lock → claim → the §8.6 routine → action row done/failed
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
	"time"

	amqp091 "github.com/rabbitmq/amqp091-go"

	"zeroops/worker/internal/crypt"
	"zeroops/worker/internal/docker"
	"zeroops/worker/internal/provision"
	"zeroops/worker/internal/queue"
	"zeroops/worker/internal/route"
	"zeroops/worker/internal/store"
)

// stopGrace is the plan §8.6 shutdown window: SIGTERM, then SIGKILL.
const stopGrace = 10 * time.Second

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
	// M5 (plan §8.5):
	Provision          *provision.Provisioner // project databases on student-db
	StudentDBContainer string                 // container to join project networks when wants_db
	EncryptionKey      []byte                 // AES-256 key for env values / db passwords
}

// jobMessage is the shared message envelope (plan §6): ids only.
type jobMessage struct {
	Type         string `json:"type"`
	DeploymentID string `json:"deployment_id"`
	ActionID     string `json:"action_id"`
}

// permanentError marks a failure no retry can fix (plan §6): the shared
// replace-container routine returns it, the caller records it on the row and
// acks — so deploy, start and restart classify the same failures identically.
type permanentError struct{ msg string }

func (e permanentError) Error() string { return e.msg }

// containerName is app-<deploymentId> — 40 chars, inside the 63-char DNS
// label limit (a longer name broke Docker DNS: "no such host").
func containerName(deploymentID string) string {
	return "app-" + deploymentID
}

// Handle dispatches one run delivery on its type. Return contract: nil → main
// acks (finished / skipped / retried / dead-lettered — DB writes happen before
// the ack); non-nil → crash, message requeues.
func (handler *Handler) Handle(ctx context.Context, delivery amqp091.Delivery) error {
	var job jobMessage
	if err := json.Unmarshal(delivery.Body, &job); err != nil {
		return handler.dead(ctx, delivery, "malformed: "+err.Error())
	}
	switch {
	case job.Type == "deploy" && job.DeploymentID != "":
		return handler.handleDeploy(ctx, delivery, job)
	case job.Type == "action" && job.ActionID != "":
		return handler.handleAction(ctx, delivery, job)
	default:
		return handler.dead(ctx, delivery, fmt.Sprintf("unexpected message type %q", job.Type))
	}
}

// handleDeploy: plan §8.2 — turn a built deployment into a live one.
func (handler *Handler) handleDeploy(ctx context.Context, delivery amqp091.Delivery, job jobMessage) error {
	logger := handler.Logger.With("deployment_id", job.DeploymentID)

	// Messages carry ids only — reload everything from the DB (AGENTS).
	projectID, slug, imageRef, wantsDb, found, err := handler.Store.LoadDeployment(ctx, job.DeploymentID)
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
		if currentStatus == "starting" {
			// Same busy-bounce as the build role: the previous owner died
			// moments ago (heartbeat_at fresh), so steal the claim only once
			// the 2 min staleness window has passed. Same attempt — this is
			// a wait, not a retry.
			logger.Info("deploy claim held elsewhere, bouncing for steal window")
			if err := handler.Broker.PublishRetry(ctx, "run", delivery, queue.ReadAttempt(delivery)); err != nil {
				return err // infrastructure: crash; the unacked message requeues
			}
			return nil
		}
		// Duplicate or a sibling replica owns it — fine: ack, no work.
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
	// Database first (plan §8.2 step 4 → §8.5): role + database must exist
	// before the container starts and gets DATABASE_URL (M5.4). Once per
	// project — later deploys see status 'ready' and skip the DDL.
	if wantsDb {
		if err := handler.Provision.Ensure(ctx, projectID); err != nil {
			// Release target is the pre-claim state ('built'), not the current
			// one — ReleaseClaim validates that.
			return handler.transient(ctx, delivery, job.DeploymentID, "built", logger, err)
		}
	}
	if err := handler.replaceContainer(ctx, projectID, job.DeploymentID, slug, imageRef, wantsDb, logger); err != nil {
		var perm permanentError
		if errors.As(err, &perm) {
			return handler.permanent(ctx, job.DeploymentID, logger, perm.Error())
		}
		return handler.transient(ctx, delivery, job.DeploymentID, "built", logger, err)
	}

	// --- finish (plan §8.2 step 6) ----------------------------------------
	live, supersededImage, err := handler.Store.MarkLive(ctx, job.DeploymentID, projectID, handler.WorkerID)
	if err != nil {
		// Claim held, status still 'starting' (tx rolled back) — release it.
		return handler.transient(ctx, delivery, job.DeploymentID, "built", logger, err)
	}
	if !live {
		logger.Info("claim lost, another worker took the job")
		return nil
	}
	logger.Info("deploy done: live", "slug", slug)

	// Prune this project's old images (plan §8.2 step 6): keep the live one
	// and the one before it — a restart of the superseded deployment still
	// needs its image. Never fails the deploy (log-only, like old-container
	// cleanup): the app is live, a prune problem is a disk leak.
	keep := []string{imageRef}
	if supersededImage != "" {
		keep = append(keep, supersededImage)
	}
	if err := handler.Docker.RemoveProjectImages(ctx, projectID, keep); err != nil {
		logger.Error("image prune failed", "error", err)
	}
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
			// Log the original cause too — otherwise a release failure masks
			// why the job was transient in the first place.
			logger.Error("release claim failed", "error", err, "cause", cause)
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

// --- replace-container (plan §8.3) -----------------------------------------

// replaceContainer is the §8.3 step 4-5 sequence, shared by deploy, start
// (container gone) and restart: ensure the project network, join Traefik
// (and student-db when the project has a database — §8.5: it reaches the app
// by joining this network), run a fresh container from imageRef, probe it,
// write the route, remove the old containers. Returns permanentError for
// health-check/image failures (plan §6 permanent) so every caller classifies
// the same failure the same way — no point retrying a container that
// immediately dies or an image that does not exist.
func (handler *Handler) replaceContainer(ctx context.Context, projectID, deploymentID, slug, imageRef string, wantsDb bool, logger *slog.Logger) error {
	networkName, err := handler.Docker.EnsureProjectNetwork(ctx, projectID)
	if err != nil {
		return err
	}
	// Traefik must live on the project network to reach the app by name
	// through the file-provider route (§8.4). Idempotent.
	if err := handler.Docker.ConnectContainer(ctx, networkName, handler.TraefikContainer); err != nil {
		return err
	}
	// student-db joins too (plan §8.5): the app connects to it at the fixed
	// hostname student-db. Idempotent, so start/restart re-run it safely.
	if wantsDb {
		if err := handler.Docker.ConnectContainer(ctx, networkName, handler.StudentDBContainer); err != nil {
			return err
		}
	}

	name := containerName(deploymentID)
	// Env is loaded fresh on every container create (plan §8.2 steps 3-4) —
	// that is exactly why a restart applies changed env vars while a running
	// container keeps its old ones.
	env, err := handler.containerEnv(ctx, projectID, wantsDb)
	if err != nil {
		return err
	}
	spec := docker.StudentSpec{
		Name:         name,
		Image:        imageRef,
		Network:      networkName,
		Env:          env,
		ProjectID:    projectID,
		DeploymentID: deploymentID,
	}
	if err := handler.Docker.RunStudent(ctx, spec); err != nil {
		if errors.Is(err, docker.ErrImageMissing) {
			return permanentError{"image missing, use Redeploy: " + err.Error()}
		}
		return err
	}

	if probeErr := handler.probe(ctx, networkName, name); probeErr != nil {
		// Plan §8.3.4: remove ONLY the new container; the old container and
		// its route are untouched — a failed deploy never takes the site down.
		if rmErr := handler.Docker.RemoveContainer(ctx, name); rmErr != nil {
			logger.Error("cleanup after failed probe did not apply", "error", rmErr)
		}
		return permanentError{"health check failed: " + probeErr.Error()}
	}

	// Route moves only now (plan §8.4), then the old containers go (§8.3.5).
	if err := handler.Router.Write(projectID, slug, name); err != nil {
		return err
	}
	if err := handler.Docker.RemoveProjectContainers(ctx, projectID, name); err != nil {
		// The app is live and routed; a failed old-container cleanup must not
		// fail the deploy — retry would be worse (it re-probes and re-routes).
		logger.Error("old container cleanup failed", "error", err)
	}
	return nil
}

// probe joins the project network, dials the app, and leaves — plan §8.3.3:
// the worker is not on the student network, so it dials from inside.
func (handler *Handler) probe(ctx context.Context, networkName, container string) error {
	leave, err := handler.Docker.JoinForProbe(ctx, networkName, handler.WorkerID)
	if err != nil {
		return err
	}
	probeErr := docker.ProbeTCP(ctx, container+":3000")
	leave()
	return probeErr
}

// containerEnv builds the student container's environment (plan §8.2 steps
// 3-4): user vars decrypted, then the reserved keys the platform owns —
// PORT (fixed 3000, §8.3) and DATABASE_URL when the project has a database
// (§8.5). The API rejects user-supplied values for both reserved keys, so
// appending them here can never be overridden (AGENTS invariant).
// Never logged: values are decrypted straight into the container spec.
func (handler *Handler) containerEnv(ctx context.Context, projectID string, wantsDb bool) ([]string, error) {
	vars, err := handler.Store.EnvVars(ctx, projectID)
	if err != nil {
		return nil, err
	}
	env := make([]string, 0, len(vars)+2)
	for _, v := range vars {
		plain, err := crypt.Decrypt(handler.EncryptionKey, v.ValueEncrypted)
		if err != nil {
			return nil, fmt.Errorf("decrypt env var %s: %w", v.Key, err)
		}
		env = append(env, v.Key+"="+plain)
	}
	env = append(env, "PORT=3000")

	if wantsDb {
		db, err := handler.Store.LoadDatabase(ctx, projectID)
		if err != nil {
			return nil, err
		}
		password, err := crypt.Decrypt(handler.EncryptionKey, db.PasswordEncrypted)
		if err != nil {
			return nil, fmt.Errorf("decrypt database password: %w", err)
		}
		// Password is base64url (unreserved in a URL), db/user are p_<hex> —
		// no percent-encoding needed. student-db resolves on the project
		// network (§8.5 joined it in replaceContainer).
		env = append(env, fmt.Sprintf("DATABASE_URL=postgres://%s:%s@%s:5432/%s",
			db.DbUser, password, handler.StudentDBContainer, db.DbName))
	}
	return env, nil
}

// --- actions (plan §8.6) ---------------------------------------------------

// handleAction executes one stop/start/restart (plan §8.6): load → skip if
// not queued → lock → claim → run → finish done/failed. Transient failures
// release the claim and go through the retry ladder; exhausting the ladder
// fails the action row so it does not stay queued forever.
func (handler *Handler) handleAction(ctx context.Context, delivery amqp091.Delivery, job jobMessage) error {
	logger := handler.Logger.With("action_id", job.ActionID)

	action, found, err := handler.Store.LoadAction(ctx, job.ActionID)
	if err != nil {
		// Pre-claim: nothing to release, but the ladder must still run — and
		// give-up fails the row (FinishAction accepts queued, so it does not
		// stay queued forever; a 0-row no-op if the row is really gone).
		return handler.actionTransient(ctx, delivery, job.ActionID, logger, err)
	}
	if !found {
		logger.Warn("action not found, skipping")
		return nil
	}
	logger = logger.With("project_id", action.ProjectID, "action_type", action.Type)

	// Already terminal (or a sibling holds the claim): nothing to do — the
	// plan's "done or failed → ack and skip" (running too, since actions have
	// no heartbeat steal to fall back on).
	if action.Status != "queued" {
		logger.Info("action skipped", "status", action.Status)
		return nil
	}

	unlock, lockAcquired, err := handler.Store.TryProjectLock(ctx, action.ProjectID)
	if err != nil {
		return handler.actionTransient(ctx, delivery, action.ID, logger, err)
	}
	if !lockAcquired {
		return handler.actionTransient(ctx, delivery, action.ID, logger,
			fmt.Errorf("project lock busy"))
	}
	defer func() {
		if unlockErr := unlock(); unlockErr != nil {
			logger.Error("advisory unlock failed", "error", unlockErr)
		}
	}()

	claimed, status, err := handler.Store.ClaimAction(ctx, action.ID)
	if err != nil {
		if releaseErr := handler.Store.ReleaseAction(ctx, action.ID); releaseErr != nil {
			logger.Error("release after failed claim did not apply", "error", releaseErr)
		}
		return handler.actionTransient(ctx, delivery, action.ID, logger, err)
	}
	if !claimed {
		logger.Info("action skipped", "status", status)
		return nil
	}
	logger.Info("action claimed")

	permanentMsg, err := handler.runAction(ctx, action)
	if err != nil {
		// Transient: hand the claim back so the retried message can re-claim,
		// then the ladder (30 s TTL doubles as the backoff).
		if releaseErr := handler.Store.ReleaseAction(ctx, action.ID); releaseErr != nil {
			return releaseErr
		}
		return handler.actionTransient(ctx, delivery, action.ID, logger, err)
	}
	if permanentMsg != "" {
		if err := handler.Store.FinishAction(ctx, action.ID, "failed", permanentMsg); err != nil {
			return err
		}
		logger.Error("action failed permanently", "cause", permanentMsg)
		return nil
	}
	if err := handler.Store.FinishAction(ctx, action.ID, "done", ""); err != nil {
		return err
	}
	logger.Info("action done")
	return nil
}

// runAction is the plan §8.6 execution itself. Returns (permanentMsg, err):
// a non-empty permanentMsg is a clean failure the row should record; a
// non-nil err is transient (release + retry). The project lock is held here.
func (handler *Handler) runAction(ctx context.Context, action store.ActionInfo) (permanentMsg string, err error) {
	switch action.Type {
	case "stop":
		// Route first: the URL must die before (or at worst together with) the
		// container — never after.
		if err := handler.Router.Delete(action.ProjectID); err != nil {
			return "", err
		}
		if err := handler.Docker.StopProjectContainers(ctx, action.ProjectID, stopGrace); err != nil {
			return "", err
		}
		if err := handler.Store.SetProjectState(ctx, action.ProjectID, "stopped", "stopped"); err != nil {
			return "", err
		}
		return "", nil

	case "start":
		if action.DeploymentID == "" || action.ImageRef == "" {
			return "nothing to start: no deployment yet — upload first", nil
		}
		name := containerName(action.DeploymentID)
		started, err := handler.Docker.StartContainer(ctx, name)
		if err != nil {
			return "", err
		}
		if started {
			// Reuse the stopped container: network + traefik (idempotent),
			// probe, route back on. No rebuild.
			networkName, err := handler.Docker.EnsureProjectNetwork(ctx, action.ProjectID)
			if err != nil {
				return "", err
			}
			if err := handler.Docker.ConnectContainer(ctx, networkName, handler.TraefikContainer); err != nil {
				return "", err
			}
			if err := handler.probe(ctx, networkName, name); err != nil {
				return "health check failed: " + err.Error(), nil
			}
			if err := handler.Router.Write(action.ProjectID, action.Slug, name); err != nil {
				return "", err
			}
		} else {
			// Container gone (pruned, crashed past recovery): rebuild from the
			// current image (plan §8.3 again).
			if err := handler.replaceContainer(ctx, action.ProjectID, action.DeploymentID, action.Slug, action.ImageRef, action.WantsDB, handler.Logger); err != nil {
				var perm permanentError
				if errors.As(err, &perm) {
					return perm.Error(), nil
				}
				return "", err
			}
		}
		if err := handler.Store.SetProjectState(ctx, action.ProjectID, "running", "running"); err != nil {
			return "", err
		}
		return "", nil

	case "restart":
		if action.DeploymentID == "" || action.ImageRef == "" {
			return "nothing to restart: no deployment yet — upload first", nil
		}
		if err := handler.replaceContainer(ctx, action.ProjectID, action.DeploymentID, action.Slug, action.ImageRef, action.WantsDB, handler.Logger); err != nil {
			var perm permanentError
			if errors.As(err, &perm) {
				return perm.Error(), nil
			}
			return "", err
		}
		if err := handler.Store.SetProjectState(ctx, action.ProjectID, "running", "running"); err != nil {
			return "", err
		}
		return "", nil
	}
	return fmt.Sprintf("unknown action type %q", action.Type), nil
}

// actionTransient is the run side of plan §6 for actions: the ladder is the
// same retry exchange as deploy; giving up records the failure on the action
// row (FinishAction accepts queued, so even a pre-claim give-up lands).
func (handler *Handler) actionTransient(ctx context.Context, delivery amqp091.Delivery, actionID string, logger *slog.Logger, cause error) error {
	gaveUp, err := handler.Broker.RetryOrGiveUp(ctx, "run", delivery, cause.Error())
	if err != nil {
		return err
	}
	if gaveUp {
		if actionID != "" {
			if finErr := handler.Store.FinishAction(ctx, actionID, "failed", "action failed after retries: "+cause.Error()); finErr != nil {
				return finErr
			}
		}
		logger.Error("gave up after retries, dead-lettered", "cause", cause)
		return nil
	}
	logger.Warn("transient error, retry scheduled", "cause", cause, "attempt", queue.ReadAttempt(delivery)+1)
	return nil
}
