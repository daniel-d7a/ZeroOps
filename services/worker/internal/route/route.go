// Package route writes Traefik's file-provider configuration for student apps
// (plan §8.4).
//
// Traefik watches the dynamic directory; one file per project:
// /dynamic/proj-<projectId>.yml. Two properties matter:
//
//   - Write to a temp file and rename: Traefik never reads a half-written file
//     (rename is atomic on the same filesystem — the volume).
//   - The file is written only AFTER the new container passed its health
//     probe, and the old file stays until then: traffic switches in one step,
//     and a failed deploy leaves the old version serving untouched.
//
// Stopping a project deletes its file — that is how "stop" takes the URL down.
package route

import (
	"fmt"
	"os"
	"path/filepath"
)

type Writer struct {
	Dir    string // Traefik's file-provider directory (DYNAMIC_CONFIG_DIR)
	Domain string // student URL suffix: <slug>.<Domain>
}

// Write makes <slug>.<Domain> route to containerName:3000 (plan §8.4).
func (w *Writer) Write(projectID, slug, containerName string) error {
	name := "proj-" + projectID
	// The rule needs Traefik's backticks around the host; YAML single quotes
	// keep them literal. No Traefik labels on containers — THIS file is the
	// entire routing story for student apps.
	host := "`" + slug + "." + w.Domain + "`"
	config := fmt.Sprintf(`http:
  routers:
    %s:
      rule: 'Host(%s)'
      entryPoints:
        - web
      service: %s
  services:
    %s:
      loadBalancer:
        servers:
          - url: http://%s:3000
`, name, host, name, name, containerName)

	if err := os.MkdirAll(w.Dir, 0o755); err != nil {
		return fmt.Errorf("create dynamic config dir: %w", err)
	}
	final := filepath.Join(w.Dir, "proj-"+projectID+".yml")
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, []byte(config), 0o644); err != nil {
		return fmt.Errorf("write route file: %w", err)
	}
	if err := os.Rename(tmp, final); err != nil {
		return fmt.Errorf("publish route file: %w", err)
	}
	return nil
}

// Delete removes the project's route (plan §8.6 stop: the URL must go down).
// Missing file is success — stop is idempotent.
func (w *Writer) Delete(projectID string) error {
	err := os.Remove(filepath.Join(w.Dir, "proj-"+projectID+".yml"))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete route file: %w", err)
	}
	return nil
}
