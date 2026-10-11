package store

// Claim-query tests (plan §11). These are SQL against a real Postgres — the
// SKIP LOCKED/CAS semantics are exactly what a mock would fake — so they run
// only when TEST_DATABASE_URL points at a migrated platform-db (scripts/
// e2e.sh sets it; plain `go test ./...` skips). Fixture rows are random UUIDs
// and removed in cleanup, so running against the live stack is safe.

import (
	"context"
	"os"
	"testing"
)

func TestClaimQueries(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set — claim queries need a live platform-db (scripts/e2e.sh runs these)")
	}
	ctx := context.Background()
	s, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := s.pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("exec %s: %v", sql, err)
		}
	}
	uuid := func() string {
		t.Helper()
		var id string
		if err := s.pool.QueryRow(ctx, `SELECT gen_random_uuid()::text`).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}

	var userID, projectID, sourceID, deploymentID = uuid(), uuid(), uuid(), uuid()
	exec(`INSERT INTO users (id, email, password_hash) VALUES ($1::uuid, 'claims-'||$1::text||'@test.local', 'x')`, userID)
	exec(`INSERT INTO projects (id, user_id, name, slug) VALUES ($1::uuid, $2::uuid, 'claims-test', 'c-'||$1::text)`, projectID, userID)
	exec(`INSERT INTO sources (id, project_id, path, size_bytes, sha256, original_filename)
	      VALUES ($1::uuid, $2::uuid, '/tmp/claims.zip', 1, repeat('a', 64), 'claims.zip')`, sourceID, projectID)
	exec(`INSERT INTO deployments (id, project_id, source_id) VALUES ($1::uuid, $2::uuid, $3::uuid)`, deploymentID, projectID, sourceID)
	t.Cleanup(func() {
		ctx := context.Background()
		for _, del := range []struct{ sql, id string }{
			{`DELETE FROM deployments WHERE id = $1`, deploymentID},
			{`DELETE FROM sources WHERE id = $1`, sourceID},
			{`DELETE FROM projects WHERE id = $1`, projectID},
			{`DELETE FROM users WHERE id = $1`, userID},
		} {
			_, _ = s.pool.Exec(ctx, del.sql, del.id)
		}
	})

	state := func() (status, worker string, startedNil bool) {
		t.Helper()
		err := s.pool.QueryRow(ctx,
			`SELECT status, COALESCE(worker_id, ''), started_at IS NULL FROM deployments WHERE id = $1`,
			deploymentID).Scan(&status, &worker, &startedNil)
		if err != nil {
			t.Fatal(err)
		}
		return
	}

	// --- build claim -------------------------------------------------------
	if claimed, _, err := s.ClaimBuild(ctx, deploymentID, "w1"); err != nil || !claimed {
		t.Fatalf("first claim: claimed=%v err=%v", claimed, err)
	}
	if status, worker, startedNil := state(); status != "building" || worker != "w1" || startedNil {
		t.Fatalf("after claim: status=%s worker=%s startedNil=%v", status, worker, startedNil)
	}

	// duplicate delivery: second worker sees "building", does no harm
	if claimed, status, err := s.ClaimBuild(ctx, deploymentID, "w2"); err != nil || claimed || status != "building" {
		t.Fatalf("duplicate claim: claimed=%v status=%q err=%v", claimed, status, err)
	}

	// started_at must survive a steal (COALESCE in ClaimBuild)
	var startedAt1 string
	if err := s.pool.QueryRow(ctx, `SELECT started_at::text FROM deployments WHERE id = $1`, deploymentID).Scan(&startedAt1); err != nil {
		t.Fatal(err)
	}

	// --- stale heartbeat is stealable after 2 minutes ----------------------
	exec(`UPDATE deployments SET heartbeat_at = now() - interval '3 minutes' WHERE id = $1`, deploymentID)
	if claimed, _, err := s.ClaimBuild(ctx, deploymentID, "w2"); err != nil || !claimed {
		t.Fatalf("steal claim: claimed=%v err=%v", claimed, err)
	}
	var startedAt2 string
	if err := s.pool.QueryRow(ctx, `SELECT started_at::text FROM deployments WHERE id = $1`, deploymentID).Scan(&startedAt2); err != nil {
		t.Fatal(err)
	}
	if startedAt1 != startedAt2 {
		t.Fatalf("started_at moved on steal: %s -> %s", startedAt1, startedAt2)
	}

	// --- MarkBuilt is a CAS on (status, worker_id) -------------------------
	if ok, err := s.MarkBuilt(ctx, deploymentID, "w1", "stolen:img"); err != nil || ok {
		t.Fatalf("stale owner must not write: ok=%v err=%v", ok, err)
	}
	if _, worker, _ := state(); worker != "w2" {
		t.Fatalf("stale MarkBuilt overwrote owner: worker=%s", worker)
	}
	if ok, err := s.MarkBuilt(ctx, deploymentID, "w2", "proj:dep1"); err != nil || !ok {
		t.Fatalf("owner MarkBuilt: ok=%v err=%v", ok, err)
	}

	// --- run claim + release rules -----------------------------------------
	if claimed, _, err := s.ClaimRun(ctx, deploymentID, "w3"); err != nil || !claimed {
		t.Fatalf("claim run: claimed=%v err=%v", claimed, err)
	}
	// the M5.4 guard: 'starting' is never a valid release target
	if err := s.ReleaseClaim(ctx, deploymentID, "w3", "starting"); err == nil {
		t.Fatal("ReleaseClaim('starting') must be rejected")
	}
	// non-owner release is a no-op (CAS on worker_id)
	if err := s.ReleaseClaim(ctx, deploymentID, "w1", "built"); err != nil {
		t.Fatalf("non-owner release: %v", err)
	}
	if status, worker, _ := state(); status != "starting" || worker != "w3" {
		t.Fatalf("non-owner release changed state: status=%s worker=%s", status, worker)
	}
	// owner hands it back to 'built' so the retry can re-claim (§6)
	if err := s.ReleaseClaim(ctx, deploymentID, "w3", "built"); err != nil {
		t.Fatalf("owner release: %v", err)
	}
	if status, worker, _ := state(); status != "built" || worker != "" {
		t.Fatalf("after release: status=%s worker=%q", status, worker)
	}
	if claimed, _, err := s.ClaimRun(ctx, deploymentID, "w4"); err != nil || !claimed {
		t.Fatalf("re-claim after release: claimed=%v err=%v", claimed, err)
	}

	// --- terminal states are unclaimable -----------------------------------
	if err := s.MarkFailed(ctx, deploymentID, "boom"); err != nil {
		t.Fatalf("mark failed: %v", err)
	}
	if status, _, _ := state(); status != "failed" {
		t.Fatalf("status=%s, want failed", status)
	}
	if claimed, status, err := s.ClaimBuild(ctx, deploymentID, "w5"); err != nil || claimed || status != "failed" {
		t.Fatalf("claim after failure: claimed=%v status=%q err=%v", claimed, status, err)
	}
	// MarkFailed only flips a project that is 'deploying' — ours was 'new'
	var projectStatus string
	if err := s.pool.QueryRow(ctx, `SELECT status FROM projects WHERE id = $1`, projectID).Scan(&projectStatus); err != nil {
		t.Fatal(err)
	}
	if projectStatus != "new" {
		t.Fatalf("project status=%s, want untouched 'new'", projectStatus)
	}

	// unknown deployment: no claim, no error, empty status
	if claimed, status, err := s.ClaimBuild(ctx, uuid(), "w1"); err != nil || claimed || status != "" {
		t.Fatalf("missing deployment: claimed=%v status=%q err=%v", claimed, status, err)
	}
}
