package types_test

import (
	"errors"
	"testing"

	"github.com/rgbyte/lattice/pkg/types"
)

func TestPrincipalSet_FailClosedAndMatching(t *testing.T) {
	t.Parallel()

	p := types.NewPrincipalSet("person-rohit", "rohit@acme.io", []string{"eng", "platform"})

	tests := []struct {
		name        string
		permissions []string
		want        bool
	}{
		{
			name:        "fail-closed on nil permissions",
			permissions: nil,
			want:        false,
		},
		{
			name:        "fail-closed on empty permissions slice",
			permissions: []string{},
			want:        false,
		},
		{
			name:        "fail-closed on blank string permission",
			permissions: []string{"   "},
			want:        false,
		},
		{
			name:        "public wildcard allowed",
			permissions: []string{"*"},
			want:        true,
		},
		{
			name:        "public keyword allowed",
			permissions: []string{"public"},
			want:        true,
		},
		{
			name:        "exact email match (case-insensitive)",
			permissions: []string{"ROHIT@acme.io"},
			want:        true,
		},
		{
			name:        "prefixed user email match",
			permissions: []string{"user:rohit@acme.io"},
			want:        true,
		},
		{
			name:        "user ID match",
			permissions: []string{"person-rohit"},
			want:        true,
		},
		{
			name:        "group match with prefix",
			permissions: []string{"group:platform"},
			want:        true,
		},
		{
			name:        "domain wildcard match",
			permissions: []string{"*@acme.io"},
			want:        true,
		},
		{
			name:        "different domain denied",
			permissions: []string{"*@other.io", "group:hr-confidential"},
			want:        false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := p.CanAccess(tc.permissions)
			if got != tc.want {
				t.Fatalf("CanAccess(%v) = %v, want %v", tc.permissions, got, tc.want)
			}
		})
	}
}

func TestEdgeValidationAndClamping(t *testing.T) {
	t.Parallel()

	validEdge := types.Edge{
		SourceID:   "doc:1",
		SourceKind: types.NodeDocument,
		TargetID:   "dec:1",
		TargetKind: types.NodeDecision,
		Relation:   types.RelDecided,
		Weight:     15.0, // Should clamp to [0.01, 10.0]
		Confidence: 1.5,  // Should clamp to [0.0, 1.0]
		Provenance: types.ProvLLM,
	}

	if err := validEdge.NormalizeAndValidate(); err != nil {
		t.Fatalf("expected valid edge, got error: %v", err)
	}
	if validEdge.ID == "" {
		t.Fatalf("expected deterministic edge ID to be populated")
	}
	if validEdge.Weight != 10.0 {
		t.Fatalf("expected weight clamped to 10.0, got %v", validEdge.Weight)
	}
	if validEdge.Confidence != 1.0 {
		t.Fatalf("expected confidence clamped to 1.0, got %v", validEdge.Confidence)
	}

	invalidRelEdge := validEdge
	invalidRelEdge.Relation = types.EdgeRelation("DROP_TABLE_USERS")
	if err := invalidRelEdge.NormalizeAndValidate(); !errors.Is(err, types.ErrInvalidRelation) {
		t.Fatalf("expected ErrInvalidRelation, got %v", err)
	}
}
