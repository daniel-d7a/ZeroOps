// Railpack execution for the build role (plan §8.1 step 4).
//
// One command, `railpack build`, whose stdout+stderr IS the build log the
// student reads in the UI — so both streams are drained line by line and
// batched into deploy_logs (stream "build") while the build runs.
package build

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"zeroops/worker/internal/store"
)

const (
	// plan §8.1 step 4: "10-minute timeout".
	buildTimeout = 10 * time.Minute
	// plan §8.1 step 4: lines are "batched about once per second".
	logFlushInterval = time.Second
	// Upper bound for one deploy_logs INSERT (also bounds collector memory).
	maxLogBatch = 500
)

// imageTag is the ONLY place an image reference is constructed (AGENTS
// invariant): it is stored in deployments.image_ref on success and read back
// from there by the run role — never hardcoded anywhere else.
func imageTag(projectID, deploymentID string) string {
	return fmt.Sprintf("proj-%s:%s", projectID, deploymentID)
}

// imageExists asks the local Docker daemon whether the tag is already built —
// the repair path (redelivery after a crash between build and MarkBuilt)
// must skip instead of rebuilding from scratch (plan §8.1 step 4).
//
// ponytail: docker CLI exec, not the Go SDK. The SDK arrives with M4's
// container work; a one-shot inspect doesn't justify the plumbing yet.
// Default when the daemon is unreachable: false → rebuild (safe: the image
// may not exist, and railpack will hit the same daemon anyway).
func imageExists(tag string) bool {
	return exec.Command("docker", "image", "inspect", tag).Run() == nil
}

// buildkitPreflight fails fast (transient) when the BuildKit daemon this
// worker builds through is down. Without it, railpack's own connection error
// would surface as a non-zero exit and be misclassified as a permanent
// "student code is broken" failure — an infra problem must stay retryable.
//
// ponytail: only inspects the docker-container:// form (our compose setup);
// remote BUILDKIT_HOST URLs are railpack's business to probe.
func buildkitPreflight() error {
	const prefix = "docker-container://"
	host := os.Getenv("BUILDKIT_HOST")
	if !strings.HasPrefix(host, prefix) {
		return nil
	}
	container := strings.TrimPrefix(host, prefix)
	out, err := exec.Command("docker", "inspect", "-f", "{{.State.Running}}", container).Output()
	if err != nil || strings.TrimSpace(string(out)) != "true" {
		return fmt.Errorf("buildkit %q is not running (build infrastructure down)", container)
	}
	return nil
}

// runRailpack runs `railpack build --name <tag> --progress plain <root>`,
// streaming every output line into deploy_logs (stream "build") batched about
// once per second, and returns:
//
//	nil                      → exit 0, image built
//	permanent error           → railpack exited non-zero: the STUDENT's code
//	                            cannot build (npm error, compile error, ...).
//	                            Full output is in the build log.
//	plain error (transient)   → infra: 10-minute timeout, worker shutdown,
//	                            railpack missing, BuildKit unreachable.
//
// Log-insert failures mid-build are dropped to the worker log (slog) instead
// of aborting: the status row stays authoritative, and MarkBuilt needs a
// healthy DB seconds later anyway — if the DB is truly down, the whole build
// goes transient through the normal path.
func runRailpack(ctx context.Context, st *store.Store, logger *slog.Logger, deploymentID, root, tag string) error {
	if err := buildkitPreflight(); err != nil {
		return err
	}

	runCtx, cancel := context.WithTimeout(ctx, buildTimeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, "railpack",
		"build", "--name", tag, "--progress", "plain", root)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start railpack: %w", err)
	}

	// Two readers (stdout, stderr) feed one channel. bufio.Reader instead of
	// Scanner: no token size limit, so one pathological 10 MB log line can
	// never stall the pipe (a stalled pipe would hang the build to timeout).
	lines := make(chan string, 256)
	var readers sync.WaitGroup
	drain := func(r io.Reader) {
		defer readers.Done()
		reader := bufio.NewReader(r)
		for {
			line, err := reader.ReadString('\n')
			if line != "" {
				lines <- strings.TrimRight(line, "\r\n")
			}
			if err != nil {
				return // EOF, or pipe closed after the process was killed
			}
		}
	}
	readers.Add(2)
	go drain(stdout)
	go drain(stderr)
	go func() { readers.Wait(); close(lines) }() // closed ⇒ both pipes done

	// Collector: batch lines, flush on the 1s tick or the batch cap. The
	// channel closing means railpack exited (CommandContext kills it on
	// timeout/shutdown) and every line is in — flush, then reap.
	var batch []string
	ticker := time.NewTicker(logFlushInterval)
	defer ticker.Stop()
	flush := func() {
		if len(batch) == 0 {
			return
		}
		if err := st.AppendLogs(ctx, deploymentID, "build", batch); err != nil {
			logger.Error("build log batch dropped", "lines", len(batch), "error", err)
		}
		batch = batch[:0]
	}

	for {
		select {
		case line, open := <-lines:
			if !open {
				flush()
				return classifyBuild(runCtx, cmd.Wait(), tag)
			}
			batch = append(batch, line)
			if len(batch) >= maxLogBatch {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

// classifyBuild maps how railpack ended onto the plan §6 failure classes.
func classifyBuild(runCtx context.Context, waitErr error, tag string) error {
	if waitErr == nil {
		return nil
	}
	switch {
	case errors.Is(runCtx.Err(), context.DeadlineExceeded):
		return fmt.Errorf("build timed out after %s (image %s)", buildTimeout, tag) // transient
	case errors.Is(runCtx.Err(), context.Canceled):
		return fmt.Errorf("build interrupted by shutdown: %w", waitErr) // transient
	default:
		// railpack died on its own: the app's build is broken (plan §6
		// "user build failure" → permanent). The details are in the build log.
		code := -1
		var exit *exec.ExitError
		if errors.As(waitErr, &exit) {
			code = exit.ExitCode()
		}
		return permanentf("build failed (railpack exit %d) — see build logs", code)
	}
}
