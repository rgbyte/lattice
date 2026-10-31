package types

import (
	"context"
	"io"
)

// VectorHit represents a single Approximate Nearest Neighbor match from VectorIndex.
type VectorHit struct {
	ID    string  `json:"id"`
	Score float64 `json:"score"`
}

// VectorIndex defines the port for O(log N) Approximate Nearest Neighbor search.
type VectorIndex interface {
	Upsert(id string, vec []float32) error
	Delete(id string) bool
	SearchWithFilter(query []float32, k int, filter NodeFilterFunc) ([]VectorHit, error)
	Len() int
	Dimension() int
	WriteTo(w io.Writer) (int64, error)
	ReadFrom(r io.Reader) (int64, error)
}

// PPROptions configures Personalized PageRank random walks.
type PPROptions struct {
	Alpha         float64 // Teleportation / restart probability (default 0.15)
	MaxIterations int     // Power iterations (default 20)
	Tolerance     float64 // L1 convergence threshold (default 1e-6)
}

// Subgraph represents a permission-filtered K-hop ego network.
type Subgraph struct {
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
}

// GraphStats summarizes the state of the in-memory CSR Context Graph.
type GraphStats struct {
	NodeCount      int            `json:"node_count"`
	EdgeCount      int            `json:"edge_count"`
	NodesByKind    map[string]int `json:"nodes_by_kind"`
	EdgesByRel     map[string]int `json:"edges_by_relation"`
	CSRCompactions uint64         `json:"csr_compactions"`
}

// GraphIndex defines the port for in-memory CSR graph traversal and authority ranking.
type GraphIndex interface {
	UpsertNode(node Node) error
	UpsertEdge(edge Edge) error
	RemoveNode(nodeID string)
	RemoveEdgesFrom(sourceID string)
	GetNode(nodeID string) (Node, bool)
	Neighbors(nodeID string, filter NodeFilterFunc) []Edge
	PersonalizedPageRank(ctx context.Context, seeds map[string]float64, filter NodeFilterFunc, opts PPROptions) (map[string]float64, error)
	ShortestPath(ctx context.Context, fromID, toID string, maxHops int, filter NodeFilterFunc) ([]Edge, error)
	Subgraph(ctx context.Context, centerIDs []string, maxHops, maxNodes int, filter NodeFilterFunc) (Subgraph, error)
	ResolveSupersession(docID string, filter NodeFilterFunc) SupersessionInfo
	Stats() GraphStats
}

// Embedder defines the port for computing dense semantic vector representations.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)
	Dimension() int
	ModelID() string
}

// EnrichmentResult holds the LLM-extracted context summary and semantic triples for a Document.
type EnrichmentResult struct {
	ContextSummary string `json:"context_summary"`
	Nodes          []Node `json:"nodes"`
	Edges          []Edge `json:"edges"`
}

// SemanticEnricher defines the port for Phase-2 asynchronous document context & triple extraction.
type SemanticEnricher interface {
	EnrichDocument(ctx context.Context, doc Document) (EnrichmentResult, error)
}

// LexicalHit represents a BM25 match from FTS5.
type LexicalHit struct {
	ChunkID   string  `json:"chunk_id"`
	DocID     string  `json:"doc_id"`
	BM25Score float64 `json:"bm25_score"`
}

// DocumentBundle groups a Document with its extracted Chunks, Nodes, and Edges for atomic persistence.
type DocumentBundle struct {
	Document Document
	Chunks   []Chunk
	Nodes    []Node
	Edges    []Edge
}

// StorageBackend defines the ACID persistence port.
type StorageBackend interface {
	UpsertDocumentBundle(ctx context.Context, bundle DocumentBundle) (bool, error)
	GetDocument(ctx context.Context, docID string) (Document, error)
	GetChunk(ctx context.Context, chunkID string) (Chunk, error)
	DeleteDocument(ctx context.Context, docID string) error
	SearchFTS(ctx context.Context, query string, limit int) ([]LexicalHit, error)
	UpsertNode(ctx context.Context, node Node) error
	UpsertEdge(ctx context.Context, edge Edge) error
	ListPendingEnrichment(ctx context.Context, limit int) ([]Document, error)
	SaveEnrichment(ctx context.Context, docID, summary string, chunks []Chunk, nodes []Node, edges []Edge) error
	Hydrate(ctx context.Context, vIdx VectorIndex, gIdx GraphIndex) error
	Close() error
}
