package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/rgbyte/lattice/pkg/admin"
	"github.com/rgbyte/lattice/pkg/engine"
	"github.com/rgbyte/lattice/pkg/server"
	"github.com/rgbyte/lattice/pkg/types"
)

func TestClientEndToEndAllOperations(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "client_e2e.db")
	eng, err := engine.NewDefaultLocal(ctx, dbPath, 64)
	if err != nil {
		t.Fatalf("NewDefaultLocal: %v", err)
	}
	defer eng.Close()

	adm, err := admin.NewService(eng)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	const adminTok = "lattice-e2e-admin-secret"
	srv, err := server.New(eng, adm, server.Config{
		AdminToken: adminTok,
	})
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}

	ts := httptest.NewServer(srv)
	defer ts.Close()

	// 1. Unauthorized client must fail with 401 APIError
	badClient := New(ts.URL, WithAdminToken("wrong-token"), WithTimeout(5*time.Second))
	if _, err := badClient.Overview(ctx); err == nil {
		t.Fatalf("expected 401 error from badClient.Overview, got nil")
	} else {
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusUnauthorized {
			t.Fatalf("expected APIError with StatusCode 401, got %v", err)
		}
	}

	// 2. Authorized client
	cli := New(ts.URL, WithAdminToken(adminTok), WithHTTPClient(ts.Client()), WithTimeout(10*time.Second))

	// 3. Index two documents (RFC-1 and RFC-2 which supersedes RFC-1) + one restricted security doc
	doc1 := types.Document{
		ID:          "doc:rfc-1",
		SourceType:  "markdown",
		DocType:     "rfc",
		Title:       "Legacy Storage Architecture v1",
		Content:     "# Storage v1\nUses SQLite WAL storage engine.\nContact @bob for details.",
		AuthorID:    "alice",
		Status:      types.StatusNormal,
		Permissions: []string{"group:eng"},
	}
	rec1, err := cli.IndexDocument(ctx, doc1, false)
	if err != nil {
		t.Fatalf("IndexDocument doc1: %v", err)
	}
	if rec1.DocID != "doc:rfc-1" || rec1.ChunksIndexed == 0 {
		t.Errorf("unexpected receipt for doc1: %+v", rec1)
	}

	doc2 := types.Document{
		ID:          "doc:rfc-2",
		SourceType:  "markdown",
		DocType:     "rfc",
		Title:       "Modern Hybrid CSR HNSW Storage Architecture v2",
		Content:     "# Storage v2\nSupersedes: doc:rfc-1\nDecision: adopt hybrid CSR + HNSW + SQLite WAL architecture.\nReviewed by @bob and @charlie.",
		AuthorID:    "alice",
		Status:      types.StatusVerified,
		Permissions: []string{"group:eng"},
	}
	rec2, err := cli.IndexDocument(ctx, doc2, false)
	if err != nil {
		t.Fatalf("IndexDocument doc2: %v", err)
	}
	if !rec2.Changed {
		t.Errorf("expected doc2 Changed = true")
	}

	secDoc := types.Document{
		ID:          "doc:sec-bridge",
		SourceType:  "markdown",
		DocType:     "postmortem",
		Title:       "Classified Security Incident Bridge",
		Content:     "Confidential incident report.",
		AuthorID:    "sec_lead",
		Status:      types.StatusVerified,
		Permissions: []string{"group:security"},
	}
	if _, err := cli.IndexDocument(ctx, secDoc, false); err != nil {
		t.Fatalf("IndexDocument secDoc: %v", err)
	}

	// 4. Overview check
	ov, err := cli.Overview(ctx)
	if err != nil {
		t.Fatalf("Overview: %v", err)
	}
	if ov.VectorCount < 3 || ov.GraphStats.NodeCount < 3 {
		t.Errorf("unexpected Overview counts: %+v", ov)
	}

	// 5. InspectDocument (verify doc:rfc-1 shows superseded by doc:rfc-2)
	docInsp, err := cli.InspectDocument(ctx, "doc:rfc-1")
	if err != nil {
		t.Fatalf("InspectDocument: %v", err)
	}
	if !docInsp.Supersession.Superseded || docInsp.Supersession.SupersededByID != "doc:rfc-2" {
		t.Errorf("expected doc:rfc-1 to be superseded by doc:rfc-2, got %+v", docInsp.Supersession)
	}

	// 6. InspectNode
	nodeInsp, err := cli.InspectNode(ctx, "person:alice", 2, 25, "alice", "alice@example.com", []string{"eng"})
	if err != nil {
		t.Fatalf("InspectNode: %v", err)
	}
	if nodeInsp.Node.ID != "person:alice" || len(nodeInsp.Neighbors) == 0 {
		t.Errorf("unexpected InspectNode result: %+v", nodeInsp)
	}

	// 7. UpsertEdge & ExplainConnection (test ACL bridge blocking)
	// Connect doc:rfc-2 -> doc:sec-bridge -> ticket:SEC-999
	_ = eng.GraphIndex().UpsertNode(types.Node{
		ID:          "ticket:SEC-999",
		Kind:        types.NodeTicket,
		Title:       "SEC-999",
		Permissions: []string{"group:eng", "group:security"},
	})
	if err := cli.UpsertEdge(ctx, types.Edge{
		SourceID:   "doc:rfc-2",
		SourceKind: types.NodeDocument,
		TargetID:   "doc:sec-bridge",
		TargetKind: types.NodeDocument,
		Relation:   types.RelReferences,
		Weight:     1.0,
		Confidence: 1.0,
		Provenance: types.ProvManual,
	}); err != nil {
		t.Fatalf("UpsertEdge 1: %v", err)
	}
	if err := cli.UpsertEdge(ctx, types.Edge{
		SourceID:   "doc:sec-bridge",
		SourceKind: types.NodeDocument,
		TargetID:   "ticket:SEC-999",
		TargetKind: types.NodeTicket,
		Relation:   types.RelReferences,
		Weight:     1.0,
		Confidence: 1.0,
		Provenance: types.ProvManual,
	}); err != nil {
		t.Fatalf("UpsertEdge 2: %v", err)
	}

	// For a user in group:eng (not in group:security), the path doc:rfc-2 -> doc:sec-bridge -> ticket:SEC-999
	// exists globally but is blocked by the unauthorized intermediate bridge doc:sec-bridge!
	exp, err := cli.ExplainConnection(ctx, "doc:rfc-2", "ticket:SEC-999", 4, "alice", "alice@example.com", []string{"eng"})
	if err != nil {
		t.Fatalf("ExplainConnection: %v", err)
	}
	if len(exp.GlobalPath) != 2 || !exp.BlockedByACL || len(exp.PrincipalPath) != 0 {
		t.Errorf("expected path to be blocked by ACL bridge! got GlobalPath=%d, PrincipalPath=%d, BlockedByACL=%v",
			len(exp.GlobalPath), len(exp.PrincipalPath), exp.BlockedByACL)
	}

	// 8. ValidateIntegrity
	valRep, err := cli.ValidateIntegrity(ctx)
	if err != nil {
		t.Fatalf("ValidateIntegrity: %v", err)
	}
	if !valRep.Valid || valRep.ErrorCount != 0 {
		t.Errorf("expected Valid=true and 0 errors, got %+v", valRep)
	}

	// 9. SimulateAccess
	simRep, err := cli.SimulateAccess(ctx, "alice", "alice@example.com", []string{"eng"}, []string{"doc:rfc-2", "doc:sec-bridge"})
	if err != nil {
		t.Fatalf("SimulateAccess: %v", err)
	}
	if len(simRep.Decisions) != 2 || !simRep.Decisions[0].Allowed || simRep.Decisions[1].Allowed {
		t.Errorf("unexpected SimulateAccess decisions: %+v", simRep.Decisions)
	}

	// 10. Search (verify doc:rfc-2 ranks above superseded doc:rfc-1 and doc:sec-bridge is filtered out)
	sRes, err := cli.Search(ctx, "SQLite WAL storage architecture", "alice", "alice@example.com", []string{"eng"}, 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(sRes.Hits) < 2 {
		t.Fatalf("expected at least 2 hits, got %d", len(sRes.Hits))
	}
	if sRes.Hits[0].Document.ID != "doc:rfc-2" {
		t.Errorf("expected authoritative doc:rfc-2 to rank #1 above superseded doc:rfc-1, got %s", sRes.Hits[0].Document.ID)
	}
	for _, h := range sRes.Hits {
		if h.Document.ID == "doc:sec-bridge" {
			t.Errorf("ACL violation: unauthorized doc:sec-bridge leaked into search hits!")
		}
	}

	// 11. TriggerEnrichmentSweep
	sweepRep, err := cli.TriggerEnrichmentSweep(ctx, 10)
	if err != nil {
		t.Fatalf("TriggerEnrichmentSweep: %v", err)
	}
	if sweepRep.Succeeded < 3 {
		t.Errorf("expected at least 3 documents enriched in sweep, got %+v", sweepRep)
	}

	// 12. RebuildIndices
	rebOv, err := cli.RebuildIndices(ctx)
	if err != nil {
		t.Fatalf("RebuildIndices: %v", err)
	}
	if rebOv.VectorCount < 3 {
		t.Errorf("unexpected VectorCount after rebuild: %d", rebOv.VectorCount)
	}

	// 13. DeleteDocument
	if err := cli.DeleteDocument(ctx, "doc:rfc-1"); err != nil {
		t.Fatalf("DeleteDocument: %v", err)
	}
	if _, err := cli.InspectDocument(ctx, "doc:rfc-1"); err == nil {
		t.Errorf("expected 404 error inspecting deleted document doc:rfc-1")
	}
}
