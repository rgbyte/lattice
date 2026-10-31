package admin

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/rgbyte/lattice/pkg/engine"
	"github.com/rgbyte/lattice/pkg/types"
)

// OverviewReport summarizes the health, scale, and configuration of the Lattice engine.
type OverviewReport struct {
	GraphStats         types.GraphStats `json:"graph_stats"`
	VectorCount        int              `json:"vector_count"`
	VectorDimension    int              `json:"vector_dimension"`
	EmbedderModel      string           `json:"embedder_model"`
	PendingEnrichCount int              `json:"pending_enrich_count"`
	CheckedAt          time.Time        `json:"checked_at"`
}

// DocumentInspection provides a complete diagnostic view of an indexed document,
// its chunks, its Context Graph vertex, incident edges, and supersession status.
type DocumentInspection struct {
	Document      types.Document         `json:"document"`
	Chunks        []types.Chunk          `json:"chunks"`
	Node          types.Node             `json:"node"`
	IncidentEdges []types.Edge           `json:"incident_edges"`
	Supersession  types.SupersessionInfo `json:"supersession"`
}

// NodeInspection captures a Context Graph vertex along with its immediate neighbors and K-hop ego subgraph.
type NodeInspection struct {
	Node      types.Node     `json:"node"`
	Neighbors []types.Edge   `json:"neighbors"`
	Subgraph  types.Subgraph `json:"subgraph"`
}

// ConnectionExplanation compares the unfiltered global shortest path against a principal-scoped
// shortest path and Personalized PageRank authority score between two vertices.
type ConnectionExplanation struct {
	FromID        string       `json:"from_id"`
	ToID          string       `json:"to_id"`
	GlobalPath    []types.Edge `json:"global_path,omitempty"`
	PrincipalPath []types.Edge `json:"principal_path,omitempty"`
	BlockedByACL  bool         `json:"blocked_by_acl"`
	PageRankScore float64      `json:"pagerank_score"`
}

// EnrichmentSweepReport summarizes a synchronous Phase-2 enrichment batch run.
type EnrichmentSweepReport struct {
	Processed      int      `json:"processed"`
	Succeeded      int      `json:"succeeded"`
	Failed         int      `json:"failed"`
	EnrichedDocIDs []string `json:"enriched_doc_ids"`
	DurationMs     float64  `json:"duration_ms"`
}

// Service provides deep system introspection, graph integrity validation, ACL simulation,
// and operational maintenance over a Lattice Engine.
type Service struct {
	eng *engine.Engine
}

// NewService creates an admin and introspection Service bound to eng.
func NewService(eng *engine.Engine) (*Service, error) {
	if eng == nil {
		return nil, fmt.Errorf("%w: engine is required", types.ErrInvalidInput)
	}
	return &Service{eng: eng}, nil
}

// SystemOverview returns a real-time snapshot of the graph, vector index, embedder, and enrichment queue.
func (s *Service) SystemOverview(ctx context.Context) (*OverviewReport, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	pending, err := s.eng.Storage().ListPendingEnrichment(ctx, 10000)
	if err != nil {
		return nil, err
	}
	return &OverviewReport{
		GraphStats:         s.eng.GraphIndex().Stats(),
		VectorCount:        s.eng.VectorIndex().Len(),
		VectorDimension:    s.eng.VectorIndex().Dimension(),
		EmbedderModel:      s.eng.Embedder().ModelID(),
		PendingEnrichCount: len(pending),
		CheckedAt:          time.Now().UTC(),
	}, nil
}

// InspectDocument retrieves a document along with all of its chunks, graph vertex,
// incident edges, and transitive supersession metadata.
func (s *Service) InspectDocument(ctx context.Context, docID string) (*DocumentInspection, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	docID = strings.TrimSpace(docID)
	if docID == "" {
		return nil, fmt.Errorf("%w: docID is required", types.ErrInvalidInput)
	}

	doc, err := s.eng.Storage().GetDocument(ctx, docID)
	if err != nil {
		return nil, err
	}

	var chunks []types.Chunk
	for i := 0; i < 4096; i++ {
		chunkID := fmt.Sprintf("%s#c%d", docID, i)
		ch, err := s.eng.Storage().GetChunk(ctx, chunkID)
		if err != nil {
			if errors.Is(err, types.ErrNotFound) {
				break
			}
			return nil, err
		}
		chunks = append(chunks, ch)
	}

	node, _ := s.eng.GraphIndex().GetNode(docID)
	edges := s.eng.GraphIndex().Neighbors(docID, nil)
	sup := s.eng.GraphIndex().ResolveSupersession(docID, nil)

	return &DocumentInspection{
		Document:      doc,
		Chunks:        chunks,
		Node:          node,
		IncidentEdges: edges,
		Supersession:  sup,
	}, nil
}

// InspectNode inspects a single vertex in the Context Graph, optionally scoped by a PrincipalSet.
func (s *Service) InspectNode(
	ctx context.Context,
	nodeID string,
	hops, maxNodes int,
	principal *types.PrincipalSet,
) (*NodeInspection, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return nil, fmt.Errorf("%w: nodeID is required", types.ErrInvalidInput)
	}
	if hops <= 0 {
		hops = 2
	}
	if maxNodes <= 0 {
		maxNodes = 50
	}

	filter := s.buildNodeFilter(ctx, principal)
	if filter != nil && !filter(nodeID) {
		return nil, types.ErrUnauthorized
	}

	node, ok := s.eng.GraphIndex().GetNode(nodeID)
	if !ok {
		return nil, types.ErrNotFound
	}

	neighbors := s.eng.GraphIndex().Neighbors(nodeID, filter)
	sg, err := s.eng.GraphIndex().Subgraph(ctx, []string{nodeID}, hops, maxNodes, filter)
	if err != nil {
		return nil, err
	}

	return &NodeInspection{
		Node:      node,
		Neighbors: neighbors,
		Subgraph:  sg,
	}, nil
}

// ExplainConnection diagnoses how two vertices are connected in the Context Graph,
// highlighting whether a confidential bridge node blocks the path for the given principal.
func (s *Service) ExplainConnection(
	ctx context.Context,
	fromID, toID string,
	maxHops int,
	principal *types.PrincipalSet,
) (*ConnectionExplanation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	fromID = strings.TrimSpace(fromID)
	toID = strings.TrimSpace(toID)
	if fromID == "" || toID == "" {
		return nil, fmt.Errorf("%w: fromID and toID are required", types.ErrInvalidInput)
	}
	if maxHops <= 0 {
		maxHops = 4
	}

	globalPath, err := s.eng.GraphIndex().ShortestPath(ctx, fromID, toID, maxHops, nil)
	if err != nil && !errors.Is(err, types.ErrNotFound) {
		return nil, err
	}

	filter := s.buildNodeFilter(ctx, principal)
	var principalPath []types.Edge
	if filter == nil {
		principalPath = globalPath
	} else {
		pPath, pErr := s.eng.GraphIndex().ShortestPath(ctx, fromID, toID, maxHops, filter)
		if pErr != nil && !errors.Is(pErr, types.ErrNotFound) {
			return nil, pErr
		}
		principalPath = pPath
	}

	blockedByACL := len(globalPath) > 0 && filter != nil && len(principalPath) == 0

	pprScores, err := s.eng.GraphIndex().PersonalizedPageRank(
		ctx,
		map[string]float64{fromID: 1.0},
		filter,
		types.PPROptions{Alpha: 0.15, MaxIterations: 20, Tolerance: 1e-6},
	)
	if err != nil {
		return nil, err
	}

	return &ConnectionExplanation{
		FromID:        fromID,
		ToID:          toID,
		GlobalPath:    globalPath,
		PrincipalPath: principalPath,
		BlockedByACL:  blockedByACL,
		PageRankScore: pprScores[toID],
	}, nil
}

// TriggerEnrichmentSweep synchronously processes up to limit pending documents for Phase-2 semantic enrichment.
func (s *Service) TriggerEnrichmentSweep(ctx context.Context, limit int) (*EnrichmentSweepReport, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 50
	}
	start := time.Now()
	pending, err := s.eng.Storage().ListPendingEnrichment(ctx, limit)
	if err != nil {
		return nil, err
	}

	report := &EnrichmentSweepReport{
		Processed: len(pending),
	}
	for _, doc := range pending {
		if _, err := s.eng.EnrichDocumentNow(ctx, doc.ID); err != nil {
			report.Failed++
		} else {
			report.Succeeded++
			report.EnrichedDocIDs = append(report.EnrichedDocIDs, doc.ID)
		}
	}
	report.DurationMs = float64(time.Since(start).Microseconds()) / 1000.0
	return report, nil
}

// RebuildIndices forces a full re-hydration of the in-memory HNSW and CSR indices from SQLite storage.
func (s *Service) RebuildIndices(ctx context.Context) (*OverviewReport, error) {
	if err := s.eng.RebuildIndices(ctx); err != nil {
		return nil, err
	}
	return s.SystemOverview(ctx)
}

// DeleteDocument removes a document and its chunks/edges from storage and indices.
func (s *Service) DeleteDocument(ctx context.Context, docID string) error {
	return s.eng.DeleteDocument(ctx, docID)
}

// UpsertCustomEdge validates and persists a manual/custom directed edge into both ACID storage and the live CSR graph.
func (s *Service) UpsertCustomEdge(ctx context.Context, edge types.Edge) error {
	if err := edge.NormalizeAndValidate(); err != nil {
		return err
	}
	if err := s.eng.Storage().UpsertEdge(ctx, edge); err != nil {
		return err
	}
	return s.eng.GraphIndex().UpsertEdge(edge)
}

// SaveVectorSnapshot writes a binary snapshot of the HNSW vector index to path.
func (s *Service) SaveVectorSnapshot(ctx context.Context, path string) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return 0, fmt.Errorf("%w: snapshot path is required", types.ErrInvalidInput)
	}
	f, err := os.Create(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	return s.eng.VectorIndex().WriteTo(f)
}

// LoadVectorSnapshot restores the HNSW vector index from a binary snapshot file at path.
func (s *Service) LoadVectorSnapshot(ctx context.Context, path string) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return 0, fmt.Errorf("%w: snapshot path is required", types.ErrInvalidInput)
	}
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	return s.eng.VectorIndex().ReadFrom(f)
}

func (s *Service) buildNodeFilter(ctx context.Context, principal *types.PrincipalSet) types.NodeFilterFunc {
	if principal == nil {
		return nil
	}
	p := *principal
	return func(nodeID string) bool {
		if node, ok := s.eng.GraphIndex().GetNode(nodeID); ok {
			if node.Kind == types.NodeDocument {
				if doc, err := s.eng.Storage().GetDocument(ctx, nodeID); err == nil {
					return p.CanAccess(doc.Permissions)
				}
			}
			return p.CanAccess(node.Permissions)
		}
		if doc, err := s.eng.Storage().GetDocument(ctx, nodeID); err == nil {
			return p.CanAccess(doc.Permissions)
		}
		return false
	}
}
