package config

import (
	"fmt"
	"os"
	"strings"
)

// Config holds the worker's environment configuration (plan §8).
// Fields are added here as milestones start consuming them.
type Config struct {
	NodeID string
	Roles  []string
}

var validRoles = map[string]bool{"build": true, "run": true}

// Load reads and validates configuration from the environment.
func Load() (Config, error) {
	cfg := Config{NodeID: envOr("NODE_ID", "node-1")}

	raw := envOr("WORKER_ROLES", "build,run")
	for _, r := range strings.Split(raw, ",") {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		if !validRoles[r] {
			return Config{}, fmt.Errorf("invalid role %q in WORKER_ROLES (want build,run)", r)
		}
		cfg.Roles = append(cfg.Roles, r)
	}
	if len(cfg.Roles) == 0 {
		return Config{}, fmt.Errorf("WORKER_ROLES is empty (want build,run)")
	}
	return cfg, nil
}

func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}
