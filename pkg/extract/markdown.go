package extract

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/rgbyte/lattice/pkg/types"
)

var (
	reHeading    = regexp.MustCompile(`^(#{1,6})\s+(.+?)\s*$`)
	reMention    = regexp.MustCompile(`(?:^|[\s(\[{,;])@([a-zA-Z][a-zA-Z0-9_-]{1,31})\b`)
	reTicketKey  = regexp.MustCompile(`\b([A-Z]{2,10}-[0-9]{1,7})\b`)
	rePRRef      = regexp.MustCompile(`(?i)\bPR\s*#([0-9]{1,7})\b`)
	reSupersedes = regexp.MustCompile(`(?i)supersedes:\s*([a-zA-Z0-9_:-]+)`)
)

// BuildContextualText constructs the enriched passage text used for both FTS5 and dense embedding.
func BuildContextualText(title, summary, headingPath, content string) string {
	var b strings.Builder
	if strings.TrimSpace(title) != "" {
		b.WriteString("Document: ")
		b.WriteString(strings.TrimSpace(title))
		b.WriteByte('\n')
	}
	if strings.TrimSpace(summary) != "" {
		b.WriteString("Context: ")
		b.WriteString(strings.TrimSpace(summary))
		b.WriteByte('\n')
	}
	if strings.TrimSpace(headingPath) != "" {
		b.WriteString("Section: ")
		b.WriteString(strings.TrimSpace(headingPath))
		b.WriteByte('\n')
	}
	b.WriteString(strings.TrimSpace(content))
	return b.String()
}

// ChunkAndExtractStructural splits a Document into heading-aware, context-prefixed Chunks
// and extracts deterministic structural Nodes and Edges (AUTHORED, MENTIONS, COLLABORATES_WITH,
// REFERENCES, SUPERSEDES) with Provenance = ProvStructural.
func ChunkAndExtractStructural(doc types.Document, maxChars int) ([]types.Chunk, []types.Node, []types.Edge) {
	if maxChars <= 0 {
		maxChars = 1200
	}

	perms := cloneSlice(doc.Permissions)
	if len(perms) == 0 {
		perms = []string{"public"}
	}
	now := doc.UpdatedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}

	chunks := chunkMarkdownByHeadings(doc, maxChars)
	nodes, edges := extractStructuralGraph(doc, perms, now)
	return chunks, nodes, edges
}

func chunkMarkdownByHeadings(doc types.Document, maxChars int) []types.Chunk {
	lines := strings.Split(strings.ReplaceAll(doc.Content, "\r\n", "\n"), "\n")

	type headingEntry struct {
		level int
		title string
	}
	var stack []headingEntry

	currentPath := func() string {
		if len(stack) == 0 {
			return doc.Title
		}
		parts := make([]string, len(stack))
		for i, h := range stack {
			parts[i] = h.title
		}
		return strings.Join(parts, " > ")
	}

	var (
		chunks   []types.Chunk
		bufLines []string
	)

	appendSegment := func(headingPath, text string) {
		text = strings.TrimSpace(text)
		if text == "" {
			return
		}
		subParts := splitTextByMaxChars(text, maxChars)
		for _, part := range subParts {
			idx := len(chunks)
			words := len(strings.Fields(part))
			tokCount := (words * 4) / 3
			if tokCount < 1 {
				tokCount = 1
			}
			chunks = append(chunks, types.Chunk{
				ID:             fmt.Sprintf("%s#c%d", doc.ID, idx),
				DocID:          doc.ID,
				ChunkIndex:     idx,
				HeadingPath:    headingPath,
				Content:        part,
				ContextualText: BuildContextualText(doc.Title, doc.ContextSummary, headingPath, part),
				TokenCount:     tokCount,
			})
		}
	}

	flush := func() {
		if len(bufLines) == 0 {
			return
		}
		raw := strings.Join(bufLines, "\n")
		bufLines = bufLines[:0]
		appendSegment(currentPath(), raw)
	}

	for _, line := range lines {
		if m := reHeading.FindStringSubmatch(line); m != nil {
			flush()
			level := len(m[1])
			title := strings.TrimSpace(m[2])
			for len(stack) > 0 && stack[len(stack)-1].level >= level {
				stack = stack[:len(stack)-1]
			}
			stack = append(stack, headingEntry{level: level, title: title})
			continue
		}
		bufLines = append(bufLines, line)
	}
	flush()

	// Ensure at least one chunk is emitted even for empty or heading-only documents
	if len(chunks) == 0 {
		fallbackText := strings.TrimSpace(doc.Content)
		if fallbackText == "" {
			fallbackText = doc.Title
		}
		if fallbackText != "" {
			appendSegment(currentPath(), fallbackText)
		}
	}

	return chunks
}

func splitTextByMaxChars(text string, maxChars int) []string {
	if len(text) <= maxChars {
		return []string{text}
	}

	var out []string
	paragraphs := strings.Split(text, "\n\n")
	var cur strings.Builder

	flushCur := func() {
		s := strings.TrimSpace(cur.String())
		if s != "" {
			out = append(out, s)
		}
		cur.Reset()
	}

	for _, p := range paragraphs {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if cur.Len() > 0 && cur.Len()+2+len(p) > maxChars {
			flushCur()
		}
		if len(p) <= maxChars {
			if cur.Len() > 0 {
				cur.WriteString("\n\n")
			}
			cur.WriteString(p)
			continue
		}
		// Paragraph itself exceeds maxChars: split by words
		words := strings.Fields(p)
		for _, w := range words {
			if cur.Len() > 0 && cur.Len()+1+len(w) > maxChars {
				flushCur()
			}
			if cur.Len() > 0 {
				cur.WriteByte(' ')
			}
			cur.WriteString(w)
		}
	}
	flushCur()
	return out
}

func extractStructuralGraph(doc types.Document, perms []string, now time.Time) ([]types.Node, []types.Edge) {
	var (
		nodes []types.Node
		edges []types.Edge
	)

	nodeSeen := make(map[string]struct{})
	addNode := func(n types.Node) {
		if n.ID == "" {
			return
		}
		if _, ok := nodeSeen[n.ID]; ok {
			return
		}
		nodeSeen[n.ID] = struct{}{}
		nodes = append(nodes, n)
	}

	edgeSeen := make(map[string]struct{})
	addEdge := func(e types.Edge) {
		e.Provenance = types.ProvStructural
		if err := e.NormalizeAndValidate(); err != nil {
			return
		}
		if _, ok := edgeSeen[e.ID]; ok {
			return
		}
		edgeSeen[e.ID] = struct{}{}
		edges = append(edges, e)
	}

	// 1. Document node
	docProps := map[string]string{
		"source_type": string(doc.SourceType),
		"doc_type":    doc.DocType,
		"status":      string(doc.Status),
	}
	if doc.URL != "" {
		docProps["url"] = doc.URL
	}
	addNode(types.Node{
		ID:          doc.ID,
		Kind:        types.NodeDocument,
		Title:       doc.Title,
		Properties:  docProps,
		Permissions: perms,
		UpdatedAt:   now,
	})

	// 2. Author node & AUTHORED edge
	authorHandle := normalizePersonHandle(doc.AuthorID)
	if authorHandle == "" && doc.AuthorEmail != "" {
		if at := strings.IndexByte(doc.AuthorEmail, '@'); at > 0 {
			authorHandle = normalizePersonHandle(doc.AuthorEmail[:at])
		}
	}
	authorNodeID := ""
	if authorHandle != "" {
		authorNodeID = "person:" + authorHandle
		authorTitle := doc.AuthorName
		if authorTitle == "" {
			authorTitle = authorHandle
		}
		addNode(types.Node{
			ID:          authorNodeID,
			Kind:        types.NodePerson,
			Title:       authorTitle,
			Properties:  map[string]string{"handle": authorHandle, "email": doc.AuthorEmail},
			Permissions: perms,
			UpdatedAt:   now,
		})
		addEdge(types.Edge{
			SourceID:   authorNodeID,
			SourceKind: types.NodePerson,
			TargetID:   doc.ID,
			TargetKind: types.NodeDocument,
			Relation:   types.RelAuthored,
			Weight:     1.0,
			Confidence: 1.0,
			Evidence:   "document author metadata",
			CreatedAt:  now,
		})
	}

	// 3. @mentions -> MENTIONS and COLLABORATES_WITH edges
	for _, m := range reMention.FindAllStringSubmatch(doc.Content, -1) {
		handle := normalizePersonHandle(m[1])
		if handle == "" {
			continue
		}
		personID := "person:" + handle
		addNode(types.Node{
			ID:          personID,
			Kind:        types.NodePerson,
			Title:       "@" + handle,
			Properties:  map[string]string{"handle": handle},
			Permissions: perms,
			UpdatedAt:   now,
		})
		addEdge(types.Edge{
			SourceID:   doc.ID,
			SourceKind: types.NodeDocument,
			TargetID:   personID,
			TargetKind: types.NodePerson,
			Relation:   types.RelMentions,
			Weight:     1.0,
			Confidence: 1.0,
			Evidence:   "@" + handle,
			CreatedAt:  now,
		})
		if authorNodeID != "" && authorNodeID != personID {
			addEdge(types.Edge{
				SourceID:   authorNodeID,
				SourceKind: types.NodePerson,
				TargetID:   personID,
				TargetKind: types.NodePerson,
				Relation:   types.RelCollaboratesWith,
				Weight:     1.0,
				Confidence: 0.9,
				Evidence:   fmt.Sprintf("%s mentioned @%s in %s", authorNodeID, handle, doc.ID),
				CreatedAt:  now,
			})
		}
	}

	// 4. Ticket & PR references -> REFERENCES edges
	for _, m := range reTicketKey.FindAllStringSubmatch(doc.Content, -1) {
		ref := m[1]
		ticketID := "ticket:" + ref
		addNode(types.Node{
			ID:          ticketID,
			Kind:        types.NodeTicket,
			Title:       ref,
			Properties:  map[string]string{"key": ref},
			Permissions: perms,
			UpdatedAt:   now,
		})
		addEdge(types.Edge{
			SourceID:   doc.ID,
			SourceKind: types.NodeDocument,
			TargetID:   ticketID,
			TargetKind: types.NodeTicket,
			Relation:   types.RelReferences,
			Weight:     1.0,
			Confidence: 1.0,
			Evidence:   ref,
			CreatedAt:  now,
		})
	}

	for _, m := range rePRRef.FindAllStringSubmatch(doc.Content, -1) {
		prNum := m[1]
		ref := "PR#" + prNum
		ticketID := "ticket:" + ref
		addNode(types.Node{
			ID:          ticketID,
			Kind:        types.NodeTicket,
			Title:       "PR #" + prNum,
			Properties:  map[string]string{"pr": prNum},
			Permissions: perms,
			UpdatedAt:   now,
		})
		addEdge(types.Edge{
			SourceID:   doc.ID,
			SourceKind: types.NodeDocument,
			TargetID:   ticketID,
			TargetKind: types.NodeTicket,
			Relation:   types.RelReferences,
			Weight:     1.0,
			Confidence: 1.0,
			Evidence:   "PR #" + prNum,
			CreatedAt:  now,
		})
	}

	// 5. Explicit Supersedes: -> SUPERSEDES edges
	for _, m := range reSupersedes.FindAllStringSubmatch(doc.Content, -1) {
		targetDocID := strings.TrimSpace(m[1])
		if targetDocID == "" || targetDocID == doc.ID {
			continue
		}
		addNode(types.Node{
			ID:          targetDocID,
			Kind:        types.NodeDocument,
			Title:       targetDocID,
			Permissions: perms,
			UpdatedAt:   now,
		})
		addEdge(types.Edge{
			SourceID:   doc.ID,
			SourceKind: types.NodeDocument,
			TargetID:   targetDocID,
			TargetKind: types.NodeDocument,
			Relation:   types.RelSupersedes,
			Weight:     1.5,
			Confidence: 1.0,
			Evidence:   m[0],
			CreatedAt:  now,
		})
	}

	return nodes, edges
}

func normalizePersonHandle(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	s = strings.TrimPrefix(s, "person:")
	s = strings.TrimPrefix(s, "user:")
	s = strings.TrimPrefix(s, "@")
	return strings.TrimSpace(s)
}
