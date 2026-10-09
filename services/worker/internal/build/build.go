// Package build implements the build role (plan §8.1): it consumes build.q
// and turns an uploaded zip into a runnable image.
//
// Message flow for one deployment:
//
//	API (upload) ──build.x──▶ build.q ──▶ this handler ──run.x──▶ run.<node>.q
//	                              │                              │
//	                 queued → building → built            starting → live
//
// Steps done here: claim → load source + verify checksum → safe unzip
// (unzip.go) → [M3.3: railpack build] → mark built + publish the run job.
//
// Failure classification (plan §6 Consumer rules) — every delivery ends in
// exactly one of:
//
//	malformed   → dead exchange (raw payload kept for inspection), ack
//	transient   → release claim → retry exchange (attempt+1, TTL 30 s) → ack;
//	              at attempt 3 → MarkFailed + dead exchange → ack
//	permanent   → MarkFailed with the cause → ack (no retry, no dead message:
//	              the DB row IS the record) — bad source, unsafe zip, missing
//	              package.json, railpack failing on the student's code
//	claim lost  → ack and skip (a sibling replica owns the job now)
//	success     → ack
package build

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"

	amqp091 "github.com/rabbitmq/amqp091-go"

	"zeroops/worker/internal/queue"
	"zeroops/worker/internal/store"
)

// workRoot is where sources are unpacked and images are built — a named
// volume, so /work survives container restarts and both replicas share
// nothing (each deployment gets its own subdirectory, deleted after use).
const workRoot = "/work"

// uploadsDir is where the API stores uploaded zips; the default matches
// docker-compose.yml (worker mounts the volume read-only).
func uploadsDir() string {
	if dir := os.Getenv("UPLOADS_DIR"); dir != "" {
		return dir
	}
	return "/data/uploads"
}

// Handler processes build jobs. It is stateless — all state lives in the DB
// (deployments) and the broker (unacked messages), so any replica can pick up
// any job.
type Handler struct {
	Store          *store.Store
	Broker         *queue.Conn
	WorkerID       string
	Logger         *slog.Logger
	ForceTransient bool // test hook (plan §11: force a transient error)
}

// jobMessage is the build/run envelope (plan §6): messages carry ids only —
// never payloads — and the worker always reloads the real state from the DB.
type jobMessage struct {
	Type         string `json:"type"`
	DeploymentID string `json:"deployment_id"`
}

// Handle processes exactly one build delivery.
//
// Return contract (cmd/worker/main.go): nil → main acks (safe: the message
// was finished, dead-lettered or handed to the retry exchange — and for the
// failed path the DB write happened first). Non-nil → infrastructure is
// broken; main crashes, the unacked message requeues, compose restarts us.
func (handler *Handler) Handle(ctx context.Context, delivery amqp091.Delivery) error {
	var job jobMessage
	if err := json.Unmarshal(delivery.Body, &job); err != nil {
		// Poison: will never parse. Keep the raw payload in the dead queue —
		// the log alone would lose the only copy of it.
		return handler.dead(ctx, delivery, "malformed: "+err.Error())
	}
	if job.Type != "build" || job.DeploymentID == "" {
		return handler.dead(ctx, delivery, fmt.Sprintf("unexpected message type %q", job.Type))
	}
	logger := handler.Logger.With("deployment_id", job.DeploymentID)

	// --- claim (atomic: exactly one replica can win) -----------------------
	claimed, currentStatus, err := handler.Store.ClaimBuild(ctx, job.DeploymentID, handler.WorkerID)
	if err != nil {
		// The UPDATE may have applied even though the call errored (lost
		// connection mid-commit). Best-effort release so the retry isn't
		// blocked by our own fresh heartbeat; if the release also fails the
		// §14 sweeper is the backstop.
		if releaseErr := handler.Store.ReleaseClaim(ctx, job.DeploymentID, handler.WorkerID, "queued"); releaseErr != nil {
			logger.Error("release after failed claim did not apply", "error", releaseErr)
		}
		return handler.transient(ctx, delivery, job.DeploymentID, "", logger, err)
	}
	if !claimed {
		if currentStatus == "built" {
			// Repair path (plan §8.1): built but the deploy publish is still
			// missing (we crashed between MarkBuilt and a confirmed publish).
			// Republish instead of rebuilding.
			if err := handler.publishDeployJob(ctx, logger, job.DeploymentID); err != nil {
				return handler.transient(ctx, delivery, job.DeploymentID, "", logger, err)
			}
			logger.Info("already built, republished deploy job")
		} else {
			// Duplicate delivery or a sibling replica owns it: ack, no work.
			logger.Info("build skipped", "status", currentStatus)
		}
		return nil
	}

	logger.Info("build claimed")

	if handler.ForceTransient {
		// Gate test hook (plan §11 "force a transient error"): fail every
		// attempt so the retry ladder runs → dead queue after 3 tries.
		return handler.transient(ctx, delivery, job.DeploymentID, "queued", logger,
			fmt.Errorf("forced transient error (FORCE_TRANSIENT test hook)"))
	}

	// --- source: load and verify (plan §8.1 step 2) ------------------------
	zipPath, checksum, found, err := handler.Store.SourceForDeployment(ctx, job.DeploymentID)
	if err != nil {
		return handler.transient(ctx, delivery, job.DeploymentID, "queued", logger, err)
	}
	if !found {
		// No row or pruned by retention — nothing to rebuild from, ever.
		return handler.permanent(ctx, job.DeploymentID, "source archive not available (missing or pruned)", logger)
	}
	fullZip := filepath.Join(uploadsDir(), zipPath)
	if err := verifyChecksum(fullZip, checksum); err != nil {
		if isPermanent(err) {
			return handler.permanent(ctx, job.DeploymentID, err.Error(), logger)
		}
		return handler.transient(ctx, delivery, job.DeploymentID, "queued", logger, err)
	}

	// --- safe unzip (plan §8.1 step 3, rules in unzip.go) ------------------
	destDir := filepath.Join(workRoot, job.DeploymentID)
	defer os.RemoveAll(destDir) // best-effort: every exit path leaves /work clean
	root, err := Extract(fullZip, destDir)
	if err != nil {
		if isPermanent(err) {
			return handler.permanent(ctx, job.DeploymentID, err.Error(), logger)
		}
		return handler.transient(ctx, delivery, job.DeploymentID, "queued", logger, err)
	}
	logger.Info("source extracted", "root", root)
	// One system-stream line so the log viewer has content from the very
	// start; the railpack build stream (M3.3) appends to the same table.
	if err := handler.Store.AppendLogs(ctx, job.DeploymentID, "system",
		[]string{"source extracted: " + root}); err != nil {
		return handler.transient(ctx, delivery, job.DeploymentID, "queued", logger, err)
	}

	// M3.3 inserts the railpack build (plan §8.1 step 4) right here:
	// stream its output into deploy_logs, capture the image ref, treat a
	// non-zero exit as a permanent failure (the student's code is broken).
	built, err := handler.Store.MarkBuilt(ctx, job.DeploymentID, handler.WorkerID)
	if err != nil {
		// Claim held, status still 'building' — release so the retry can claim.
		return handler.transient(ctx, delivery, job.DeploymentID, "queued", logger, err)
	}
	if !built {
		logger.Info("claim lost, another worker took the job")
		return nil
	}

	// Publish only AFTER the final DB write: if the publish fails we go
	// transient with status already 'built', and the redelivery takes the
	// repair path above — never a duplicate build.
	if err := handler.publishDeployJob(ctx, logger, job.DeploymentID); err != nil {
		return handler.transient(ctx, delivery, job.DeploymentID, "", logger, err)
	}
	logger.Info("build done, deploy job published")
	return nil
}

// permanent fails a build for good (plan §6 permanent class): record the
// cause on the deployments row and ack — no retry ladder, no dead message,
// because no retry could ever succeed and the DB row IS the record.
func (handler *Handler) permanent(ctx context.Context, deploymentID, cause string, logger *slog.Logger) error {
	if err := handler.Store.MarkFailed(ctx, deploymentID, cause); err != nil {
		return err // DB unreachable: crash, the unacked message requeues
	}
	logger.Error("build failed permanently", "cause", cause)
	return nil // main acks — finished, nothing to retry
}

// verifyChecksum recomputes the zip's sha256 and compares it with the value
// the API recorded at upload (plan §8.1 step 2) — catches a file replaced on
// disk between upload and build. Missing file or mismatch = permanent; any
// other read error (EIO, ...) = transient.
func verifyChecksum(fullZip, want string) error {
	file, err := os.Open(fullZip)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return permanentf("source zip missing from storage: %v", err)
		}
		return fmt.Errorf("open source zip: %w", err)
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return fmt.Errorf("read source zip: %w", err)
	}
	if got := hex.EncodeToString(hash.Sum(nil)); got != want {
		return permanentf("source checksum mismatch (expected %s, got %s)", want, got)
	}
	return nil
}

// transient applies the plan §6 retry rule to one failed delivery:
//
//  1. release our claim (if we hold one) so the redelivery can claim afresh;
//  2. publish to the retry exchange (attempt+1) or, at MaxAttempts, to the
//     dead exchange;
//  3. on give-up, record the failure in the DB — message first (confirmed),
//     DB write next, then main acks: never ack before the final write.
//
// releaseTo is "" when no claim is held (failure before/at the claim, or the
// status already advanced to 'built').
func (handler *Handler) transient(ctx context.Context, delivery amqp091.Delivery, deploymentID, releaseTo string, logger *slog.Logger, cause error) error {
	if releaseTo != "" {
		if err := handler.Store.ReleaseClaim(ctx, deploymentID, handler.WorkerID, releaseTo); err != nil {
			return err // DB unreachable: crash; broker requeues (§14 sweeper backstops)
		}
	}
	gaveUp, err := handler.Broker.RetryOrGiveUp(ctx, "build", delivery, cause.Error())
	if err != nil {
		return err
	}
	if gaveUp {
		if err := handler.Store.MarkFailed(ctx, deploymentID, "build failed after retries: "+cause.Error()); err != nil {
			return err
		}
		logger.Error("gave up after retries, dead-lettered", "cause", cause)
		return nil
	}
	logger.Warn("transient error, retry scheduled", "cause", cause, "attempt", queue.ReadAttempt(delivery)+1)
	return nil
}

// dead parks an unprocessable message in build.dead.q (plan §6: inspection).
func (handler *Handler) dead(ctx context.Context, delivery amqp091.Delivery, reason string) error {
	if err := handler.Broker.PublishDead(ctx, "build", delivery, reason); err != nil {
		return err
	}
	handler.Logger.Warn("message dead-lettered", "reason", reason)
	return nil
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
