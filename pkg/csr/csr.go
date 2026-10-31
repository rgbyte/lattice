package csr

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rgbyte/lattice/pkg/types"
)

// Index is a thread-safe in-memory Context Graph backed by Compressed Sparse Row (CSR)
// and Reverse-CSR arrays for O(E) Personalized PageRank, bidirectional BFS shortest path,
// ego-network extraction, and transitive document supersession resolution.
type Index struct {
	mu sync.RWMutex

	// Raw mutable graph state
	nodes   map[string]types.Node
	edgeMap map[string]types.Edge // keyed by deterministic or explicit edge.ID

	// CSR compilation state
	dirty       bool
	compactions uint64

	// Bidirectional dense integer vertex mapping [0, N)
	idxToID []string
	idToIdx map[string]uint32

	// Forward CSR (outgoing edges from u: rowPtr[u] .. rowPtr[u+1])
	rowPtr  []int
	colIdx  []uint32
	weights []float64
	edges   []types.Edge
	outSum  []float64 // precomputed total outgoing weight per vertex

	// Reverse CSR (incoming edges to v: revRowPtr[v] .. revRowPtr[v+1])
	revRowPtr  []int
	revColIdx  []uint32
	revWeights []float64
	revEdges   []types.Edge
}

var _ types.GraphIndex = (*Index)(nil)

// NewIndex constructs an empty CSR GraphIndex.
func NewIndex() *Index {
	return &Index{
		nodes:   make(map[string]types.Node),
		edgeMap: make(map[string]types.Edge),
		idToIdx: make(map[string]uint32),
		dirty:   true,
	}
}

// UpsertNode inserts or updates a vertex in the graph.
func (idx *Index) UpsertNode(node types.Node) error {
	node.ID = strings.TrimSpace(node.ID)
	if node.ID == "" {
		return fmt.Errorf("%w: node id is required", types.ErrInvalidInput)
	}
	if node.Kind == "" {
		node.Kind = types.NodeDocument
	}
	if node.UpdatedAt.IsZero() {
		node.UpdatedAt = time.Now().UTC()
	}

	idx.mu.Lock()
	defer idx.mu.Unlock()

	existing, existed := idx.nodes[node.ID]
	if existed {
		if len(node.Permissions) == 0 && len(existing.Permissions) > 0 {
			node.Permissions = existing.Permissions
		}
		if (node.Title == "" || node.Title == node.ID) && existing.Title != "" {
			node.Title = existing.Title
		}
	}
	idx.nodes[node.ID] = node
	if !existed {
		idx.dirty = true
	}
	return nil
}

// UpsertEdge normalizes, validates, and inserts or updates a directed edge.
// Placeholder nodes are automatically registered with fail-closed (nil) permissions if either endpoint does not yet exist.
func (idx *Index) UpsertEdge(edge types.Edge) error {
	if err := edge.NormalizeAndValidate(); err != nil {
		return err
	}

	idx.mu.Lock()
	defer idx.mu.Unlock()

	if _, ok := idx.nodes[edge.SourceID]; !ok {
		idx.nodes[edge.SourceID] = types.Node{
			ID:          edge.SourceID,
			Kind:        edge.SourceKind,
			Title:       edge.SourceID,
			Permissions: nil,
			UpdatedAt:   edge.CreatedAt,
		}
	}
	if _, ok := idx.nodes[edge.TargetID]; !ok {
		idx.nodes[edge.TargetID] = types.Node{
			ID:          edge.TargetID,
			Kind:        edge.TargetKind,
			Title:       edge.TargetID,
			Permissions: nil,
			UpdatedAt:   edge.CreatedAt,
		}
	}

	idx.edgeMap[edge.ID] = edge
	idx.dirty = true
	return nil
}

// RemoveNode deletes a node and all incident (incoming or outgoing) edges.
func (idx *Index) RemoveNode(nodeID string) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	if _, ok := idx.nodes[nodeID]; !ok {
		return
	}
	delete(idx.nodes, nodeID)
	for id, e := range idx.edgeMap {
		if e.SourceID == nodeID || e.TargetID == nodeID {
			delete(idx.edgeMap, id)
		}
	}
	idx.dirty = true
}

// RemoveEdgesFrom deletes all outgoing edges originating from sourceID.
func (idx *Index) RemoveEdgesFrom(sourceID string) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	removed := false
	for id, e := range idx.edgeMap {
		if e.SourceID == sourceID {
			delete(idx.edgeMap, id)
			removed = true
		}
	}
	if removed {
		idx.dirty = true
	}
}

// GetNode retrieves a node by ID.
func (idx *Index) GetNode(nodeID string) (types.Node, bool) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	n, ok := idx.nodes[nodeID]
	return n, ok
}

// ensureCompiledRLock returns with idx.mu RLocked and CSR structures guaranteed up-to-date.
func (idx *Index) ensureCompiledRLock() {
	idx.mu.RLock()
	if !idx.dirty {
		return
	}
	idx.mu.RUnlock()

	idx.mu.Lock()
	if idx.dirty {
		idx.compileCSRLocked()
	}
	idx.mu.Unlock()

	idx.mu.RLock()
}

// compileCSRLocked rebuilds the forward and reverse CSR arrays from idx.nodes and idx.edges.
// Caller must hold idx.mu.Lock().
func (idx *Index) compileCSRLocked() {
	n := len(idx.nodes)
	idxToID := make([]string, 0, n)
	for id := range idx.nodes {
		idxToID = append(idxToID, id)
	}
	sort.Strings(idxToID)

	idToIdx := make(map[string]uint32, n)
	for i, id := range idxToID {
		idToIdx[id] = uint32(i)
	}

	// Collect valid edges and sort deterministically
	validEdges := make([]types.Edge, 0, len(idx.edgeMap))
	for _, e := range idx.edgeMap {
		_, okS := idToIdx[e.SourceID]
		_, okT := idToIdx[e.TargetID]
		if okS && okT {
			validEdges = append(validEdges, e)
		}
	}

	// Sort forward edges by (sourceIdx, targetIdx, relation, ID)
	sort.Slice(validEdges, func(i, j int) bool {
		su, sv := idToIdx[validEdges[i].SourceID], idToIdx[validEdges[j].SourceID]
		if su != sv {
			return su < sv
		}
		tu, tv := idToIdx[validEdges[i].TargetID], idToIdx[validEdges[j].TargetID]
		if tu != tv {
			return tu < tv
		}
		if validEdges[i].Relation != validEdges[j].Relation {
			return validEdges[i].Relation < validEdges[j].Relation
		}
		return validEdges[i].ID < validEdges[j].ID
	})

	m := len(validEdges)
	rowPtr := make([]int, n+1)
	colIdx := make([]uint32, m)
	weights := make([]float64, m)
	edges := make([]types.Edge, m)
	outSum := make([]float64, n)

	for k, e := range validEdges {
		u := idToIdx[e.SourceID]
		v := idToIdx[e.TargetID]
		w := e.Weight * e.Confidence
		if w <= 0 {
			w = 1.0
		}
		rowPtr[u+1]++
		colIdx[k] = v
		weights[k] = w
		edges[k] = e
		outSum[u] += w
	}
	for i := 0; i < n; i++ {
		rowPtr[i+1] += rowPtr[i]
	}

	// Build Reverse CSR (incoming edges sorted by targetIdx, sourceIdx, relation, ID)
	revSorted := make([]types.Edge, m)
	copy(revSorted, validEdges)
	sort.Slice(revSorted, func(i, j int) bool {
		tu, tv := idToIdx[revSorted[i].TargetID], idToIdx[revSorted[j].TargetID]
		if tu != tv {
			return tu < tv
		}
		su, sv := idToIdx[revSorted[i].SourceID], idToIdx[revSorted[j].SourceID]
		if su != sv {
			return su < sv
		}
		if revSorted[i].Relation != revSorted[j].Relation {
			return revSorted[i].Relation < revSorted[j].Relation
		}
		return revSorted[i].ID < revSorted[j].ID
	})

	revRowPtr := make([]int, n+1)
	revColIdx := make([]uint32, m)
	revWeights := make([]float64, m)
	revEdges := make([]types.Edge, m)

	for k, e := range revSorted {
		u := idToIdx[e.SourceID]
		v := idToIdx[e.TargetID]
		w := e.Weight * e.Confidence
		if w <= 0 {
			w = 1.0
		}
		revRowPtr[v+1]++
		revColIdx[k] = u
		revWeights[k] = w
		revEdges[k] = e
	}
	for i := 0; i < n; i++ {
		revRowPtr[i+1] += revRowPtr[i]
	}

	idx.idxToID = idxToID
	idx.idToIdx = idToIdx
	idx.rowPtr = rowPtr
	idx.colIdx = colIdx
	idx.weights = weights
	idx.edges = edges
	idx.outSum = outSum
	idx.revRowPtr = revRowPtr
	idx.revColIdx = revColIdx
	idx.revWeights = revWeights
	idx.revEdges = revEdges
	idx.dirty = false
	idx.compactions++
}

// Neighbors returns all outgoing and incoming edges incident to nodeID where both endpoints satisfy filter.
func (idx *Index) Neighbors(nodeID string, filter types.NodeFilterFunc) []types.Edge {
	idx.ensureCompiledRLock()
	defer idx.mu.RUnlock()

	if filter != nil && !filter(nodeID) {
		return nil
	}
	u, ok := idx.idToIdx[nodeID]
	if !ok {
		return nil
	}

	seen := make(map[string]struct{})
	var result []types.Edge

	// Outgoing edges u -> v
	for k := idx.rowPtr[u]; k < idx.rowPtr[u+1]; k++ {
		e := idx.edges[k]
		if filter != nil && !filter(e.TargetID) {
			continue
		}
		if _, dup := seen[e.ID]; !dup {
			seen[e.ID] = struct{}{}
			result = append(result, e)
		}
	}

	// Incoming edges v -> u
	for k := idx.revRowPtr[u]; k < idx.revRowPtr[u+1]; k++ {
		e := idx.revEdges[k]
		if filter != nil && !filter(e.SourceID) {
			continue
		}
		if _, dup := seen[e.ID]; !dup {
			seen[e.ID] = struct{}{}
			result = append(result, e)
		}
	}

	return result
}

// PersonalizedPageRank executes power iteration over the CSR arrays with strict ACL security pre-filtering.
// Any node where filter(nodeID) == false is completely excluded from seed personalization, outgoing transitions,
// and incoming mass accumulation so confidential bridge nodes can never leak structural authority.
func (idx *Index) PersonalizedPageRank(
	ctx context.Context,
	seeds map[string]float64,
	filter types.NodeFilterFunc,
	opts types.PPROptions,
) (map[string]float64, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	idx.ensureCompiledRLock()
	defer idx.mu.RUnlock()

	n := len(idx.idxToID)
	if n == 0 || len(seeds) == 0 {
		return map[string]float64{}, nil
	}

	alpha := opts.Alpha
	if alpha <= 0 || alpha >= 1.0 {
		alpha = 0.15
	}
	maxIter := opts.MaxIterations
	if maxIter <= 0 {
		maxIter = 20
	}
	tol := opts.Tolerance
	if tol <= 0 {
		tol = 1e-6
	}

	// Precompute node authorization bitmap once per query
	allowed := make([]bool, n)
	for i, id := range idx.idxToID {
		allowed[i] = filter == nil || filter(id)
	}

	// Build normalized personalization vector p over authorized seeds only
	p := make([]float64, n)
	var seedSum float64
	var seedIndices []int
	for id, w := range seeds {
		if w <= 0 {
			continue
		}
		if u, ok := idx.idToIdx[id]; ok && allowed[u] {
			p[u] = w
			seedSum += w
			seedIndices = append(seedIndices, int(u))
		}
	}
	if seedSum == 0 {
		return map[string]float64{}, nil
	}
	for _, u := range seedIndices {
		p[u] /= seedSum
	}

	// Precompute authorized outgoing weight sums (treating graph as bidirectional for contextual authority flow,
	// or forward + reverse with dampened reverse weight so both cited and citing entities receive authority).
	// Specifically, forward CSR edges carry weight w, and reverse CSR edges carry weight 0.5*w so authority
	// flows both from authors/docs to topics and from topics/symbols back to docs.
	const reverseWeightFactor = 0.5
	authOutSum := make([]float64, n)
	for u := 0; u < n; u++ {
		if !allowed[u] {
			continue
		}
		var sum float64
		for k := idx.rowPtr[u]; k < idx.rowPtr[u+1]; k++ {
			v := idx.colIdx[k]
			if allowed[v] {
				sum += idx.weights[k]
			}
		}
		for k := idx.revRowPtr[u]; k < idx.revRowPtr[u+1]; k++ {
			v := idx.revColIdx[k]
			if allowed[v] {
				sum += idx.revWeights[k] * reverseWeightFactor
			}
		}
		authOutSum[u] = sum
	}

	scores := make([]float64, n)
	copy(scores, p)
	nextScores := make([]float64, n)

	for iter := 0; iter < maxIter; iter++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		// Base teleportation restart mass
		for i := 0; i < n; i++ {
			if allowed[i] {
				nextScores[i] = alpha * p[i]
			} else {
				nextScores[i] = 0
			}
		}

		var danglingMass float64
		for u := 0; u < n; u++ {
			if !allowed[u] || scores[u] == 0 {
				continue
			}
			totalW := authOutSum[u]
			if totalW <= 0 {
				danglingMass += scores[u]
				continue
			}
			scale := (1.0 - alpha) * scores[u] / totalW
			for k := idx.rowPtr[u]; k < idx.rowPtr[u+1]; k++ {
				v := idx.colIdx[k]
				if allowed[v] {
					nextScores[v] += scale * idx.weights[k]
				}
			}
			for k := idx.revRowPtr[u]; k < idx.revRowPtr[u+1]; k++ {
				v := idx.revColIdx[k]
				if allowed[v] {
					nextScores[v] += scale * (idx.revWeights[k] * reverseWeightFactor)
				}
			}
		}

		// Redistribute dangling mass back to personalization seeds
		if danglingMass > 0 {
			dScale := (1.0 - alpha) * danglingMass
			for _, u := range seedIndices {
				nextScores[u] += dScale * p[u]
			}
		}

		// Compute L1 diff and swap
		var diff float64
		for i := 0; i < n; i++ {
			diff += math.Abs(nextScores[i] - scores[i])
			scores[i] = nextScores[i]
		}
		if diff < tol {
			break
		}
	}

	out := make(map[string]float64)
	for i, s := range scores {
		if allowed[i] && s > 1e-12 {
			out[idx.idxToID[i]] = s
		}
	}
	return out, nil
}

// ShortestPath finds a shortest undirected path (up to maxHops, default 4) between fromID and toID
// where every intermediate and terminal vertex satisfies filter.
func (idx *Index) ShortestPath(
	ctx context.Context,
	fromID, toID string,
	maxHops int,
	filter types.NodeFilterFunc,
) ([]types.Edge, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if maxHops <= 0 {
		maxHops = 4
	}

	idx.ensureCompiledRLock()
	defer idx.mu.RUnlock()

	if filter != nil && (!filter(fromID) || !filter(toID)) {
		return nil, types.ErrNotFound
	}
	startIdx, okS := idx.idToIdx[fromID]
	targetIdx, okT := idx.idToIdx[toID]
	if !okS || !okT {
		return nil, types.ErrNotFound
	}
	if startIdx == targetIdx {
		return []types.Edge{}, nil
	}

	n := len(idx.idxToID)
	visited := make([]bool, n)
	parentVertex := make([]uint32, n)
	parentEdge := make([]types.Edge, n)
	depth := make([]int, n)

	visited[startIdx] = true
	queue := []uint32{startIdx}

	for len(queue) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		u := queue[0]
		queue = queue[1:]

		if depth[u] >= maxHops {
			continue
		}

		// Traverse outgoing CSR edges
		for k := idx.rowPtr[u]; k < idx.rowPtr[u+1]; k++ {
			v := idx.colIdx[k]
			if visited[v] {
				continue
			}
			if filter != nil && !filter(idx.idxToID[v]) {
				continue
			}
			visited[v] = true
			parentVertex[v] = u
			parentEdge[v] = idx.edges[k]
			depth[v] = depth[u] + 1
			if v == targetIdx {
				return reconstructPath(startIdx, targetIdx, parentVertex, parentEdge), nil
			}
			queue = append(queue, v)
		}

		// Traverse incoming CSR edges
		for k := idx.revRowPtr[u]; k < idx.revRowPtr[u+1]; k++ {
			v := idx.revColIdx[k]
			if visited[v] {
				continue
			}
			if filter != nil && !filter(idx.idxToID[v]) {
				continue
			}
			visited[v] = true
			parentVertex[v] = u
			parentEdge[v] = idx.revEdges[k]
			depth[v] = depth[u] + 1
			if v == targetIdx {
				return reconstructPath(startIdx, targetIdx, parentVertex, parentEdge), nil
			}
			queue = append(queue, v)
		}
	}

	return nil, types.ErrNotFound
}

func reconstructPath(startIdx, targetIdx uint32, parentVertex []uint32, parentEdge []types.Edge) []types.Edge {
	var rev []types.Edge
	curr := targetIdx
	for curr != startIdx {
		rev = append(rev, parentEdge[curr])
		curr = parentVertex[curr]
	}
	path := make([]types.Edge, len(rev))
	for i := range rev {
		path[i] = rev[len(rev)-1-i]
	}
	return path
}

// Subgraph extracts a K-hop ego network around centerIDs respecting filter and maxNodes.
func (idx *Index) Subgraph(
	ctx context.Context,
	centerIDs []string,
	maxHops, maxNodes int,
	filter types.NodeFilterFunc,
) (types.Subgraph, error) {
	if err := ctx.Err(); err != nil {
		return types.Subgraph{}, err
	}
	if maxHops <= 0 {
		maxHops = 2
	}
	if maxNodes <= 0 {
		maxNodes = 100
	}

	idx.ensureCompiledRLock()
	defer idx.mu.RUnlock()

	n := len(idx.idxToID)
	visited := make([]bool, n)
	depth := make([]int, n)
	var queue []uint32
	var collectedVertices []uint32

	for _, cid := range centerIDs {
		if filter != nil && !filter(cid) {
			continue
		}
		if u, ok := idx.idToIdx[cid]; ok && !visited[u] {
			if len(collectedVertices) >= maxNodes {
				break
			}
			visited[u] = true
			depth[u] = 0
			queue = append(queue, u)
			collectedVertices = append(collectedVertices, u)
		}
	}

	for len(queue) > 0 && len(collectedVertices) < maxNodes {
		if err := ctx.Err(); err != nil {
			return types.Subgraph{}, err
		}
		u := queue[0]
		queue = queue[1:]
		if depth[u] >= maxHops {
			continue
		}

		// Outgoing neighbors
		for k := idx.rowPtr[u]; k < idx.rowPtr[u+1]; k++ {
			if len(collectedVertices) >= maxNodes {
				break
			}
			v := idx.colIdx[k]
			if !visited[v] && (filter == nil || filter(idx.idxToID[v])) {
				visited[v] = true
				depth[v] = depth[u] + 1
				queue = append(queue, v)
				collectedVertices = append(collectedVertices, v)
			}
		}

		// Incoming neighbors
		for k := idx.revRowPtr[u]; k < idx.revRowPtr[u+1]; k++ {
			if len(collectedVertices) >= maxNodes {
				break
			}
			v := idx.revColIdx[k]
			if !visited[v] && (filter == nil || filter(idx.idxToID[v])) {
				visited[v] = true
				depth[v] = depth[u] + 1
				queue = append(queue, v)
				collectedVertices = append(collectedVertices, v)
			}
		}
	}

	nodesOut := make([]types.Node, 0, len(collectedVertices))
	for _, u := range collectedVertices {
		id := idx.idxToID[u]
		if node, ok := idx.nodes[id]; ok {
			nodesOut = append(nodesOut, node)
		}
	}

	// Collect all induced edges between visited vertices
	edgeSeen := make(map[string]struct{})
	var edgesOut []types.Edge
	for _, u := range collectedVertices {
		for k := idx.rowPtr[u]; k < idx.rowPtr[u+1]; k++ {
			v := idx.colIdx[k]
			if visited[v] {
				e := idx.edges[k]
				if _, dup := edgeSeen[e.ID]; !dup {
					edgeSeen[e.ID] = struct{}{}
					edgesOut = append(edgesOut, e)
				}
			}
		}
	}

	return types.Subgraph{
		Nodes: nodesOut,
		Edges: edgesOut,
	}, nil
}

// ResolveSupersession checks incoming RelSupersedes edges (`newer -[SUPERSEDES]-> docID`)
// and follows transitive supersession chains up to 5 hops across authorized nodes.
func (idx *Index) ResolveSupersession(docID string, filter types.NodeFilterFunc) types.SupersessionInfo {
	idx.ensureCompiledRLock()
	defer idx.mu.RUnlock()

	if filter != nil && !filter(docID) {
		return types.SupersessionInfo{PenaltyFactor: 1.0}
	}
	currIdx, ok := idx.idToIdx[docID]
	if !ok {
		return types.SupersessionInfo{PenaltyFactor: 1.0}
	}

	const maxSupersessionHops = 5
	var latestID string
	var latestTime time.Time
	superseded := false

	visited := map[uint32]bool{currIdx: true}

	for hop := 0; hop < maxSupersessionHops; hop++ {
		foundNext := false
		var bestNextIdx uint32
		var bestEdge types.Edge

		// Look for incoming SUPERSEDES edges: source -[SUPERSEDES]-> currIdx
		for k := idx.revRowPtr[currIdx]; k < idx.revRowPtr[currIdx+1]; k++ {
			e := idx.revEdges[k]
			if e.Relation != types.RelSupersedes {
				continue
			}
			srcIdx := idx.revColIdx[k]
			if visited[srcIdx] {
				continue
			}
			srcID := idx.idxToID[srcIdx]
			if filter != nil && !filter(srcID) {
				continue
			}
			if !foundNext || e.CreatedAt.After(bestEdge.CreatedAt) || (e.CreatedAt.Equal(bestEdge.CreatedAt) && srcID > idx.idxToID[bestNextIdx]) {
				foundNext = true
				bestNextIdx = srcIdx
				bestEdge = e
			}
		}

		if !foundNext {
			break
		}
		visited[bestNextIdx] = true
		currIdx = bestNextIdx
		latestID = idx.idxToID[currIdx]
		latestTime = bestEdge.CreatedAt
		superseded = true
	}

	if !superseded {
		return types.SupersessionInfo{
			Superseded:    false,
			PenaltyFactor: 1.0,
		}
	}

	return types.SupersessionInfo{
		Superseded:     true,
		SupersededByID: latestID,
		PenaltyFactor:  0.25,
		RecordedAt:     latestTime,
	}
}

// Stats returns a summary of the current graph state.
func (idx *Index) Stats() types.GraphStats {
	idx.ensureCompiledRLock()
	defer idx.mu.RUnlock()

	nodesByKind := make(map[string]int)
	for _, n := range idx.nodes {
		nodesByKind[string(n.Kind)]++
	}
	edgesByRel := make(map[string]int)
	for _, e := range idx.edgeMap {
		edgesByRel[string(e.Relation)]++
	}

	return types.GraphStats{
		NodeCount:      len(idx.nodes),
		EdgeCount:      len(idx.edgeMap),
		NodesByKind:    nodesByKind,
		EdgesByRel:     edgesByRel,
		CSRCompactions: idx.compactions,
	}
}
