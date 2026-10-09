// Package docker wraps the Docker Engine API for the run role (plan §8.3).
//
// Two AGENTS invariants shape everything here:
//
//   - Student containers only ever join net-<projectId> — never the platform,
//     edge or db networks. One container, one project network, no exceptions.
//   - No Traefik labels. Routing is written by the file provider AFTER the
//     health probe passes (package route), so a failed deploy never moves
//     traffic away from the old, still-running version.
//
// All calls take a context and return wrapped errors: the run handler classifies
// them (plan §6) — ErrImageMissing is permanent, everything else is transient.
package docker

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/docker/errdefs"
)

// ErrImageMissing: the image this deployment needs is not on the node
// (pruned, or a different node later). plan §8.6: permanent, "use Redeploy".
var ErrImageMissing = errors.New("image missing")

// Limits and timeouts from plan §8.3.
const (
	cpuLimit        = 500_000_000      // 0.5 CPU (NanoCPUs)
	memoryLimit     = 1 << 30          // 1 GiB, MemorySwap equal → no swap
	pidsLimit       = 256              // fork-bomb guard
	restartMaxRetry = 5                // restart=on-failure:5
	probeTimeout    = 60 * time.Second // §8.3.3: "up to 60 seconds"
	probeInterval   = 500 * time.Millisecond
	probeDial       = 2 * time.Second
)

type Client struct {
	cli *client.Client
}

// New connects to the local daemon and pings it — an unreachable socket must
// crash the worker at startup, not on the first deploy (config philosophy).
func New() (*Client, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("create docker client: %w", err)
	}
	if _, err := cli.Ping(context.Background()); err != nil {
		cli.Close()
		return nil, fmt.Errorf("docker daemon unreachable: %w", err)
	}
	return &Client{cli: cli}, nil
}

// EnsureProjectNetwork creates net-<projectId> if missing (idempotent) and
// returns its name. The label marks it as platform-owned so nothing sweeps it
// by accident.
func (c *Client) EnsureProjectNetwork(ctx context.Context, projectID string) (string, error) {
	name := "net-" + projectID
	_, err := c.cli.NetworkCreate(ctx, name, network.CreateOptions{
		Labels: map[string]string{"platform.project": projectID},
	})
	if err != nil && !errdefs.IsConflict(err) {
		return "", fmt.Errorf("create network %s: %w", name, err)
	}
	return name, nil
}

// ConnectContainer attaches a platform container (traefik, student-db) to the
// project network. "Already connected" is success — every later deploy for
// this project re-runs the same step.
func (c *Client) ConnectContainer(ctx context.Context, networkName, containerName string) error {
	if err := c.cli.NetworkConnect(ctx, networkName, containerName, nil); err != nil &&
		!errdefs.IsConflict(err) && !strings.Contains(err.Error(), "already exists") {
		return fmt.Errorf("connect %s to %s: %w", containerName, networkName, err)
	}
	return nil
}

// JoinForProbe connects THIS worker container to the project network so it can
// dial the app by container name (plan §8.3.3), and returns the leave func.
// The leave uses a fresh context: the caller's may already be cancelled by the
// time the probe finishes, and a leaked connection would pin the network.
func (c *Client) JoinForProbe(ctx context.Context, networkName, workerID string) (func(), error) {
	if err := c.cli.NetworkConnect(ctx, networkName, workerID, nil); err != nil &&
		!errdefs.IsConflict(err) && !strings.Contains(err.Error(), "already exists") {
		return nil, fmt.Errorf("connect worker to %s: %w", networkName, err)
	}
	return func() {
		leaveCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := c.cli.NetworkDisconnect(leaveCtx, networkName, workerID, false); err != nil {
			// Best effort: a stuck connection just keeps the worker on a
			// student network until it restarts — not worth failing over.
			_ = err
		}
	}, nil
}

// StudentSpec describes the container to run (plan §8.3 step 2).
type StudentSpec struct {
	Name         string // app-<deploymentId> (≤63 chars, DNS-label limit)
	Image        string // deployments.image_ref
	Network      string // net-<projectId>, the ONLY network
	Env          []string
	ProjectID    string // label platform.project
	DeploymentID string // label platform.deployment
}

// RunStudent removes any leftover with the same name (idempotent retries),
// then creates and starts the container with the plan §8.3 limits. On a
// create/start failure the half-made container is removed best-effort — the
// next retry's leftover sweep guarantees cleanup either way.
func (c *Client) RunStudent(ctx context.Context, spec StudentSpec) error {
	if _, err := c.cli.ContainerInspect(ctx, spec.Name); err == nil {
		if err := c.cli.ContainerRemove(ctx, spec.Name, container.RemoveOptions{Force: true}); err != nil &&
			!errdefs.IsNotFound(err) {
			return fmt.Errorf("remove leftover %s: %w", spec.Name, err)
		}
	} else if !errdefs.IsNotFound(err) {
		return fmt.Errorf("inspect %s: %w", spec.Name, err)
	}

	config := &container.Config{
		Image: spec.Image,
		Env:   spec.Env,
		Labels: map[string]string{
			"platform.project":    spec.ProjectID,
			"platform.deployment": spec.DeploymentID,
		},
	}
	// Resource limits live on the embedded Resources struct (plan §8.3 step 2).
	pids := int64(pidsLimit)
	host := &container.HostConfig{
		Resources: container.Resources{
			NanoCPUs:   cpuLimit,
			Memory:     memoryLimit,
			MemorySwap: memoryLimit, // swap == memory → effectively no swap
			PidsLimit:  &pids,       // *int64: nil = leave daemon default alone
		},
		RestartPolicy: container.RestartPolicy{Name: container.RestartPolicyOnFailure, MaximumRetryCount: restartMaxRetry},
		LogConfig: container.LogConfig{
			Type:   "json-file",
			Config: map[string]string{"max-size": "10m", "max-file": "3"},
		},
		SecurityOpt: []string{"no-new-privileges"}, // plan: not privileged, no socket
		NetworkMode: container.NetworkMode(spec.Network),
	}
	networking := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{spec.Network: {}},
	}

	created, err := c.cli.ContainerCreate(ctx, config, host, networking, nil, spec.Name)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return fmt.Errorf("%w: %s", ErrImageMissing, spec.Image)
		}
		return fmt.Errorf("create %s: %w", spec.Name, err)
	}
	if err := c.cli.ContainerStart(ctx, created.ID, container.StartOptions{}); err != nil {
		_ = c.cli.ContainerRemove(ctx, created.ID, container.RemoveOptions{Force: true})
		return fmt.Errorf("start %s: %w", spec.Name, err)
	}
	return nil
}

// ProbeTCP dials host:port until it accepts or probeTimeout passes (plan
// §8.3.3). Accepting TCP is the health signal for M4: the app process is up
// and bound — anything smarter (HTTP codes) arrives when the spec asks for it.
func ProbeTCP(ctx context.Context, addr string) error {
	deadline := time.Now().Add(probeTimeout)
	var lastErr error
	for {
		dialer := net.Dialer{Timeout: probeDial}
		conn, err := dialer.DialContext(ctx, "tcp", addr)
		if err == nil {
			conn.Close()
			return nil
		}
		lastErr = err
		if time.Now().After(deadline) {
			return fmt.Errorf("no answer on %s after %s: %w", addr, probeTimeout, lastErr)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(probeInterval):
		}
	}
}

// StopProjectContainers stops every running container of the project with a
// grace period, KEEPING them for a later start (plan §8.6 stop — reversible
// without a rebuild). Docker's on-failure restart policy never resurrects a
// manually stopped container, which is exactly the "not undone by the restart
// policy" clause in the plan; nothing extra is needed for that.
func (c *Client) StopProjectContainers(ctx context.Context, projectID string, grace time.Duration) error {
	containers, err := c.listProject(ctx, true, projectID)
	if err != nil {
		return err
	}
	seconds := int(grace.Seconds())
	for _, ctr := range containers {
		if ctr.State != "running" {
			continue
		}
		if err := c.cli.ContainerStop(ctx, ctr.ID, container.StopOptions{Timeout: &seconds}); err != nil &&
			!errdefs.IsNotFound(err) {
			return fmt.Errorf("stop %s: %w", ctr.Names[0], err)
		}
	}
	return nil
}

// StartContainer starts a previously stopped container (plan §8.6 start:
// reuse it when it still exists — no rebuild). ok=false means there is
// nothing to start; the caller then rebuilds from image_ref. Starting an
// already-running container is a Docker "not modified" — success here.
func (c *Client) StartContainer(ctx context.Context, name string) (ok bool, err error) {
	if _, err := c.cli.ContainerInspect(ctx, name); err != nil {
		if errdefs.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("inspect %s: %w", name, err)
	}
	if err := c.cli.ContainerStart(ctx, name, container.StartOptions{}); err != nil &&
		!errdefs.IsNotModified(err) {
		return false, fmt.Errorf("start %s: %w", name, err)
	}
	return true, nil
}

// RemoveContainer force-removes one container by name (plan §8.3 step 4:
// the failed new container goes, the old one stays). Missing = success.
func (c *Client) RemoveContainer(ctx context.Context, name string) error {
	if err := c.cli.ContainerRemove(ctx, name, container.RemoveOptions{Force: true}); err != nil &&
		!errdefs.IsNotFound(err) {
		return fmt.Errorf("remove %s: %w", name, err)
	}
	return nil
}

// RemoveProjectContainers force-removes every container of the project except
// keep (plan §8.3 step 5: the switch — old versions go only after the new
// container is proven healthy and the route already points at it).
func (c *Client) RemoveProjectContainers(ctx context.Context, projectID, keep string) error {
	containers, err := c.listProject(ctx, true, projectID)
	if err != nil {
		return err
	}
	for _, ctr := range containers {
		name := strings.TrimPrefix(ctr.Names[0], "/")
		if name == keep {
			continue
		}
		if err := c.cli.ContainerRemove(ctx, ctr.ID, container.RemoveOptions{Force: true}); err != nil &&
			!errdefs.IsNotFound(err) {
			return fmt.Errorf("remove old container %s: %w", name, err)
		}
	}
	return nil
}

// RunningStudentContainers counts running student containers on this node —
// the capacity guard of plan §8.2 step 1 (all replicas share one daemon here;
// with several machines this becomes per-node).
func (c *Client) RunningStudentContainers(ctx context.Context) (int, error) {
	// Existence filter: just "has the platform.project label".
	args := filters.NewArgs(filters.Arg("label", "platform.project"))
	list, err := c.cli.ContainerList(ctx, container.ListOptions{Filters: args})
	if err != nil {
		return 0, fmt.Errorf("list student containers: %w", err)
	}
	return len(list), nil
}

func (c *Client) listProject(ctx context.Context, all bool, projectID string) ([]types.Container, error) {
	args := filters.NewArgs(filters.Arg("label", "platform.project="+projectID))
	list, err := c.cli.ContainerList(ctx, container.ListOptions{All: all, Filters: args})
	if err != nil {
		return nil, fmt.Errorf("list containers of project %s: %w", projectID, err)
	}
	return list, nil
}
