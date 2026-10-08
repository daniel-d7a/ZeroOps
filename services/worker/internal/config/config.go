// Package config loads and validates the worker's environment (plan §8).
//
// Philosophy: every variable the worker needs is read and checked once, here,
// at startup — a missing AMQP_URL must crash the container immediately instead
// of surfacing hours later as a nil pointer in some job path.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// minSchemaMigration: rows required in drizzle.__drizzle_migrations before
// this worker runs (plan §2, AGENTS invariant). Bump when adding a migration.
const minSchemaMigration = 5 // 0000 users, 0001 sessions, 0002 projects, 0003 deployments, 0004 workers

// Config holds the worker's validated environment. Fields are added here as
// milestones start consuming them (BASE_DOMAIN, UPLOADS_DIR, ... arrive with
// M3/M4).
type Config struct {
	NodeID             string   // which node this replica belongs to (run.<node>.q)
	Roles              []string // which queues this replica consumes: build, run
	AMQPURL            string
	PlatformDBURL      string
	BuildPrefetch      int // unacked build jobs allowed at once (heavy → 1)
	RunPrefetch        int // unacked run jobs allowed at once (light → 4)
	MinSchemaMigration int
	ForceTransient     bool // test hook: every build job fails transiently (plan §11 gate)
}

// Roles the worker understands. Anything else in WORKER_ROLES is a typo and
// must not silently become "no consumers".
var validRoles = map[string]bool{"build": true, "run": true}

// Load reads and validates configuration from the environment. Returned error
// messages name the offending variable — they end up in container logs.
func Load() (Config, error) {
	workerConfig := Config{
		NodeID:             envOr("NODE_ID", "node-1"),
		MinSchemaMigration: minSchemaMigration,
	}

	// WORKER_ROLES is a comma-separated list: "build,run" (compose default).
	// The same binary can run as build-only or run-only replicas.
	rawRoles := envOr("WORKER_ROLES", "build,run")
	for _, role := range strings.Split(rawRoles, ",") {
		role = strings.TrimSpace(role)
		if role == "" { // tolerate "a,,b" and trailing commas
			continue
		}
		if !validRoles[role] {
			return Config{}, fmt.Errorf("invalid role %q in WORKER_ROLES (want build,run)", role)
		}
		workerConfig.Roles = append(workerConfig.Roles, role)
	}
	if len(workerConfig.Roles) == 0 {
		return Config{}, fmt.Errorf("WORKER_ROLES is empty (want build,run)")
	}

	var err error
	if workerConfig.BuildPrefetch, err = envInt("BUILD_PREFETCH", 1); err != nil {
		return Config{}, err
	}
	if workerConfig.BuildPrefetch < 1 {
		return Config{}, fmt.Errorf("BUILD_PREFETCH must be >= 1")
	}
	if workerConfig.RunPrefetch, err = envInt("RUN_PREFETCH", 4); err != nil {
		return Config{}, err
	}
	if workerConfig.RunPrefetch < 1 {
		return Config{}, fmt.Errorf("RUN_PREFETCH must be >= 1")
	}

	// No defaults for connection strings — a default would point at the wrong
	// broker/db in production and hide the misconfiguration.
	if workerConfig.AMQPURL = strings.TrimSpace(os.Getenv("AMQP_URL")); workerConfig.AMQPURL == "" {
		return Config{}, fmt.Errorf("AMQP_URL is not set")
	}
	if workerConfig.PlatformDBURL = strings.TrimSpace(os.Getenv("PLATFORM_DB_URL")); workerConfig.PlatformDBURL == "" {
		return Config{}, fmt.Errorf("PLATFORM_DB_URL is not set")
	}

	// FORCE_TRANSIENT: gate test hook (plan §11 "force a transient error").
	// Unset/empty is the normal case; a typo must error, not silently disable.
	switch strings.ToLower(envOr("FORCE_TRANSIENT", "")) {
	case "", "0", "false", "no", "off":
		workerConfig.ForceTransient = false
	case "1", "true", "yes", "on":
		workerConfig.ForceTransient = true
	default:
		return Config{}, fmt.Errorf("FORCE_TRANSIENT must be a boolean, got %q", os.Getenv("FORCE_TRANSIENT"))
	}
	return workerConfig, nil
}

// HasRole reports whether the given role is enabled for this replica.
func (workerConfig Config) HasRole(role string) bool {
	for _, enabledRole := range workerConfig.Roles {
		if enabledRole == role {
			return true
		}
	}
	return false
}

// envOr returns the trimmed variable, or defaultValue when unset/empty.
func envOr(key, defaultValue string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return defaultValue
}

// envInt parses an integer variable, or returns defaultValue when unset.
// Non-numeric values are config errors, not silently swallowed defaults.
func envInt(key string, defaultValue int) (int, error) {
	rawValue := strings.TrimSpace(os.Getenv(key))
	if rawValue == "" {
		return defaultValue, nil
	}
	parsedValue, err := strconv.Atoi(rawValue)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer, got %q", key, rawValue)
	}
	return parsedValue, nil
}
