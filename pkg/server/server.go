package server

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/rgbyte/lattice/pkg/admin"
	"github.com/rgbyte/lattice/pkg/engine"
	"github.com/rgbyte/lattice/pkg/types"
)

const defaultMaxBodyBytes int64 = 4 << 20 // 4 MiB

// Config configures the Lattice HTTP Server & Admin Console.
type Config struct {
	AdminToken   string
	MaxBodyBytes int64
}

// Server exposes the Lattice Introspection, Validation, Search & Admin REST API
// and the embedded single-page Web Console.
type Server struct {
	eng *engine.Engine
	adm *admin.Service
	cfg Config
	mux *http.ServeMux
}

// New constructs a new HTTP Server wrapping eng and adm.
func New(eng *engine.Engine, adm *admin.Service, cfg Config) (*Server, error) {
	if eng == nil {
		return nil, fmt.Errorf("%w: engine is required", types.ErrInvalidInput)
	}
	if adm == nil {
		var err error
		adm, err = admin.NewService(eng)
		if err != nil {
			return nil, err
		}
	}
	cfg.AdminToken = strings.TrimSpace(cfg.AdminToken)
	if cfg.MaxBodyBytes <= 0 {
		cfg.MaxBodyBytes = defaultMaxBodyBytes
	}

	s := &Server{
		eng: eng,
		adm: adm,
		cfg: cfg,
		mux: http.NewServeMux(),
	}
	s.registerRoutes()
	return s, nil
}

// ServeHTTP implements http.Handler with MaxBytesReader and constant-time Bearer token authentication.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Body != nil {
		r.Body = http.MaxBytesReader(w, r.Body, s.cfg.MaxBodyBytes)
	}

	if strings.HasPrefix(r.URL.Path, "/api/") && s.cfg.AdminToken != "" {
		if !s.authorizeBearer(r) {
			writeError(w, http.StatusUnauthorized, "unauthorized: missing or invalid bearer token")
			return
		}
	}

	s.mux.ServeHTTP(w, r)
}

func (s *Server) authorizeBearer(r *http.Request) bool {
	authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
	const prefix = "Bearer "
	if !strings.HasPrefix(authHeader, prefix) {
		return false
	}
	provided := strings.TrimSpace(strings.TrimPrefix(authHeader, prefix))
	// Compare fixed-size SHA-256 digests so token length is never leaked via timing (SEC-CP-02).
	providedHash := sha256.Sum256([]byte(provided))
	expectedHash := sha256.Sum256([]byte(s.cfg.AdminToken))
	return subtle.ConstantTimeCompare(providedHash[:], expectedHash[:]) == 1
}

func (s *Server) registerRoutes() {
	s.mux.HandleFunc("/", s.handleConsole)
	s.mux.HandleFunc("/api/v1/introspect/overview", s.handleOverview)
	s.mux.HandleFunc("/api/v1/introspect/graph", s.handleExportGraph)
	s.mux.HandleFunc("/api/v1/introspect/document", s.handleInspectDocument)
	s.mux.HandleFunc("/api/v1/introspect/node", s.handleInspectNode)
	s.mux.HandleFunc("/api/v1/introspect/explain", s.handleExplainConnection)
	s.mux.HandleFunc("/api/v1/validate", s.handleValidateIntegrity)
	s.mux.HandleFunc("/api/v1/validate/acl-simulate", s.handleSimulateAccess)
	s.mux.HandleFunc("/api/v1/search", s.handleSearch)
	s.mux.HandleFunc("/api/v1/admin/documents", s.handleAdminDocuments)
	s.mux.HandleFunc("/api/v1/admin/edges", s.handleAdminEdges)
	s.mux.HandleFunc("/api/v1/admin/enrich-sweep", s.handleAdminEnrichSweep)
	s.mux.HandleFunc("/api/v1/admin/rebuild", s.handleAdminRebuild)
	s.mux.HandleFunc("/api/v1/admin/crawl", s.handleAdminCrawl)
}

func (s *Server) handleConsole(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		writeError(w, http.StatusNotFound, "endpoint not found")
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'unsafe-inline'; script-src 'unsafe-inline'")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(consoleHTML))
}

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	rep, err := s.adm.SystemOverview(r.Context())
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

func (s *Server) handleExportGraph(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	q := r.URL.Query()
	maxNodes, _ := strconv.Atoi(q.Get("max_nodes"))

	var principal *types.PrincipalSet
	userID := strings.TrimSpace(q.Get("user_id"))
	email := strings.TrimSpace(q.Get("email"))
	groupsRaw := strings.TrimSpace(q.Get("groups"))
	if userID != "" || email != "" || groupsRaw != "" {
		p := types.NewPrincipalSet(userID, email, splitCSV(groupsRaw))
		principal = &p
	}

	snap, err := s.adm.ExportGraph(r.Context(), maxNodes, principal)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, snap)
}

func (s *Server) handleAdminCrawl(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req admin.CrawlRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body: "+err.Error())
		return
	}
	rep, err := s.adm.CrawlDirectory(r.Context(), req)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

func (s *Server) handleInspectDocument(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	docID := strings.TrimSpace(r.URL.Query().Get("id"))
	rep, err := s.adm.InspectDocument(r.Context(), docID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

func (s *Server) handleInspectNode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	q := r.URL.Query()
	nodeID := strings.TrimSpace(q.Get("id"))
	hops, _ := strconv.Atoi(q.Get("hops"))
	maxNodes, _ := strconv.Atoi(q.Get("max_nodes"))

	var principal *types.PrincipalSet
	userID := strings.TrimSpace(q.Get("user_id"))
	email := strings.TrimSpace(q.Get("email"))
	groupsRaw := strings.TrimSpace(q.Get("groups"))
	if userID != "" || email != "" || groupsRaw != "" {
		p := types.NewPrincipalSet(userID, email, splitCSV(groupsRaw))
		principal = &p
	}

	rep, err := s.adm.InspectNode(r.Context(), nodeID, hops, maxNodes, principal)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

type explainRequest struct {
	FromID    string   `json:"from_id"`
	ToID      string   `json:"to_id"`
	MaxHops   int      `json:"max_hops"`
	UserID    string   `json:"user_id"`
	UserEmail string   `json:"user_email"`
	Groups    []string `json:"groups"`
}

func (s *Server) handleExplainConnection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req explainRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body: "+err.Error())
		return
	}
	var principal *types.PrincipalSet
	if strings.TrimSpace(req.UserID) != "" || strings.TrimSpace(req.UserEmail) != "" || len(req.Groups) > 0 {
		p := types.NewPrincipalSet(req.UserID, req.UserEmail, req.Groups)
		principal = &p
	}
	rep, err := s.adm.ExplainConnection(r.Context(), req.FromID, req.ToID, req.MaxHops, principal)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

func (s *Server) handleValidateIntegrity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	rep, err := s.adm.ValidateIntegrity(r.Context())
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

type aclSimulateRequest struct {
	UserID    string   `json:"user_id"`
	UserEmail string   `json:"user_email"`
	Groups    []string `json:"groups"`
	TargetIDs []string `json:"target_ids"`
}

func (s *Server) handleSimulateAccess(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req aclSimulateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body: "+err.Error())
		return
	}
	p := types.NewPrincipalSet(req.UserID, req.UserEmail, req.Groups)
	rep, err := s.adm.SimulateAccess(r.Context(), p, req.TargetIDs)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

type searchPayload struct {
	Query       string   `json:"query"`
	UserID      string   `json:"user_id"`
	UserEmail   string   `json:"user_email"`
	Groups      []string `json:"groups"`
	Limit       int      `json:"limit"`
	SourceTypes []string `json:"source_types,omitempty"`
	DocTypes    []string `json:"doc_types,omitempty"`
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req searchPayload
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body: "+err.Error())
		return
	}
	p := types.NewPrincipalSet(req.UserID, req.UserEmail, req.Groups)
	res, err := s.eng.Search(r.Context(), engine.SearchRequest{
		Query:       req.Query,
		Principal:   p,
		Limit:       req.Limit,
		SourceTypes: req.SourceTypes,
		DocTypes:    req.DocTypes,
	})
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

type indexDocRequest struct {
	Document    types.Document `json:"document"`
	AsyncEnrich bool           `json:"async_enrich"`
}

func (s *Server) handleAdminDocuments(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var req indexDocRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json body: "+err.Error())
			return
		}
		receipt, err := s.eng.IndexDocument(r.Context(), req.Document, req.AsyncEnrich)
		if err != nil {
			writeDomainError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, receipt)

	case http.MethodDelete:
		docID := strings.TrimSpace(r.URL.Query().Get("id"))
		if err := s.adm.DeleteDocument(r.Context(), docID); err != nil {
			writeDomainError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"deleted": docID})

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleAdminEdges(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var edge types.Edge
	if err := json.NewDecoder(r.Body).Decode(&edge); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body: "+err.Error())
		return
	}
	if err := s.adm.UpsertCustomEdge(r.Context(), edge); err != nil {
		writeDomainError(w, err)
		return
	}
	_ = edge.NormalizeAndValidate()
	writeJSON(w, http.StatusOK, edge)
}

type enrichSweepRequest struct {
	Limit int `json:"limit"`
}

func (s *Server) handleAdminEnrichSweep(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req enrichSweepRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	rep, err := s.adm.TriggerEnrichmentSweep(r.Context(), req.Limit)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

func (s *Server) handleAdminRebuild(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	rep, err := s.adm.RebuildIndices(r.Context())
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

func splitCSV(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func writeDomainError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, types.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, types.ErrUnauthorized):
		writeError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, types.ErrInvalidInput), errors.Is(err, types.ErrInvalidRelation), errors.Is(err, types.ErrDimensionMismatch):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		writeError(w, http.StatusGatewayTimeout, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}
