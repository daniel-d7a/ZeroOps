// Command worker is the ZeroOps worker process (plan §8).
//
// Startup order matters — every step fails fast before the next begins:
//
//  1. load + validate env config (bad config must never half-start)
//  2. open the platform DB and pass the schema gate (never run against an
//     older schema than the API's migrations — see internal/store.CheckSchema)
//  3. open the broker (fail fast if definitions.json was never imported)
//  4. start the heartbeat goroutine (upsert our row in workers every 15s)
//  5. start one consume loop per enabled role (build and/or run)
//  6. block until SIGINT/SIGTERM, then drain and exit
//
// Shutdown contract: on signal the consume loops stop accepting new messages,
// in-flight jobs notice the canceled context and requeue themselves, then the
// deferred broker Close() requeues anything still unacked. RabbitMQ keeps
// unacked messages safe, so no job is ever lost to a restart.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	amqp091 "github.com/rabbitmq/amqp091-go"

	"zeroops/worker/internal/build"
	"zeroops/worker/internal/config"
	"zeroops/worker/internal/docker"
	"zeroops/worker/internal/log"
	"zeroops/worker/internal/provision"
	"zeroops/worker/internal/queue"
	"zeroops/worker/internal/route"
	"zeroops/worker/internal/run"
	"zeroops/worker/internal/store"
)

func main() {
	// The worker's identity is its container hostname (plan §8: replicas are
	// stateless and named by hostname, so scaling = more hostnames).
	workerID, err := os.Hostname()
	if err != nil || workerID == "" {
		workerID = "unknown"
	}
	logger := log.New(workerID)
	defer logger.Info("worker stopped")

	cfg, err := config.Load()
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	// signal.NotifyContext: ctx is canceled the moment SIGINT/SIGTERM arrives,
	// which is how every goroutine and in-flight query learns to wind down.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// One timestamp for the process lifetime: each heartbeat tick re-sends it,
	// so a restart visibly resets started_at instead of drifting.
	processStartedAt := time.Now().Unix()

	logger.Info("worker started", "node_id", cfg.NodeID, "roles", cfg.Roles)

	// --- platform DB -------------------------------------------------------
	datastore, err := store.Open(ctx, cfg.PlatformDBURL)
	if err != nil {
		logger.Error("database unavailable", "error", err)
		os.Exit(1)
	}
	defer datastore.Close()

	// Schema gate (AGENTS invariant): the API owns migrations via drizzle;
	// this worker refuses to run queries against a schema older than it knows.
	if err := datastore.CheckSchema(ctx, cfg.MinSchemaMigration); err != nil {
		logger.Error("schema check failed", "error", err)
		os.Exit(1)
	}

	// --- broker ------------------------------------------------------------
	broker, err := queue.Open(cfg.AMQPURL, logger)
	if err != nil {
		logger.Error("broker unavailable", "error", err)
		os.Exit(1)
	}
	defer broker.Close() // closes channels + connection; unacked deliveries requeue

	// Heartbeat runs in the background for the whole process lifetime (plan
	// §8.7): it tells the API and other workers "this replica is alive", and
	// gives the claim logic a staleness signal to steal jobs from dead replicas.
	go heartbeatLoop(ctx, datastore, logger, workerID, cfg, processStartedAt)

	// --- consume loops (one goroutine per enabled role) --------------------
	// A WaitGroup lets main wait for both loops to finish their graceful exit
	// before the deferred broker.Close() runs (defers run in reverse order).
	var consumeLoops sync.WaitGroup
	startConsumeLoop := func(queueName string, deliveries <-chan amqp091.Delivery, handleDelivery deliveryHandler) {
		consumeLoops.Add(1)
		go func() {
			defer consumeLoops.Done()
			consumeLoop(ctx, queueName, deliveries, logger, handleDelivery)
		}()
	}

	buildRole := &build.Handler{
		Store:          datastore,
		Broker:         broker,
		WorkerID:       workerID,
		Logger:         logger,
		ForceTransient: cfg.ForceTransient,
	}
	if cfg.HasRole("build") {
		buildDeliveries, err := broker.ConsumeBuild(cfg.BuildPrefetch)
		if err != nil {
			logger.Error("cannot consume build.q", "error", err)
			os.Exit(1)
		}
		logger.Info("consuming build.q", "prefetch", cfg.BuildPrefetch)
		startConsumeLoop("build.q", buildDeliveries, buildRole.Handle)
	}

	if cfg.HasRole("run") {
		dockerClient, err := docker.New()
		if err != nil {
			logger.Error("docker unavailable", "error", err)
			os.Exit(1)
		}
		// Student-db provisioning (plan §8.5) — pool is lazy, no ping: a
		// student-db outage surfaces as a retried deploy, not a crash-loop.
		provisioner := provision.New(datastore, cfg.StudentDBURL, cfg.EncryptionKey, logger)
		defer provisioner.Close()
		runRole := &run.Handler{
			Store:              datastore,
			Broker:             broker,
			WorkerID:           workerID,
			Logger:             logger,
			Docker:             dockerClient,
			Router:             &route.Writer{Dir: cfg.DynamicConfigDir, Domain: cfg.BaseDomain},
			TraefikContainer:   cfg.TraefikContainer,
			MaxRunning:         cfg.MaxRunningPerNode,
			Provision:          provisioner,
			StudentDBContainer: cfg.StudentDBContainer,
			EncryptionKey:      cfg.EncryptionKey,
		}
		runQueueName := "run." + cfg.NodeID + ".q"
		runDeliveries, err := broker.ConsumeRun(cfg.NodeID, cfg.RunPrefetch)
		if err != nil {
			logger.Error("cannot consume run queue", "queue", runQueueName, "error", err)
			os.Exit(1)
		}
		logger.Info("consuming run queue", "queue", runQueueName, "prefetch", cfg.RunPrefetch)
		startConsumeLoop(runQueueName, runDeliveries, runRole.Handle)
	}

	// Block here until the signal handler cancels ctx and both loops return.
	consumeLoops.Wait()
}

// deliveryHandler processes exactly one message. All failure classification
// (plan §6: malformed → dead exchange, transient → retry exchange ×3 → dead,
// permanent → DB failure + ack) happens INSIDE the handler, so its contract
// is just two outcomes — which keeps the ack rule trivial to audit:
//
//	err == nil  → main acks. Safe because the handler only returns nil after
//	              the message is finished, deliberately skipped, handed to the
//	              retry exchange, or dead-lettered — and any DB failure record
//	              was written BEFORE the ack (AGENTS: ack after final write).
//	err != nil → infrastructure is broken; main crashes (os.Exit). The unacked
//	              message requeues via the broker and compose restarts us;
//	              in-flight jobs interrupted by shutdown are nacked-requeue.
type deliveryHandler func(context.Context, amqp091.Delivery) error

// consumeLoop owns one queue: receive a delivery, hand it to the role's
// handler, translate the handler's answer into an ack/nack. It runs until the
// context is canceled (shutdown) or the channel dies (connection lost).
func consumeLoop(ctx context.Context, queueName string, deliveries <-chan amqp091.Delivery, logger *slog.Logger, handleDelivery deliveryHandler) {
	for {
		select {
		case <-ctx.Done():
			// Graceful shutdown: stop accepting new messages. Anything
			// in-flight has already been nacked-requeue by the branch below,
			// and anything unacked when the connection closes is requeued by
			// RabbitMQ itself.
			logger.Info("shutdown: stopped consuming", "queue", queueName)
			return
		case delivery, ok := <-deliveries:
			if !ok {
				// The broker closed our delivery channel: the connection is
				// gone. Crash instead of spinning on a dead channel; the
				// broker requeues our unacked messages for other replicas.
				logger.Error("delivery channel closed (connection lost)", "queue", queueName)
				os.Exit(1)
			}

			err := handleDelivery(ctx, delivery)
			if err != nil {
				if ctx.Err() != nil {
					// We were interrupted mid-job (shutdown raced the work):
					// give the message back so another replica finishes it.
					logger.Info("interrupted, requeued in-flight job", "queue", queueName, "error", err)
					delivery.Nack(false, true)
					return
				}
				// Real infrastructure failure — fail fast and loudly. The
				// unacked message requeues; compose restarts the replica.
				logger.Error("job failed", "queue", queueName, "error", err)
				os.Exit(1)
			}

			// Handler returned nil = fully handled (finished, skipped, retried
			// or dead-lettered) — the ack that closes the message's life.
			if err := delivery.Ack(false); err != nil {
				logger.Error("ack failed", "queue", queueName, "error", err)
				os.Exit(1)
			}
		}
	}
}

// heartbeatLoop upserts this replica's row in workers immediately and then
// every 15 seconds (plan §8.7). It stops silently when ctx is canceled.
func heartbeatLoop(ctx context.Context, datastore *store.Store, logger *slog.Logger, workerID string, cfg config.Config, processStartedAt int64) {
	sendHeartbeat := func() {
		// Errors are logged, not fatal: a failed heartbeat must never take the
		// worker down (the claim logic treats a stale heartbeat as recoverable).
		if err := datastore.UpsertWorker(ctx, workerID, cfg.Roles, cfg.NodeID, processStartedAt); err != nil && ctx.Err() == nil {
			logger.Error("worker heartbeat failed", "error", err)
		}
	}
	sendHeartbeat()

	heartbeatTicker := time.NewTicker(15 * time.Second)
	defer heartbeatTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-heartbeatTicker.C:
			sendHeartbeat()
		}
	}
}
