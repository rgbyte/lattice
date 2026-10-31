package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/rgbyte/lattice/pkg/admin"
	"github.com/rgbyte/lattice/pkg/engine"
	"github.com/rgbyte/lattice/pkg/types"
)

const maxResponseBytes int64 = 8 << 20 // 8 MiB

// APIError represents a structured HTTP error returned by the Lattice Server.
type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("lattice api http %d: %s", e.StatusCode, e.Message)
}

// Option configures a Lattice thin HTTP Client.
type Option func(*Client)

// WithAdminToken sets the Bearer authentication token sent on every request.
func WithAdminToken(token string) Option {
	return func(c *Client) {
		c.adminToken = strings.TrimSpace(token)
	}
}

// WithHTTPClient overrides the underlying *http.Client.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) {
		if hc != nil {
			c.httpClient = hc
		}
	}
}

// WithTimeout configures the HTTP client request timeout.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) {
		if d > 0 {
			c.httpClient.Timeout = d
		}
	}
}

// Client is a zero-dependency thin Go SDK for the Lattice Introspection, Validation,
// Search, and Admin REST API.
type Client struct {
	baseURL    string
	adminToken string
	httpClient *http.Client
}

// New constructs a new Lattice thin Client targeting baseURL (e.g. "http://127.0.0.1:8080").
func New(baseURL string, opts ...Option) *Client {
	c := &Client{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Overview fetches real-time system health and index scale telemetry.
func (c *Client) Overview(ctx context.Context) (*admin.OverviewReport, error) {
	var out admin.OverviewReport
	if err := c.doJSON(ctx, http.MethodGet, "/api/v1/introspect/overview", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// InspectDocument retrieves a document along with its chunks, graph vertex, incident edges, and supersession state.
func (c *Client) InspectDocument(ctx context.Context, docID string) (*admin.DocumentInspection, error) {
	q := url.Values{}
	q.Set("id", docID)
	var out admin.DocumentInspection
	if err := c.doJSON(ctx, http.MethodGet, "/api/v1/introspect/document?"+q.Encode(), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// InspectNode retrieves a vertex's properties, immediate neighbors, and K-hop ego subgraph.
func (c *Client) InspectNode(
	ctx context.Context,
	nodeID string,
	hops, maxNodes int,
	userID, userEmail string,
	groups []string,
) (*admin.NodeInspection, error) {
	q := url.Values{}
	q.Set("id", nodeID)
	if hops > 0 {
		q.Set("hops", strconv.Itoa(hops))
	}
	if maxNodes > 0 {
		q.Set("max_nodes", strconv.Itoa(maxNodes))
	}
	if userID != "" {
		q.Set("user_id", userID)
	}
	if userEmail != "" {
		q.Set("email", userEmail)
	}
	if len(groups) > 0 {
		q.Set("groups", strings.Join(groups, ","))
	}
	var out admin.NodeInspection
	if err := c.doJSON(ctx, http.MethodGet, "/api/v1/introspect/node?"+q.Encode(), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ExplainConnection compares the global shortest path against a principal-filtered path and PageRank score.
func (c *Client) ExplainConnection(
	ctx context.Context,
	fromID, toID string,
	maxHops int,
	userID, userEmail string,
	groups []string,
) (*admin.ConnectionExplanation, error) {
	payload := map[string]any{
		"from_id":    fromID,
		"to_id":      toID,
		"max_hops":   maxHops,
		"user_id":    userID,
		"user_email": userEmail,
		"groups":     groups,
	}
	var out admin.ConnectionExplanation
	if err := c.doJSON(ctx, http.MethodPost, "/api/v1/introspect/explain", payload, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ValidateIntegrity audits the Context Graph for circular supersession chains, dangling endpoints, and fail-closed nodes.
func (c *Client) ValidateIntegrity(ctx context.Context) (*admin.ValidationReport, error) {
	var out admin.ValidationReport
	if err := c.doJSON(ctx, http.MethodPost, "/api/v1/validate", map[string]any{}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SimulateAccess evaluates a principal's access against a list of target document or node IDs.
func (c *Client) SimulateAccess(
	ctx context.Context,
	userID, userEmail string,
	groups, targetIDs []string,
) (*admin.ACLSimulationReport, error) {
	payload := map[string]any{
		"user_id":    userID,
		"user_email": userEmail,
		"groups":     groups,
		"target_ids": targetIDs,
	}
	var out admin.ACLSimulationReport
	if err := c.doJSON(ctx, http.MethodPost, "/api/v1/validate/acl-simulate", payload, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Search executes a principal-scoped hybrid Context Graph query.
func (c *Client) Search(
	ctx context.Context,
	query, userID, userEmail string,
	groups []string,
	limit int,
) (*engine.SearchResponse, error) {
	payload := map[string]any{
		"query":      query,
		"user_id":    userID,
		"user_email": userEmail,
		"groups":     groups,
		"limit":      limit,
	}
	var out engine.SearchResponse
	if err := c.doJSON(ctx, http.MethodPost, "/api/v1/search", payload, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// IndexDocument indexes a document via the remote Lattice server.
func (c *Client) IndexDocument(ctx context.Context, doc types.Document, asyncEnrich bool) (*engine.IndexReceipt, error) {
	payload := map[string]any{
		"document":     doc,
		"async_enrich": asyncEnrich,
	}
	var out engine.IndexReceipt
	if err := c.doJSON(ctx, http.MethodPost, "/api/v1/admin/documents", payload, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteDocument removes a document and its chunks/edges from the remote Lattice server.
func (c *Client) DeleteDocument(ctx context.Context, docID string) error {
	q := url.Values{}
	q.Set("id", docID)
	var out map[string]any
	return c.doJSON(ctx, http.MethodDelete, "/api/v1/admin/documents?"+q.Encode(), nil, &out)
}

// UpsertEdge inserts or updates a custom directed edge in the remote Context Graph.
func (c *Client) UpsertEdge(ctx context.Context, edge types.Edge) error {
	var out types.Edge
	return c.doJSON(ctx, http.MethodPost, "/api/v1/admin/edges", edge, &out)
}

// TriggerEnrichmentSweep runs a synchronous Phase-2 enrichment batch on the remote server.
func (c *Client) TriggerEnrichmentSweep(ctx context.Context, limit int) (*admin.EnrichmentSweepReport, error) {
	payload := map[string]any{"limit": limit}
	var out admin.EnrichmentSweepReport
	if err := c.doJSON(ctx, http.MethodPost, "/api/v1/admin/enrich-sweep", payload, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RebuildIndices forces a full re-hydration of the in-memory HNSW and CSR indices from SQLite storage.
func (c *Client) RebuildIndices(ctx context.Context) (*admin.OverviewReport, error) {
	var out admin.OverviewReport
	if err := c.doJSON(ctx, http.MethodPost, "/api/v1/admin/rebuild", map[string]any{}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) doJSON(ctx context.Context, method, path string, reqBody any, respOut any) error {
	var bodyReader io.Reader
	if reqBody != nil {
		b, err := json.Marshal(reqBody)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.adminToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.adminToken)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	limited := io.LimitReader(resp.Body, maxResponseBytes)
	rawBytes, err := io.ReadAll(limited)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var errEnv struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(rawBytes, &errEnv)
		msg := errEnv.Error
		if msg == "" {
			msg = strings.TrimSpace(string(rawBytes))
		}
		return &APIError{
			StatusCode: resp.StatusCode,
			Message:    msg,
		}
	}

	if respOut != nil && len(rawBytes) > 0 {
		if err := json.Unmarshal(rawBytes, respOut); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}
