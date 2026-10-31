package hnsw

import (
	"container/heap"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"math/rand"
	"sort"
	"sync"

	"github.com/rgbyte/lattice/pkg/types"
)

const (
	snapshotMagic   uint32 = 0x484E5357 // "HNSW"
	snapshotVersion uint32 = 1
)

// Config configures the HNSW approximate nearest neighbor index.
type Config struct {
	Dimension      int
	M              int
	EfConstruction int
	EfSearch       int
	Seed           int64
}

type hnswNode struct {
	id      string
	vec     []float32
	level   int
	deleted bool
	friends [][]uint32 // friends[layer] holds internal indices of neighbors at that layer
}

// Index is a multi-layer Hierarchical Navigable Small World (HNSW) graph index.
// It implements types.VectorIndex and is safe for concurrent use.
type Index struct {
	mu             sync.RWMutex
	dim            int
	m              int
	mMax0          int
	efConstruction int
	efSearch       int
	levelMult      float64
	rng            *rand.Rand

	nodes      []*hnswNode
	idToIdx    map[string]uint32
	entryPoint int // -1 when empty
	maxLevel   int
	activeCnt  int
}

var _ types.VectorIndex = (*Index)(nil)

// NewIndex constructs a new HNSW Index with the given configuration.
func NewIndex(cfg Config) (*Index, error) {
	if cfg.Dimension <= 0 {
		return nil, fmt.Errorf("%w: dimension must be > 0", types.ErrInvalidInput)
	}
	if cfg.M <= 0 {
		cfg.M = 16
	}
	if cfg.EfConstruction <= 0 {
		cfg.EfConstruction = 128
	}
	if cfg.EfSearch <= 0 {
		cfg.EfSearch = 64
	}
	seed := cfg.Seed
	if seed == 0 {
		seed = 42
	}
	m := cfg.M
	if m < 2 {
		m = 2
	}
	return &Index{
		dim:            cfg.Dimension,
		m:              m,
		mMax0:          m * 2,
		efConstruction: cfg.EfConstruction,
		efSearch:       cfg.EfSearch,
		levelMult:      1.0 / math.Log(float64(m)),
		rng:            rand.New(rand.NewSource(seed)),
		idToIdx:        make(map[string]uint32),
		entryPoint:     -1,
		maxLevel:       -1,
	}, nil
}

// Dimension returns the vector dimensionality of the index.
func (idx *Index) Dimension() int {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.dim
}

// Len returns the number of active (non-deleted) vectors in the index.
func (idx *Index) Len() int {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.activeCnt
}

func normalizeL2(v []float32) []float32 {
	out := make([]float32, len(v))
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	norm := math.Sqrt(sum)
	if norm > 1e-12 {
		inv := float32(1.0 / norm)
		for i, x := range v {
			out[i] = x * inv
		}
	} else {
		copy(out, v)
	}
	return out
}

func dotProduct(a, b []float32) float32 {
	var sum float32
	for i := 0; i < len(a); i++ {
		sum += a[i] * b[i]
	}
	return sum
}

func (idx *Index) randomLevel() int {
	u := idx.rng.Float64()
	if u <= 1e-12 {
		u = 1e-12
	}
	lvl := int(-math.Log(u) * idx.levelMult)
	if lvl > 16 {
		lvl = 16
	}
	return lvl
}

// Upsert inserts or updates a vector by ID.
func (idx *Index) Upsert(id string, vec []float32) error {
	if id == "" {
		return fmt.Errorf("%w: empty vector id", types.ErrInvalidInput)
	}
	idx.mu.Lock()
	defer idx.mu.Unlock()

	if len(vec) != idx.dim {
		return fmt.Errorf("%w: got %d, want %d", types.ErrDimensionMismatch, len(vec), idx.dim)
	}

	normVec := normalizeL2(vec)

	if prevIdx, exists := idx.idToIdx[id]; exists {
		prevNode := idx.nodes[prevIdx]
		if !prevNode.deleted {
			prevNode.deleted = true
			idx.activeCnt--
		}
		delete(idx.idToIdx, id)
	}

	level := idx.randomLevel()
	friends := make([][]uint32, level+1)
	for l := 0; l <= level; l++ {
		maxM := idx.m
		if l == 0 {
			maxM = idx.mMax0
		}
		friends[l] = make([]uint32, 0, maxM)
	}

	newIdx := uint32(len(idx.nodes))
	node := &hnswNode{
		id:      id,
		vec:     normVec,
		level:   level,
		deleted: false,
		friends: friends,
	}
	idx.nodes = append(idx.nodes, node)
	idx.idToIdx[id] = newIdx
	idx.activeCnt++

	if idx.entryPoint < 0 {
		idx.entryPoint = int(newIdx)
		idx.maxLevel = level
		return nil
	}

	currObj := uint32(idx.entryPoint)
	currSim := dotProduct(normVec, idx.nodes[currObj].vec)

	// Phase 1: Greedy descent from top layer down to level+1
	for l := idx.maxLevel; l > level; l-- {
		changed := true
		for changed {
			changed = false
			for _, nbIdx := range idx.nodes[currObj].friends[l] {
				sim := dotProduct(normVec, idx.nodes[nbIdx].vec)
				if sim > currSim {
					currSim = sim
					currObj = nbIdx
					changed = true
				}
			}
		}
	}

	// Phase 2: From min(level, maxLevel) down to 0, search layer & connect bidirectionally
	topConnectLevel := level
	if idx.maxLevel < topConnectLevel {
		topConnectLevel = idx.maxLevel
	}

	ep := []distItem{{idx: currObj, sim: currSim}}
	for l := topConnectLevel; l >= 0; l-- {
		candidates := idx.searchLayerLocked(normVec, ep, idx.efConstruction, l)
		maxM := idx.m
		if l == 0 {
			maxM = idx.mMax0
		}
		selected := idx.selectNeighborsHeuristicLocked(normVec, candidates, maxM)
		node.friends[l] = make([]uint32, len(selected))
		for i, c := range selected {
			node.friends[l][i] = c.idx
		}
		// Add bidirectional connections and prune if overflowing maxM
		for _, c := range selected {
			nb := idx.nodes[c.idx]
			if !containsUint32(nb.friends[l], newIdx) {
				nb.friends[l] = append(nb.friends[l], newIdx)
				if len(nb.friends[l]) > maxM {
					idx.pruneNeighborsLocked(nb, l, maxM)
				}
			}
		}
		ep = candidates
	}

	if level > idx.maxLevel {
		idx.maxLevel = level
		idx.entryPoint = int(newIdx)
	}
	return nil
}

func containsUint32(slice []uint32, val uint32) bool {
	for _, x := range slice {
		if x == val {
			return true
		}
	}
	return false
}

// selectNeighborsHeuristicLocked implements Malkov & Yashunin Algorithm 4 (heuristic neighbor selection)
// to preserve long-range inter-cluster bridges while filling remaining slots with closest candidates.
func (idx *Index) selectNeighborsHeuristicLocked(q []float32, candidates []distItem, maxM int) []distItem {
	if len(candidates) <= maxM {
		return candidates
	}
	selected := make([]distItem, 0, maxM)
	discarded := make([]distItem, 0, len(candidates))

	for _, c := range candidates {
		if len(selected) >= maxM {
			break
		}
		if idx.nodes[c.idx].deleted {
			discarded = append(discarded, c)
			continue
		}
		cVec := idx.nodes[c.idx].vec
		good := true
		// c is diverse if its similarity to q is greater than its similarity to any already-selected neighbor
		for _, s := range selected {
			simToSelected := dotProduct(cVec, idx.nodes[s.idx].vec)
			if simToSelected > c.sim {
				good = false
				break
			}
		}
		if good {
			selected = append(selected, c)
		} else {
			discarded = append(discarded, c)
		}
	}

	for _, d := range discarded {
		if len(selected) >= maxM {
			break
		}
		selected = append(selected, d)
	}
	return selected
}

func (idx *Index) pruneNeighborsLocked(node *hnswNode, level int, maxM int) {
	items := make([]distItem, len(node.friends[level]))
	for i, nbIdx := range node.friends[level] {
		items[i] = distItem{
			idx: nbIdx,
			sim: dotProduct(node.vec, idx.nodes[nbIdx].vec),
		}
	}
	// Prefer active nodes, then higher similarity
	sort.Slice(items, func(i, j int) bool {
		di := idx.nodes[items[i].idx].deleted
		dj := idx.nodes[items[j].idx].deleted
		if di != dj {
			return !di
		}
		return items[i].sim > items[j].sim
	})
	selected := idx.selectNeighborsHeuristicLocked(node.vec, items, maxM)
	node.friends[level] = node.friends[level][:len(selected)]
	for i, it := range selected {
		node.friends[level][i] = it.idx
	}
}

// Delete marks a vector as deleted (tombstoned). Returns true if the vector existed and was active.
func (idx *Index) Delete(id string) bool {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	nodeIdx, exists := idx.idToIdx[id]
	if !exists {
		return false
	}
	node := idx.nodes[nodeIdx]
	if node.deleted {
		delete(idx.idToIdx, id)
		return false
	}
	node.deleted = true
	delete(idx.idToIdx, id)
	idx.activeCnt--
	return true
}

// SearchWithFilter performs multi-layer HNSW search with security pre-filtering at layer 0.
// Graph edges of all nodes (including filtered or tombstoned ones) are still traversed to preserve
// small-world navigability, while only active nodes satisfying filter are collected into results.
func (idx *Index) SearchWithFilter(query []float32, k int, filter types.NodeFilterFunc) ([]types.VectorHit, error) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	if len(query) != idx.dim {
		return nil, fmt.Errorf("%w: got %d, want %d", types.ErrDimensionMismatch, len(query), idx.dim)
	}
	if k <= 0 || idx.activeCnt == 0 || idx.entryPoint < 0 {
		return nil, nil
	}

	q := normalizeL2(query)
	currObj := uint32(idx.entryPoint)
	currSim := dotProduct(q, idx.nodes[currObj].vec)

	for l := idx.maxLevel; l > 0; l-- {
		changed := true
		for changed {
			changed = false
			for _, nbIdx := range idx.nodes[currObj].friends[l] {
				sim := dotProduct(q, idx.nodes[nbIdx].vec)
				if sim > currSim {
					currSim = sim
					currObj = nbIdx
					changed = true
				}
			}
		}
	}

	ef := idx.efSearch
	if k*2 > ef {
		ef = k * 2
	}
	if filter != nil && ef < idx.activeCnt {
		// Expand beam width moderately when filtering to maintain high recall under strict ACLs
		expanded := ef * 2
		if expanded > idx.activeCnt {
			expanded = idx.activeCnt
		}
		if expanded > ef {
			ef = expanded
		}
	}

	hits := idx.searchLayerFilteredLocked(q, uint32(currObj), ef, k, filter)
	return hits, nil
}

// searchLayerLocked runs standard HNSW beam search at the given layer and returns candidates sorted descending by similarity.
func (idx *Index) searchLayerLocked(q []float32, entryPoints []distItem, ef int, level int) []distItem {
	visited := make([]bool, len(idx.nodes))
	candidates := &maxSimHeap{}
	results := &minSimHeap{}

	for _, ep := range entryPoints {
		if int(ep.idx) < len(visited) && !visited[ep.idx] {
			visited[ep.idx] = true
			heap.Push(candidates, ep)
			heap.Push(results, ep)
			if results.Len() > ef {
				heap.Pop(results)
			}
		}
	}

	for candidates.Len() > 0 {
		curr := heap.Pop(candidates).(distItem)
		worstSim := (*results)[0].sim
		if curr.sim < worstSim && results.Len() >= ef {
			break
		}
		node := idx.nodes[curr.idx]
		if level < len(node.friends) {
			for _, nbIdx := range node.friends[level] {
				if !visited[nbIdx] {
					visited[nbIdx] = true
					sim := dotProduct(q, idx.nodes[nbIdx].vec)
					if results.Len() < ef || sim > (*results)[0].sim {
						item := distItem{idx: nbIdx, sim: sim}
						heap.Push(candidates, item)
						heap.Push(results, item)
						if results.Len() > ef {
							heap.Pop(results)
						}
					}
				}
			}
		}
	}

	out := make([]distItem, results.Len())
	for i := len(out) - 1; i >= 0; i-- {
		out[i] = heap.Pop(results).(distItem)
	}
	return out
}

// searchLayerFilteredLocked traverses layer 0 using a traversal beam of width ef while separately
// collecting only active and filter-authorized nodes into the return list.
func (idx *Index) searchLayerFilteredLocked(q []float32, entryIdx uint32, ef int, k int, filter types.NodeFilterFunc) []types.VectorHit {
	visited := make([]bool, len(idx.nodes))
	candidates := &maxSimHeap{}
	traversalWindow := &minSimHeap{}
	filteredResults := &minSimHeap{}

	entrySim := dotProduct(q, idx.nodes[entryIdx].vec)
	visited[entryIdx] = true
	entryItem := distItem{idx: entryIdx, sim: entrySim}
	heap.Push(candidates, entryItem)
	heap.Push(traversalWindow, entryItem)

	entryNode := idx.nodes[entryIdx]
	if !entryNode.deleted && (filter == nil || filter(entryNode.id)) {
		heap.Push(filteredResults, entryItem)
	}

	for candidates.Len() > 0 {
		curr := heap.Pop(candidates).(distItem)
		worstTraverse := (*traversalWindow)[0].sim
		// Only stop early if both traversal beam is full AND we have at least k valid filtered hits
		// whose worst score exceeds curr.sim.
		if curr.sim < worstTraverse && traversalWindow.Len() >= ef && filteredResults.Len() >= k {
			break
		}

		node := idx.nodes[curr.idx]
		if len(node.friends) > 0 {
			for _, nbIdx := range node.friends[0] {
				if !visited[nbIdx] {
					visited[nbIdx] = true
					nbNode := idx.nodes[nbIdx]
					sim := dotProduct(q, nbNode.vec)
					item := distItem{idx: nbIdx, sim: sim}

					// Always consider for graph navigation beam
					if traversalWindow.Len() < ef || sim > (*traversalWindow)[0].sim || filteredResults.Len() < k {
						heap.Push(candidates, item)
						heap.Push(traversalWindow, item)
						if traversalWindow.Len() > ef {
							heap.Pop(traversalWindow)
						}
					}

					// Only collect into candidate return list if active and permitted by filter
					if !nbNode.deleted && (filter == nil || filter(nbNode.id)) {
						if filteredResults.Len() < ef || sim > (*filteredResults)[0].sim {
							heap.Push(filteredResults, item)
							if filteredResults.Len() > ef {
								heap.Pop(filteredResults)
							}
						}
					}
				}
			}
		}
	}

	allHits := make([]distItem, filteredResults.Len())
	for i := len(allHits) - 1; i >= 0; i-- {
		allHits[i] = heap.Pop(filteredResults).(distItem)
	}
	if len(allHits) > k {
		allHits = allHits[:k]
	}

	hits := make([]types.VectorHit, len(allHits))
	for i, it := range allHits {
		hits[i] = types.VectorHit{
			ID:    idx.nodes[it.idx].id,
			Score: float64(it.sim),
		}
	}
	return hits
}

// WriteTo serializes the entire HNSW index to w in a deterministic binary format.
func (idx *Index) WriteTo(w io.Writer) (int64, error) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	cw := &countingWriter{w: w}
	writeU32 := func(v uint32) error {
		return binary.Write(cw, binary.LittleEndian, v)
	}
	writeI32 := func(v int32) error {
		return binary.Write(cw, binary.LittleEndian, v)
	}

	if err := writeU32(snapshotMagic); err != nil {
		return cw.n, err
	}
	if err := writeU32(snapshotVersion); err != nil {
		return cw.n, err
	}
	if err := writeU32(uint32(idx.dim)); err != nil {
		return cw.n, err
	}
	if err := writeU32(uint32(idx.m)); err != nil {
		return cw.n, err
	}
	if err := writeU32(uint32(idx.efConstruction)); err != nil {
		return cw.n, err
	}
	if err := writeU32(uint32(idx.efSearch)); err != nil {
		return cw.n, err
	}
	if err := writeI32(int32(idx.entryPoint)); err != nil {
		return cw.n, err
	}
	if err := writeI32(int32(idx.maxLevel)); err != nil {
		return cw.n, err
	}
	if err := writeU32(uint32(idx.activeCnt)); err != nil {
		return cw.n, err
	}
	if err := writeU32(uint32(len(idx.nodes))); err != nil {
		return cw.n, err
	}

	for _, n := range idx.nodes {
		idBytes := []byte(n.id)
		if err := writeU32(uint32(len(idBytes))); err != nil {
			return cw.n, err
		}
		if _, err := cw.Write(idBytes); err != nil {
			return cw.n, err
		}
		var delByte uint8
		if n.deleted {
			delByte = 1
		}
		if err := binary.Write(cw, binary.LittleEndian, delByte); err != nil {
			return cw.n, err
		}
		if err := writeU32(uint32(n.level)); err != nil {
			return cw.n, err
		}
		for _, val := range n.vec {
			if err := binary.Write(cw, binary.LittleEndian, math.Float32bits(val)); err != nil {
				return cw.n, err
			}
		}
		if err := writeU32(uint32(len(n.friends))); err != nil {
			return cw.n, err
		}
		for _, layerFriends := range n.friends {
			if err := writeU32(uint32(len(layerFriends))); err != nil {
				return cw.n, err
			}
			for _, nb := range layerFriends {
				if err := writeU32(nb); err != nil {
					return cw.n, err
				}
			}
		}
	}

	return cw.n, nil
}

// ReadFrom deserializes a binary HNSW snapshot from r, replacing the current index state.
func (idx *Index) ReadFrom(r io.Reader) (int64, error) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	cr := &countingReader{r: r}
	readU32 := func() (uint32, error) {
		var v uint32
		err := binary.Read(cr, binary.LittleEndian, &v)
		return v, err
	}
	readI32 := func() (int32, error) {
		var v int32
		err := binary.Read(cr, binary.LittleEndian, &v)
		return v, err
	}

	magic, err := readU32()
	if err != nil {
		return cr.n, err
	}
	if magic != snapshotMagic {
		return cr.n, fmt.Errorf("%w: hnsw invalid snapshot magic", types.ErrInvalidInput)
	}
	version, err := readU32()
	if err != nil {
		return cr.n, err
	}
	if version != snapshotVersion {
		return cr.n, fmt.Errorf("%w: hnsw unsupported snapshot version %d", types.ErrInvalidInput, version)
	}

	dim, err := readU32()
	if err != nil {
		return cr.n, err
	}
	if dim == 0 || dim > 4096 {
		return cr.n, fmt.Errorf("%w: hnsw snapshot dimension %d out of bounds", types.ErrInvalidInput, dim)
	}
	m, err := readU32()
	if err != nil {
		return cr.n, err
	}
	efC, err := readU32()
	if err != nil {
		return cr.n, err
	}
	efS, err := readU32()
	if err != nil {
		return cr.n, err
	}
	ep, err := readI32()
	if err != nil {
		return cr.n, err
	}
	maxLvl, err := readI32()
	if err != nil {
		return cr.n, err
	}
	activeCnt, err := readU32()
	if err != nil {
		return cr.n, err
	}
	nodeCount, err := readU32()
	if err != nil {
		return cr.n, err
	}
	if nodeCount > 10_000_000 || activeCnt > nodeCount || (ep >= 0 && uint32(ep) >= nodeCount) {
		return cr.n, fmt.Errorf("%w: hnsw snapshot node bounds invalid", types.ErrInvalidInput)
	}

	nodes := make([]*hnswNode, nodeCount)
	idToIdx := make(map[string]uint32, activeCnt)

	for i := uint32(0); i < nodeCount; i++ {
		idLen, err := readU32()
		if err != nil {
			return cr.n, err
		}
		if idLen > 4096 {
			return cr.n, fmt.Errorf("%w: hnsw snapshot id length %d exceeds limit", types.ErrInvalidInput, idLen)
		}
		idBytes := make([]byte, idLen)
		if _, err := io.ReadFull(cr, idBytes); err != nil {
			return cr.n, err
		}
		var delByte uint8
		if err := binary.Read(cr, binary.LittleEndian, &delByte); err != nil {
			return cr.n, err
		}
		level, err := readU32()
		if err != nil {
			return cr.n, err
		}
		vec := make([]float32, dim)
		for d := uint32(0); d < dim; d++ {
			bits, err := readU32()
			if err != nil {
				return cr.n, err
			}
			vec[d] = math.Float32frombits(bits)
		}
		numLayers, err := readU32()
		if err != nil {
			return cr.n, err
		}
		if numLayers > 32 {
			return cr.n, fmt.Errorf("%w: hnsw snapshot layer count %d exceeds limit", types.ErrInvalidInput, numLayers)
		}
		friends := make([][]uint32, numLayers)
		for l := uint32(0); l < numLayers; l++ {
			fLen, err := readU32()
			if err != nil {
				return cr.n, err
			}
			if fLen > 1024 {
				return cr.n, fmt.Errorf("%w: hnsw snapshot neighbor count %d exceeds limit", types.ErrInvalidInput, fLen)
			}
			layerFriends := make([]uint32, fLen)
			for f := uint32(0); f < fLen; f++ {
				nb, err := readU32()
				if err != nil {
					return cr.n, err
				}
				if nb >= nodeCount {
					return cr.n, fmt.Errorf("%w: hnsw snapshot neighbor index %d out of range", types.ErrInvalidInput, nb)
				}
				layerFriends[f] = nb
			}
			friends[l] = layerFriends
		}
		deleted := delByte != 0
		idStr := string(idBytes)
		nodes[i] = &hnswNode{
			id:      idStr,
			vec:     vec,
			level:   int(level),
			deleted: deleted,
			friends: friends,
		}
		if !deleted {
			idToIdx[idStr] = i
		}
	}

	idx.dim = int(dim)
	idx.m = int(m)
	idx.mMax0 = int(m) * 2
	idx.efConstruction = int(efC)
	idx.efSearch = int(efS)
	idx.levelMult = 1.0 / math.Log(float64(idx.m))
	idx.entryPoint = int(ep)
	idx.maxLevel = int(maxLvl)
	idx.activeCnt = int(activeCnt)
	idx.nodes = nodes
	idx.idToIdx = idToIdx
	return cr.n, nil
}

type countingWriter struct {
	w io.Writer
	n int64
}

func (cw *countingWriter) Write(p []byte) (int, error) {
	n, err := cw.w.Write(p)
	cw.n += int64(n)
	return n, err
}

type countingReader struct {
	r io.Reader
	n int64
}

func (cr *countingReader) Read(p []byte) (int, error) {
	n, err := cr.r.Read(p)
	cr.n += int64(n)
	return n, err
}

type distItem struct {
	idx uint32
	sim float32
}

// maxSimHeap pops the item with the HIGHEST similarity first.
type maxSimHeap []distItem

func (h maxSimHeap) Len() int           { return len(h) }
func (h maxSimHeap) Less(i, j int) bool { return h[i].sim > h[j].sim }
func (h maxSimHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *maxSimHeap) Push(x any)        { *h = append(*h, x.(distItem)) }
func (h *maxSimHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	*h = old[:n-1]
	return item
}

// minSimHeap pops the item with the LOWEST similarity first.
type minSimHeap []distItem

func (h minSimHeap) Len() int           { return len(h) }
func (h minSimHeap) Less(i, j int) bool { return h[i].sim < h[j].sim }
func (h minSimHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *minSimHeap) Push(x any)        { *h = append(*h, x.(distItem)) }
func (h *minSimHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	*h = old[:n-1]
	return item
}
