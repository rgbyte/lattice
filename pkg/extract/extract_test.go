package extract

import (
	"strings"
	"testing"

	"github.com/rgbyte/lattice/pkg/types"
)

func TestExtractGoFile(t *testing.T) {
	src := `package auth

import (
	"context"
	"fmt"
	"github.com/rgbyte/lattice/pkg/types"
)

// Authenticator validates user principals.
type Authenticator interface {
	Verify(ctx context.Context, token string) (types.PrincipalSet, error)
}

// TokenService implements Authenticator using HMAC tokens.
type TokenService struct {
	secret []byte
}

// NewTokenService constructs a new TokenService.
func NewTokenService(secret string) *TokenService {
	validateSecret(secret)
	return &TokenService{secret: []byte(secret)}
}

// Verify checks the token signature and parses claims.
func (s *TokenService) Verify(ctx context.Context, token string) (types.PrincipalSet, error) {
	if !s.checkFormat(token) {
		return types.PrincipalSet{}, fmt.Errorf("invalid format")
	}
	return parseClaims(token), nil
}

func (s *TokenService) checkFormat(token string) bool {
	return len(token) > 10
}

func validateSecret(secret string) {
	if len(secret) == 0 {
		panic("empty secret")
	}
}

func parseClaims(token string) types.PrincipalSet {
	return types.NewPrincipalSet("u1", "u1@example.com", nil)
}
`
	perms := []string{"group:eng", "user:alice@example.com"}
	symbols, nodes, edges, err := ExtractGoFile("doc:auth_go", "pkg/auth/auth.go", src, perms)
	if err != nil {
		t.Fatalf("ExtractGoFile failed: %v", err)
	}

	// Verify symbols: Authenticator (interface), TokenService (struct), NewTokenService (func),
	// Verify (method), checkFormat (method), validateSecret (func), parseClaims (func)
	symByName := make(map[string]types.CodeSymbol)
	for _, s := range symbols {
		symByName[s.Name] = s
	}

	expectedSyms := map[string]struct {
		kind     string
		exported bool
		receiver string
	}{
		"Authenticator":   {kind: "interface", exported: true, receiver: ""},
		"TokenService":    {kind: "struct", exported: true, receiver: ""},
		"NewTokenService": {kind: "func", exported: true, receiver: ""},
		"Verify":          {kind: "method", exported: true, receiver: "TokenService"},
		"checkFormat":     {kind: "method", exported: false, receiver: "TokenService"},
		"validateSecret":  {kind: "func", exported: false, receiver: ""},
		"parseClaims":     {kind: "func", exported: false, receiver: ""},
	}

	for name, exp := range expectedSyms {
		got, ok := symByName[name]
		if !ok {
			t.Errorf("missing expected symbol %q", name)
			continue
		}
		if got.Kind != exp.kind {
			t.Errorf("symbol %q kind = %q, want %q", name, got.Kind, exp.kind)
		}
		if got.Exported != exp.exported {
			t.Errorf("symbol %q exported = %v, want %v", name, got.Exported, exp.exported)
		}
		if got.Receiver != exp.receiver {
			t.Errorf("symbol %q receiver = %q, want %q", name, got.Receiver, exp.receiver)
		}
		if got.ID != "sym:auth."+name {
			t.Errorf("symbol %q ID = %q, want %q", name, got.ID, "sym:auth."+name)
		}
		if got.StartLine <= 0 || got.EndLine < got.StartLine {
			t.Errorf("symbol %q invalid line range: %d..%d", name, got.StartLine, got.EndLine)
		}
	}

	// Verify nodes have permissions inherited
	nodeByID := make(map[string]types.Node)
	for _, n := range nodes {
		nodeByID[n.ID] = n
		if len(n.Permissions) != len(perms) {
			t.Errorf("node %q permissions = %v, want %v", n.ID, n.Permissions, perms)
		}
	}
	if _, ok := nodeByID["sym:auth.TokenService"]; !ok {
		t.Errorf("missing symbol node sym:auth.TokenService")
	}

	// Verify edges: DEFINES_SYMBOL, CALLS, DEPENDS_ON
	var foundDefines, foundCallNewToValidate, foundCallVerifyToCheck, foundCallVerifyToParse, foundDependsOn bool
	for _, e := range edges {
		if e.Provenance != types.ProvDeterministicAST {
			t.Errorf("edge %+v provenance = %q, want %q", e, e.Provenance, types.ProvDeterministicAST)
		}
		switch {
		case e.SourceID == "doc:auth_go" && e.Relation == types.RelDefinesSymbol && e.TargetID == "sym:auth.TokenService":
			foundDefines = true
		case e.SourceID == "sym:auth.NewTokenService" && e.Relation == types.RelCalls && e.TargetID == "sym:auth.validateSecret":
			foundCallNewToValidate = true
		case e.SourceID == "sym:auth.Verify" && e.Relation == types.RelCalls && e.TargetID == "sym:auth.checkFormat":
			foundCallVerifyToCheck = true
		case e.SourceID == "sym:auth.Verify" && e.Relation == types.RelCalls && e.TargetID == "sym:auth.parseClaims":
			foundCallVerifyToParse = true
		case e.Relation == types.RelDependsOn && strings.Contains(e.TargetID, "github.com/rgbyte/lattice/pkg/types"):
			foundDependsOn = true
		}
	}

	if !foundDefines {
		t.Errorf("missing DEFINES_SYMBOL edge from doc:auth_go to sym:auth.TokenService")
	}
	if !foundCallNewToValidate {
		t.Errorf("missing CALLS edge sym:auth.NewTokenService -> sym:auth.validateSecret")
	}
	if !foundCallVerifyToCheck {
		t.Errorf("missing CALLS edge sym:auth.Verify -> sym:auth.checkFormat")
	}
	if !foundCallVerifyToParse {
		t.Errorf("missing CALLS edge sym:auth.Verify -> sym:auth.parseClaims")
	}
	if !foundDependsOn {
		t.Errorf("missing DEPENDS_ON edge for import github.com/rgbyte/lattice/pkg/types")
	}
}

func TestExtractGoFileInvalidSyntax(t *testing.T) {
	_, _, _, err := ExtractGoFile("doc:bad", "bad.go", "package bad\nfunc unclosed(", []string{"public"})
	if err == nil {
		t.Fatalf("expected error on invalid Go syntax, got nil")
	}
}

func TestChunkAndExtractStructural(t *testing.T) {
	content := `# Architecture Overview
Supersedes: doc:rfc-001

We are migrating our storage engine to SQLite WAL mode.
Contact @alice and @bob_smith for details (ignore email bob@example.com).

## Storage Layer
This addresses ticket ENG-123 and SEC-45 as well as PR #42.

### Indexing Details
` + strings.Repeat("The HNSW index maintains multi-layer proximity graphs for fast ANN search. ", 15)

	doc := types.Document{
		ID:             "doc:rfc-002",
		Title:          "RFC-002: Context Graph Storage",
		ContextSummary: "Migration to hybrid CSR + HNSW + SQLite WAL architecture.",
		Content:        content,
		AuthorID:       "charlie",
		AuthorName:     "Charlie",
		Status:         types.StatusVerified,
		Permissions:    []string{"group:engineering"},
	}

	chunks, nodes, edges := ChunkAndExtractStructural(doc, 300)
	if len(chunks) < 3 {
		t.Fatalf("expected at least 3 heading-aware chunks, got %d", len(chunks))
	}

	// Verify chunk IDs, HeadingPaths, and ContextualText
	foundStorageHeading := false
	foundIndexingHeading := false
	for i, c := range chunks {
		expectedID := "doc:rfc-002#c" + string(rune('0'+i))
		if i < 10 && c.ID != expectedID {
			t.Errorf("chunk[%d] ID = %q, want %q", i, c.ID, expectedID)
		}
		if strings.Contains(c.HeadingPath, "Architecture Overview > Storage Layer") {
			foundStorageHeading = true
		}
		if strings.Contains(c.HeadingPath, "Architecture Overview > Storage Layer > Indexing Details") {
			foundIndexingHeading = true
		}
		if !strings.Contains(c.ContextualText, doc.Title) || !strings.Contains(c.ContextualText, doc.ContextSummary) {
			t.Errorf("chunk[%d] ContextualText missing title or context summary: %q", i, c.ContextualText)
		}
	}
	if !foundStorageHeading {
		t.Errorf("expected chunk with HeadingPath 'Architecture Overview > Storage Layer'")
	}
	if !foundIndexingHeading {
		t.Errorf("expected chunk with HeadingPath 'Architecture Overview > Storage Layer > Indexing Details'")
	}

	// Verify nodes inherit doc.Permissions
	nodeMap := make(map[string]types.Node)
	for _, n := range nodes {
		nodeMap[n.ID] = n
		if len(n.Permissions) == 0 || n.Permissions[0] != "group:engineering" {
			t.Errorf("node %q did not inherit permissions: %v", n.ID, n.Permissions)
		}
	}

	// Verify extracted edges
	edgeSet := make(map[string]types.Edge)
	for _, e := range edges {
		key := e.SourceID + "|" + string(e.Relation) + "|" + e.TargetID
		edgeSet[key] = e
		if e.Provenance != types.ProvStructural {
			t.Errorf("edge %s provenance = %q, want %q", key, e.Provenance, types.ProvStructural)
		}
	}

	expectedEdges := []string{
		"person:charlie|AUTHORED|doc:rfc-002",
		"doc:rfc-002|MENTIONS|person:alice",
		"doc:rfc-002|MENTIONS|person:bob_smith",
		"person:charlie|COLLABORATES_WITH|person:alice",
		"person:charlie|COLLABORATES_WITH|person:bob_smith",
		"doc:rfc-002|REFERENCES|ticket:ENG-123",
		"doc:rfc-002|REFERENCES|ticket:SEC-45",
		"doc:rfc-002|REFERENCES|ticket:PR#42",
		"doc:rfc-002|SUPERSEDES|doc:rfc-001",
	}

	for _, exp := range expectedEdges {
		if _, ok := edgeSet[exp]; !ok {
			t.Errorf("missing expected structural edge %q (got edges: %v)", exp, keysOf(edgeSet))
		}
	}

	// Ensure bob@example.com did not produce person:example
	if _, bad := edgeSet["doc:rfc-002|MENTIONS|person:example"]; bad {
		t.Errorf("email address falsely matched as @mention")
	}
}

func keysOf(m map[string]types.Edge) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestExtractPDFBytes_And_StructuralGraph(t *testing.T) {
	// Construct a valid synthetic PDF with Title, Author, and BT...ET text operators
	samplePDF := `%PDF-1.4
1 0 obj
<< /Type /Catalog /Pages 2 0 R >>
endobj
2 0 obj
<< /Type /Pages /Kids [3 0 R] /Count 1 >>
endobj
3 0 obj
<< /Type /Page /Parent 2 0 R /Contents 4 0 R >>
endobj
4 0 obj
<< /Length 260 >>
stream
BT
/F1 14 Tf
(1. Zero-Trust Passkey Authentication) Tj T*
(Authored with @alice and @rohit for service:auth-gateway.) Tj T*
[(Addresses ) -120 (SEC-701 ) -120 (and ) -120 (PR #88.)] TJ T*
(Decision: Enforce FIDO2 hardware passkeys for admin plane.) Tj T*
ET
endstream
endobj
5 0 obj
<< /Title (Enterprise Passkey Security Whitepaper) /Author (rohit) >>
endobj
%%EOF`

	meta, err := ExtractPDFBytes([]byte(samplePDF))
	if err != nil {
		t.Fatalf("ExtractPDFBytes failed: %v", err)
	}
	if meta.Title != "Enterprise Passkey Security Whitepaper" {
		t.Errorf("Title = %q, want 'Enterprise Passkey Security Whitepaper'", meta.Title)
	}
	if meta.Author != "rohit" {
		t.Errorf("Author = %q, want 'rohit'", meta.Author)
	}
	if !strings.Contains(meta.Content, "# Zero-Trust Passkey Authentication") {
		t.Errorf("expected numbered PDF heading to normalize to Markdown heading, got:\n%s", meta.Content)
	}

	doc := types.Document{
		ID:          "doc:whitepaper.pdf",
		SourceType:  "filesystem",
		DocType:     "pdf",
		Title:       meta.Title,
		AuthorID:    meta.Author,
		Content:     meta.Content,
		Permissions: []string{"group:eng"},
	}
	chunks, nodes, edges := ChunkAndExtractStructural(doc, 500)
	if len(chunks) == 0 {
		t.Fatalf("expected chunks from PDF")
	}

	nodeKinds := make(map[types.NodeKind]int)
	for _, n := range nodes {
		nodeKinds[n.Kind]++
	}
	if nodeKinds[types.NodeTopic] == 0 || nodeKinds[types.NodeDecision] == 0 || nodeKinds[types.NodeService] == 0 {
		t.Fatalf("expected topic, decision, and service nodes from PDF, got kinds: %+v", nodeKinds)
	}
	if len(edges) < 6 {
		t.Fatalf("expected >= 6 structural edges from PDF, got %d", len(edges))
	}
}
