package store

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rgbyte/lattice/pkg/types"
)

// mockVectorIndex records hydrated vectors for testing Hydrate().
type mockVectorIndex struct {
	mu   sync.Mutex
	vecs map[string][]float32
}

func newMockVectorIndex() *mockVectorIndex {
	return &mockVectorIndex{vecs: make(map[string][]float32)}
}

func (m *mockVectorIndex) Upsert(id string, vec []float32) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]float32, len(vec))
	copy(cp, vec)
	m.vecs[id] = cp
	return nil
}
func (m *mockVectorIndex) Delete(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.vecs, id)
	return true
}
func (m *mockVectorIndex) SearchWithFilter(query []float32, k int, filter types.NodeFilterFunc) ([]types.VectorHit, error) {
	return nil, nil
}
func (m *mockVectorIndex) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.vecs)
}
func (m *mockVectorIndex) Dimension() int                        { return 4 }
func (m *mockVectorIndex) WriteTo(w io.Writer) (int64, error)    { return 0, nil }
func (m *mockVectorIndex) ReadFrom(r io.Reader) (int64, error)   { return 0, nil }

// mockGraphIndex records hydrated nodes and edges for testing Hydrate().
type mockGraphIndex struct {
	mu    sync.Mutex
	nodes map[string]types.Node
	edges map[string]types.Edge
}

func newMockGraphIndex() *mockGraphIndex {
	return &mockGraphIndex{
		nodes: make(map[string]types.Node),
		edges: make(map[string]types.Edge),
	}
}

func (g *mockGraphIndex) UpsertNode(node types.Node) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.nodes[node.ID] = node
	return nil
}
func (g *mockGraphIndex) UpsertEdge(edge types.Edge) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.edges[edge.ID] = edge
	return nil
}
func (g *mockGraphIndex) RemoveNode(nodeID string)          {}
func (g *mockGraphIndex) RemoveEdgesFrom(sourceID string)   {}
func (g *mockGraphIndex) GetNode(nodeID string) (types.Node, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	n, ok := g.nodes[nodeID]
	return n, ok
}
func (g *mockGraphIndex) Neighbors(nodeID string, filter types.NodeFilterFunc) []types.Edge {
	return nil
}
func (g *mockGraphIndex) PersonalizedPageRank(ctx context.Context, seeds map[string]float64, filter types.NodeFilterFunc, opts types.PPROptions) (map[string]float64, error) {
	return nil, nil
}
func (g *mockGraphIndex) ShortestPath(ctx context.Context, fromID, toID string, maxHops int, filter types.NodeFilterFunc) ([]types.Edge, error) {
	return nil, nil
}
func (g *mockGraphIndex) Subgraph(ctx context.Context, centerIDs []string, maxHops, maxNodes int, filter types.NodeFilterFunc) (types.Subgraph, error) {
	return types.Subgraph{}, nil
}
func (g *mockGraphIndex) ResolveSupersession(docID string, filter types.NodeFilterFunc) types.SupersessionInfo {
	return types.SupersessionInfo{PenaltyFactor: 1.0}
}
func (g *mockGraphIndex) Stats() types.GraphStats {
	return types.GraphStats{}
}

func TestSanitizeFTS5Query(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		wantSub  string
		forbid   []string
		wantZero bool
	}{
		{
			name:    "normal keywords",
			input:   "sqlite wal concurrency",
			wantSub: `"sqlite"* OR "wal"* OR "concurrency"*`,
		},
		{
			name:    "adversarial SQL and FTS5 injection",
			input:   `foo" OR NEAR(a,b) -- DROP TABLE cg_documents; * ^ : ( )`,
			wantSub: `"foo"*`,
			forbid:  []string{" OR OR ", "NEAR(", "--", ";", "^", ":"},
		},
		{
			name:     "only operators and punctuation",
			input:    `AND OR NOT NEAR * " ' ; -- ()`,
			wantZero: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SanitizeFTS5Query(tc.input)
			if tc.wantZero {
				if got != "" {
					t.Errorf("SanitizeFTS5Query(%q) = %q, want empty", tc.input, got)
				}
				return
			}
			if !strings.Contains(got, tc.wantSub) {
				t.Errorf("SanitizeFTS5Query(%q) = %q, want substring %q", tc.input, got, tc.wantSub)
			}
			for _, f := range tc.forbid {
				if strings.Contains(got, f) {
					t.Errorf("SanitizeFTS5Query(%q) = %q, contains forbidden %q", tc.input, got, f)
				}
			}
		})
	}
}

func TestSQLiteStoreCRUDIdempotencyFTSAndHydrate(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "context_graph.db")
	st, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore failed: %v", err)
	}
	defer st.Close()

	now := time.Now().UTC().Truncate(time.Second)
	doc := types.Document{
		ID:               "doc:arch-1",
		SourceType:       "markdown",
		DocType:          "rfc",
		Title:            "Hybrid Retrieval Engine Architecture",
		Content:          "Uses HNSW vector index, CSR graph Personalized PageRank, and SQLite FTS5 BM25.",
		URL:              "https://internal.example.com/rfc/arch-1",
		AuthorID:         "alice",
		AuthorName:       "Alice",
		AuthorEmail:      "alice@example.com",
		Status:           types.StatusVerified,
		Permissions:      []string{"group:eng", "user:alice@example.com"},
		Metadata:         map[string]string{"team": "search"},
		ContentHash:      "hash-v1",
		CreatedAt:        now,
		UpdatedAt:        now,
		EnrichmentStatus: "pending",
	}

	chunks := []types.Chunk{
		{
			ID:             "doc:arch-1#c0",
			DocID:          "doc:arch-1",
			ChunkIndex:     0,
			HeadingPath:    "Architecture > Overview",
			Content:        doc.Content,
			ContextualText: "Document: " + doc.Title + "\n" + doc.Content,
			TokenCount:     16,
			Vector:         []float32{0.5, 0.5, 0.5, 0.5},
		},
	}

	nodes := []types.Node{
		{
			ID:          "doc:arch-1",
			Kind:        types.NodeDocument,
			Title:       doc.Title,
			Permissions: doc.Permissions,
			UpdatedAt:   now,
		},
		{
			ID:          "person:alice",
			Kind:        types.NodePerson,
			Title:       "Alice",
			Permissions: doc.Permissions,
			UpdatedAt:   now,
		},
	}

	edges := []types.Edge{
		{
			SourceID:   "person:alice",
			SourceKind: types.NodePerson,
			TargetID:   "doc:arch-1",
			TargetKind: types.NodeDocument,
			Relation:   types.RelAuthored,
			Weight:     1.0,
			Confidence: 1.0,
			Provenance: types.ProvStructural,
			CreatedAt:  now,
		},
	}

	bundle := types.DocumentBundle{
		Document: doc,
		Chunks:   chunks,
		Nodes:    nodes,
		Edges:    edges,
	}

	// 1. First UpsertDocumentBundle should return changed = true
	changed, err := st.UpsertDocumentBundle(ctx, bundle)
	if err != nil {
		t.Fatalf("UpsertDocumentBundle #1 failed: %v", err)
	}
	if !changed {
		t.Errorf("UpsertDocumentBundle #1 changed = false, want true")
	}

	// 2. Second UpsertDocumentBundle with identical ContentHash should be idempotent (changed = false)
	changed2, err := st.UpsertDocumentBundle(ctx, bundle)
	if err != nil {
		t.Fatalf("UpsertDocumentBundle #2 failed: %v", err)
	}
	if changed2 {
		t.Errorf("UpsertDocumentBundle #2 with same ContentHash returned changed = true, want false")
	}

	// 3. GetDocument & GetChunk verification
	gotDoc, err := st.GetDocument(ctx, "doc:arch-1")
	if err != nil {
		t.Fatalf("GetDocument failed: %v", err)
	}
	if gotDoc.Title != doc.Title || len(gotDoc.Permissions) != 2 || gotDoc.Metadata["team"] != "search" {
		t.Errorf("GetDocument mismatch: %+v", gotDoc)
	}

	gotChunk, err := st.GetChunk(ctx, "doc:arch-1#c0")
	if err != nil {
		t.Fatalf("GetChunk failed: %v", err)
	}
	if len(gotChunk.Vector) != 4 || gotChunk.Vector[0] != 0.5 {
		t.Errorf("GetChunk vector mismatch: %v", gotChunk.Vector)
	}

	// 4. FTS5 search including adversarial injection strings
	hits, err := st.SearchFTS(ctx, `Personalized PageRank" OR NEAR(a,b) -- DROP TABLE cg_documents;`, 10)
	if err != nil {
		t.Fatalf("SearchFTS with adversarial input failed: %v", err)
	}
	if len(hits) != 1 || hits[0].ChunkID != "doc:arch-1#c0" || hits[0].BM25Score <= 0 {
		t.Errorf("SearchFTS hits = %+v, want 1 hit for doc:arch-1#c0 with positive BM25Score", hits)
	}

	// 5. ListPendingEnrichment & SaveEnrichment
	pending, err := st.ListPendingEnrichment(ctx, 10)
	if err != nil {
		t.Fatalf("ListPendingEnrichment failed: %v", err)
	}
	if len(pending) != 1 || pending[0].ID != "doc:arch-1" {
		t.Errorf("ListPendingEnrichment = %+v, want [doc:arch-1]", pending)
	}

	enrichedChunks := []types.Chunk{
		{
			ID:             "doc:arch-1#c0",
			DocID:          "doc:arch-1",
			ChunkIndex:     0,
			HeadingPath:    "Architecture > Overview",
			Content:        doc.Content,
			ContextualText: "Document: " + doc.Title + "\nContext: Tri-modal retrieval.\n" + doc.Content,
			TokenCount:     20,
			Vector:         []float32{0.6, 0.4, 0.5, 0.5},
		},
	}
	enrichedNodes := []types.Node{
		{
			ID:          "decision:use-sqlite-wal",
			Kind:        types.NodeDecision,
			Title:       "Use SQLite WAL mode",
			Permissions: doc.Permissions,
			UpdatedAt:   now,
		},
	}
	enrichedEdges := []types.Edge{
		{
			SourceID:   "doc:arch-1",
			SourceKind: types.NodeDocument,
			TargetID:   "decision:use-sqlite-wal",
			TargetKind: types.NodeDecision,
			Relation:   types.RelDecided,
			Weight:     1.5,
			Confidence: 0.95,
			Provenance: types.ProvLLM,
			CreatedAt:  now,
		},
	}

	if err := st.SaveEnrichment(ctx, "doc:arch-1", "Tri-modal retrieval.", enrichedChunks, enrichedNodes, enrichedEdges); err != nil {
		t.Fatalf("SaveEnrichment failed: %v", err)
	}

	pendingAfter, err := st.ListPendingEnrichment(ctx, 10)
	if err != nil {
		t.Fatalf("ListPendingEnrichment after SaveEnrichment failed: %v", err)
	}
	if len(pendingAfter) != 0 {
		t.Errorf("expected 0 pending documents after SaveEnrichment, got %d", len(pendingAfter))
	}

	// 6. Hydrate into mock VectorIndex and GraphIndex
	vIdx := newMockVectorIndex()
	gIdx := newMockGraphIndex()
	if err := st.Hydrate(ctx, vIdx, gIdx); err != nil {
		t.Fatalf("Hydrate failed: %v", err)
	}
	if vIdx.Len() != 1 {
		t.Errorf("hydrated vIdx.Len() = %d, want 1", vIdx.Len())
	}
	if _, ok := gIdx.GetNode("decision:use-sqlite-wal"); !ok {
		t.Errorf("hydrated gIdx missing node decision:use-sqlite-wal")
	}

	// 7. DeleteDocument removes document and chunks
	if err := st.DeleteDocument(ctx, "doc:arch-1"); err != nil {
		t.Fatalf("DeleteDocument failed: %v", err)
	}
	if _, err := st.GetDocument(ctx, "doc:arch-1"); !errors.Is(err, types.ErrNotFound) {
		t.Errorf("GetDocument after delete err = %v, want ErrNotFound", err)
	}
}
