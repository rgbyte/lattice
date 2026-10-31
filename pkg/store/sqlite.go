package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"github.com/rgbyte/lattice/pkg/types"
)

var reSafeFTSToken = regexp.MustCompile(`[a-zA-Z0-9_]{2,}`)

// SanitizeFTS5Query strips all FTS5 syntax operators/punctuation and quotes each
// safe alphanumeric token as `"tok"*` joined by ` OR ` so FTS5 syntax injection is impossible.
func SanitizeFTS5Query(q string) string {
	matches := reSafeFTSToken.FindAllString(q, 24)
	if len(matches) == 0 {
		return ""
	}
	var parts []string
	seen := make(map[string]struct{}, len(matches))
	for _, raw := range matches {
		upper := strings.ToUpper(raw)
		if upper == "AND" || upper == "OR" || upper == "NOT" || upper == "NEAR" {
			continue
		}
		lower := strings.ToLower(raw)
		if _, exists := seen[lower]; exists {
			continue
		}
		seen[lower] = struct{}{}
		parts = append(parts, fmt.Sprintf(`"%s"*`, lower))
	}
	return strings.Join(parts, " OR ")
}

// SQLiteStore implements types.StorageBackend backed by pure-Go modernc.org/sqlite
// in WAL mode with FTS5 full-text indexing.
type SQLiteStore struct {
	db      *sql.DB
	writeMu sync.Mutex
}

var _ types.StorageBackend = (*SQLiteStore)(nil)

const schemaDDL = `
CREATE TABLE IF NOT EXISTS cg_documents (
	id                TEXT PRIMARY KEY,
	source_type       TEXT NOT NULL DEFAULT '',
	doc_type          TEXT NOT NULL DEFAULT '',
	title             TEXT NOT NULL DEFAULT '',
	content           TEXT NOT NULL DEFAULT '',
	context_summary   TEXT NOT NULL DEFAULT '',
	url               TEXT NOT NULL DEFAULT '',
	file_path         TEXT NOT NULL DEFAULT '',
	author_id         TEXT NOT NULL DEFAULT '',
	author_name       TEXT NOT NULL DEFAULT '',
	author_email      TEXT NOT NULL DEFAULT '',
	channel_or_repo   TEXT NOT NULL DEFAULT '',
	status            TEXT NOT NULL DEFAULT 'normal',
	permissions_json  TEXT NOT NULL DEFAULT '[]',
	metadata_json     TEXT NOT NULL DEFAULT '{}',
	content_hash      TEXT NOT NULL DEFAULT '',
	created_at        INTEGER NOT NULL DEFAULT 0,
	updated_at        INTEGER NOT NULL DEFAULT 0,
	enrichment_status TEXT NOT NULL DEFAULT 'pending'
);

CREATE INDEX IF NOT EXISTS idx_cg_documents_enrichment
	ON cg_documents(enrichment_status, updated_at DESC);

CREATE TABLE IF NOT EXISTS cg_chunks (
	id              TEXT PRIMARY KEY,
	doc_id          TEXT NOT NULL REFERENCES cg_documents(id) ON DELETE CASCADE,
	chunk_index     INTEGER NOT NULL DEFAULT 0,
	heading_path    TEXT NOT NULL DEFAULT '',
	content         TEXT NOT NULL DEFAULT '',
	contextual_text TEXT NOT NULL DEFAULT '',
	token_count     INTEGER NOT NULL DEFAULT 1,
	vector_blob     BLOB
);

CREATE INDEX IF NOT EXISTS idx_cg_chunks_doc_id
	ON cg_chunks(doc_id, chunk_index);

CREATE VIRTUAL TABLE IF NOT EXISTS cg_chunks_fts USING fts5(
	chunk_id UNINDEXED,
	doc_id UNINDEXED,
	title,
	heading_path,
	contextual_text,
	tokenize = 'porter unicode61'
);

CREATE TABLE IF NOT EXISTS cg_nodes (
	id               TEXT PRIMARY KEY,
	kind             TEXT NOT NULL,
	title            TEXT NOT NULL DEFAULT '',
	properties_json  TEXT NOT NULL DEFAULT '{}',
	permissions_json TEXT NOT NULL DEFAULT '[]',
	updated_at       INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_cg_nodes_kind
	ON cg_nodes(kind);

CREATE TABLE IF NOT EXISTS cg_edges (
	id          TEXT PRIMARY KEY,
	source_id   TEXT NOT NULL,
	source_kind TEXT NOT NULL,
	target_id   TEXT NOT NULL,
	target_kind TEXT NOT NULL,
	relation    TEXT NOT NULL,
	weight      REAL NOT NULL DEFAULT 1.0,
	confidence  REAL NOT NULL DEFAULT 1.0,
	provenance  TEXT NOT NULL DEFAULT 'structural',
	evidence    TEXT NOT NULL DEFAULT '',
	valid_from  INTEGER,
	valid_until INTEGER,
	created_at  INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_cg_edges_source ON cg_edges(source_id);
CREATE INDEX IF NOT EXISTS idx_cg_edges_target ON cg_edges(target_id);
CREATE INDEX IF NOT EXISTS idx_cg_edges_rel    ON cg_edges(relation);
`

// NewSQLiteStore opens or creates a SQLite database at dsn, configures WAL mode,
// busy_timeout=5000, foreign_keys=ON, and initializes all Context Graph tables.
func NewSQLiteStore(dsn string) (*SQLiteStore, error) {
	if strings.TrimSpace(dsn) == "" {
		dsn = ":memory:"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %q: %w", dsn, err)
	}

	if dsn == ":memory:" || strings.Contains(dsn, ":memory:") {
		db.SetMaxOpenConns(1)
	} else {
		db.SetMaxOpenConns(8)
	}

	pragmas := []string{
		"PRAGMA journal_mode = WAL;",
		"PRAGMA synchronous = NORMAL;",
		"PRAGMA busy_timeout = 5000;",
		"PRAGMA foreign_keys = ON;",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("exec pragma %q: %w", p, err)
		}
	}

	if _, err := db.Exec(schemaDDL); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("init sqlite schema: %w", err)
	}

	return &SQLiteStore{db: db}, nil
}

// UpsertDocumentBundle atomically persists a Document along with its Chunks, FTS5 entries,
// Nodes, and Edges. If a document with the same ID and identical ContentHash already exists,
// it returns (false, nil) without redundant writes.
func (s *SQLiteStore) UpsertDocumentBundle(ctx context.Context, bundle types.DocumentBundle) (bool, error) {
	doc := bundle.Document
	if strings.TrimSpace(doc.ID) == "" {
		return false, fmt.Errorf("%w: document ID is required", types.ErrInvalidInput)
	}
	if doc.ContentHash == "" {
		doc.ContentHash = computeDocHash(doc)
	}
	now := time.Now().UTC()
	if doc.CreatedAt.IsZero() {
		doc.CreatedAt = now
	}
	if doc.UpdatedAt.IsZero() {
		doc.UpdatedAt = now
	}
	if doc.Status == "" {
		doc.Status = types.StatusNormal
	}
	if doc.EnrichmentStatus == "" {
		doc.EnrichmentStatus = "pending"
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	// 1. Check ContentHash idempotency
	var existingHash string
	err := s.db.QueryRowContext(ctx, `SELECT content_hash FROM cg_documents WHERE id = ?`, doc.ID).Scan(&existingHash)
	if err == nil && existingHash != "" && existingHash == doc.ContentHash {
		return false, nil
	} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	// 2. Remove stale FTS5 rows, chunks, and outgoing edges for this document
	if _, err := tx.ExecContext(ctx, `DELETE FROM cg_chunks_fts WHERE doc_id = ?`, doc.ID); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM cg_chunks WHERE doc_id = ?`, doc.ID); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM cg_edges WHERE source_id = ?`, doc.ID); err != nil {
		return false, err
	}

	permsJSON := marshalStringSlice(doc.Permissions)
	metaJSON := marshalStringMap(doc.Metadata)

	// 3. Upsert document row
	_, err = tx.ExecContext(ctx, `
		INSERT INTO cg_documents (
			id, source_type, doc_type, title, content, context_summary,
			url, file_path, author_id, author_name, author_email, channel_or_repo,
			status, permissions_json, metadata_json, content_hash,
			created_at, updated_at, enrichment_status
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			source_type       = excluded.source_type,
			doc_type          = excluded.doc_type,
			title             = excluded.title,
			content           = excluded.content,
			context_summary   = excluded.context_summary,
			url               = excluded.url,
			file_path         = excluded.file_path,
			author_id         = excluded.author_id,
			author_name       = excluded.author_name,
			author_email      = excluded.author_email,
			channel_or_repo   = excluded.channel_or_repo,
			status            = excluded.status,
			permissions_json  = excluded.permissions_json,
			metadata_json     = excluded.metadata_json,
			content_hash      = excluded.content_hash,
			updated_at        = excluded.updated_at,
			enrichment_status = excluded.enrichment_status
	`,
		doc.ID, doc.SourceType, doc.DocType, doc.Title, doc.Content, doc.ContextSummary,
		doc.URL, doc.FilePath, doc.AuthorID, doc.AuthorName, doc.AuthorEmail, doc.ChannelOrRepo,
		string(doc.Status), permsJSON, metaJSON, doc.ContentHash,
		doc.CreatedAt.Unix(), doc.UpdatedAt.Unix(), doc.EnrichmentStatus,
	)
	if err != nil {
		return false, err
	}

	// 4. Insert chunks and FTS5 rows
	for _, c := range bundle.Chunks {
		if c.DocID == "" {
			c.DocID = doc.ID
		}
		if c.ContextualText == "" {
			c.ContextualText = c.Content
		}
		vecBlob := encodeFloat32Slice(c.Vector)
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO cg_chunks (
				id, doc_id, chunk_index, heading_path, content, contextual_text, token_count, vector_blob
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		`, c.ID, c.DocID, c.ChunkIndex, c.HeadingPath, c.Content, c.ContextualText, c.TokenCount, vecBlob); err != nil {
			return false, err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO cg_chunks_fts (chunk_id, doc_id, title, heading_path, contextual_text)
			VALUES (?, ?, ?, ?, ?)
		`, c.ID, c.DocID, doc.Title, c.HeadingPath, c.ContextualText); err != nil {
			return false, err
		}
	}

	// 5. Upsert Nodes
	for _, n := range bundle.Nodes {
		if err := upsertNodeTx(ctx, tx, n); err != nil {
			return false, err
		}
	}

	// 6. Upsert Edges
	for _, e := range bundle.Edges {
		if err := upsertEdgeTx(ctx, tx, e); err != nil {
			return false, err
		}
	}

	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func (s *SQLiteStore) GetDocument(ctx context.Context, docID string) (types.Document, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, source_type, doc_type, title, content, context_summary,
		       url, file_path, author_id, author_name, author_email, channel_or_repo,
		       status, permissions_json, metadata_json, content_hash,
		       created_at, updated_at, enrichment_status
		FROM cg_documents WHERE id = ?
	`, docID)

	return scanDocument(row)
}

func (s *SQLiteStore) GetChunk(ctx context.Context, chunkID string) (types.Chunk, error) {
	var (
		c       types.Chunk
		vecBlob []byte
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT id, doc_id, chunk_index, heading_path, content, contextual_text, token_count, vector_blob
		FROM cg_chunks WHERE id = ?
	`, chunkID).Scan(
		&c.ID, &c.DocID, &c.ChunkIndex, &c.HeadingPath, &c.Content, &c.ContextualText, &c.TokenCount, &vecBlob,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return types.Chunk{}, types.ErrNotFound
	}
	if err != nil {
		return types.Chunk{}, err
	}
	c.Vector = decodeFloat32Slice(vecBlob)
	return c, nil
}

func (s *SQLiteStore) DeleteDocument(ctx context.Context, docID string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DELETE FROM cg_chunks_fts WHERE doc_id = ?`, docID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM cg_chunks WHERE doc_id = ?`, docID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM cg_edges WHERE source_id = ? OR target_id = ?`, docID, docID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM cg_nodes WHERE id = ?`, docID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM cg_documents WHERE id = ?`, docID); err != nil {
		return err
	}
	return tx.Commit()
}

// SearchFTS executes a sanitized, parameterized FTS5 BM25 search over cg_chunks_fts.
func (s *SQLiteStore) SearchFTS(ctx context.Context, query string, limit int) ([]types.LexicalHit, error) {
	safeQuery := SanitizeFTS5Query(query)
	if safeQuery == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 20
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT chunk_id, doc_id, -bm25(cg_chunks_fts, 0.0, 0.0, 3.0, 2.0, 1.0) AS score
		FROM cg_chunks_fts
		WHERE cg_chunks_fts MATCH ?
		ORDER BY score DESC
		LIMIT ?
	`, safeQuery, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var hits []types.LexicalHit
	for rows.Next() {
		var h types.LexicalHit
		if err := rows.Scan(&h.ChunkID, &h.DocID, &h.BM25Score); err != nil {
			return nil, err
		}
		if h.BM25Score <= 0 {
			h.BM25Score = 1e-6
		}
		hits = append(hits, h)
	}
	return hits, rows.Err()
}

func (s *SQLiteStore) UpsertNode(ctx context.Context, node types.Node) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := upsertNodeTx(ctx, tx, node); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLiteStore) UpsertEdge(ctx context.Context, edge types.Edge) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := upsertEdgeTx(ctx, tx, edge); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLiteStore) ListPendingEnrichment(ctx context.Context, limit int) ([]types.Document, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, source_type, doc_type, title, content, context_summary,
		       url, file_path, author_id, author_name, author_email, channel_or_repo,
		       status, permissions_json, metadata_json, content_hash,
		       created_at, updated_at, enrichment_status
		FROM cg_documents
		WHERE enrichment_status = 'pending' OR enrichment_status = ''
		ORDER BY updated_at DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var docs []types.Document
	for rows.Next() {
		doc, err := scanDocument(rows)
		if err != nil {
			return nil, err
		}
		docs = append(docs, doc)
	}
	return docs, rows.Err()
}

func (s *SQLiteStore) SaveEnrichment(ctx context.Context, docID, summary string, chunks []types.Chunk, nodes []types.Node, edges []types.Edge) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC().Unix()
	var docTitle string
	_ = tx.QueryRowContext(ctx, `SELECT title FROM cg_documents WHERE id = ?`, docID).Scan(&docTitle)

	if _, err := tx.ExecContext(ctx, `
		UPDATE cg_documents
		SET context_summary = ?, enrichment_status = 'done', updated_at = ?
		WHERE id = ?
	`, summary, now, docID); err != nil {
		return err
	}

	if len(chunks) > 0 {
		if _, err := tx.ExecContext(ctx, `DELETE FROM cg_chunks_fts WHERE doc_id = ?`, docID); err != nil {
			return err
		}
		for _, c := range chunks {
			vecBlob := encodeFloat32Slice(c.Vector)
			if _, err := tx.ExecContext(ctx, `
				UPDATE cg_chunks
				SET contextual_text = ?, vector_blob = ?
				WHERE id = ?
			`, c.ContextualText, vecBlob, c.ID); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO cg_chunks_fts (chunk_id, doc_id, title, heading_path, contextual_text)
				VALUES (?, ?, ?, ?, ?)
			`, c.ID, docID, docTitle, c.HeadingPath, c.ContextualText); err != nil {
				return err
			}
		}
	}

	for _, n := range nodes {
		if err := upsertNodeTx(ctx, tx, n); err != nil {
			return err
		}
	}
	for _, e := range edges {
		if err := upsertEdgeTx(ctx, tx, e); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// Hydrate streams all persisted Nodes, Edges, and Chunk Vectors into the in-memory
// GraphIndex and VectorIndex upon engine startup.
func (s *SQLiteStore) Hydrate(ctx context.Context, vIdx types.VectorIndex, gIdx types.GraphIndex) error {
	if gIdx != nil {
		nodeRows, err := s.db.QueryContext(ctx, `
			SELECT id, kind, title, properties_json, permissions_json, updated_at
			FROM cg_nodes
		`)
		if err != nil {
			return err
		}
		for nodeRows.Next() {
			var (
				n         types.Node
				kindStr   string
				propsJSON string
				permsJSON string
				updatedAt int64
			)
			if err := nodeRows.Scan(&n.ID, &kindStr, &n.Title, &propsJSON, &permsJSON, &updatedAt); err != nil {
				_ = nodeRows.Close()
				return err
			}
			n.Kind = types.NodeKind(kindStr)
			n.Properties = unmarshalStringMap(propsJSON)
			n.Permissions = unmarshalStringSlice(permsJSON)
			n.UpdatedAt = time.Unix(updatedAt, 0).UTC()
			if err := gIdx.UpsertNode(n); err != nil {
				_ = nodeRows.Close()
				return err
			}
		}
		if err := nodeRows.Err(); err != nil {
			_ = nodeRows.Close()
			return err
		}
		if err := nodeRows.Close(); err != nil {
			return err
		}

		edgeRows, err := s.db.QueryContext(ctx, `
			SELECT id, source_id, source_kind, target_id, target_kind, relation,
			       weight, confidence, provenance, evidence, valid_from, valid_until, created_at
			FROM cg_edges
		`)
		if err != nil {
			return err
		}
		for edgeRows.Next() {
			var (
				e          types.Edge
				srcKind    string
				tgtKind    string
				relStr     string
				provStr    string
				validFrom  sql.NullInt64
				validUntil sql.NullInt64
				createdAt  int64
			)
			if err := edgeRows.Scan(
				&e.ID, &e.SourceID, &srcKind, &e.TargetID, &tgtKind, &relStr,
				&e.Weight, &e.Confidence, &provStr, &e.Evidence, &validFrom, &validUntil, &createdAt,
			); err != nil {
				_ = edgeRows.Close()
				return err
			}
			e.SourceKind = types.NodeKind(srcKind)
			e.TargetKind = types.NodeKind(tgtKind)
			e.Relation = types.EdgeRelation(relStr)
			e.Provenance = types.Provenance(provStr)
			if validFrom.Valid {
				t := time.Unix(validFrom.Int64, 0).UTC()
				e.ValidFrom = &t
			}
			if validUntil.Valid {
				t := time.Unix(validUntil.Int64, 0).UTC()
				e.ValidUntil = &t
			}
			e.CreatedAt = time.Unix(createdAt, 0).UTC()
			if err := gIdx.UpsertEdge(e); err != nil {
				_ = edgeRows.Close()
				return err
			}
		}
		if err := edgeRows.Err(); err != nil {
			_ = edgeRows.Close()
			return err
		}
		if err := edgeRows.Close(); err != nil {
			return err
		}
	}

	if vIdx != nil {
		chunkRows, err := s.db.QueryContext(ctx, `
			SELECT id, vector_blob FROM cg_chunks WHERE vector_blob IS NOT NULL
		`)
		if err != nil {
			return err
		}
		for chunkRows.Next() {
			var (
				id      string
				vecBlob []byte
			)
			if err := chunkRows.Scan(&id, &vecBlob); err != nil {
				_ = chunkRows.Close()
				return err
			}
			vec := decodeFloat32Slice(vecBlob)
			if len(vec) > 0 {
				if err := vIdx.Upsert(id, vec); err != nil {
					_ = chunkRows.Close()
					return err
				}
			}
		}
		if err := chunkRows.Err(); err != nil {
			_ = chunkRows.Close()
			return err
		}
		if err := chunkRows.Close(); err != nil {
			return err
		}
	}

	return nil
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

func upsertNodeTx(ctx context.Context, tx *sql.Tx, n types.Node) error {
	if strings.TrimSpace(n.ID) == "" {
		return fmt.Errorf("%w: node ID is required", types.ErrInvalidInput)
	}
	if n.Kind == "" {
		n.Kind = types.NodeTopic
	}
	if n.UpdatedAt.IsZero() {
		n.UpdatedAt = time.Now().UTC()
	}
	propsJSON := marshalStringMap(n.Properties)
	permsJSON := marshalStringSlice(n.Permissions)
	_, err := tx.ExecContext(ctx, `
		INSERT INTO cg_nodes (id, kind, title, properties_json, permissions_json, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			kind             = excluded.kind,
			title            = CASE WHEN excluded.title = '' OR excluded.title = excluded.id THEN cg_nodes.title ELSE excluded.title END,
			properties_json  = excluded.properties_json,
			permissions_json = CASE WHEN excluded.permissions_json = '[]' THEN cg_nodes.permissions_json ELSE excluded.permissions_json END,
			updated_at       = excluded.updated_at
	`, n.ID, string(n.Kind), n.Title, propsJSON, permsJSON, n.UpdatedAt.Unix())
	return err
}

func upsertEdgeTx(ctx context.Context, tx *sql.Tx, e types.Edge) error {
	if err := e.NormalizeAndValidate(); err != nil {
		return err
	}
	var validFrom, validUntil any
	if e.ValidFrom != nil {
		validFrom = e.ValidFrom.Unix()
	}
	if e.ValidUntil != nil {
		validUntil = e.ValidUntil.Unix()
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO cg_edges (
			id, source_id, source_kind, target_id, target_kind, relation,
			weight, confidence, provenance, evidence, valid_from, valid_until, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			weight      = excluded.weight,
			confidence  = excluded.confidence,
			provenance  = excluded.provenance,
			evidence    = excluded.evidence,
			valid_from  = excluded.valid_from,
			valid_until = excluded.valid_until
	`,
		e.ID, e.SourceID, string(e.SourceKind), e.TargetID, string(e.TargetKind), string(e.Relation),
		e.Weight, e.Confidence, string(e.Provenance), e.Evidence, validFrom, validUntil, e.CreatedAt.Unix(),
	)
	return err
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanDocument(r rowScanner) (types.Document, error) {
	var (
		doc       types.Document
		statusStr string
		permsJSON string
		metaJSON  string
		createdAt int64
		updatedAt int64
	)
	err := r.Scan(
		&doc.ID, &doc.SourceType, &doc.DocType, &doc.Title, &doc.Content, &doc.ContextSummary,
		&doc.URL, &doc.FilePath, &doc.AuthorID, &doc.AuthorName, &doc.AuthorEmail, &doc.ChannelOrRepo,
		&statusStr, &permsJSON, &metaJSON, &doc.ContentHash,
		&createdAt, &updatedAt, &doc.EnrichmentStatus,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return types.Document{}, types.ErrNotFound
	}
	if err != nil {
		return types.Document{}, err
	}
	doc.Status = types.DocumentStatus(statusStr)
	doc.Permissions = unmarshalStringSlice(permsJSON)
	doc.Metadata = unmarshalStringMap(metaJSON)
	doc.CreatedAt = time.Unix(createdAt, 0).UTC()
	doc.UpdatedAt = time.Unix(updatedAt, 0).UTC()
	return doc, nil
}

func computeDocHash(doc types.Document) string {
	h := sha256.New()
	_, _ = h.Write([]byte(doc.Title))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(doc.Content))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(strings.Join(doc.Permissions, ",")))
	return hex.EncodeToString(h.Sum(nil))
}

func encodeFloat32Slice(v []float32) []byte {
	if len(v) == 0 {
		return nil
	}
	buf := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}

func decodeFloat32Slice(b []byte) []float32 {
	if len(b) == 0 || len(b)%4 != 0 {
		return nil
	}
	n := len(b) / 4
	out := make([]float32, n)
	for i := 0; i < n; i++ {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return out
}

func marshalStringSlice(s []string) string {
	if len(s) == 0 {
		return "[]"
	}
	b, err := json.Marshal(s)
	if err != nil {
		return "[]"
	}
	return string(b)
}

func unmarshalStringSlice(raw string) []string {
	if raw == "" || raw == "[]" {
		return nil
	}
	var out []string
	_ = json.Unmarshal([]byte(raw), &out)
	return out
}

func marshalStringMap(m map[string]string) string {
	if len(m) == 0 {
		return "{}"
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func unmarshalStringMap(raw string) map[string]string {
	if raw == "" || raw == "{}" {
		return nil
	}
	var out map[string]string
	_ = json.Unmarshal([]byte(raw), &out)
	return out
}
