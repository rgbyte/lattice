package csr

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rgbyte/lattice/pkg/types"
)

func TestCSR_NodeEdgeCRUDAndCompaction(t *testing.T) {
	idx := NewIndex()

	if err := idx.UpsertNode(types.Node{
		ID:          "person:alice",
		Kind:        types.NodePerson,
		Title:       "Alice",
		Permissions: []string{"*"},
	}); err != nil {
		t.Fatalf("UpsertNode alice: %v", err)
	}

	if err := idx.UpsertNode(types.Node{
		ID:          "doc:rfc-1",
		Kind:        types.NodeDocument,
		Title:       "RFC 1",
		Permissions: []string{"*"},
	}); err != nil {
		t.Fatalf("UpsertNode rfc-1: %v", err)
	}

	if err := idx.UpsertEdge(types.Edge{
		SourceID:   "person:alice",
		SourceKind: types.NodePerson,
		TargetID:   "doc:rfc-1",
		TargetKind: types.NodeDocument,
		Relation:   types.RelAuthored,
		Weight:     2.0,
	}); err != nil {
		t.Fatalf("UpsertEdge: %v", err)
	}

	// Invalid edge relation should fail
	if err := idx.UpsertEdge(types.Edge{
		SourceID: "person:alice",
		TargetID: "doc:rfc-1",
		Relation: "INVALID_REL",
	}); !errors.Is(err, types.ErrInvalidRelation) {
		t.Fatalf("expected ErrInvalidRelation, got %v", err)
	}

	st := idx.Stats()
	if st.NodeCount != 2 || st.EdgeCount != 1 {
		t.Fatalf("unexpected stats: %+v", st)
	}
	if st.CSRCompactions != 1 {
		t.Fatalf("expected 1 CSR compaction, got %d", st.CSRCompactions)
	}

	// Second read without mutation should not re-compact
	nbs := idx.Neighbors("person:alice", nil)
	if len(nbs) != 1 || nbs[0].TargetID != "doc:rfc-1" {
		t.Fatalf("unexpected neighbors for alice: %+v", nbs)
	}
	// Incoming neighbor lookup on doc:rfc-1
	inNbs := idx.Neighbors("doc:rfc-1", nil)
	if len(inNbs) != 1 || inNbs[0].SourceID != "person:alice" {
		t.Fatalf("unexpected neighbors for doc:rfc-1: %+v", inNbs)
	}
	if idx.Stats().CSRCompactions != 1 {
		t.Fatalf("expected CSRCompactions to remain 1 on read-only operations, got %d", idx.Stats().CSRCompactions)
	}

	// RemoveEdgesFrom
	idx.RemoveEdgesFrom("person:alice")
	if len(idx.Neighbors("person:alice", nil)) != 0 {
		t.Fatalf("expected 0 neighbors after RemoveEdgesFrom")
	}

	// RemoveNode
	idx.RemoveNode("doc:rfc-1")
	if _, ok := idx.GetNode("doc:rfc-1"); ok {
		t.Fatalf("expected doc:rfc-1 to be removed")
	}
}

func TestCSR_PersonalizedPageRank_ConvergenceAndRanking(t *testing.T) {
	idx := NewIndex()

	// Hub-and-spoke authority graph:
	// doc:core is referenced by doc:a, doc:b, doc:c.
	// doc:peripheral is only referenced by doc:c.
	edges := []types.Edge{
		{SourceID: "doc:a", TargetID: "doc:core", Relation: types.RelReferences, Weight: 2.0},
		{SourceID: "doc:b", TargetID: "doc:core", Relation: types.RelReferences, Weight: 2.0},
		{SourceID: "doc:c", TargetID: "doc:core", Relation: types.RelReferences, Weight: 2.0},
		{SourceID: "doc:c", TargetID: "doc:peripheral", Relation: types.RelReferences, Weight: 1.0},
	}
	for _, e := range edges {
		if err := idx.UpsertEdge(e); err != nil {
			t.Fatalf("UpsertEdge: %v", err)
		}
	}

	seeds := map[string]float64{
		"doc:a": 1.0,
		"doc:b": 1.0,
		"doc:c": 1.0,
	}

	scores, err := idx.PersonalizedPageRank(context.Background(), seeds, nil, types.PPROptions{
		Alpha:         0.15,
		MaxIterations: 30,
		Tolerance:     1e-7,
	})
	if err != nil {
		t.Fatalf("PersonalizedPageRank failed: %v", err)
	}

	coreScore := scores["doc:core"]
	periphScore := scores["doc:peripheral"]
	if coreScore <= periphScore {
		t.Fatalf("expected doc:core (%f) to rank higher than doc:peripheral (%f)", coreScore, periphScore)
	}
}

func TestCSR_Security_BridgeNodeSideChannelPrevention(t *testing.T) {
	idx := NewIndex()

	// Graph topology:
	// person:alice --[AUTHORED]--> doc:secret --[MENTIONS]--> topic:m&a
	// Also a public node connected to alice:
	// person:alice --[AUTHORED]--> doc:public
	if err := idx.UpsertEdge(types.Edge{
		SourceID:   "person:alice",
		SourceKind: types.NodePerson,
		TargetID:   "doc:secret",
		TargetKind: types.NodeDocument,
		Relation:   types.RelAuthored,
		Weight:     5.0,
	}); err != nil {
		t.Fatalf("UpsertEdge: %v", err)
	}
	if err := idx.UpsertEdge(types.Edge{
		SourceID:   "doc:secret",
		SourceKind: types.NodeDocument,
		TargetID:   "topic:m&a",
		TargetKind: types.NodeTopic,
		Relation:   types.RelMentions,
		Weight:     5.0,
	}); err != nil {
		t.Fatalf("UpsertEdge: %v", err)
	}
	if err := idx.UpsertEdge(types.Edge{
		SourceID:   "person:alice",
		SourceKind: types.NodePerson,
		TargetID:   "doc:public",
		TargetKind: types.NodeDocument,
		Relation:   types.RelAuthored,
		Weight:     1.0,
	}); err != nil {
		t.Fatalf("UpsertEdge: %v", err)
	}

	// Filter denies access to "doc:secret", allows everything else (including topic:m&a)
	filter := func(id string) bool {
		return id != "doc:secret"
	}

	// 1. PersonalizedPageRank seeded at person:alice MUST NOT leak any score to topic:m&a or doc:secret
	scores, err := idx.PersonalizedPageRank(
		context.Background(),
		map[string]float64{"person:alice": 1.0},
		filter,
		types.PPROptions{Alpha: 0.15, MaxIterations: 25, Tolerance: 1e-6},
	)
	if err != nil {
		t.Fatalf("PersonalizedPageRank: %v", err)
	}
	if s, ok := scores["doc:secret"]; ok || s > 0 {
		t.Fatalf("SECURITY VIOLATION: unauthorized node doc:secret received PPR score %f", s)
	}
	if s, ok := scores["topic:m&a"]; ok || s > 0 {
		t.Fatalf("SECURITY VIOLATION: bridge node leaked PPR score %f to topic:m&a", s)
	}
	if scores["doc:public"] <= 0 {
		t.Fatalf("expected authorized neighbor doc:public to receive positive PPR score, got %f", scores["doc:public"])
	}

	// 2. ShortestPath between person:alice and topic:m&a MUST return ErrNotFound when doc:secret is filtered
	path, err := idx.ShortestPath(context.Background(), "person:alice", "topic:m&a", 4, filter)
	if !errors.Is(err, types.ErrNotFound) {
		t.Fatalf("SECURITY VIOLATION: expected ErrNotFound for ShortestPath across secret bridge, got path=%+v, err=%v", path, err)
	}

	// Unfiltered ShortestPath should find the 2-hop path
	unfilteredPath, err := idx.ShortestPath(context.Background(), "person:alice", "topic:m&a", 4, nil)
	if err != nil || len(unfilteredPath) != 2 {
		t.Fatalf("expected 2-hop path when unfiltered, got len=%d, err=%v", len(unfilteredPath), err)
	}

	// 3. Subgraph centered on person:alice MUST NOT include doc:secret or topic:m&a
	sg, err := idx.Subgraph(context.Background(), []string{"person:alice"}, 2, 50, filter)
	if err != nil {
		t.Fatalf("Subgraph: %v", err)
	}
	for _, n := range sg.Nodes {
		if n.ID == "doc:secret" || n.ID == "topic:m&a" {
			t.Fatalf("SECURITY VIOLATION: Subgraph leaked node %q", n.ID)
		}
	}
	for _, e := range sg.Edges {
		if e.SourceID == "doc:secret" || e.TargetID == "doc:secret" {
			t.Fatalf("SECURITY VIOLATION: Subgraph leaked edge %+v", e)
		}
	}
}

func TestCSR_TransitiveResolveSupersession(t *testing.T) {
	idx := NewIndex()
	baseTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	// Chain: rfc-v3 -[SUPERSEDES]-> rfc-v2 -[SUPERSEDES]-> rfc-v1
	if err := idx.UpsertEdge(types.Edge{
		SourceID:  "doc:rfc-v2",
		TargetID:  "doc:rfc-v1",
		Relation:  types.RelSupersedes,
		CreatedAt: baseTime.Add(24 * time.Hour),
	}); err != nil {
		t.Fatalf("UpsertEdge v2->v1: %v", err)
	}
	if err := idx.UpsertEdge(types.Edge{
		SourceID:  "doc:rfc-v3",
		TargetID:  "doc:rfc-v2",
		Relation:  types.RelSupersedes,
		CreatedAt: baseTime.Add(48 * time.Hour),
	}); err != nil {
		t.Fatalf("UpsertEdge v3->v2: %v", err)
	}

	// Unfiltered: rfc-v1 is transitively superseded by rfc-v3 with PenaltyFactor 0.25
	info := idx.ResolveSupersession("doc:rfc-v1", nil)
	if !info.Superseded {
		t.Fatalf("expected doc:rfc-v1 to be superseded")
	}
	if info.SupersededByID != "doc:rfc-v3" {
		t.Fatalf("expected transitive supersession by doc:rfc-v3, got %q", info.SupersededByID)
	}
	if info.PenaltyFactor != 0.25 {
		t.Fatalf("expected PenaltyFactor=0.25, got %f", info.PenaltyFactor)
	}

	// Active head rfc-v3 is not superseded
	headInfo := idx.ResolveSupersession("doc:rfc-v3", nil)
	if headInfo.Superseded || headInfo.PenaltyFactor != 1.0 {
		t.Fatalf("expected doc:rfc-v3 not to be superseded, got %+v", headInfo)
	}

	// If rfc-v3 is unauthorized for the caller, rfc-v1 resolves to rfc-v2 instead of leaking rfc-v3
	filterNoV3 := func(id string) bool {
		return id != "doc:rfc-v3"
	}
	filteredInfo := idx.ResolveSupersession("doc:rfc-v1", filterNoV3)
	if !filteredInfo.Superseded || filteredInfo.SupersededByID != "doc:rfc-v2" {
		t.Fatalf("expected filtered supersession to stop at doc:rfc-v2, got %+v", filteredInfo)
	}
}
