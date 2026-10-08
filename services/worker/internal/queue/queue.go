// Package queue wraps the RabbitMQ connection (plan §6).
//
// Topology concept: the queues/exchanges/bindings themselves are declared in
// rabbitmq/definitions.json — the single source of truth imported when the
// broker container first starts. Services never redeclare them; they *passively*
// check at startup (DeclarePassive = "does this exist? fail if not"), which
// turns a missing/outdated definitions file into a loud startup error instead
// of mysterious runtime routing failures.
//
// Reliability rules encoded here (plan §6 / AGENTS):
//
//   - Persistent messages (survive a broker restart).
//   - mandatory=true: if the routing key matches no queue, the broker *returns*
//     the message to us instead of silently dropping it — we log it.
//   - publisher confirms: Publish returns before the broker has durably
//     stored the message; WaitContext blocks until the broker confirms, so
//     "publish returned nil" really means "the message exists".
//
// Connection vs channel (AMQP model): one TCP connection multiplexes many
// channels. Qos (prefetch) is per-*channel*, so every consumer role gets its
// own channel — otherwise build's prefetch=1 would throttle run's prefetch=4.
package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	amqp091 "github.com/rabbitmq/amqp091-go"
)

// Conn is the worker's broker handle: one connection, a confirm-enabled
// publish channel, and one consumer channel per role (opened in consume).
type Conn struct {
	connection     *amqp091.Connection
	publishChannel *amqp091.Channel // confirms enabled; used for all publishes
	logger         *slog.Logger
}

// Open dials the broker, enables confirms, passively verifies the topology
// and starts draining returned (unroutable) mandatory messages. Any failure
// here is fatal: better to crash at startup than half-work without routing.
func Open(amqpURL string, logger *slog.Logger) (*Conn, error) {
	connection, err := amqp091.Dial(amqpURL)
	if err != nil {
		return nil, fmt.Errorf("connect amqp: %w", err)
	}
	publishChannel, err := connection.Channel()
	if err != nil {
		connection.Close()
		return nil, fmt.Errorf("open channel: %w", err)
	}
	// Confirm mode: publishes get acknowledged by the broker (see PublishDeploy).
	if err := publishChannel.Confirm(false); err != nil {
		connection.Close()
		return nil, fmt.Errorf("enable confirms: %w", err)
	}

	// Passive topology checks — definitions.json is the source of truth.
	if err := publishChannel.ExchangeDeclarePassive("build.x", "direct", true, false, false, false, nil); err != nil {
		connection.Close()
		return nil, fmt.Errorf("build.x missing (definitions.json not imported?): %w", err)
	}
	if _, err := publishChannel.QueueDeclarePassive("build.q", true, false, false, false, nil); err != nil {
		connection.Close()
		return nil, fmt.Errorf("build.q missing: %w", err)
	}
	if err := publishChannel.ExchangeDeclarePassive("run.x", "direct", true, false, false, false, nil); err != nil {
		connection.Close()
		return nil, fmt.Errorf("run.x missing: %w", err)
	}

	broker := &Conn{connection: connection, publishChannel: publishChannel, logger: logger}

	// A mandatory publish that matches no queue comes back on the "return"
	// channel instead of vanishing. We only need to notice and log it —
	// failing the job after a confirmed-but-returned publish would risk
	// doing the work twice.
	returnedMessages := publishChannel.NotifyReturn(make(chan amqp091.Return, 16))
	go func() {
		for returnedMessage := range returnedMessages {
			broker.logger.Error("unroutable message returned",
				"exchange", returnedMessage.Exchange,
				"routing_key", returnedMessage.RoutingKey)
		}
	}()

	return broker, nil
}

// ConsumeBuild subscribes to build.q (BUILD_PREFETCH=1: builds are heavy, so
// a replica never has more than one in flight).
func (c *Conn) ConsumeBuild(prefetch int) (<-chan amqp091.Delivery, error) {
	return c.consume("build.q", prefetch)
}

// ConsumeRun subscribes to this node's own run queue (run.<node>.q): jobs are
// routed by node id, so every replica on the node competes for the same queue
// and RabbitMQ distributes them automatically.
func (c *Conn) ConsumeRun(nodeID string, prefetch int) (<-chan amqp091.Delivery, error) {
	return c.consume("run."+nodeID+".q", prefetch)
}

// consume opens a dedicated channel (Qos is per-channel), applies the
// prefetch limit and returns the delivery stream. Manual acks
// (autoAck=false): the message stays unacked — invisible to other consumers —
// until the handler explicitly acks it, which is what makes "crash mid-job →
// another replica retries it" work. On failure the channel is closed so it
// doesn't leak.
func (c *Conn) consume(queueName string, prefetch int) (<-chan amqp091.Delivery, error) {
	consumerChannel, err := c.connection.Channel()
	if err != nil {
		return nil, fmt.Errorf("open channel for %s: %w", queueName, err)
	}
	// Prefetch = how many unacked messages the broker may hand us at once.
	if err := consumerChannel.Qos(prefetch, 0, false); err != nil {
		consumerChannel.Close()
		return nil, fmt.Errorf("qos: %w", err)
	}
	deliveries, err := consumerChannel.Consume(queueName, "", false, false, false, false, nil)
	if err != nil {
		consumerChannel.Close()
		return nil, fmt.Errorf("consume %s: %w", queueName, err)
	}
	return deliveries, nil
}

// PublishDeploy hands the finished build to the run role: the message goes to
// the run.x exchange with the project's node id as routing key (direct
// exchange → only that node's queue receives it). mandatory + confirm as
// described in the package docs: nil here means the broker durably has it.
func (c *Conn) PublishDeploy(ctx context.Context, nodeID, deploymentID string) error {
	body, err := json.Marshal(map[string]string{"type": "deploy", "deployment_id": deploymentID})
	if err != nil {
		return err
	}
	deferredConfirm, err := c.publishChannel.PublishWithDeferredConfirmWithContext(ctx, "run.x", nodeID, true, false, amqp091.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp091.Persistent, // survives a broker restart
		MessageId:    deploymentID,       // for tracing/debugging in the broker
		Body:         body,
	})
	if err != nil {
		return fmt.Errorf("publish deploy: %w", err)
	}
	// Wait for the broker's confirm before telling the caller "published".
	confirmed, err := deferredConfirm.WaitContext(ctx)
	if err != nil {
		return fmt.Errorf("wait for confirm: %w", err)
	}
	if !confirmed {
		return fmt.Errorf("publish not confirmed")
	}
	return nil
}

// Close tears down the channels and connection. Anything still unacked is
// requeued by the broker — that is the safety net behind every crash and
// every graceful shutdown.
func (c *Conn) Close() error {
	if err := c.publishChannel.Close(); err != nil {
		c.connection.Close()
		return err
	}
	return c.connection.Close()
}
