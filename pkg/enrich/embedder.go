package enrich

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/rgbyte/lattice/pkg/types"
)

const (
	defaultEmbedDim = 384
	minEmbedDim     = 64
	conceptDimCount = 32
)

// enterpriseConcepts defines 32 canonical engineering & organizational concept clusters.
// Projecting tokens onto these clusters before signed feature hashing gives LocalEmbedder
// strong semantic recall across synonyms (e.g., "incident"/"outage", "auth"/"jwt").
var enterpriseConcepts = [conceptDimCount][]string{
	0:  {"auth", "authentication", "authorize", "authorization", "jwt", "oauth", "token", "principal", "acl", "permission", "rbac", "security", "hmac", "secret", "credential"},
	1:  {"sql", "sqlite", "postgres", "database", "db", "storage", "wal", "transaction", "acid", "schema", "table", "query", "fts", "fts5", "persistence"},
	2:  {"incident", "outage", "postmortem", "sev1", "sev2", "root", "cause", "failure", "crash", "downtime", "mitigation", "rollback", "pager", "oncall", "sre"},
	3:  {"graph", "csr", "node", "edge", "vertex", "pagerank", "ppr", "walk", "hop", "subgraph", "topology", "network", "neighbor", "traversal"},
	4:  {"vector", "embedding", "hnsw", "ann", "cosine", "similarity", "dense", "matryoshka", "dimension", "nearest", "search", "retrieval", "rrf", "hybrid"},
	5:  {"latency", "throughput", "performance", "p99", "p95", "sla", "slo", "timeout", "bottleneck", "memory", "cpu", "cache", "gc", "benchmark", "optimize"},
	6:  {"decision", "decided", "adr", "rfc", "architecture", "design", "proposal", "consensus", "tradeoff", "approved", "rationale"},
	7:  {"supersede", "supersedes", "superseded", "deprecated", "obsolete", "replace", "replaced", "migration", "legacy", "v1", "v2"},
	8:  {"api", "http", "rest", "grpc", "endpoint", "handler", "request", "response", "middleware", "server", "client", "gateway", "webhook"},
	9:  {"code", "ast", "function", "method", "struct", "interface", "package", "symbol", "compile", "golang", "go", "parser", "import", "call"},
	10: {"deploy", "deployment", "kubernetes", "k8s", "docker", "container", "cluster", "helm", "ci", "cd", "pipeline", "release", "production"},
	11: {"test", "testing", "tdd", "unit", "integration", "mock", "assert", "verify", "coverage", "fuzz", "race", "validation"},
	12: {"llm", "ollama", "openai", "prompt", "enrich", "enrichment", "extract", "extraction", "semantic", "ontology", "triple", "summary"},
	13: {"ticket", "jira", "issue", "pr", "pull", "commit", "branch", "merge", "review", "bug", "feature", "backlog"},
	14: {"person", "author", "owner", "team", "engineer", "lead", "expert", "collaborate", "mention", "channel", "slack", "discussion"},
	15: {"concurrent", "concurrency", "mutex", "rwmutex", "lock", "atomic", "goroutine", "worker", "queue", "channel", "async", "thread"},
	16: {"retry", "backoff", "circuit", "breaker", "rate", "limit", "throttle", "overload", "exhaustion", "pool", "connection"},
	17: {"encrypt", "encryption", "tls", "mtls", "certificate", "key", "rotate", "rotation", "audit", "compliance", "vulnerability", "injection"},
	18: {"log", "logging", "metric", "metrics", "trace", "tracing", "telemetry", "prometheus", "grafana", "alert", "monitor", "observability"},
	19: {"chunk", "document", "markdown", "heading", "section", "passage", "context", "contextual", "bm25", "lexical", "token"},
	20: {"billing", "payment", "stripe", "invoice", "subscription", "customer", "tier", "quota", "cost", "pricing"},
	21: {"frontend", "ui", "react", "browser", "dom", "css", "html", "component", "dashboard", "visualization", "render"},
	22: {"stream", "kafka", "pubsub", "event", "broker", "consumer", "producer", "topic", "offset", "webhook", "ingest", "ingestion"},
	23: {"config", "configuration", "flag", "environment", "env", "parameter", "setting", "default", "override"},
	24: {"error", "exception", "panic", "recover", "fault", "corrupt", "invalid", "malformed", "reject", "fallback"},
	25: {"scale", "scaling", "shard", "sharding", "partition", "replica", "replication", "distributed", "raft", "consensus"},
	26: {"file", "filesystem", "disk", "io", "snapshot", "backup", "restore", "archive", "blob", "s3"},
	27: {"schedule", "cron", "job", "batch", "periodic", "timer", "interval", "background", "daemon"},
	28: {"model", "inference", "gpu", "tensor", "quantize", "weights", "neural", "transformer", "attention"},
	29: {"policy", "governance", "retention", "gdpr", "pii", "redact", "sanitize", "filter", "allowlist", "denylist"},
	30: {"dependency", "depends", "upstream", "downstream", "library", "module", "sdk", "version", "upgrade"},
	31: {"index", "indexing", "compaction", "tombstone", "hydrate", "hydration", "upsert", "delete", "crud"},
}

var conceptLookup map[string][]int

func init() {
	conceptLookup = make(map[string][]int, 512)
	for cid, words := range enterpriseConcepts {
		for _, w := range words {
			stem := simpleStem(w)
			conceptLookup[w] = appendUniqueInt(conceptLookup[w], cid)
			if stem != w {
				conceptLookup[stem] = appendUniqueInt(conceptLookup[stem], cid)
			}
		}
	}
}

func appendUniqueInt(slice []int, val int) []int {
	for _, existing := range slice {
		if existing == val {
			return slice
		}
	}
	return append(slice, val)
}

// LocalEmbedder computes deterministic dense vectors in pure Go using
// Enterprise Concept Projections + Signed Feature Hashing (Weinberger et al.)
// over word unigrams, stems, bigrams, and character n-grams.
type LocalEmbedder struct {
	dim int
}

// NewLocalEmbedder creates a deterministic pure-Go embedder with the given dimension (default 384).
func NewLocalEmbedder(dim int) *LocalEmbedder {
	if dim <= 0 {
		dim = defaultEmbedDim
	}
	if dim < minEmbedDim {
		dim = minEmbedDim
	}
	return &LocalEmbedder{dim: dim}
}

func (e *LocalEmbedder) Dimension() int {
	return e.dim
}

func (e *LocalEmbedder) ModelID() string {
	return fmt.Sprintf("local-concept-hash-%dd", e.dim)
}

func (e *LocalEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	vec := make([]float32, e.dim)
	tokens := tokenizeForEmbedding(text)
	if len(tokens) == 0 {
		vec[0] = 1.0
		return vec, nil
	}

	hashSpace := e.dim - conceptDimCount
	addHashedFeature := func(feat string, weight float32) {
		h := fnv64a(feat)
		idx := conceptDimCount + int(h%uint64(hashSpace))
		sign := float32(1.0)
		if (h>>63)&1 == 1 {
			sign = -1.0
		}
		vec[idx] += sign * weight
	}

	for i, tok := range tokens {
		if isStopword(tok) {
			continue
		}
		stem := simpleStem(tok)

		// 1. Project onto enterprise concept clusters
		if cids, ok := conceptLookup[tok]; ok {
			for _, cid := range cids {
				vec[cid] += 2.2
			}
		} else if cids, ok := conceptLookup[stem]; ok {
			for _, cid := range cids {
				vec[cid] += 1.8
			}
		}

		// 2. Hash unigram + stem
		addHashedFeature("u:"+tok, 1.5)
		if stem != tok && len(stem) >= 3 {
			addHashedFeature("s:"+stem, 1.2)
		}

		// 3. Hash word bigram
		if i+1 < len(tokens) && !isStopword(tokens[i+1]) {
			addHashedFeature("b:"+tok+"_"+tokens[i+1], 1.1)
		}

		// 4. Hash character 4-grams for subword/identifier resilience
		if len(tok) >= 4 {
			for j := 0; j+4 <= len(tok); j++ {
				addHashedFeature("c:"+tok[j:j+4], 0.35)
			}
		}
	}

	return FitAndNormalizeVector(vec, e.dim), nil
}

func (e *LocalEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		v, err := e.Embed(ctx, t)
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}

// OllamaEmbedder calls an Ollama `/api/embed` endpoint with native batching,
// Matryoshka dimension slicing/padding, and seamless fallback to LocalEmbedder.
type OllamaEmbedder struct {
	baseURL  string
	model    string
	dim      int
	client   *http.Client
	fallback types.Embedder
}

// NewOllamaEmbedder constructs an OllamaEmbedder with automatic fallback.
func NewOllamaEmbedder(baseURL, model string, dim int, timeout time.Duration, fallback types.Embedder) *OllamaEmbedder {
	if dim <= 0 {
		dim = defaultEmbedDim
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	if fallback == nil {
		fallback = NewLocalEmbedder(dim)
	}
	if model == "" {
		model = "nomic-embed-text"
	}
	return &OllamaEmbedder{
		baseURL:  strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		model:    model,
		dim:      dim,
		client:   &http.Client{Timeout: timeout},
		fallback: fallback,
	}
}

func (o *OllamaEmbedder) Dimension() int {
	return o.dim
}

func (o *OllamaEmbedder) ModelID() string {
	if o.baseURL == "" {
		return o.fallback.ModelID()
	}
	return fmt.Sprintf("ollama:%s-%dd", o.model, o.dim)
}

func (o *OllamaEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	vecs, err := o.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(vecs) == 0 {
		return o.fallback.Embed(ctx, text)
	}
	return vecs[0], nil
}

func (o *OllamaEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	if o.baseURL == "" {
		return o.fallback.EmbedBatch(ctx, texts)
	}

	reqPayload := map[string]any{
		"model": o.model,
		"input": texts,
	}
	bodyBytes, err := json.Marshal(reqPayload)
	if err != nil {
		return o.fallback.EmbedBatch(ctx, texts)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/api/embed", bytes.NewReader(bodyBytes))
	if err != nil {
		return o.fallback.EmbedBatch(ctx, texts)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.client.Do(req)
	if err != nil {
		return o.fallback.EmbedBatch(ctx, texts)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return o.fallback.EmbedBatch(ctx, texts)
	}

	var parsed struct {
		Embeddings [][]float32 `json:"embeddings"`
		Embedding  []float32   `json:"embedding"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return o.fallback.EmbedBatch(ctx, texts)
	}

	rawVecs := parsed.Embeddings
	if len(rawVecs) == 0 && len(parsed.Embedding) > 0 && len(texts) == 1 {
		rawVecs = [][]float32{parsed.Embedding}
	}
	if len(rawVecs) != len(texts) {
		return o.fallback.EmbedBatch(ctx, texts)
	}

	out := make([][]float32, len(rawVecs))
	for i, v := range rawVecs {
		if len(v) == 0 {
			return o.fallback.EmbedBatch(ctx, texts)
		}
		out[i] = FitAndNormalizeVector(v, o.dim)
	}
	return out, nil
}

// FitAndNormalizeVector slices or zero-pads vec to dim (Matryoshka truncation) and L2-normalizes it.
func FitAndNormalizeVector(vec []float32, dim int) []float32 {
	out := make([]float32, dim)
	copy(out, vec)
	var sumSq float64
	for _, v := range out {
		sumSq += float64(v) * float64(v)
	}
	if sumSq <= 1e-12 {
		out[0] = 1.0
		return out
	}
	invNorm := float32(1.0 / math.Sqrt(sumSq))
	for i := range out {
		out[i] *= invNorm
	}
	return out
}

func tokenizeForEmbedding(s string) []string {
	var tokens []string
	var buf strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			buf.WriteRune(r)
		} else if buf.Len() > 0 {
			tok := buf.String()
			buf.Reset()
			if len(tok) >= 2 {
				tokens = append(tokens, tok)
			}
		}
	}
	if buf.Len() >= 2 {
		tokens = append(tokens, buf.String())
	}
	return tokens
}

func simpleStem(w string) string {
	if len(w) <= 4 {
		return w
	}
	for _, suffix := range []string{"ation", "ition", "ments", "ment", "ness", "ings", "ing", "edly", "ed", "ies", "es", "ly", "s"} {
		if strings.HasSuffix(w, suffix) && len(w)-len(suffix) >= 3 {
			base := w[:len(w)-len(suffix)]
			if suffix == "ies" {
				return base + "y"
			}
			return base
		}
	}
	return w
}

func isStopword(w string) bool {
	switch w {
	case "the", "and", "for", "that", "this", "with", "from", "are", "was", "were", "have", "has", "had", "not", "but", "you", "all", "can", "her", "his", "one", "our", "out", "who", "will", "into", "than", "then", "them", "these", "they", "been", "its", "also":
		return true
	default:
		return false
	}
}

func fnv64a(s string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))
	return h.Sum64()
}
