package log

import (
	"log/slog"
	"os"
)

// New returns a JSON logger (plan §8.7) that stamps every line with
// worker_id — the worker's hostname. Callers add deployment_id/project_id/
// action_id per line with logger.With(...).
func New(workerID string) *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("worker_id", workerID)
}
