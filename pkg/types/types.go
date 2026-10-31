package types

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// NodeKind defines the canonical entity categories in the Context Graph.
type NodeKind string

const (
	NodeDocument NodeKind = "doc"
	NodeChunk    NodeKind = "chunk"
	NodePerson   NodeKind = "person"
	NodeSymbol   NodeKind = "symbol"
	NodeTopic    NodeKind = "topic"
	NodeDecision NodeKind = "decision"
	NodeTicket   NodeKind = "ticket"
	NodeService  NodeKind = "service"
	NodeRepo     NodeKind = "repo"
)

// EdgeRelation defines the allowlisted directed edge types in the Context Graph.
type EdgeRelation string

const (
	RelAuthored         EdgeRelation = "AUTHORED"
	RelMentions         EdgeRelation = "MENTIONS"
	RelCollaboratesWith EdgeRelation = "COLLABORATES_WITH"
	RelDefinesSymbol    EdgeRelation = "DEFINES_SYMBOL"
	RelCalls            EdgeRelation = "CALLS"
	RelReferences       EdgeRelation = "REFERENCES"
	RelDecided          EdgeRelation = "DECIDED"
	RelSupersedes       EdgeRelation = "SUPERSEDES"
	RelContradicts      EdgeRelation = "CONTRADICTS"
	RelRootCauseOf      EdgeRelation = "ROOT_CAUSE_OF"
	RelDependsOn        EdgeRelation = "DEPENDS_ON"
	RelExpertIn         EdgeRelation = "EXPERT_IN"
	RelBelongsTo        EdgeRelation = "BELONGS_TO"
)

var allowedRelations = map[EdgeRelation]struct{}{
	RelAuthored:         {},
	RelMentions:         {},
	RelCollaboratesWith: {},
	RelDefinesSymbol:    {},
	RelCalls:            {},
	RelReferences:       {},
	RelDecided:          {},
	RelSupersedes:       {},
	RelContradicts:      {},
	RelRootCauseOf:      {},
	RelDependsOn:        {},
	RelExpertIn:         {},
	RelBelongsTo:        {},
}

// IsValidRelation returns true iff rel belongs to the strict ontology allowlist.
func IsValidRelation(rel EdgeRelation) bool {
	_, ok := allowedRelations[rel]
	return ok
}

// Provenance tracks how an entity or edge was discovered.
type Provenance string

const (
	ProvDeterministicAST Provenance = "ast"
	ProvStructural       Provenance = "structural"
	ProvLLM              Provenance = "llm"
	ProvActivity         Provenance = "activity"
	ProvManual           Provenance = "manual"
)

// DocumentStatus represents lifecycle state for authority ranking.
type DocumentStatus string

const (
	StatusVerified   DocumentStatus = "verified"
	StatusNormal     DocumentStatus = "normal"
	StatusDraft      DocumentStatus = "draft"
	StatusDeprecated DocumentStatus = "deprecated"
)

// Node represents a vertex in the Context Graph.
type Node struct {
	ID          string            `json:"id"`
	Kind        NodeKind          `json:"kind"`
	Title       string            `json:"title"`
	Properties  map[string]string `json:"properties,omitempty"`
	Permissions []string          `json:"permissions"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// Edge represents a directed, weighted, provenance-tracked relationship between two Nodes.
type Edge struct {
	ID         string       `json:"id"`
	SourceID   string       `json:"source_id"`
	SourceKind NodeKind     `json:"source_kind"`
	TargetID   string       `json:"target_id"`
	TargetKind NodeKind     `json:"target_kind"`
	Relation   EdgeRelation `json:"relation"`
	Weight     float64      `json:"weight"`
	Confidence float64      `json:"confidence"`
	Provenance Provenance   `json:"provenance"`
	Evidence   string       `json:"evidence,omitempty"`
	ValidFrom  *time.Time   `json:"valid_from,omitempty"`
	ValidUntil *time.Time   `json:"valid_until,omitempty"`
	CreatedAt  time.Time    `json:"created_at"`
}

// DeterministicEdgeID generates a collision-resistant 16-hex-char ID for (source, relation, target).
func DeterministicEdgeID(sourceID string, relation EdgeRelation, targetID string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%s", sourceID, relation, targetID)))
	return hex.EncodeToString(sum[:8])
}

// NormalizeAndValidate validates edge endpoints and relation allowlist, clamps Weight/Confidence,
// and populates ID and CreatedAt if missing.
func (e *Edge) NormalizeAndValidate() error {
	e.SourceID = strings.TrimSpace(e.SourceID)
	e.TargetID = strings.TrimSpace(e.TargetID)
	if e.SourceID == "" || e.TargetID == "" {
		return fmt.Errorf("%w: edge source_id and target_id are required", ErrInvalidInput)
	}
	if !IsValidRelation(e.Relation) {
		return fmt.Errorf("%w: %q", ErrInvalidRelation, e.Relation)
	}
	if e.SourceKind == "" {
		e.SourceKind = NodeDocument
	}
	if e.TargetKind == "" {
		e.TargetKind = NodeTopic
	}
	if e.Weight <= 0 {
		e.Weight = 1.0
	} else if e.Weight > 10.0 {
		e.Weight = 10.0
	}
	if e.Confidence <= 0 {
		e.Confidence = 1.0
	} else if e.Confidence > 1.0 {
		e.Confidence = 1.0
	}
	if e.Provenance == "" {
		e.Provenance = ProvStructural
	}
	if e.ID == "" {
		e.ID = DeterministicEdgeID(e.SourceID, e.Relation, e.TargetID)
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now().UTC()
	}
	return nil
}

// Document represents an ingested enterprise artifact.
type Document struct {
	ID               string            `json:"id"`
	SourceType       string            `json:"source_type"`
	DocType          string            `json:"doc_type"`
	Title            string            `json:"title"`
	Content          string            `json:"content"`
	ContextSummary   string            `json:"context_summary,omitempty"`
	URL              string            `json:"url"`
	FilePath         string            `json:"file_path,omitempty"`
	AuthorID         string            `json:"author_id,omitempty"`
	AuthorName       string            `json:"author_name,omitempty"`
	AuthorEmail      string            `json:"author_email,omitempty"`
	ChannelOrRepo    string            `json:"channel_or_repo,omitempty"`
	Status           DocumentStatus    `json:"status"`
	Permissions      []string          `json:"permissions"`
	Metadata         map[string]string `json:"metadata,omitempty"`
	ContentHash      string            `json:"content_hash"`
	CreatedAt        time.Time         `json:"created_at"`
	UpdatedAt        time.Time         `json:"updated_at"`
	EnrichmentStatus string            `json:"enrichment_status"`
}

// Chunk represents a semantically self-contained passage within a Document.
type Chunk struct {
	ID             string    `json:"id"`
	DocID          string    `json:"doc_id"`
	ChunkIndex     int       `json:"chunk_index"`
	HeadingPath    string    `json:"heading_path"`
	Content        string    `json:"content"`
	ContextualText string    `json:"contextual_text"`
	TokenCount     int       `json:"token_count"`
	Vector         []float32 `json:"vector,omitempty"`
}

// CodeSymbol represents a parsed programming language construct (function, method, struct, interface).
type CodeSymbol struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Kind       string   `json:"kind"`
	Package    string   `json:"package"`
	Receiver   string   `json:"receiver,omitempty"`
	Signature  string   `json:"signature"`
	DocComment string   `json:"doc_comment,omitempty"`
	FilePath   string   `json:"file_path"`
	StartLine  int      `json:"start_line"`
	EndLine    int      `json:"end_line"`
	Exported   bool     `json:"exported"`
	Calls      []string `json:"calls,omitempty"`
	Imports    []string `json:"imports,omitempty"`
}

// SupersessionInfo captures whether a document has been deprecated/superseded by a newer document.
type SupersessionInfo struct {
	Superseded     bool      `json:"superseded"`
	SupersededByID string    `json:"superseded_by_id,omitempty"`
	PenaltyFactor  float64   `json:"penalty_factor"`
	RecordedAt     time.Time `json:"recorded_at,omitempty"`
}
