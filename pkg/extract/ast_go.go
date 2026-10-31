package extract

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/rgbyte/lattice/pkg/types"
)

// ExtractGoFile parses Go source code using standard library go/ast and extracts
// deterministic CodeSymbols, symbol/package Nodes, and DEFINES_SYMBOL, CALLS, and DEPENDS_ON Edges.
func ExtractGoFile(docID, filePath, src string, permissions []string) ([]types.CodeSymbol, []types.Node, []types.Edge, error) {
	if strings.TrimSpace(src) == "" {
		return nil, nil, nil, fmt.Errorf("%w: empty Go source", types.ErrInvalidInput)
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filePath, src, parser.ParseComments)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("parse go file %q: %w", filePath, err)
	}

	pkgName := "main"
	if file.Name != nil && file.Name.Name != "" {
		pkgName = file.Name.Name
	}

	perms := cloneSlice(permissions)
	now := time.Now().UTC()

	var (
		symbols []types.CodeSymbol
		nodes   []types.Node
		edges   []types.Edge
		imports []string
	)

	nodeSeen := make(map[string]struct{})
	addNode := func(n types.Node) {
		if _, ok := nodeSeen[n.ID]; ok {
			return
		}
		nodeSeen[n.ID] = struct{}{}
		nodes = append(nodes, n)
	}

	edgeSeen := make(map[string]struct{})
	addEdge := func(e types.Edge) {
		e.Provenance = types.ProvDeterministicAST
		if err := e.NormalizeAndValidate(); err != nil {
			return
		}
		if _, ok := edgeSeen[e.ID]; ok {
			return
		}
		edgeSeen[e.ID] = struct{}{}
		edges = append(edges, e)
	}

	// Ensure document node exists if docID is non-empty
	if docID != "" {
		addNode(types.Node{
			ID:          docID,
			Kind:        types.NodeDocument,
			Title:       filePath,
			Properties:  map[string]string{"file_path": filePath, "package": pkgName},
			Permissions: perms,
			UpdatedAt:   now,
		})
	}

	// 1. Extract imports -> RelDependsOn edges
	for _, imp := range file.Imports {
		if imp.Path == nil {
			continue
		}
		impPath, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			impPath = strings.Trim(imp.Path.Value, `"`)
		}
		if impPath == "" {
			continue
		}
		imports = append(imports, impPath)
		targetID := "pkg:" + impPath
		addNode(types.Node{
			ID:          targetID,
			Kind:        types.NodeSymbol,
			Title:       impPath,
			Properties:  map[string]string{"kind": "package", "import_path": impPath},
			Permissions: perms,
			UpdatedAt:   now,
		})
		if docID != "" {
			addEdge(types.Edge{
				SourceID:   docID,
				SourceKind: types.NodeDocument,
				TargetID:   targetID,
				TargetKind: types.NodeSymbol,
				Relation:   types.RelDependsOn,
				Weight:     1.0,
				Confidence: 1.0,
				Evidence:   "import " + impPath,
				CreatedAt:  now,
			})
		}
	}

	// Map of symbol short name -> canonical symbol ID for resolving intra-file/package calls.
	definedSymbolIDs := make(map[string]string)
	type funcRecord struct {
		symIndex int
		symID    string
		body     *ast.BlockStmt
	}
	var funcRecords []funcRecord

	// 2. First pass over declarations: collect structs, interfaces, functions, and methods.
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			if d.Tok != token.TYPE {
				continue
			}
			for _, spec := range d.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok || ts.Name == nil {
					continue
				}
				kind := ""
				switch ts.Type.(type) {
				case *ast.StructType:
					kind = "struct"
				case *ast.InterfaceType:
					kind = "interface"
				default:
					continue
				}
				name := ts.Name.Name
				symID := fmt.Sprintf("sym:%s.%s", pkgName, name)
				docComment := extractComment(ts.Doc, d.Doc)
				startPos := fset.Position(ts.Pos())
				endPos := fset.Position(ts.End())
				sig := fmt.Sprintf("type %s %s", name, kind)

				sym := types.CodeSymbol{
					ID:         symID,
					Name:       name,
					Kind:       kind,
					Package:    pkgName,
					Signature:  sig,
					DocComment: docComment,
					FilePath:   filePath,
					StartLine:  startPos.Line,
					EndLine:    endPos.Line,
					Exported:   isExported(name),
					Imports:    imports,
				}
				symbols = append(symbols, sym)
				definedSymbolIDs[name] = symID

				addNode(types.Node{
					ID:   symID,
					Kind: types.NodeSymbol,
					Title: fmt.Sprintf("%s.%s", pkgName, name),
					Properties: map[string]string{
						"symbol_kind": kind,
						"package":     pkgName,
						"file_path":   filePath,
						"signature":   sig,
					},
					Permissions: perms,
					UpdatedAt:   now,
				})
				if docID != "" {
					addEdge(types.Edge{
						SourceID:   docID,
						SourceKind: types.NodeDocument,
						TargetID:   symID,
						TargetKind: types.NodeSymbol,
						Relation:   types.RelDefinesSymbol,
						Weight:     1.0,
						Confidence: 1.0,
						Evidence:   sig,
						CreatedAt:  now,
					})
				}
			}

		case *ast.FuncDecl:
			if d.Name == nil {
				continue
			}
			name := d.Name.Name
			kind := "func"
			recvType := ""
			if d.Recv != nil && len(d.Recv.List) > 0 {
				kind = "method"
				recvType = formatReceiverType(d.Recv.List[0].Type)
			}

			symID := fmt.Sprintf("sym:%s.%s", pkgName, name)
			// If a function/method name already exists in this file, disambiguate with receiver
			if _, exists := definedSymbolIDs[name]; exists && recvType != "" {
				symID = fmt.Sprintf("sym:%s.%s.%s", pkgName, recvType, name)
			}
			definedSymbolIDs[name] = symID
			if recvType != "" {
				definedSymbolIDs[recvType+"."+name] = symID
			}

			sig := formatFuncSignature(fset, d)
			docComment := extractComment(d.Doc, nil)
			startPos := fset.Position(d.Pos())
			endPos := fset.Position(d.End())

			sym := types.CodeSymbol{
				ID:         symID,
				Name:       name,
				Kind:       kind,
				Package:    pkgName,
				Receiver:   recvType,
				Signature:  sig,
				DocComment: docComment,
				FilePath:   filePath,
				StartLine:  startPos.Line,
				EndLine:    endPos.Line,
				Exported:   isExported(name),
				Imports:    imports,
			}
			idx := len(symbols)
			symbols = append(symbols, sym)
			funcRecords = append(funcRecords, funcRecord{
				symIndex: idx,
				symID:    symID,
				body:     d.Body,
			})

			props := map[string]string{
				"symbol_kind": kind,
				"package":     pkgName,
				"file_path":   filePath,
				"signature":   sig,
			}
			if recvType != "" {
				props["receiver"] = recvType
			}
			addNode(types.Node{
				ID:          symID,
				Kind:        types.NodeSymbol,
				Title:       fmt.Sprintf("%s.%s", pkgName, name),
				Properties:  props,
				Permissions: perms,
				UpdatedAt:   now,
			})
			if docID != "" {
				addEdge(types.Edge{
					SourceID:   docID,
					SourceKind: types.NodeDocument,
					TargetID:   symID,
					TargetKind: types.NodeSymbol,
					Relation:   types.RelDefinesSymbol,
					Weight:     1.0,
					Confidence: 1.0,
					Evidence:   sig,
					CreatedAt:  now,
				})
			}
		}
	}

	// 3. Second pass: inspect function bodies for ast.CallExpr to build call graph.
	for _, rec := range funcRecords {
		if rec.body == nil {
			continue
		}
		callSet := make(map[string]struct{})
		var calls []string

		ast.Inspect(rec.body, func(n ast.Node) bool {
			ce, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			calleeName := ""
			qualifiedCall := ""
			switch fun := ce.Fun.(type) {
			case *ast.Ident:
				calleeName = fun.Name
				qualifiedCall = fun.Name
			case *ast.SelectorExpr:
				if fun.Sel != nil {
					calleeName = fun.Sel.Name
					if xIdent, ok := fun.X.(*ast.Ident); ok {
						qualifiedCall = xIdent.Name + "." + fun.Sel.Name
					} else {
						qualifiedCall = fun.Sel.Name
					}
				}
			}
			if calleeName == "" {
				return true
			}
			if _, seen := callSet[qualifiedCall]; !seen {
				callSet[qualifiedCall] = struct{}{}
				calls = append(calls, qualifiedCall)
			}
			if targetSymID, ok := definedSymbolIDs[calleeName]; ok && targetSymID != rec.symID {
				addEdge(types.Edge{
					SourceID:   rec.symID,
					SourceKind: types.NodeSymbol,
					TargetID:   targetSymID,
					TargetKind: types.NodeSymbol,
					Relation:   types.RelCalls,
					Weight:     1.0,
					Confidence: 1.0,
					Evidence:   fmt.Sprintf("%s calls %s", rec.symID, targetSymID),
					CreatedAt:  now,
				})
			}
			return true
		})

		symbols[rec.symIndex].Calls = calls
	}

	return symbols, nodes, edges, nil
}

func formatReceiverType(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return formatReceiverType(t.X)
	case *ast.IndexExpr:
		return formatReceiverType(t.X)
	case *ast.IndexListExpr:
		return formatReceiverType(t.X)
	default:
		return ""
	}
}

func formatFuncSignature(fset *token.FileSet, fn *ast.FuncDecl) string {
	cp := *fn
	cp.Body = nil
	cp.Doc = nil
	var buf bytes.Buffer
	if err := format.Node(&buf, fset, &cp); err != nil {
		return "func " + fn.Name.Name
	}
	return strings.TrimSpace(buf.String())
}

func extractComment(groups ...*ast.CommentGroup) string {
	for _, g := range groups {
		if g != nil {
			txt := strings.TrimSpace(g.Text())
			if txt != "" {
				return txt
			}
		}
	}
	return ""
}

func isExported(name string) bool {
	for _, r := range name {
		return unicode.IsUpper(r)
	}
	return false
}

func cloneSlice(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}
