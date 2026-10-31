# Lattice (`github.com/rgbyte/lattice`)

**Lattice** is a high-performance, local-first **Context Graph, HNSW Vector Index, and Personalized PageRank Engine** written in **100% Pure Go** (zero CGO).

Designed as a standalone, dependency-inverted core engine for enterprise knowledge discovery and Grounded Work AI, Lattice combines:
- **SQLite WAL ACID Storage (`pkg/store`)** — Single-file transactional source of truth with FTS5 BM25 lexical search and injection-hardened query sanitization.
- **Hierarchical Navigable Small World Vector Index (`pkg/hnsw`)** — `O(log N)` Approximate Nearest Neighbor (ANN) vector search with in-traversal ACL permission pre-filtering and binary snapshots.
- **Compressed Sparse Row Graph & Personalized PageRank (`pkg/csr`)** — Sub-millisecond multi-hop random walks with restart, shortest-path explainability, ego-network extraction, and transitive `SUPERSEDES` deprecation resolution.
- **Deterministic AST & Structural Extractor (`pkg/extract`)** — Native `go/ast` symbol & call-graph extractor (`DEFINES_SYMBOL`, `CALLS`, `DEPENDS_ON`) paired with heading-aware Markdown breadcrumb chunking.
- **Two-Tier Neural Embedder & Hardened LLM Enricher (`pkg/enrich`)** — Batch Ollama embeddings (`nomic-embed-text` with Matryoshka dimension slicing + offline fallback) and prompt-injection-hardened semantic triple & Contextual Retrieval header extraction.
- **Unified Context Graph Engine (`pkg/engine`)** — Synchronous Phase-1 structural indexing + asynchronous Phase-2 background LLM enrichment + 4-stage hybrid retrieval (FTS5 BM25 + HNSW Vector + ACL-Isolated PageRank + Supersession/Freshness).

## Layered Architecture

```
pkg/
├── types/     # Layer 1: Domain Ontology, Fail-Closed PrincipalSet ACLs & Port Interfaces
├── hnsw/      # Layer 2: Multi-Layer HNSW ANN Vector Index (In-Traversal ACL Pre-Filtering)
├── csr/       # Layer 3: CSR Adjacency Graph, ACL-Isolated Personalized PageRank & Shortest Path
├── extract/   # Layer 4: Deterministic Go AST Symbol/Call-Graph & Markdown Structural Extractor
├── enrich/    # Layer 5: Neural Batch Embedder & Prompt-Injection-Hardened LLM Triple Enricher
├── store/     # Layer 6: Pure-Go SQLite WAL ACID Backend + FTS5 Sanitizer + Index Hydration
└── engine/    # Layer 7: Unified Context Graph Engine & Async Enrichment Worker Pool
```

## Security Invariants

1. **Fail-Closed ACLs (`types.PrincipalSet`):** Missing or empty permissions always deny access (`CanAccess == false`).
2. **In-Traversal HNSW & CSR Filtering:** Unauthorized nodes are filtered *during* HNSW beam search and CSR Personalized PageRank power iterations so confidential documents cannot leak via transitive authority scores or bridge paths.
3. **FTS5 & SQL Parameterization:** All SQLite operations use parameterized queries and strict alphanumeric FTS5 token sanitization.
4. **LLM Untrusted Content Isolation:** Document content is isolated inside `<untrusted_document_content>` delimiters, and extracted relations are validated against a strict schema allowlist (`types.IsValidRelation`) with clamped weights and per-document edge caps.
