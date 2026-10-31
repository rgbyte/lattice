package admin

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
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
	Scope         string   `json:"scope"` // "all" (default), "docs", or "code"
	MaxFiles      int      `json:"max_files"`
	MaxFileBytes  int64    `json:"max_file_bytes"`
	IncludeTestGo bool     `json:"include_test_go"`
	Permissions   []string `json:"permissions"`
	AsyncEnrich   bool     `json:"async_enrich"`
}

// FileCrawlSummary summarizes the extracted artifacts for a single file during a folder crawl.
type FileCrawlSummary struct {
	DocID          string `json:"doc_id"`
	Title          string `json:"title"`
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
	Scope            string             `json:"scope"`
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

var docCrawlExts = map[string]string{
	".pdf":      "pdf",
	".md":       "markdown",
	".mdx":      "markdown",
	".markdown": "markdown",
	".txt":      "text",
	".rst":      "text",
	".adoc":     "text",
	".html":     "html",
	".htm":      "html",
	".docx":     "docx",
	".csv":      "text",
	".yaml":     "yaml",
	".yml":      "yaml",
	".json":     "json",
}

var codeCrawlExts = map[string]string{
	".go":  "go",
	".sql": "sql",
	".sh":  "shell",
	".py":  "python",
	".ts":  "typescript",
	".tsx": "typescript",
	".js":  "javascript",
}

var (
	reFirstH1      = regexp.MustCompile(`(?m)^#\s+(.+?)\s*$`)
	reTitleMeta    = regexp.MustCompile(`(?im)^title\s*:\s*["']?([^"'\n]+)["']?\s*$`)
	reStatusMeta   = regexp.MustCompile(`(?im)^status\s*:\s*(verified|deprecated|draft|normal)\b`)
	reBacktickTerm = regexp.MustCompile("`([A-Za-z][A-Za-z0-9_.]{2,48})`")
	reHTMLTags     = regexp.MustCompile(`<[^>]+>`)
)

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
// package dependencies, and Markdown/text/HTML/DOCX structural entities, and either returns a pure in-memory
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

	scope := strings.ToLower(strings.TrimSpace(req.Scope))
	if scope != "docs" && scope != "code" {
		scope = "all"
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
		docType, isDoc := docCrawlExts[ext]
		codeType, isCode := codeCrawlExts[ext]

		switch scope {
		case "docs":
			if !isDoc {
				filesSkipped++
				return nil
			}
		case "code":
			if !isCode {
				filesSkipped++
				return nil
			}
			docType = codeType
		default:
			if isDoc {
				// keep docType
			} else if isCode {
				docType = codeType
			} else {
				filesSkipped++
				return nil
			}
		}

		fi, err := d.Info()
		maxAllowedBytes := req.MaxFileBytes
		if docType == "pdf" || docType == "docx" {
			if maxAllowedBytes < 16<<20 {
				maxAllowedBytes = 16 << 20 // 16 MiB for PDF and DOCX documents
			}
		}
		if err != nil || fi.Size() == 0 || fi.Size() > maxAllowedBytes {
			filesSkipped++
			return nil
		}

		var (
			content   string
			pdfTitle  string
			pdfAuthor string
		)
		if docType == "pdf" {
			rawBytes, err := os.ReadFile(path)
			if err != nil {
				filesSkipped++
				return nil
			}
			pdfMeta, err := extract.ExtractPDFFile(path, rawBytes)
			if err != nil || strings.TrimSpace(pdfMeta.Content) == "" {
				filesSkipped++
				return nil
			}
			content = pdfMeta.Content
			pdfTitle = pdfMeta.Title
			pdfAuthor = pdfMeta.Author
		} else if docType == "docx" {
			extracted, err := extractDocxText(path)
			if err != nil || strings.TrimSpace(extracted) == "" {
				filesSkipped++
				return nil
			}
			content = extracted
		} else {
			rawBytes, err := os.ReadFile(path)
			if err != nil {
				filesSkipped++
				return nil
			}
			probe := rawBytes
			if len(probe) > 512 {
				probe = probe[:512]
			}
			if bytes.IndexByte(probe, 0) >= 0 {
				filesSkipped++
				return nil
			}
			content = string(rawBytes)
			if docType == "html" {
				content = strings.TrimSpace(reHTMLTags.ReplaceAllString(content, " "))
			}
		}

		relPath, err := filepath.Rel(absRoot, path)
		if err != nil {
			relPath = name
		}
		relSlash := filepath.ToSlash(relPath)
		docID := "doc:" + relSlash
		now := fi.ModTime().UTC()

		title := relSlash
		if pdfTitle != "" {
			title = pdfTitle
		} else if isDoc {
			if m := reTitleMeta.FindStringSubmatch(content); len(m) > 1 {
				title = strings.TrimSpace(m[1])
			} else if m := reFirstH1.FindStringSubmatch(content); len(m) > 1 {
				title = strings.TrimSpace(m[1])
			}
		}

		status := types.StatusNormal
		if m := reStatusMeta.FindStringSubmatch(content); len(m) > 1 {
			switch strings.ToLower(m[1]) {
			case "verified":
				status = types.StatusVerified
			case "deprecated":
				status = types.StatusDeprecated
			case "draft":
				status = types.StatusDraft
			}
		}

		doc := types.Document{
			ID:          docID,
			SourceType:  "filesystem",
			DocType:     docType,
			Title:       title,
			FilePath:    relSlash,
			URL:         "file://" + filepath.ToSlash(path),
			AuthorID:    pdfAuthor,
			Content:     content,
			Permissions: perms,
			Status:      status,
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

	nodeMap := make(map[string]types.Node)
	edgeMap := make(map[string]types.Edge)
	globalSymbols := make(map[string]string)
	docByBase := make(map[string]string)

	// Also seed globalSymbols with any existing symbol nodes in the live GraphIndex so docs-only crawls
	// can still link backtick references (`IndexDocument`, `PrincipalSet`) to already-indexed code symbols!
	for _, existingNode := range s.eng.GraphIndex().AllNodes() {
		if existingNode.Kind == types.NodeSymbol {
			globalSymbols[existingNode.Title] = existingNode.ID
			if dot := strings.LastIndexByte(existingNode.Title, '.'); dot > 0 && dot+1 < len(existingNode.Title) {
				short := existingNode.Title[dot+1:]
				if _, ok := globalSymbols[short]; !ok {
					globalSymbols[short] = existingNode.ID
				}
			}
		}
	}

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
			Title:          art.doc.Title,
			RelPath:        art.relPath,
			DocType:        art.doc.DocType,
			Bytes:          art.bytes,
			ChunksCount:    len(art.chunks),
			SymbolsCount:   len(art.symbols),
			NodesExtracted: len(art.nodes),
			EdgesExtracted: len(art.edges),
		})
	}

	// Second pass: resolve cross-file Go calls, sibling file links, and Doc -> CodeSymbol backtick references
	var crossFileEdges []types.Edge
	now := time.Now().UTC()
	for _, art := range artifacts {
		for _, sym := range art.symbols {
			for _, call := range sym.Calls {
				targetSymID, ok := globalSymbols[call]
				if !ok && !strings.Contains(call, ".") {
					targetSymID, ok = globalSymbols[sym.Package+"."+call]
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

		if art.doc.DocType != "go" {
			// 1. Sibling file references
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

			// 2. Backticked Code Symbol references (`IndexDocument`, `CrawlDirectory`, `PrincipalSet`)
			for _, m := range reBacktickTerm.FindAllStringSubmatch(art.doc.Content, -1) {
				term := strings.TrimSpace(m[1])
				if targetSymID, ok := globalSymbols[term]; ok && targetSymID != "" {
					if _, inSubgraph := nodeMap[targetSymID]; !inSubgraph {
						if liveNode, existsLive := s.eng.GraphIndex().GetNode(targetSymID); existsLive {
							addNode(liveNode)
						}
					}
					se := types.Edge{
						SourceID:   art.doc.ID,
						SourceKind: types.NodeDocument,
						TargetID:   targetSymID,
						TargetKind: types.NodeSymbol,
						Relation:   types.RelReferences,
						Weight:     1.1,
						Confidence: 0.92,
						Provenance: types.ProvStructural,
						Evidence:   " mentions symbol `" + term + "`",
						CreatedAt:  now,
					}
					if err := se.NormalizeAndValidate(); err == nil {
						if _, exists := edgeMap[se.ID]; !exists {
							edgeMap[se.ID] = se
							crossFileEdges = append(crossFileEdges, se)
						}
					}
				}
			}
		}
	}

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
		Scope:            scope,
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

// extractDocxText extracts plain text paragraphs from an Office Open XML (.docx) archive using pure Go.
func extractDocxText(path string) (string, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return "", err
	}
	defer zr.Close()

	for _, f := range zr.File {
		if f.Name != "word/document.xml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", err
		}
		defer rc.Close()

		dec := xml.NewDecoder(io.LimitReader(rc, 4<<20))
		var b strings.Builder
		for {
			tok, err := dec.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				return "", err
			}
			switch t := tok.(type) {
			case xml.StartElement:
				if t.Name.Local == "p" && b.Len() > 0 {
					b.WriteByte('\n')
				}
			case xml.CharData:
				b.Write(t)
			}
		}
		return strings.TrimSpace(b.String()), nil
	}
	return "", fmt.Errorf("word/document.xml not found in docx")
}
