package engine_test

import (
	"context"
	"errors"
	"math"
	"path/filepath"
	"testing"

	"github.com/rgbyte/lattice/pkg/csr"
	"github.com/rgbyte/lattice/pkg/engine"
	"github.com/rgbyte/lattice/pkg/types"
)

func TestAuditRegression_SEC01_CrossDocumentSupersedesCannotOverwriteConfidentialACL(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "sec01.db")

	eng, err := engine.NewDefaultLocal(ctx, dbPath, 384)
	if err != nil {
		t.Fatalf("NewDefaultLocal: %v", err)
	}
	defer eng.Close()

	// 1. Index a confidential board-only document connecting @alice to topic:merger
	_, err = eng.IndexDocument(ctx, types.Document{
		ID:          "doc:board-secret",
		SourceType:  "gdrive",
		DocType:     "memo",
		Title:       "Confidential Board Merger Terms",
		Content:     "Confidential merger terms reviewed by @alice and @bob for Project Omega.",
		URL:         "https://drive.local/board-secret",
		AuthorID:    "ceo",
		Permissions: []string{"group:board"},
	}, false)
	if err != nil {
		t.Fatalf("index board-secret: %v", err)
	}

	// 2. An attacker indexes a public document claiming "Supersedes: doc:board-secret"
	// attempting to overwrite doc:board-secret's graph vertex permissions to ["*"].
	_, err = eng.IndexDocument(ctx, types.Document{
		ID:          "doc:public-attacker",
		SourceType:  "notion",
		DocType:     "rfc",
		Title:       "Public Notes on Merger",
		Content:     "Supersedes: doc:board-secret\nPublic notes mentioning @rohit.",
		URL:         "https://notion.local/public-attacker",
		AuthorID:    "rohit",
		Permissions: []string{"*"},
	}, false)
	if err != nil {
		t.Fatalf("index public-attacker: %v", err)
	}
	_, err = eng.EnrichDocumentNow(ctx, "doc:public-attacker")
	if err != nil {
		t.Fatalf("enrich public-attacker: %v", err)
	}

	// 3. Query as rohit (NOT in group:board): doc:board-secret must remain strictly unauthorized
	rohit := types.NewPrincipalSet("rohit", "rohit@acme.io", []string{"eng"})
	resp, err := eng.Search(ctx, engine.SearchRequest{
		Query:     "Confidential Board Merger Terms Project Omega",
		Principal: rohit,
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	for _, h := range resp.Hits {
		if h.Document.ID == "doc:board-secret" {
			t.Fatalf("SEC-01 regression: confidential document leaked after cross-document Supersedes reference")
		}
		for _, edge := range h.ExplanationPath {
			if edge.SourceID == "doc:board-secret" || edge.TargetID == "doc:board-secret" {
				t.Fatalf("SEC-01 regression: confidential document acted as an authorized bridge in ExplanationPath")
			}
		}
	}
}

func TestAuditRegression_SEC02_PlaceholderNodesFailClosed_And_SEC04_NaNClamping(t *testing.T) {
	t.Parallel()

	g := csr.NewIndex()
	err := g.UpsertEdge(types.Edge{
		SourceID:   "doc:unindexed-src",
		SourceKind: types.NodeDocument,
		TargetID:   "doc:unindexed-dst",
		TargetKind: types.NodeDocument,
		Relation:   types.RelReferences,
		Weight:     math.NaN(),
		Confidence: math.Inf(1),
	})
	if err != nil {
		t.Fatalf("UpsertEdge: %v", err)
	}

	node, ok := g.GetNode("doc:unindexed-dst")
	if !ok {
		t.Fatalf("expected placeholder node to exist")
	}
	p := types.NewPrincipalSet("rohit", "rohit@acme.io", []string{"eng"})
	if p.CanAccess(node.Permissions) {
		t.Fatalf("SEC-02 regression: auto-created placeholder node must be fail-closed (nil permissions), got %v", node.Permissions)
	}
}

func TestAuditRegression_DESIGN01_ContextCancellation_And_DESIGN05_StaleEdgeCleanup(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "design.db")

	eng, err := engine.NewDefaultLocal(ctx, dbPath, 384)
	if err != nil {
		t.Fatalf("NewDefaultLocal: %v", err)
	}
	defer eng.Close()

	// 1. Index doc mentioning @legacypeer
	_, err = eng.IndexDocument(ctx, types.Document{
		ID:          "doc:evolving",
		Title:       "Evolving Runbook",
		Content:     "Contact @legacypeer for assistance.",
		Permissions: []string{"*"},
	}, false)
	if err != nil {
		t.Fatalf("index v1: %v", err)
	}

	// 2. Update doc removing @legacypeer and mentioning @newpeer instead
	_, err = eng.IndexDocument(ctx, types.Document{
		ID:          "doc:evolving",
		Title:       "Evolving Runbook",
		Content:     "Contact @newpeer for assistance.",
		Permissions: []string{"*"},
	}, false)
	if err != nil {
		t.Fatalf("index v2: %v", err)
	}

	neighbors := eng.GraphIndex().Neighbors("doc:evolving", nil)
	for _, ed := range neighbors {
		if ed.TargetID == "person:legacypeer" {
			t.Fatalf("DESIGN-05 regression: stale edge to person:legacypeer was not cleaned up on document update")
		}
	}

	// 3. Canceled context must return context.Canceled
	canceledCtx, cancel := context.WithCancel(ctx)
	cancel()
	_, err = eng.Search(canceledCtx, engine.SearchRequest{
		Query:     "Runbook",
		Principal: types.NewPrincipalSet("rohit", "rohit@acme.io", nil),
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("DESIGN-01 regression: expected context.Canceled, got %v", err)
	}
}
