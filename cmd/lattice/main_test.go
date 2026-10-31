package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rgbyte/lattice/pkg/admin"
	"github.com/rgbyte/lattice/pkg/engine"
	"github.com/rgbyte/lattice/pkg/server"
)

func TestCLI_RemoteThinClient_And_LocalDBModes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "cli_test.db")

	eng, err := engine.NewDefaultLocal(ctx, dbPath, 384)
	if err != nil {
		t.Fatalf("NewDefaultLocal: %v", err)
	}
	defer eng.Close()

	adm, err := admin.NewService(eng)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	srv, err := server.New(eng, adm, server.Config{AdminToken: "secret-cli-token"})
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	ts := httptest.NewServer(srv)
	defer ts.Close()

	runCLI := func(args ...string) (int, string, string) {
		var outBuf, errBuf bytes.Buffer
		fullArgs := append([]string{"--server", ts.URL, "--token", "secret-cli-token"}, args...)
		code := Run(ctx, fullArgs, &outBuf, &errBuf)
		return code, outBuf.String(), errBuf.String()
	}

	// 1. Index two documents via 'admin index-doc'
	code, out, errStr := runCLI("admin", "index-doc", "doc:arch-v1", "Legacy Storage", "We use flat files with @alice.", "*@acme.io")
	if code != 0 {
		t.Fatalf("admin index-doc v1 failed (%d): %s", code, errStr)
	}
	if !strings.Contains(out, `"doc_id": "doc:arch-v1"`) {
		t.Fatalf("unexpected index-doc output: %s", out)
	}

	code, _, errStr = runCLI("admin", "index-doc", "doc:arch-v2", "Lattice HNSW Storage", "Supersedes: doc:arch-v1\nDecision: We decided to adopt HNSW + CSR with @alice and @rohit.", "*@acme.io")
	if code != 0 {
		t.Fatalf("admin index-doc v2 failed (%d): %s", code, errStr)
	}

	// 2. Run 'overview'
	code, out, errStr = runCLI("overview")
	if code != 0 {
		t.Fatalf("overview failed (%d): %s", code, errStr)
	}
	var ov admin.OverviewReport
	if err := json.Unmarshal([]byte(out), &ov); err != nil {
		t.Fatalf("unmarshal overview: %v", err)
	}
	if ov.VectorCount < 2 {
		t.Fatalf("expected >= 2 vectors, got %d", ov.VectorCount)
	}

	// 3. Run 'validate'
	code, out, errStr = runCLI("validate")
	if code != 0 {
		t.Fatalf("validate failed (%d): %s", code, errStr)
	}
	if !strings.Contains(out, `"valid": true`) {
		t.Fatalf("expected valid: true, got %s", out)
	}

	// 4. Run 'inspect-doc'
	code, out, errStr = runCLI("inspect-doc", "doc:arch-v1")
	if code != 0 {
		t.Fatalf("inspect-doc failed (%d): %s", code, errStr)
	}
	if !strings.Contains(out, `"superseded_by_id": "doc:arch-v2"`) {
		t.Fatalf("expected doc:arch-v1 to show superseded_by_id doc:arch-v2, got %s", out)
	}

	// 5. Run 'inspect-node' & 'explain'
	code, out, errStr = runCLI("--user-id", "rohit", "--email", "rohit@acme.io", "inspect-node", "person:alice")
	if code != 0 {
		t.Fatalf("inspect-node failed (%d): %s", code, errStr)
	}
	if !strings.Contains(out, `"person:alice"`) {
		t.Fatalf("unexpected inspect-node output: %s", out)
	}

	code, out, errStr = runCLI("--user-id", "rohit", "--email", "rohit@acme.io", "explain", "doc:arch-v2", "doc:arch-v1")
	if code != 0 {
		t.Fatalf("explain failed (%d): %s", code, errStr)
	}
	if !strings.Contains(out, `"SUPERSEDES"`) {
		t.Fatalf("expected SUPERSEDES in explain output: %s", out)
	}

	// 6. Run 'simulate-acl'
	code, out, errStr = runCLI("--user-id", "rohit", "--email", "rohit@acme.io", "simulate-acl", "doc:arch-v2")
	if code != 0 {
		t.Fatalf("simulate-acl failed (%d): %s", code, errStr)
	}
	if !strings.Contains(out, `"allowed": true`) {
		t.Fatalf("expected allowed: true in simulate-acl: %s", out)
	}

	// 7. Run 'admin enrich-sweep', 'search', 'admin rebuild', and 'admin delete-doc'
	code, _, errStr = runCLI("admin", "enrich-sweep")
	if code != 0 {
		t.Fatalf("enrich-sweep failed (%d): %s", code, errStr)
	}

	code, out, errStr = runCLI("--user-id", "rohit", "--email", "rohit@acme.io", "search", "HNSW CSR storage")
	if code != 0 {
		t.Fatalf("search failed (%d): %s", code, errStr)
	}
	if !strings.Contains(out, `"doc:arch-v2"`) {
		t.Fatalf("expected doc:arch-v2 in search hits: %s", out)
	}

	code, _, errStr = runCLI("admin", "rebuild")
	if code != 0 {
		t.Fatalf("rebuild failed (%d): %s", code, errStr)
	}

	code, _, errStr = runCLI("admin", "delete-doc", "doc:arch-v1")
	if code != 0 {
		t.Fatalf("delete-doc failed (%d): %s", code, errStr)
	}
}
