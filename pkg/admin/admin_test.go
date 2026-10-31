package admin

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/rgbyte/lattice/pkg/engine"
	"github.com/rgbyte/lattice/pkg/types"
)

func newTestAdminService(t *testing.T) (*Service, *engine.Engine) {
	t.Helper()
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "lattice_admin_test.db")
	eng, err := engine.NewDefaultLocal(ctx, dbPath, 64)
	if err != nil {
		t.Fatalf("NewDefaultLocal failed: %v", err)
	}
	t.Cleanup(func() {
		_ = eng.Close()
	})
	svc, err := NewService(eng)
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}
	return svc, eng
}

func TestAdmin_OverviewAndInspectDocumentAndNode(t *testing.T) {
	ctx := context.Background()
	svc, eng := newTestAdminService(t)

	doc1 := types.Document{
		ID:          "doc:rfc-auth",
		SourceType:  "github",
		DocType:     "rfc",
		Title:       "Zero-Trust ACL Architecture",
		Content:     "# Overview\nAlice (@alice) designed the ACL Engine for #security.\n\n## Details\nFail-closed validation.",
		AuthorID:    "alice",
		Status:      types.StatusVerified,
		Permissions: []string{"*@acme.io"},
	}
	if _, err := eng.IndexDocument(ctx, doc1, false); err != nil {
		t.Fatalf("IndexDocument doc1: %v", err)
	}

	overview, err := svc.SystemOverview(ctx)
	if err != nil {
		t.Fatalf("SystemOverview: %v", err)
	}
	if overview.VectorCount == 0 || overview.VectorDimension != 64 {
		t.Fatalf("unexpected vector overview: %+v", overview)
	}
	if overview.GraphStats.NodeCount == 0 || overview.PendingEnrichCount != 1 {
		t.Fatalf("unexpected graph/pending overview: %+v", overview)
	}

	// InspectDocument
	docInsp, err := svc.InspectDocument(ctx, "doc:rfc-auth")
	if err != nil {
		t.Fatalf("InspectDocument: %v", err)
	}
	if docInsp.Document.Title != doc1.Title {
		t.Fatalf("expected title %q, got %q", doc1.Title, docInsp.Document.Title)
	}
	if len(docInsp.Chunks) == 0 {
		t.Fatalf("expected non-empty chunks in DocumentInspection")
	}
	if len(docInsp.IncidentEdges) == 0 {
		t.Fatalf("expected incident edges on doc:rfc-auth")
	}

	// InspectNode with authorized principal
	alicePrincipal := types.NewPrincipalSet("alice", "alice@acme.io", []string{"eng"})
	nodeInsp, err := svc.InspectNode(ctx, "doc:rfc-auth", 2, 25, &alicePrincipal)
	if err != nil {
		t.Fatalf("InspectNode authorized: %v", err)
	}
	if nodeInsp.Node.ID != "doc:rfc-auth" || len(nodeInsp.Subgraph.Nodes) == 0 {
		t.Fatalf("unexpected NodeInspection: %+v", nodeInsp)
	}

	// InspectNode with unauthorized principal
	extPrincipal := types.NewPrincipalSet("ext", "bob@external.org", nil)
	extPrincipal.AllowPublic = false
	if _, err := svc.InspectNode(ctx, "doc:rfc-auth", 2, 25, &extPrincipal); err == nil {
		t.Fatalf("expected ErrUnauthorized for external principal inspecting internal node")
	}
}

func TestAdmin_ExplainConnection_BlockedByACL(t *testing.T) {
	ctx := context.Background()
	svc, eng := newTestAdminService(t)

	// Build a path: person:alice -> doc:confidential -> topic:merger
	_ = eng.GraphIndex().UpsertNode(types.Node{
		ID:          "person:alice",
		Kind:        types.NodePerson,
		Title:       "Alice",
		Permissions: []string{"*"},
	})
	_ = eng.GraphIndex().UpsertNode(types.Node{
		ID:          "doc:confidential",
		Kind:        types.NodeDocument,
		Title:       "Confidential M&A Memo",
		Permissions: []string{"group:exec"},
	})
	_ = eng.GraphIndex().UpsertNode(types.Node{
		ID:          "topic:merger",
		Kind:        types.NodeTopic,
		Title:       "Merger",
		Permissions: []string{"*"},
	})

	if err := svc.UpsertCustomEdge(ctx, types.Edge{
		SourceID:   "person:alice",
		SourceKind: types.NodePerson,
		TargetID:   "doc:confidential",
		TargetKind: types.NodeDocument,
		Relation:   types.RelAuthored,
		Weight:     3.0,
	}); err != nil {
		t.Fatalf("UpsertCustomEdge 1: %v", err)
	}
	if err := svc.UpsertCustomEdge(ctx, types.Edge{
		SourceID:   "doc:confidential",
		SourceKind: types.NodeDocument,
		TargetID:   "topic:merger",
		TargetKind: types.NodeTopic,
		Relation:   types.RelMentions,
		Weight:     3.0,
	}); err != nil {
		t.Fatalf("UpsertCustomEdge 2: %v", err)
	}

	// 1. General employee without "group:exec" cannot traverse doc:confidential
	emp := types.NewPrincipalSet("bob", "bob@acme.io", []string{"eng"})
	expBlocked, err := svc.ExplainConnection(ctx, "person:alice", "topic:merger", 4, &emp)
	if err != nil {
		t.Fatalf("ExplainConnection emp: %v", err)
	}
	if len(expBlocked.GlobalPath) != 2 {
		t.Fatalf("expected 2-hop GlobalPath, got %d", len(expBlocked.GlobalPath))
	}
	if len(expBlocked.PrincipalPath) != 0 {
		t.Fatalf("expected empty PrincipalPath for unauthorized employee, got %+v", expBlocked.PrincipalPath)
	}
	if !expBlocked.BlockedByACL {
		t.Fatalf("expected BlockedByACL=true when confidential bridge blocks path")
	}
	if expBlocked.PageRankScore != 0 {
		t.Fatalf("expected 0 PageRankScore across confidential bridge, got %f", expBlocked.PageRankScore)
	}

	// 2. Executive in "group:exec" can traverse the path
	exec := types.NewPrincipalSet("carol", "carol@acme.io", []string{"exec"})
	expAllowed, err := svc.ExplainConnection(ctx, "person:alice", "topic:merger", 4, &exec)
	if err != nil {
		t.Fatalf("ExplainConnection exec: %v", err)
	}
	if expAllowed.BlockedByACL {
		t.Fatalf("expected BlockedByACL=false for executive")
	}
	if len(expAllowed.PrincipalPath) != 2 {
		t.Fatalf("expected 2-hop PrincipalPath for executive, got %d", len(expAllowed.PrincipalPath))
	}
	if expAllowed.PageRankScore <= 0 {
		t.Fatalf("expected positive PageRankScore for executive, got %f", expAllowed.PageRankScore)
	}
}

func TestAdmin_ValidateIntegrity_And_SimulateAccess(t *testing.T) {
	ctx := context.Background()
	svc, eng := newTestAdminService(t)

	// Insert a node with empty permissions (triggers ISSUE_FAIL_CLOSED_NODE)
	_ = eng.GraphIndex().UpsertNode(types.Node{
		ID:          "doc:orphan",
		Kind:        types.NodeDocument,
		Title:       "Orphaned Doc",
		Permissions: nil,
	})

	// Insert a circular supersession chain: doc:v1 -[SUPERSEDES]-> doc:v2 -[SUPERSEDES]-> doc:v1
	_ = eng.GraphIndex().UpsertNode(types.Node{
		ID:          "doc:v1",
		Kind:        types.NodeDocument,
		Title:       "V1",
		Permissions: []string{"*"},
	})
	_ = eng.GraphIndex().UpsertNode(types.Node{
		ID:          "doc:v2",
		Kind:        types.NodeDocument,
		Title:       "V2",
		Permissions: []string{"group:security"},
	})
	_ = svc.UpsertCustomEdge(ctx, types.Edge{
		SourceID: "doc:v1",
		TargetID: "doc:v2",
		Relation: types.RelSupersedes,
	})
	_ = svc.UpsertCustomEdge(ctx, types.Edge{
		SourceID: "doc:v2",
		TargetID: "doc:v1",
		Relation: types.RelSupersedes,
	})

	valReport, err := svc.ValidateIntegrity(ctx)
	if err != nil {
		t.Fatalf("ValidateIntegrity: %v", err)
	}
	if valReport.Valid {
		t.Fatalf("expected Valid=false when supersession cycle exists")
	}
	if valReport.ErrorCount == 0 || valReport.WarningCount == 0 {
		t.Fatalf("expected both errors and warnings, got errors=%d, warnings=%d", valReport.ErrorCount, valReport.WarningCount)
	}

	foundFailClosed := false
	foundCycle := false
	for _, iss := range valReport.Issues {
		if iss.Code == IssueFailClosedNode && iss.EntityID == "doc:orphan" {
			foundFailClosed = true
		}
		if iss.Code == IssueSupersessionCycle {
			foundCycle = true
		}
	}
	if !foundFailClosed || !foundCycle {
		t.Fatalf("expected IssueFailClosedNode and IssueSupersessionCycle, got issues: %+v", valReport.Issues)
	}

	// SimulateAccess
	principal := types.NewPrincipalSet("alice", "alice@acme.io", []string{"security"})
	sim, err := svc.SimulateAccess(ctx, principal, []string{"doc:orphan", "doc:v1", "doc:v2", "doc:nonexistent"})
	if err != nil {
		t.Fatalf("SimulateAccess: %v", err)
	}
	if len(sim.Decisions) != 4 {
		t.Fatalf("expected 4 decisions, got %d", len(sim.Decisions))
	}
	if sim.Decisions[0].Allowed {
		t.Errorf("expected doc:orphan to be denied (fail-closed)")
	}
	if !sim.Decisions[1].Allowed || sim.Decisions[1].MatchedRule != "*" {
		t.Errorf("expected doc:v1 to be allowed via '*', got %+v", sim.Decisions[1])
	}
	if !sim.Decisions[2].Allowed || sim.Decisions[2].MatchedRule != "group:security" {
		t.Errorf("expected doc:v2 to be allowed via 'group:security', got %+v", sim.Decisions[2])
	}
	if sim.Decisions[3].Allowed {
		t.Errorf("expected doc:nonexistent to be denied")
	}
}

func TestAdmin_EnrichmentSweep_Rebuild_Delete_And_VectorSnapshots(t *testing.T) {
	ctx := context.Background()
	svc, eng := newTestAdminService(t)

	doc := types.Document{
		ID:          "doc:sweep-1",
		SourceType:  "slack",
		DocType:     "postmortem",
		Title:       "Redis Failover Postmortem",
		Content:     "Alice (@alice) resolved the #redis failover incident.",
		Permissions: []string{"*"},
	}
	if _, err := eng.IndexDocument(ctx, doc, false); err != nil {
		t.Fatalf("IndexDocument: %v", err)
	}

	// TriggerEnrichmentSweep
	sweep, err := svc.TriggerEnrichmentSweep(ctx, 10)
	if err != nil {
		t.Fatalf("TriggerEnrichmentSweep: %v", err)
	}
	if sweep.Processed != 1 || sweep.Succeeded != 1 || len(sweep.EnrichedDocIDs) != 1 {
		t.Fatalf("unexpected sweep report: %+v", sweep)
	}

	// SaveVectorSnapshot & LoadVectorSnapshot
	snapPath := filepath.Join(t.TempDir(), "vectors.hnsw")
	written, err := svc.SaveVectorSnapshot(ctx, snapPath)
	if err != nil || written <= 0 {
		t.Fatalf("SaveVectorSnapshot failed: bytes=%d, err=%v", written, err)
	}
	readBytes, err := svc.LoadVectorSnapshot(ctx, snapPath)
	if err != nil || readBytes != written {
		t.Fatalf("LoadVectorSnapshot failed: read=%d, written=%d, err=%v", readBytes, written, err)
	}

	// RebuildIndices
	rebuiltOverview, err := svc.RebuildIndices(ctx)
	if err != nil {
		t.Fatalf("RebuildIndices: %v", err)
	}
	if rebuiltOverview.VectorCount == 0 {
		t.Fatalf("expected non-zero vectors after RebuildIndices")
	}

	// DeleteDocument
	if err := svc.DeleteDocument(ctx, "doc:sweep-1"); err != nil {
		t.Fatalf("DeleteDocument: %v", err)
	}
	if _, err := svc.InspectDocument(ctx, "doc:sweep-1"); err == nil {
		t.Fatalf("expected error inspecting deleted document")
	}
}

func TestAdmin_CrawlDirectory_DryRun_And_LiveCommit_And_ExportGraph(t *testing.T) {
	ctx := context.Background()
	svc, eng := newTestAdminService(t)

	// Create a realistic sample directory with Go source files and Markdown docs
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "auth.go"), []byte(`package auth

import "fmt"

type TokenVerifier struct{}

func VerifyToken(tok string) bool {
	fmt.Println(tok)
	return validateSignature(tok)
}

func validateSignature(tok string) bool {
	return tok != ""
}
`), 0600); err != nil {
		t.Fatalf("write auth.go: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "gateway.go"), []byte(`package gateway

import "example.com/auth"

func HandleRequest(tok string) bool {
	return auth.VerifyToken(tok)
}
`), 0600); err != nil {
		t.Fatalf("write gateway.go: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte(`# Auth Gateway Architecture
Authored by @alice and @bob.
Tracks ticket SEC-101 and PR #42.
See auth.go and gateway.go for implementation details.
`), 0600); err != nil {
		t.Fatalf("write README.md: %v", err)
	}

	// 1. Dry-Run Mode: must extract full graph (files, symbols, cross-file calls, mentions, tickets) WITHOUT mutating Engine
	dryRep, err := svc.CrawlDirectory(ctx, CrawlRequest{
		RootPath:    dir,
		DryRun:      true,
		Permissions: []string{"group:eng"},
	})
	if err != nil {
		t.Fatalf("CrawlDirectory(DryRun=true): %v", err)
	}
	if !dryRep.DryRun {
		t.Fatalf("expected DryRun=true in report")
	}
	if dryRep.FilesMatched != 3 {
		t.Fatalf("expected 3 matched files, got %d", dryRep.FilesMatched)
	}
	if dryRep.SymbolsExtracted < 4 {
		t.Fatalf("expected >= 4 extracted Go symbols, got %d", dryRep.SymbolsExtracted)
	}
	if len(dryRep.Subgraph.Nodes) < 8 || len(dryRep.Subgraph.Edges) < 8 {
		t.Fatalf("expected rich subgraph in dry-run, got %d nodes and %d edges", len(dryRep.Subgraph.Nodes), len(dryRep.Subgraph.Edges))
	}
	if len(dryRep.PageRankScores) == 0 {
		t.Fatalf("expected PageRankScores in dry-run report")
	}
	// Verify Engine was NOT mutated during DryRun
	if eng.GraphIndex().Stats().NodeCount != 0 || eng.VectorIndex().Len() != 0 {
		t.Fatalf("expected 0 nodes and 0 vectors in live Engine after DryRun, got nodes=%d vectors=%d",
			eng.GraphIndex().Stats().NodeCount, eng.VectorIndex().Len())
	}

	// 2. Live Commit Mode (DryRun=false): persists documents, vectors, and cross-file edges into Engine
	liveRep, err := svc.CrawlDirectory(ctx, CrawlRequest{
		RootPath:    dir,
		DryRun:      false,
		Permissions: []string{"group:eng"},
	})
	if err != nil {
		t.Fatalf("CrawlDirectory(DryRun=false): %v", err)
	}
	if liveRep.DryRun {
		t.Fatalf("expected DryRun=false in live report")
	}
	if eng.GraphIndex().Stats().NodeCount < 8 || eng.VectorIndex().Len() < 3 {
		t.Fatalf("expected live Engine to be populated after DryRun=false, got nodes=%d vectors=%d",
			eng.GraphIndex().Stats().NodeCount, eng.VectorIndex().Len())
	}

	// 3. ExportGraph: verify PrincipalSet filtering works
	engPrincipal := types.NewPrincipalSet("alice", "alice@acme.io", []string{"eng"})
	snapAllowed, err := svc.ExportGraph(ctx, 100, &engPrincipal)
	if err != nil {
		t.Fatalf("ExportGraph allowed: %v", err)
	}
	if len(snapAllowed.Nodes) < 8 || len(snapAllowed.Edges) < 8 {
		t.Fatalf("expected >= 8 nodes and edges for authorized principal, got %d nodes, %d edges",
			len(snapAllowed.Nodes), len(snapAllowed.Edges))
	}

	unauthPrincipal := types.NewPrincipalSet("eve", "eve@external.io", []string{"guest"})
	snapBlocked, err := svc.ExportGraph(ctx, 100, &unauthPrincipal)
	if err != nil {
		t.Fatalf("ExportGraph blocked: %v", err)
	}
	if len(snapBlocked.Nodes) != 0 || len(snapBlocked.Edges) != 0 {
		t.Fatalf("expected 0 nodes and 0 edges for unauthorized principal, got %d nodes, %d edges",
			len(snapBlocked.Nodes), len(snapBlocked.Edges))
	}
}
