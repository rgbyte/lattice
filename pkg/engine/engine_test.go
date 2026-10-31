package engine_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/rgbyte/lattice/pkg/engine"
	"github.com/rgbyte/lattice/pkg/types"
)

func TestEngine_EndToEndIntegration_AST_Supersession_ACLs_And_Enrichment(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "lattice_integration.db")

	eng, err := engine.NewDefaultLocal(ctx, dbPath, 384)
	if err != nil {
		t.Fatalf("NewDefaultLocal failed: %v", err)
	}
	defer eng.Close()

	// 1. Verify Fail-Closed Invariant on Indexing (missing permissions must be rejected)
	_, err = eng.IndexDocument(ctx, types.Document{
		ID:      "doc:missing-acl",
		Title:   "Missing ACL Doc",
		Content: "Should fail",
	}, false)
	if err == nil {
		t.Fatalf("expected error when indexing document without permissions")
	}

	// 2. Index a Legacy RFC v1 (which will be superseded by RFC v2)
	_, err = eng.IndexDocument(ctx, types.Document{
		ID:          "doc:rfc-auth-v1",
		SourceType:  "notion",
		DocType:     "rfc",
		Title:       "RFC-010: Legacy Session Cookie Authentication",
		Content:     "# Overview\nWe use legacy session cookies for authentication in gateway-service. Collaborating with @alice.",
		URL:         "https://notion.so/rfc-010",
		AuthorID:    "bob",
		AuthorName:  "Bob Smith",
		AuthorEmail: "bob@acme.io",
		Status:      types.StatusNormal,
		Permissions: []string{"*@acme.io"},
		UpdatedAt:   time.Now().Add(-30 * 24 * time.Hour),
	}, false)
	if err != nil {
		t.Fatalf("index rfc-v1: %v", err)
	}

	// 3. Index Verified RFC v2 that explicitly supersedes RFC v1
	_, err = eng.IndexDocument(ctx, types.Document{
		ID:          "doc:rfc-auth-v2",
		SourceType:  "notion",
		DocType:     "rfc",
		Title:       "RFC-024: Zero-Trust OAuth2 & mTLS Authentication Architecture",
		Content:     "Supersedes: doc:rfc-auth-v1\n# Decision\nDecision: We decided to migrate authentication from session cookies to OAuth2 JWTs and mTLS. Authored with @rohit and @alice. Fixes SEC-402.",
		URL:         "https://notion.so/rfc-024",
		AuthorID:    "alice",
		AuthorName:  "Alice Chen",
		AuthorEmail: "alice@acme.io",
		Status:      types.StatusVerified,
		Permissions: []string{"*@acme.io"},
		UpdatedAt:   time.Now(),
	}, false)
	if err != nil {
		t.Fatalf("index rfc-v2: %v", err)
	}

	// 4. Run Phase-2 Semantic Enrichment on RFC v2
	enrichRes, err := eng.EnrichDocumentNow(ctx, "doc:rfc-auth-v2")
	if err != nil {
		t.Fatalf("EnrichDocumentNow failed: %v", err)
	}
	if enrichRes.ContextSummary == "" {
		t.Fatalf("expected non-empty ContextSummary from enricher")
	}

	// 5. Index a Confidential Executive M&A Document (restricted to group:exec)
	_, err = eng.IndexDocument(ctx, types.Document{
		ID:          "doc:exec-secret-auth",
		SourceType:  "gdrive",
		DocType:     "memo",
		Title:       "Confidential M&A Authentication Due Diligence",
		Content:     "OAuth2 authentication token security audit for Project Titan acquisition.",
		URL:         "https://drive.google.com/secret",
		AuthorID:    "ceo",
		AuthorName:  "CEO",
		AuthorEmail: "ceo@acme.io",
		Status:      types.StatusVerified,
		Permissions: []string{"group:exec"},
	}, false)
	if err != nil {
		t.Fatalf("index secret doc: %v", err)
	}

	// 6. Index a Go source file to verify AST symbol & call-graph extraction
	goSrc := `package auth

import "crypto/tls"

type TokenVerifier struct {
	issuer string
}

func (v *TokenVerifier) VerifyJWT(token string) bool {
	return validateSignature(token)
}

func validateSignature(raw string) bool {
	_ = tls.VersionTLS13
	return len(raw) > 0
}
`
	receipt, err := eng.IndexDocument(ctx, types.Document{
		ID:          "doc:code-verifier-go",
		SourceType:  "github",
		DocType:     "go",
		Title:       "auth/verifier.go",
		FilePath:    "internal/auth/verifier.go",
		Content:     goSrc,
		URL:         "https://github.com/acme/core/blob/main/internal/auth/verifier.go",
		AuthorID:    "rohit",
		AuthorName:  "Rohit Gupta",
		AuthorEmail: "rohit@acme.io",
		Status:      types.StatusVerified,
		Permissions: []string{"*@acme.io"},
	}, false)
	if err != nil {
		t.Fatalf("index go file: %v", err)
	}
	if receipt.SymbolsIndexed < 3 {
		t.Fatalf("expected at least 3 Go AST symbols (TokenVerifier, VerifyJWT, validateSignature), got %d", receipt.SymbolsIndexed)
	}

	// 7. Execute Search as Engineer (rohit@acme.io, NOT in group:exec)
	engPrincipal := types.NewPrincipalSet("rohit", "rohit@acme.io", []string{"eng"})
	resp, err := eng.Search(ctx, engine.SearchRequest{
		Query:     "OAuth2 authentication session cookies architecture",
		Principal: engPrincipal,
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(resp.Hits) < 2 {
		t.Fatalf("expected at least 2 hits, got %d", len(resp.Hits))
	}

	// Verify Security Invariant: Confidential group:exec doc MUST NOT appear
	for _, h := range resp.Hits {
		if h.Document.ID == "doc:exec-secret-auth" {
			t.Fatalf("SECURITY VIOLATION: unauthorized document doc:exec-secret-auth leaked to engineer principal")
		}
	}

	// Verify Supersession & Ranking Invariant: RFC v2 must outrank superseded RFC v1
	var v1Hit, v2Hit *engine.SearchHit
	for i := range resp.Hits {
		switch resp.Hits[i].Document.ID {
		case "doc:rfc-auth-v1":
			v1Hit = &resp.Hits[i]
		case "doc:rfc-auth-v2":
			v2Hit = &resp.Hits[i]
		}
	}
	if v1Hit == nil || v2Hit == nil {
		t.Fatalf("expected both rfc-v1 and rfc-v2 in results")
	}
	if !v1Hit.Supersession.Superseded || v1Hit.Supersession.SupersededByID != "doc:rfc-auth-v2" {
		t.Fatalf("expected rfc-v1 to be marked superseded by doc:rfc-auth-v2, got %+v", v1Hit.Supersession)
	}
	if v2Hit.Score.FinalScore <= v1Hit.Score.FinalScore {
		t.Fatalf("expected active verified RFC v2 (%.4f) to outrank superseded RFC v1 (%.4f)",
			v2Hit.Score.FinalScore, v1Hit.Score.FinalScore)
	}
	if len(v2Hit.ExplanationPath) == 0 {
		t.Fatalf("expected non-empty shortest-path graph explanation from person:rohit to doc:rfc-auth-v2")
	}

	// 8. Verify persistence & Hydration across Engine restart
	if err := eng.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	eng2, err := engine.NewDefaultLocal(ctx, dbPath, 384)
	if err != nil {
		t.Fatalf("re-open NewDefaultLocal failed: %v", err)
	}
	defer eng2.Close()

	if eng2.VectorIndex().Len() == 0 {
		t.Fatalf("expected hydrated VectorIndex to contain chunks after restart")
	}
	if eng2.GraphIndex().Stats().NodeCount == 0 {
		t.Fatalf("expected hydrated GraphIndex to contain nodes after restart")
	}
}
