package engine

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rgbyte/lattice/pkg/csr"
	"github.com/rgbyte/lattice/pkg/enrich"
	"github.com/rgbyte/lattice/pkg/extract"
	"github.com/rgbyte/lattice/pkg/hnsw"
	"github.com/rgbyte/lattice/pkg/store"
	"github.com/rgbyte/lattice/pkg/types"
)

// RankingWeights controls the multi-stage hybrid + Context Graph scoring formula.
type RankingWeights struct {
	LexicalBM25       float64 `json:"lexical_bm25"`
	VectorSemantic    float64 `json:"vector_semantic"`
	GraphPageRank     float64 `json:"graph_pagerank"`
	VerificationBoost float64 `json:"verification_boost"`
	FreshnessBoost    float64 `json:"freshness_boost"`
}

// DefaultRankingWeights returns balanced enterprise search weights.
func DefaultRankingWeights() RankingWeights {
	return RankingWeights{
		LexicalBM25:       0.32,
		VectorSemantic:    0.38,
		GraphPageRank:     0.20,
		VerificationBoost: 0.10,
		FreshnessBoost:    0.05,
	}
}

// Option configures an Engine instance.
type Option func(*Engine)

// WithRankingWeights overrides the default ranking weights.
func WithRankingWeights(w RankingWeights) Option {
	return func(e *Engine) {
		e.weights = w
	}
}

// WithAsyncWorkers configures the background Phase-2 LLM enrichment worker pool size.
func WithAsyncWorkers(workers, queueSize int) Option {
	return func(e *Engine) {
		if workers > 0 {
			e.workerCount = workers
		}
		if queueSize > 0 {
			e.queueSize = queueSize
		}
	}
}

// SearchRequest represents a security-scoped hybrid Context Graph query.
type SearchRequest struct {
	Query       string             `json:"query"`
	Principal   types.PrincipalSet `json:"principal"`
	Limit       int                `json:"limit"`
	SourceTypes []string           `json:"source_types,omitempty"`
	DocTypes    []string           `json:"doc_types,omitempty"`
}

// ScoreBreakdown exposes the transparent contribution of each ranking stage.
type ScoreBreakdown struct {
	FinalScore        float64 `json:"final_score"`
	LexicalRRF        float64 `json:"lexical_rrf"`
	VectorSimilarity  float64 `json:"vector_similarity"`
	GraphPageRank     float64 `json:"graph_pagerank"`
	LifecycleBoost    float64 `json:"lifecycle_boost"`
	SupersessionRatio float64 `json:"supersession_ratio"`
}

// SearchHit represents a ranked, explainable document result.
type SearchHit struct {
	Document        types.Document         `json:"document"`
	MatchedChunk    types.Chunk            `json:"matched_chunk"`
	Score           ScoreBreakdown         `json:"score"`
	Supersession    types.SupersessionInfo `json:"supersession"`
	ExplanationPath []types.Edge           `json:"explanation_path,omitempty"`
	WhyRanked       []string               `json:"why_ranked"`
}

// SearchResponse wraps ranked hits alongside graph telemetry.
type SearchResponse struct {
	Query      string            `json:"query"`
	Hits       []SearchHit       `json:"hits"`
	GraphStats types.GraphStats  `json:"graph_stats"`
	LatencyMs  float64           `json:"latency_ms"`
}

// IndexReceipt reports the deterministic artifacts extracted during Phase-1 indexing.
type IndexReceipt struct {
	DocID           string             `json:"doc_id"`
	Changed         bool               `json:"changed"`
	ChunksIndexed   int                `json:"chunks_indexed"`
	SymbolsIndexed  int                `json:"symbols_indexed"`
	NodesExtracted  int                `json:"nodes_extracted"`
	EdgesExtracted  int                `json:"edges_extracted"`
	CodeSymbols     []types.CodeSymbol `json:"code_symbols,omitempty"`
	QueuedForEnrich bool               `json:"queued_for_enrich"`
}

// Engine is the unified facade orchestrating ACID Storage, HNSW Vector Search,
// CSR Personalized PageRank, Deterministic AST/Structural Extraction, and Async LLM Enrichment.
type Engine struct {
	store       types.StorageBackend
	vIdx        types.VectorIndex
	gIdx        types.GraphIndex
	embedder    types.Embedder
	enricher    types.SemanticEnricher
	weights     RankingWeights
	workerCount int
	queueSize   int

	aclMu       sync.RWMutex
	docPerms    map[string][]string
	chunkToDoc  map[string]string
	docCache    map[string]types.Document
	chunkCache  map[string]types.Chunk

	enrichCh    chan string
	cancelBg    context.CancelFunc
	wg          sync.WaitGroup
	closed      atomic.Bool
}

// New constructs and hydrates a ready-to-query Engine from explicit ports.
func New(
	ctx context.Context,
	st types.StorageBackend,
	vIdx types.VectorIndex,
	gIdx types.GraphIndex,
	embedder types.Embedder,
	enricher types.SemanticEnricher,
	opts ...Option,
) (*Engine, error) {
	if st == nil || vIdx == nil || gIdx == nil || embedder == nil {
		return nil, fmt.Errorf("%w: store, vector index, graph index, and embedder are required", types.ErrInvalidInput)
	}
	if enricher == nil {
		enricher = enrich.NewEnricher(enrich.Config{})
	}

	eng := &Engine{
		store:       st,
		vIdx:        vIdx,
		gIdx:        gIdx,
		embedder:    embedder,
		enricher:    enricher,
		weights:     DefaultRankingWeights(),
		workerCount: 1,
		queueSize:   128,
		docPerms:    make(map[string][]string),
		chunkToDoc:  make(map[string]string),
		docCache:    make(map[string]types.Document),
		chunkCache:  make(map[string]types.Chunk),
	}
	for _, opt := range opts {
		opt(eng)
	}

	if err := st.Hydrate(ctx, vIdx, gIdx); err != nil {
		return nil, fmt.Errorf("hydrate indices: %w", err)
	}

	bgCtx, cancel := context.WithCancel(context.Background())
	eng.cancelBg = cancel
	eng.enrichCh = make(chan string, eng.queueSize)
	for i := 0; i < eng.workerCount; i++ {
		eng.wg.Add(1)
		go eng.enrichmentWorker(bgCtx)
	}

	return eng, nil
}

// NewDefaultLocal creates a batteries-included Engine backed by SQLite WAL, HNSW, CSR, and Ollama/Local embedders.
func NewDefaultLocal(ctx context.Context, sqliteDSN string, dim int, opts ...Option) (*Engine, error) {
	if dim <= 0 {
		dim = 384
	}
	st, err := store.NewSQLiteStore(sqliteDSN)
	if err != nil {
		return nil, err
	}
	vIdx, err := hnsw.NewIndex(hnsw.Config{
		Dimension:      dim,
		M:              16,
		EfConstruction: 100,
		EfSearch:       64,
	})
	if err != nil {
		_ = st.Close()
		return nil, err
	}
	gIdx := csr.NewIndex()
	localEmb := enrich.NewLocalEmbedder(dim)
	enricher := enrich.NewEnricher(enrich.Config{})
	return New(ctx, st, vIdx, gIdx, localEmb, enricher, opts...)
}

// IndexDocument executes Phase-1 synchronous structural/AST extraction, embedding, ACID persistence,
// and live HNSW/CSR updates. If asyncEnrich is true, it queues the document for Phase-2 LLM enrichment.
func (e *Engine) IndexDocument(ctx context.Context, doc types.Document, asyncEnrich bool) (*IndexReceipt, error) {
	if e.closed.Load() {
		return nil, types.ErrEngineClosed
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	doc.ID = strings.TrimSpace(doc.ID)
	doc.Title = strings.TrimSpace(doc.Title)
	if doc.ID == "" || doc.Title == "" {
		return nil, fmt.Errorf("%w: document ID and Title are required", types.ErrInvalidInput)
	}
	// Fail-closed security invariant: documents must declare explicit permissions.
	if len(doc.Permissions) == 0 {
		return nil, fmt.Errorf("%w: document must specify non-empty permissions (e.g. [\"*\"] or principal ACLs)", types.ErrInvalidInput)
	}
	if doc.Status == "" {
		doc.Status = types.StatusNormal
	}
	now := time.Now().UTC()
	if doc.CreatedAt.IsZero() {
		doc.CreatedAt = now
	}
	if doc.UpdatedAt.IsZero() {
		doc.UpdatedAt = now
	}
	if doc.EnrichmentStatus == "" {
		doc.EnrichmentStatus = "pending"
	}

	var allNodes []types.Node
	var allEdges []types.Edge
	var codeSymbols []types.CodeSymbol

	// Document vertex itself
	docNode := types.Node{
		ID:          doc.ID,
		Kind:        types.NodeDocument,
		Title:       doc.Title,
		Permissions: doc.Permissions,
		UpdatedAt:   doc.UpdatedAt,
		Properties: map[string]string{
			"source_type": doc.SourceType,
			"doc_type":    doc.DocType,
			"status":      string(doc.Status),
		},
	}
	allNodes = append(allNodes, docNode)

	// 0. If document is a raw PDF payload (%PDF-...), decode its text and metadata first
	if strings.HasPrefix(strings.TrimSpace(doc.Content), "%PDF") {
		if pdfMeta, err := extract.ExtractPDFBytes([]byte(doc.Content)); err == nil {
			doc.Content = pdfMeta.Content
			if doc.DocType == "" {
				doc.DocType = "pdf"
			}
			if doc.AuthorID == "" && pdfMeta.Author != "" {
				doc.AuthorID = pdfMeta.Author
			}
		}
	}

	// 1. Go AST deterministic extraction for .go files or go code docs
	if strings.HasSuffix(strings.ToLower(doc.FilePath), ".go") || strings.EqualFold(doc.DocType, "go") {
		syms, astNodes, astEdges, err := extract.ExtractGoFile(doc.ID, doc.FilePath, doc.Content, doc.Permissions)
		if err == nil {
			codeSymbols = syms
			allNodes = append(allNodes, astNodes...)
			allEdges = append(allEdges, astEdges...)
		}
	}

	// 2. Structural Markdown/text chunking & deterministic entity/edge extraction
	chunks, structNodes, structEdges := extract.ChunkAndExtractStructural(doc, 1000)
	allNodes = append(allNodes, structNodes...)
	allEdges = append(allEdges, structEdges...)

	// 3. Batch compute dense embeddings
	texts := make([]string, len(chunks))
	for i := range chunks {
		texts[i] = chunks[i].ContextualText
	}
	if len(texts) > 0 {
		vecs, err := e.embedder.EmbedBatch(ctx, texts)
		if err != nil {
			return nil, fmt.Errorf("embed chunks: %w", err)
		}
		for i := range chunks {
			if i < len(vecs) {
				chunks[i].Vector = vecs[i]
			}
		}
	}

	bundle := types.DocumentBundle{
		Document: doc,
		Chunks:   chunks,
		Nodes:    allNodes,
		Edges:    allEdges,
	}

	changed, err := e.store.UpsertDocumentBundle(ctx, bundle)
	if err != nil {
		return nil, fmt.Errorf("store bundle: %w", err)
	}

	// 4. Live-update in-memory ACL caches, HNSW index, and CSR graph
	e.aclMu.Lock()
	e.docPerms[doc.ID] = append([]string(nil), doc.Permissions...)
	e.docCache[doc.ID] = doc
	for _, ch := range chunks {
		e.chunkToDoc[ch.ID] = doc.ID
		e.chunkCache[ch.ID] = ch
	}
	e.aclMu.Unlock()

	for _, ch := range chunks {
		if len(ch.Vector) == e.vIdx.Dimension() {
			_ = e.vIdx.Upsert(ch.ID, ch.Vector)
		}
	}
	e.gIdx.RemoveEdgesFrom(doc.ID)
	for _, n := range allNodes {
		_ = e.gIdx.UpsertNode(n)
	}
	for _, ed := range allEdges {
		_ = e.gIdx.UpsertEdge(ed)
	}

	queued := false
	if asyncEnrich {
		select {
		case e.enrichCh <- doc.ID:
			queued = true
		default:
		}
	}

	return &IndexReceipt{
		DocID:           doc.ID,
		Changed:         changed,
		ChunksIndexed:   len(chunks),
		SymbolsIndexed:  len(codeSymbols),
		NodesExtracted:  len(allNodes),
		EdgesExtracted:  len(allEdges),
		CodeSymbols:     codeSymbols,
		QueuedForEnrich: queued,
	}, nil
}

// EnrichDocumentNow synchronously executes Phase-2 semantic enrichment (Contextual Retrieval header +
// semantic graph triples), re-embeds the document's chunks with the enriched summary, and updates SQLite + HNSW + CSR.
func (e *Engine) EnrichDocumentNow(ctx context.Context, docID string) (*types.EnrichmentResult, error) {
	if e.closed.Load() {
		return nil, types.ErrEngineClosed
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	doc, err := e.store.GetDocument(ctx, docID)
	if err != nil {
		return nil, err
	}

	res, err := e.enricher.EnrichDocument(ctx, doc)
	if err != nil {
		return nil, err
	}

	doc.ContextSummary = res.ContextSummary
	doc.EnrichmentStatus = "enriched"

	// Re-chunk and re-embed with the enriched ContextSummary prepended (Contextual Retrieval)
	chunks, _, _ := extract.ChunkAndExtractStructural(doc, 1000)
	texts := make([]string, len(chunks))
	for i := range chunks {
		texts[i] = chunks[i].ContextualText
	}
	if len(texts) > 0 {
		vecs, err := e.embedder.EmbedBatch(ctx, texts)
		if err != nil {
			return nil, err
		}
		for i := range chunks {
			if i < len(vecs) {
				chunks[i].Vector = vecs[i]
			}
		}
	}

	if err := e.store.SaveEnrichment(ctx, doc.ID, res.ContextSummary, chunks, res.Nodes, res.Edges); err != nil {
		return nil, err
	}

	e.aclMu.Lock()
	e.docCache[doc.ID] = doc
	for _, ch := range chunks {
		e.chunkToDoc[ch.ID] = doc.ID
		e.chunkCache[ch.ID] = ch
	}
	e.aclMu.Unlock()

	for _, ch := range chunks {
		if len(ch.Vector) == e.vIdx.Dimension() {
			_ = e.vIdx.Upsert(ch.ID, ch.Vector)
		}
	}
	for _, n := range res.Nodes {
		_ = e.gIdx.UpsertNode(n)
	}
	for _, ed := range res.Edges {
		_ = e.gIdx.UpsertEdge(ed)
	}

	return &res, nil
}

func (e *Engine) enrichmentWorker(ctx context.Context) {
	defer e.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case docID, ok := <-e.enrichCh:
			if !ok {
				return
			}
			c, cancel := context.WithTimeout(ctx, 15*time.Second)
			_, _ = e.EnrichDocumentNow(c, docID)
			cancel()
		}
	}
}

// buildFilters constructs fail-closed chunk and node filter predicates for the given PrincipalSet.
func (e *Engine) buildFilters(ctx context.Context, principal types.PrincipalSet) (types.NodeFilterFunc, types.NodeFilterFunc) {
	canAccessDoc := func(docID string) bool {
		e.aclMu.RLock()
		perms, ok := e.docPerms[docID]
		e.aclMu.RUnlock()
		if ok {
			return principal.CanAccess(perms)
		}
		doc, err := e.store.GetDocument(ctx, docID)
		if err != nil {
			return false
		}
		e.aclMu.Lock()
		e.docPerms[docID] = doc.Permissions
		e.docCache[docID] = doc
		e.aclMu.Unlock()
		return principal.CanAccess(doc.Permissions)
	}

	chunkFilter := func(chunkID string) bool {
		e.aclMu.RLock()
		docID, ok := e.chunkToDoc[chunkID]
		e.aclMu.RUnlock()
		if !ok {
			if idx := strings.IndexByte(chunkID, '#'); idx > 0 {
				docID = chunkID[:idx]
			} else {
				ch, err := e.store.GetChunk(ctx, chunkID)
				if err != nil {
					return false
				}
				docID = ch.DocID
			}
		}
		return canAccessDoc(docID)
	}

	nodeFilter := func(nodeID string) bool {
		// Security Invariant (SEC-01): Always consult authoritative document ACLs first
		// whenever nodeID corresponds to an indexed document or NodeDocument vertex.
		e.aclMu.RLock()
		_, isTrackedDoc := e.docPerms[nodeID]
		e.aclMu.RUnlock()
		if isTrackedDoc {
			return canAccessDoc(nodeID)
		}
		if node, ok := e.gIdx.GetNode(nodeID); ok {
			if node.Kind == types.NodeDocument {
				return canAccessDoc(nodeID)
			}
			if len(node.Permissions) > 0 {
				return principal.CanAccess(node.Permissions)
			}
		}
		return canAccessDoc(nodeID)
	}

	return chunkFilter, nodeFilter
}

// Search executes the 4-stage Hybrid + Context Graph retrieval pipeline with strict fail-closed ACL enforcement.
func (e *Engine) Search(ctx context.Context, req SearchRequest) (*SearchResponse, error) {
	if e.closed.Load() {
		return nil, types.ErrEngineClosed
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	start := time.Now()
	if req.Limit <= 0 {
		req.Limit = 10
	}

	chunkFilter, nodeFilter := e.buildFilters(ctx, req.Principal)

	// Stage 1A: Lexical BM25 via SQLite FTS5
	lexHits, err := e.store.SearchFTS(ctx, req.Query, req.Limit*4)
	if err != nil && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	lexRank := make(map[string]int, len(lexHits))
	r := 1
	for _, lh := range lexHits {
		if chunkFilter(lh.ChunkID) {
			lexRank[lh.ChunkID] = r
			r++
		}
	}

	// Stage 1B: Neural Vector Search via HNSW with In-Traversal ACL Pre-Filtering
	vecScores := make(map[string]float64)
	vecRank := make(map[string]int)
	if strings.TrimSpace(req.Query) != "" {
		qVec, err := e.embedder.Embed(ctx, req.Query)
		if err == nil && len(qVec) == e.vIdx.Dimension() {
			vHits, err := e.vIdx.SearchWithFilter(qVec, req.Limit*4, chunkFilter)
			if err == nil {
				for idx, vh := range vHits {
					vecScores[vh.ID] = vh.Score
					vecRank[vh.ID] = idx + 1
				}
			}
		}
	}

	// Collect union of authorized candidate chunks
	candidateChunks := make(map[string]struct{})
	for cid := range lexRank {
		candidateChunks[cid] = struct{}{}
	}
	for cid := range vecRank {
		candidateChunks[cid] = struct{}{}
	}

	// Group best chunk per document & build seed set for Personalized PageRank
	type docCandidate struct {
		doc      types.Document
		bestChunk types.Chunk
		lexRRF   float64
		vecSim   float64
		combinedStage1 float64
	}
	docMap := make(map[string]*docCandidate)
	seeds := make(map[string]float64)

	if req.Principal.UserID != "" {
		seeds["person:"+req.Principal.UserID] = 1.0
		seeds[req.Principal.UserID] = 1.0
	}

	for cid := range candidateChunks {
		ch, ok := e.getChunkCached(ctx, cid)
		if !ok {
			continue
		}
		doc, ok := e.getDocCached(ctx, ch.DocID)
		if !ok || !req.Principal.CanAccess(doc.Permissions) {
			continue
		}
		if !matchesFilters(doc, req.SourceTypes, req.DocTypes) {
			continue
		}

		var lRRF float64
		if rk, ok := lexRank[cid]; ok {
			lRRF = 1.0 / float64(10+rk)
		}
		vSim := vecScores[cid]
		if rk, ok := vecRank[cid]; ok {
			vSim = math.Max(vSim, 1.0/float64(10+rk))
		}
		s1 := e.weights.LexicalBM25*lRRF + e.weights.VectorSemantic*vSim

		existing, exists := docMap[doc.ID]
		if !exists || s1 > existing.combinedStage1 {
			docMap[doc.ID] = &docCandidate{
				doc:            doc,
				bestChunk:      ch,
				lexRRF:         lRRF,
				vecSim:         vSim,
				combinedStage1: s1,
			}
		}
		seeds[doc.ID] += s1
	}

	// Stage 2: ACL-Isolated Personalized PageRank over CSR Graph
	pprScores, err := e.gIdx.PersonalizedPageRank(ctx, seeds, nodeFilter, types.PPROptions{
		Alpha:         0.15,
		MaxIterations: 20,
		Tolerance:     1e-6,
	})
	if err != nil && ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Normalize max PPR score for interpretable [0, 1] scaling
	maxPPR := 0.0
	for _, sc := range pprScores {
		if sc > maxPPR {
			maxPPR = sc
		}
	}

	// Stage 3 & 4: Lifecycle Supersession, Status Boost, Freshness & ShortestPath Explainability
	hits := make([]SearchHit, 0, len(docMap))
	userNodeID := "person:" + req.Principal.UserID
	for docID, cand := range docMap {
		normPPR := 0.0
		if maxPPR > 0 {
			normPPR = pprScores[docID] / maxPPR
		}

		var lifecycleBoost float64
		switch cand.doc.Status {
		case types.StatusVerified:
			lifecycleBoost = e.weights.VerificationBoost
		case types.StatusDeprecated:
			lifecycleBoost = -0.25
		}

		// Supersession resolution via CSR Graph
		sup := e.gIdx.ResolveSupersession(docID, nodeFilter)
		supRatio := 1.0
		if sup.Superseded && sup.PenaltyFactor > 0 {
			supRatio = sup.PenaltyFactor
		}

		rawScore := cand.combinedStage1 + e.weights.GraphPageRank*normPPR + lifecycleBoost
		finalScore := rawScore * supRatio

		var reasons []string
		if cand.lexRRF > 0 {
			reasons = append(reasons, "Lexical BM25 match")
		}
		if cand.vecSim > 0.25 {
			reasons = append(reasons, fmt.Sprintf("Semantic HNSW similarity (%.2f)", cand.vecSim))
		}
		if normPPR > 0.1 {
			reasons = append(reasons, fmt.Sprintf("Context Graph PageRank authority (%.2f)", normPPR))
		}
		if cand.doc.Status == types.StatusVerified {
			reasons = append(reasons, "Verified authoritative document")
		}
		if sup.Superseded {
			reasons = append(reasons, fmt.Sprintf("Demoted: superseded by %s", sup.SupersededByID))
		}

		var path []types.Edge
		if req.Principal.UserID != "" {
			if p, err := e.gIdx.ShortestPath(ctx, userNodeID, docID, 4, nodeFilter); err == nil && len(p) > 0 {
				path = p
			}
		}

		hits = append(hits, SearchHit{
			Document:     cand.doc,
			MatchedChunk: cand.bestChunk,
			Score: ScoreBreakdown{
				FinalScore:        finalScore,
				LexicalRRF:        cand.lexRRF,
				VectorSimilarity:  cand.vecSim,
				GraphPageRank:     normPPR,
				LifecycleBoost:    lifecycleBoost,
				SupersessionRatio: supRatio,
			},
			Supersession:    sup,
			ExplanationPath: path,
			WhyRanked:       reasons,
		})
	}

	sort.Slice(hits, func(i, j int) bool {
		if math.Abs(hits[i].Score.FinalScore-hits[j].Score.FinalScore) < 1e-9 {
			return hits[i].Document.ID < hits[j].Document.ID
		}
		return hits[i].Score.FinalScore > hits[j].Score.FinalScore
	})
	if len(hits) > req.Limit {
		hits = hits[:req.Limit]
	}

	return &SearchResponse{
		Query:      req.Query,
		Hits:       hits,
		GraphStats: e.gIdx.Stats(),
		LatencyMs:  float64(time.Since(start).Microseconds()) / 1000.0,
	}, nil
}

func (e *Engine) getDocCached(ctx context.Context, docID string) (types.Document, bool) {
	e.aclMu.RLock()
	d, ok := e.docCache[docID]
	e.aclMu.RUnlock()
	if ok {
		return d, true
	}
	doc, err := e.store.GetDocument(ctx, docID)
	if err != nil {
		return types.Document{}, false
	}
	e.aclMu.Lock()
	e.docCache[docID] = doc
	e.docPerms[docID] = doc.Permissions
	e.aclMu.Unlock()
	return doc, true
}

func (e *Engine) getChunkCached(ctx context.Context, chunkID string) (types.Chunk, bool) {
	e.aclMu.RLock()
	c, ok := e.chunkCache[chunkID]
	e.aclMu.RUnlock()
	if ok {
		return c, true
	}
	ch, err := e.store.GetChunk(ctx, chunkID)
	if err != nil {
		return types.Chunk{}, false
	}
	e.aclMu.Lock()
	e.chunkCache[chunkID] = ch
	e.chunkToDoc[chunkID] = ch.DocID
	e.aclMu.Unlock()
	return ch, true
}

func matchesFilters(doc types.Document, sources, docTypes []string) bool {
	if len(sources) > 0 {
		matched := false
		for _, s := range sources {
			if strings.EqualFold(doc.SourceType, s) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if len(docTypes) > 0 {
		matched := false
		for _, dt := range docTypes {
			if strings.EqualFold(doc.DocType, dt) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

// GraphIndex exposes the underlying GraphIndex for direct subgraph/path inspection.
func (e *Engine) GraphIndex() types.GraphIndex {
	return e.gIdx
}

// VectorIndex exposes the underlying VectorIndex.
func (e *Engine) VectorIndex() types.VectorIndex {
	return e.vIdx
}

// Storage exposes the underlying StorageBackend.
func (e *Engine) Storage() types.StorageBackend {
	return e.store
}

// Embedder exposes the underlying Embedder.
func (e *Engine) Embedder() types.Embedder {
	return e.embedder
}

// DeleteDocument atomically removes a document, its chunks, and its graph edges from
// SQLite storage, HNSW vector index, CSR graph index, and in-memory ACL caches.
func (e *Engine) DeleteDocument(ctx context.Context, docID string) error {
	if e.closed.Load() {
		return types.ErrEngineClosed
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	docID = strings.TrimSpace(docID)
	if docID == "" {
		return fmt.Errorf("%w: docID is required", types.ErrInvalidInput)
	}

	e.aclMu.Lock()
	delete(e.docPerms, docID)
	delete(e.docCache, docID)
	var chunkIDs []string
	for cid, did := range e.chunkToDoc {
		if did == docID {
			chunkIDs = append(chunkIDs, cid)
			delete(e.chunkToDoc, cid)
			delete(e.chunkCache, cid)
		}
	}
	e.aclMu.Unlock()

	for _, cid := range chunkIDs {
		e.vIdx.Delete(cid)
	}
	// Also delete sequential chunk IDs up to 64 in case cache was cold after restart
	for i := 0; i < 64; i++ {
		e.vIdx.Delete(fmt.Sprintf("%s#c%d", docID, i))
	}
	e.gIdx.RemoveNode(docID)

	return e.store.DeleteDocument(ctx, docID)
}

// RebuildIndices clears in-memory caches and re-hydrates the VectorIndex and GraphIndex
// from the durable StorageBackend.
func (e *Engine) RebuildIndices(ctx context.Context) error {
	if e.closed.Load() {
		return types.ErrEngineClosed
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	e.aclMu.Lock()
	e.docPerms = make(map[string][]string)
	e.chunkToDoc = make(map[string]string)
	e.docCache = make(map[string]types.Document)
	e.chunkCache = make(map[string]types.Chunk)
	e.aclMu.Unlock()

	return e.store.Hydrate(ctx, e.vIdx, e.gIdx)
}

// Close gracefully stops background enrichment workers and closes the storage backend.
func (e *Engine) Close() error {
	if e.closed.CompareAndSwap(false, true) {
		if e.cancelBg != nil {
			e.cancelBg()
		}
		close(e.enrichCh)
		e.wg.Wait()
		return e.store.Close()
	}
	return nil
}
