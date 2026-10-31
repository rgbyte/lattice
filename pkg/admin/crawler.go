package admin

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/rgbyte/lattice/pkg/csr"
	"github.com/rgbyte/lattice/pkg/extract"
	"github.com/rgbyte/lattice/pkg/types"
)

// GraphSnapshot represents a visualizable snapshot of the Context Graph along with PageRank centrality scores.
type GraphSnapshot struct {
	Nodes          []types.Node       `json:"nodes"`
	Edges          []types.Edge       `json:"edges"`
	Stats          types.GraphStats   `json:"stats"`
	PageRankScores map[string]float64 `json:"pagerank_scores"`
}

// CrawlRequest configures a local directory Context Graph generation run (dry-run or live commit).
type CrawlRequest struct {
	RootPath      string   `json:"root_path"`
	DryRun        bool     `json:"dry_run"`
	MaxFiles      int      `json:"max_files"`
	MaxFileBytes  int64    `json:"max_file_bytes"`
	IncludeTestGo bool     `json:"include_test_go"`
	Permissions   []string `json:"permissions"`
	AsyncEnrich   bool     `json:"async_enrich"`
}

// FileCrawlSummary summarizes the extracted artifacts for a single file during a folder crawl.
type FileCrawlSummary struct {
	DocID          string `json:"doc_id"`
	RelPath        string `json:"rel_path"`
	DocType        string `json:"doc_type"`
	Bytes          int64  `json:"bytes"`
	ChunksCount    int    `json:"chunks_count"`
	SymbolsCount   int    `json:"symbols_count"`
	NodesExtracted int    `json:"nodes_extracted"`
	EdgesExtracted int    `json:"edges_extracted"`
}

// CrawlReport contains the complete output of a directory crawl (both dry-run preview and live index).
type CrawlReport struct {
	RootPath         string             `json:"root_path"`
	DryRun           bool               `json:"dry_run"`
	FilesScanned     int                `json:"files_scanned"`
	FilesMatched     int                `json:"files_matched"`
	FilesSkipped     int                `json:"files_skipped"`
	ChunksGenerated  int                `json:"chunks_generated"`
	SymbolsExtracted int                `json:"symbols_extracted"`
	NodesByKind      map[string]int     `json:"nodes_by_kind"`
	EdgesByRelation  map[string]int     `json:"edges_by_relation"`
	Files            []FileCrawlSummary `json:"files"`
	Subgraph         types.Subgraph     `json:"subgraph"`
	PageRankScores   map[string]float64 `json:"pagerank_scores"`
	DurationMs       float64            `json:"duration_ms"`
}

var skippedDirNames = map[string]struct{}{
	".git":         {},
	".github":      {},
	".gemini":      {},
	".idea":        {},
	".vscode":      {},
	".next":        {},
	"node_modules": {},
	"vendor":       {},
	"bin":          {},
	"dist":         {},
	"build":        {},
	"target":       {},
	"__pycache__":  {},
	"data":         {},
	"coverage":     {},
}

var supportedCrawlExts = map[string]string{
	".go":   "go",
	".md":   "markdown",
	".txt":  "text",
	".yaml": "yaml",
	".yml":  "yaml",
	".json": "json",
	".sql":  "sql",
	".sh":   "shell",
	".py":   "python",
	".ts":   "typescript",
	".tsx":  "typescript",
	".js":   "javascript",
}

// ExportGraph returns a visualizable snapshot of the live in-memory Context Graph,
// optionally filtered by a PrincipalSet, along with global PageRank authority scores.
func (s *Service) ExportGraph(
	ctx context.Context,
	maxNodes int,
	principal *types.PrincipalSet,
) (*GraphSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if maxNodes <= 0 {
		maxNodes = 300
	} else if maxNodes > 2000 {
		maxNodes = 2000
	}

	filter := s.buildNodeFilter(ctx, principal)
	allNodes := s.eng.GraphIndex().AllNodes()
	allEdges := s.eng.GraphIndex().AllEdges()

	allowedNodeMap := make(map[string]types.Node, len(allNodes))
	var nodes []types.Node
	for _, n := range allNodes {
		if filter != nil && !filter(n.ID) {
			continue
		}
		allowedNodeMap[n.ID] = n
		nodes = append(nodes, n)
	}

	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].ID < nodes[j].ID
	})
	if len(nodes) > maxNodes {
		nodes = nodes[:maxNodes]
		trimmed := make(map[string]types.Node, len(nodes))
		for _, n := range nodes {
			trimmed[n.ID] = n
		}
		allowedNodeMap = trimmed
	}

	var edges []types.Edge
	for _, e := range allEdges {
		if _, srcOK := allowedNodeMap[e.SourceID]; !srcOK {
			continue
		}
		if _, dstOK := allowedNodeMap[e.TargetID]; !dstOK {
			continue
		}
		edges = append(edges, e)
	}
	sort.Slice(edges, func(i, j int) bool {
		return edges[i].ID < edges[j].ID
	})

	seeds := make(map[string]float64, len(nodes))
	for _, n := range nodes {
		seeds[n.ID] = 1.0
	}
	ppr, _ := s.eng.GraphIndex().PersonalizedPageRank(ctx, seeds, filter, types.PPROptions{
		Alpha:         0.15,
		MaxIterations: 20,
		Tolerance:     1e-6,
	})

	return &GraphSnapshot{
		Nodes:          nodes,
		Edges:          edges,
		Stats:          s.eng.GraphIndex().Stats(),
		PageRankScores: ppr,
	}, nil
}

// CrawlDirectory recursively scans a local folder path, extracts Go AST symbols, call graphs,
// package dependencies, and Markdown/text structural entities, and either returns a pure in-memory
// dry-run preview graph (when req.DryRun is true) or indexes the folder into the live Engine.
func (s *Service) CrawlDirectory(ctx context.Context, req CrawlRequest) (*CrawlReport, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	start := time.Now()

	rawRoot := strings.TrimSpace(req.RootPath)
	if rawRoot == "" {
		return nil, fmt.Errorf("%w: root_path is required", types.ErrInvalidInput)
	}
	absRoot, err := filepath.Abs(filepath.Clean(rawRoot))
	if err != nil {
		return nil, fmt.Errorf("%w: invalid root_path %q: %v", types.ErrInvalidInput, rawRoot, err)
	}
	info, err := os.Stat(absRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: directory %q does not exist", types.ErrNotFound, absRoot)
		}
		return nil, fmt.Errorf("%w: stat %q: %v", types.ErrInvalidInput, absRoot, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%w: %q is not a directory", types.ErrInvalidInput, absRoot)
	}

	if req.MaxFiles <= 0 {
		req.MaxFiles = 250
	} else if req.MaxFiles > 2000 {
		req.MaxFiles = 2000
	}
	if req.MaxFileBytes <= 0 {
		req.MaxFileBytes = 512 * 1024 // 512 KiB
	}
	perms := req.Permissions
	if len(perms) == 0 {
		perms = []string{"*"}
	}

	type parsedFileArtifact struct {
		doc     types.Document
		relPath string
		bytes   int64
		chunks  []types.Chunk
		symbols []types.CodeSymbol
		nodes   []types.Node
		edges   []types.Edge
	}

	var (
		filesScanned int
		filesSkipped int
		artifacts    []parsedFileArtifact
	)

	walkErr := filepath.WalkDir(absRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			filesSkipped++
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if path != absRoot {
				if _, skip := skippedDirNames[name]; skip || (strings.HasPrefix(name, ".") && name != ".") {
					return filepath.SkipDir
				}
			}
			return nil
		}

		// Skip symlinks to prevent directory traversal loops
		if d.Type()&fs.ModeSymlink != 0 {
			filesSkipped++
			return nil
		}

		filesScanned++
		if len(artifacts) >= req.MaxFiles {
			filesSkipped++
			return nil
		}

		name := d.Name()
		if strings.HasPrefix(name, ".") {
			filesSkipped++
			return nil
		}
		if !req.IncludeTestGo && strings.HasSuffix(strings.ToLower(name), "_test.go") {
			filesSkipped++
			return nil
		}

		ext := strings.ToLower(filepath.Ext(name))
		docType, supported := supportedCrawlExts[ext]
		if !supported {
			filesSkipped++
			return nil
		}

		fi, err := d.Info()
		if err != nil || fi.Size() == 0 || fi.Size() > req.MaxFileBytes {
			filesSkipped++
			return nil
		}

		rawBytes, err := os.ReadFile(path)
		if err != nil {
			filesSkipped++
			return nil
		}
		// Reject binary files containing NUL bytes in the first 512 bytes
		probe := rawBytes
		if len(probe) > 512 {
			probe = probe[:512]
		}
		if bytes.IndexByte(probe, 0) >= 0 {
			filesSkipped++
			return nil
		}

		relPath, err := filepath.Rel(absRoot, path)
		if err != nil {
			relPath = name
		}
		relSlash := filepath.ToSlash(relPath)
		docID := "doc:" + relSlash
		content := string(rawBytes)
		now := fi.ModTime().UTC()

		doc := types.Document{
			ID:          docID,
			SourceType:  "filesystem",
			DocType:     docType,
			Title:       relSlash,
			FilePath:    relSlash,
			URL:         "file://" + filepath.ToSlash(path),
			Content:     content,
			Permissions: perms,
			Status:      types.StatusNormal,
			CreatedAt:   now,
			UpdatedAt:   now,
		}

		var fileNodes []types.Node
		var fileEdges []types.Edge
		var codeSyms []types.CodeSymbol

		if docType == "go" {
			syms, astNodes, astEdges, astErr := extract.ExtractGoFile(docID, relSlash, content, perms)
			if astErr == nil {
				codeSyms = syms
				fileNodes = append(fileNodes, astNodes...)
				fileEdges = append(fileEdges, astEdges...)
			}
		}

		chunks, structNodes, structEdges := extract.ChunkAndExtractStructural(doc, 1000)
		fileNodes = append(fileNodes, structNodes...)
		fileEdges = append(fileEdges, structEdges...)

		artifacts = append(artifacts, parsedFileArtifact{
			doc:     doc,
			relPath: relSlash,
			bytes:   fi.Size(),
			chunks:  chunks,
			symbols: codeSyms,
			nodes:   fileNodes,
			edges:   fileEdges,
		})
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}

	// Deduplicate nodes & edges and resolve cross-file symbol calls and document references
	nodeMap := make(map[string]types.Node)
	edgeMap := make(map[string]types.Edge)
	globalSymbols := make(map[string]string) // qualified "pkg.Name" or "Name" -> symbol Node ID
	docByBase := make(map[string]string)     // filename or relPath -> docID

	addNode := func(n types.Node) {
		if n.ID == "" {
			return
		}
		if existing, ok := nodeMap[n.ID]; ok {
			if existing.Kind == types.NodeDocument {
				return
			}
		}
		nodeMap[n.ID] = n
	}
	addEdge := func(e types.Edge) {
		if e.Provenance == "" {
			e.Provenance = types.ProvStructural
		}
		if err := e.NormalizeAndValidate(); err != nil {
			return
		}
		edgeMap[e.ID] = e
	}

	totalChunks := 0
	totalSymbols := 0
	summaries := make([]FileCrawlSummary, 0, len(artifacts))

	for _, art := range artifacts {
		totalChunks += len(art.chunks)
		totalSymbols += len(art.symbols)
		docByBase[art.relPath] = art.doc.ID
		docByBase[filepath.Base(art.relPath)] = art.doc.ID

		for _, n := range art.nodes {
			addNode(n)
		}
		for _, e := range art.edges {
			addEdge(e)
		}
		for _, sym := range art.symbols {
			globalSymbols[sym.Package+"."+sym.Name] = sym.ID
			if sym.Exported {
				if _, exists := globalSymbols[sym.Name]; !exists {
					globalSymbols[sym.Name] = sym.ID
				}
			}
		}

		summaries = append(summaries, FileCrawlSummary{
			DocID:          art.doc.ID,
			RelPath:        art.relPath,
			DocType:        art.doc.DocType,
			Bytes:          art.bytes,
			ChunksCount:    len(art.chunks),
			SymbolsCount:   len(art.symbols),
			NodesExtracted: len(art.nodes),
			EdgesExtracted: len(art.edges),
		})
	}

	// Second pass: resolve cross-file Go function calls (e.g. engine.New calling csr.NewIndex or store.NewSQLiteStore)
	var crossFileEdges []types.Edge
	now := time.Now().UTC()
	for _, art := range artifacts {
		for _, sym := range art.symbols {
			for _, call := range sym.Calls {
				targetSymID, ok := globalSymbols[call]
				if !ok {
					// Also try unqualified if in the same package
					if !strings.Contains(call, ".") {
						targetSymID, ok = globalSymbols[sym.Package+"."+call]
					}
				}
				if ok && targetSymID != "" && targetSymID != sym.ID {
					ce := types.Edge{
						SourceID:   sym.ID,
						SourceKind: types.NodeSymbol,
						TargetID:   targetSymID,
						TargetKind: types.NodeSymbol,
						Relation:   types.RelCalls,
						Weight:     1.0,
						Confidence: 0.95,
						Provenance: types.ProvDeterministicAST,
						Evidence:   fmt.Sprintf("%s calls %s", sym.ID, targetSymID),
						CreatedAt:  now,
					}
					if err := ce.NormalizeAndValidate(); err == nil {
						if _, exists := edgeMap[ce.ID]; !exists {
							edgeMap[ce.ID] = ce
							crossFileEdges = append(crossFileEdges, ce)
						}
					}
				}
			}
		}

		// Resolve Markdown/text references to sibling files in the crawled directory
		if art.doc.DocType == "markdown" || art.doc.DocType == "text" {
			for refName, targetDocID := range docByBase {
				if len(refName) < 4 || targetDocID == art.doc.ID {
					continue
				}
				if strings.Contains(art.doc.Content, refName) {
					re := types.Edge{
						SourceID:   art.doc.ID,
						SourceKind: types.NodeDocument,
						TargetID:   targetDocID,
						TargetKind: types.NodeDocument,
						Relation:   types.RelReferences,
						Weight:     1.0,
						Confidence: 0.9,
						Provenance: types.ProvStructural,
						Evidence:   "references file " + refName,
						CreatedAt:  now,
					}
					if err := re.NormalizeAndValidate(); err == nil {
						if _, exists := edgeMap[re.ID]; !exists {
							edgeMap[re.ID] = re
							crossFileEdges = append(crossFileEdges, re)
						}
					}
				}
			}
		}
	}

	// If not DryRun, commit all crawled documents and cross-file edges into the live Engine
	if !req.DryRun {
		for _, art := range artifacts {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if _, err := s.eng.IndexDocument(ctx, art.doc, req.AsyncEnrich); err != nil {
				return nil, fmt.Errorf("index %s: %w", art.relPath, err)
			}
		}
		for _, ce := range crossFileEdges {
			_ = s.UpsertCustomEdge(ctx, ce)
		}
	}

	// Build temporary CSR index to compute PageRank authority and summary statistics over the crawled subgraph
	previewCSR := csr.NewIndex()
	nodes := make([]types.Node, 0, len(nodeMap))
	nodesByKind := make(map[string]int)
	for _, n := range nodeMap {
		nodes = append(nodes, n)
		nodesByKind[string(n.Kind)]++
		_ = previewCSR.UpsertNode(n)
	}
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].ID < nodes[j].ID
	})

	edges := make([]types.Edge, 0, len(edgeMap))
	edgesByRel := make(map[string]int)
	for _, e := range edgeMap {
		edges = append(edges, e)
		edgesByRel[string(e.Relation)]++
		_ = previewCSR.UpsertEdge(e)
	}
	sort.Slice(edges, func(i, j int) bool {
		return edges[i].ID < edges[j].ID
	})

	seeds := make(map[string]float64, len(nodes))
	for _, n := range nodes {
		seeds[n.ID] = 1.0
	}
	pprScores, _ := previewCSR.PersonalizedPageRank(ctx, seeds, nil, types.PPROptions{
		Alpha:         0.15,
		MaxIterations: 20,
		Tolerance:     1e-6,
	})

	return &CrawlReport{
		RootPath:         absRoot,
		DryRun:           req.DryRun,
		FilesScanned:     filesScanned,
		FilesMatched:     len(artifacts),
		FilesSkipped:     filesSkipped,
		ChunksGenerated:  totalChunks,
		SymbolsExtracted: totalSymbols,
		NodesByKind:      nodesByKind,
		EdgesByRelation:  edgesByRel,
		Files:            summaries,
		Subgraph: types.Subgraph{
			Nodes: nodes,
			Edges: edges,
		},
		PageRankScores: pprScores,
		DurationMs:     float64(time.Since(start).Microseconds()) / 1000.0,
	}, nil
}
