package hnsw

import (
	"bytes"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"testing"

	"github.com/rgbyte/lattice/pkg/types"
)

func TestHNSW_DimensionMismatch(t *testing.T) {
	idx, err := NewIndex(Config{Dimension: 8})
	if err != nil {
		t.Fatalf("NewIndex failed: %v", err)
	}
	if idx.Dimension() != 8 {
		t.Fatalf("expected Dimension()=8, got %d", idx.Dimension())
	}

	if err := idx.Upsert("v1", []float32{1, 2, 3}); !errors.Is(err, types.ErrDimensionMismatch) {
		t.Fatalf("expected ErrDimensionMismatch on Upsert, got %v", err)
	}

	if err := idx.Upsert("v1", []float32{1, 0, 0, 0, 0, 0, 0, 0}); err != nil {
		t.Fatalf("unexpected Upsert error: %v", err)
	}

	if _, err := idx.SearchWithFilter([]float32{1, 2}, 5, nil); !errors.Is(err, types.ErrDimensionMismatch) {
		t.Fatalf("expected ErrDimensionMismatch on SearchWithFilter, got %v", err)
	}
}

func TestHNSW_SyntheticClustersAndRecall(t *testing.T) {
	const (
		dim         = 32
		numClusters = 5
		perCluster  = 40
	)
	idx, err := NewIndex(Config{
		Dimension:      dim,
		M:              16,
		EfConstruction: 128,
		EfSearch:       64,
		Seed:           1337,
	})
	if err != nil {
		t.Fatalf("NewIndex: %v", err)
	}

	rng := rand.New(rand.NewSource(99))
	centroids := make([][]float32, numClusters)
	for c := 0; c < numClusters; c++ {
		vec := make([]float32, dim)
		vec[c*6] = 10.0 // Orthogonal strong signal per cluster
		centroids[c] = normalizeL2(vec)
	}

	for c := 0; c < numClusters; c++ {
		for i := 0; i < perCluster; i++ {
			vec := make([]float32, dim)
			for d := 0; d < dim; d++ {
				vec[d] = centroids[c][d] + float32(rng.NormFloat64()*0.05)
			}
			id := fmt.Sprintf("cluster-%d-item-%d", c, i)
			if err := idx.Upsert(id, vec); err != nil {
				t.Fatalf("Upsert %s: %v", id, err)
			}
		}
	}

	if idx.Len() != numClusters*perCluster {
		t.Fatalf("expected Len=%d, got %d", numClusters*perCluster, idx.Len())
	}

	// Query each cluster centroid and verify top-10 results belong to that cluster
	for c := 0; c < numClusters; c++ {
		hits, err := idx.SearchWithFilter(centroids[c], 10, nil)
		if err != nil {
			t.Fatalf("SearchWithFilter: %v", err)
		}
		if len(hits) != 10 {
			t.Fatalf("expected 10 hits, got %d", len(hits))
		}
		prefix := fmt.Sprintf("cluster-%d-", c)
		for rank, h := range hits {
			if len(h.ID) < len(prefix) || h.ID[:len(prefix)] != prefix {
				t.Errorf("cluster %d rank %d returned wrong cluster ID %s (score=%f)", c, rank, h.ID, h.Score)
			}
		}
	}
}

func TestHNSW_SearchWithFilter_ACLSecurity(t *testing.T) {
	const dim = 16
	idx, err := NewIndex(Config{
		Dimension:      dim,
		M:              16,
		EfConstruction: 100,
		EfSearch:       64,
		Seed:           42,
	})
	if err != nil {
		t.Fatalf("NewIndex: %v", err)
	}

	query := make([]float32, dim)
	query[0] = 1.0

	// Insert 30 secret vectors that are nearly identical to the query
	for i := 0; i < 30; i++ {
		vec := make([]float32, dim)
		vec[0] = 1.0
		vec[1] = float32(i) * 0.001
		if err := idx.Upsert(fmt.Sprintf("secret:%d", i), vec); err != nil {
			t.Fatalf("Upsert secret: %v", err)
		}
	}

	// Insert 10 public vectors that are slightly further away
	for i := 0; i < 10; i++ {
		vec := make([]float32, dim)
		vec[0] = 0.7
		vec[2] = 0.7 + float32(i)*0.01
		if err := idx.Upsert(fmt.Sprintf("public:%d", i), vec); err != nil {
			t.Fatalf("Upsert public: %v", err)
		}
	}

	// Filter only allows "public:*"
	filter := func(id string) bool {
		return len(id) >= 7 && id[:7] == "public:"
	}

	hits, err := idx.SearchWithFilter(query, 5, filter)
	if err != nil {
		t.Fatalf("SearchWithFilter: %v", err)
	}
	if len(hits) != 5 {
		t.Fatalf("expected 5 authorized hits, got %d", len(hits))
	}
	for _, h := range hits {
		if !filter(h.ID) {
			t.Fatalf("SECURITY VIOLATION: unauthorized vector %q returned by SearchWithFilter", h.ID)
		}
	}
}

func TestHNSW_DeleteAndUpsertUpdate(t *testing.T) {
	idx, err := NewIndex(Config{Dimension: 4, Seed: 7})
	if err != nil {
		t.Fatalf("NewIndex: %v", err)
	}

	if err := idx.Upsert("doc:1", []float32{1, 0, 0, 0}); err != nil {
		t.Fatalf("Upsert doc:1: %v", err)
	}
	if err := idx.Upsert("doc:2", []float32{0, 1, 0, 0}); err != nil {
		t.Fatalf("Upsert doc:2: %v", err)
	}
	if idx.Len() != 2 {
		t.Fatalf("expected Len=2, got %d", idx.Len())
	}

	// Update doc:1 to point along axis 2 (index 1)
	if err := idx.Upsert("doc:1", []float32{0, 1, 0, 0}); err != nil {
		t.Fatalf("Upsert update doc:1: %v", err)
	}
	if idx.Len() != 2 {
		t.Fatalf("expected Len=2 after update, got %d", idx.Len())
	}

	hits, err := idx.SearchWithFilter([]float32{0, 1, 0, 0}, 2, nil)
	if err != nil {
		t.Fatalf("SearchWithFilter: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("expected 2 hits, got %d", len(hits))
	}
	if hits[0].Score < 0.999 || hits[1].Score < 0.999 {
		t.Fatalf("expected updated doc:1 and doc:2 both to have score ~1.0, got %+v", hits)
	}

	// Delete doc:1
	if !idx.Delete("doc:1") {
		t.Fatalf("expected Delete(doc:1) = true")
	}
	if idx.Delete("doc:1") {
		t.Fatalf("expected second Delete(doc:1) = false")
	}
	if idx.Len() != 1 {
		t.Fatalf("expected Len=1 after delete, got %d", idx.Len())
	}

	hitsAfter, err := idx.SearchWithFilter([]float32{0, 1, 0, 0}, 5, nil)
	if err != nil {
		t.Fatalf("SearchWithFilter after delete: %v", err)
	}
	if len(hitsAfter) != 1 || hitsAfter[0].ID != "doc:2" {
		t.Fatalf("expected only doc:2 after delete, got %+v", hitsAfter)
	}
}

func TestHNSW_SnapshotRoundtrip(t *testing.T) {
	const dim = 8
	idx, err := NewIndex(Config{Dimension: dim, M: 8, EfConstruction: 64, EfSearch: 32, Seed: 11})
	if err != nil {
		t.Fatalf("NewIndex: %v", err)
	}

	rng := rand.New(rand.NewSource(11))
	for i := 0; i < 50; i++ {
		v := make([]float32, dim)
		for d := 0; d < dim; d++ {
			v[d] = rng.Float32()*2 - 1
		}
		if err := idx.Upsert(fmt.Sprintf("node-%d", i), v); err != nil {
			t.Fatalf("Upsert: %v", err)
		}
	}
	idx.Delete("node-5")
	idx.Delete("node-15")

	q := []float32{0.5, -0.2, 0.8, 0.1, -0.4, 0.3, 0.9, -0.7}
	expectedHits, err := idx.SearchWithFilter(q, 10, nil)
	if err != nil {
		t.Fatalf("SearchWithFilter before snapshot: %v", err)
	}

	var buf bytes.Buffer
	written, err := idx.WriteTo(&buf)
	if err != nil {
		t.Fatalf("WriteTo failed: %v", err)
	}
	if written != int64(buf.Len()) {
		t.Fatalf("WriteTo byte count mismatch: returned %d, buffer has %d", written, buf.Len())
	}

	restored, err := NewIndex(Config{Dimension: dim})
	if err != nil {
		t.Fatalf("NewIndex restored: %v", err)
	}
	readBytes, err := restored.ReadFrom(&buf)
	if err != nil {
		t.Fatalf("ReadFrom failed: %v", err)
	}
	if readBytes != written {
		t.Fatalf("ReadFrom byte count mismatch: read %d, written %d", readBytes, written)
	}
	if restored.Len() != idx.Len() {
		t.Fatalf("restored Len=%d, want %d", restored.Len(), idx.Len())
	}

	actualHits, err := restored.SearchWithFilter(q, 10, nil)
	if err != nil {
		t.Fatalf("SearchWithFilter after restore: %v", err)
	}
	if len(actualHits) != len(expectedHits) {
		t.Fatalf("hit count mismatch: got %d, want %d", len(actualHits), len(expectedHits))
	}
	for i := range expectedHits {
		if actualHits[i].ID != expectedHits[i].ID {
			t.Errorf("rank %d ID mismatch: got %s, want %s", i, actualHits[i].ID, expectedHits[i].ID)
		}
	}
}

func TestHNSW_ConcurrentStress(t *testing.T) {
	const dim = 16
	idx, err := NewIndex(Config{Dimension: dim, M: 12, EfConstruction: 64, EfSearch: 32, Seed: 101})
	if err != nil {
		t.Fatalf("NewIndex: %v", err)
	}

	var wg sync.WaitGroup
	// 4 writer goroutines
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(int64(workerID + 1000)))
			for i := 0; i < 60; i++ {
				vec := make([]float32, dim)
				for d := 0; d < dim; d++ {
					vec[d] = rng.Float32()
				}
				id := fmt.Sprintf("w%d-item%d", workerID, i)
				if err := idx.Upsert(id, vec); err != nil {
					t.Errorf("concurrent Upsert failed: %v", err)
				}
				if i%10 == 0 && i > 0 {
					idx.Delete(fmt.Sprintf("w%d-item%d", workerID, i-5))
				}
			}
		}(w)
	}

	// 4 reader goroutines
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func(readerID int) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(int64(readerID + 2000)))
			for i := 0; i < 80; i++ {
				q := make([]float32, dim)
				for d := 0; d < dim; d++ {
					q[d] = rng.Float32()
				}
				_, err := idx.SearchWithFilter(q, 5, func(id string) bool {
					return len(id) > 0
				})
				if err != nil {
					t.Errorf("concurrent SearchWithFilter failed: %v", err)
				}
			}
		}(r)
	}

	wg.Wait()
	if idx.Len() <= 0 {
		t.Fatalf("expected positive index length after concurrent stress, got %d", idx.Len())
	}
}
