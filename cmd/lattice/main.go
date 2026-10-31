package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/rgbyte/lattice/pkg/admin"
	"github.com/rgbyte/lattice/pkg/client"
	"github.com/rgbyte/lattice/pkg/engine"
	"github.com/rgbyte/lattice/pkg/server"
	"github.com/rgbyte/lattice/pkg/types"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(Run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

// Run executes the lattice CLI and returns an exit code (0 on success, non-zero on error/validation failure).
// It supports operating either as a remote Thin HTTP Client (--server) or directly against a local SQLite DB (--db).
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("lattice", flag.ContinueOnError)
	fs.SetOutput(stderr)

	serverURL := fs.String("server", os.Getenv("LATTICE_SERVER_URL"), "Remote Lattice control-plane URL (e.g. http://127.0.0.1:9090)")
	adminToken := fs.String("token", os.Getenv("LATTICE_ADMIN_TOKEN"), "Bearer admin token for remote server authentication")
	dbPath := fs.String("db", "lattice.db", "Local SQLite database path (used when --server is empty or for 'serve')")
	addr := fs.String("addr", "127.0.0.1:9090", "Listen address for 'serve' command")
	userID := fs.String("user-id", "admin", "Principal user ID for ACL simulation, search, or path explanation")
	userEmail := fs.String("email", "admin@company.local", "Principal email for ACL simulation, search, or path explanation")
	groupsCSV := fs.String("groups", "", "Comma-separated group memberships for principal evaluation")
	hops := fs.Int("hops", 2, "Maximum traversal hops for inspect-node or explain")
	limit := fs.Int("limit", 10, "Maximum results or enrichment batch limit")
	dryRun := fs.Bool("dry-run", false, "Dry-run mode for folder crawl (extracts & previews graph without writing to SQLite)")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	rest := fs.Args()
	if len(rest) == 0 {
		printUsage(stderr)
		return 2
	}

	cmd := strings.ToLower(strings.TrimSpace(rest[0]))
	subArgs := rest[1:]
	groups := parseCSV(*groupsCSV)

	if cmd == "serve" {
		return runServe(ctx, *dbPath, *addr, *adminToken, stdout, stderr)
	}

	// If --server is provided, execute via the Thin HTTP Client SDK (pkg/client)
	if strings.TrimSpace(*serverURL) != "" {
		cli := client.New(*serverURL, client.WithAdminToken(*adminToken))
		return runViaThinClient(ctx, cli, cmd, subArgs, *userID, *userEmail, groups, *hops, *limit, *dryRun, stdout, stderr)
	}

	// Otherwise, open local Engine + Admin Service directly against --db
	eng, err := engine.NewDefaultLocal(ctx, *dbPath, 384)
	if err != nil {
		fmt.Fprintf(stderr, "error opening local engine (%s): %v\n", *dbPath, err)
		return 1
	}
	defer eng.Close()

	adm, err := admin.NewService(eng)
	if err != nil {
		fmt.Fprintf(stderr, "error creating admin service: %v\n", err)
		return 1
	}

	// Mount an in-process Server and route through pkg/client for 100% behavioral parity
	srv, err := server.New(eng, adm, server.Config{AdminToken: *adminToken})
	if err != nil {
		fmt.Fprintf(stderr, "error creating control-plane handler: %v\n", err)
		return 1
	}
	localTransport := &inMemoryRoundTripper{handler: srv}
	cli := client.New("http://lattice.local",
		client.WithAdminToken(*adminToken),
		client.WithHTTPClient(&http.Client{Transport: localTransport, Timeout: 30 * time.Second}),
	)
	return runViaThinClient(ctx, cli, cmd, subArgs, *userID, *userEmail, groups, *hops, *limit, *dryRun, stdout, stderr)
}

func runViaThinClient(
	ctx context.Context,
	cli *client.Client,
	cmd string,
	subArgs []string,
	userID, userEmail string,
	groups []string,
	hops, limit int,
	globalDryRun bool,
	stdout, stderr io.Writer,
) int {
	switch cmd {
	case "overview":
		rep, err := cli.Overview(ctx)
		if err != nil {
			fmt.Fprintf(stderr, "overview error: %v\n", err)
			return 1
		}
		writePrettyJSON(stdout, rep)
		return 0

	case "graph":
		snap, err := cli.ExportGraph(ctx, limit*30, userID, userEmail, groups)
		if err != nil {
			fmt.Fprintf(stderr, "graph error: %v\n", err)
			return 1
		}
		writePrettyJSON(stdout, snap)
		return 0

	case "crawl":
		return execCrawlSubcommand(ctx, cli, subArgs, globalDryRun, stdout, stderr)

	case "validate":
		rep, err := cli.ValidateIntegrity(ctx)
		if err != nil {
			fmt.Fprintf(stderr, "validate error: %v\n", err)
			return 1
		}
		writePrettyJSON(stdout, rep)
		if !rep.Valid {
			return 1
		}
		return 0

	case "inspect-doc":
		if len(subArgs) < 1 {
			fmt.Fprintln(stderr, "usage: lattice inspect-doc <doc-id>")
			return 2
		}
		rep, err := cli.InspectDocument(ctx, subArgs[0])
		if err != nil {
			fmt.Fprintf(stderr, "inspect-doc error: %v\n", err)
			return 1
		}
		writePrettyJSON(stdout, rep)
		return 0

	case "inspect-node":
		if len(subArgs) < 1 {
			fmt.Fprintln(stderr, "usage: lattice inspect-node <node-id>")
			return 2
		}
		rep, err := cli.InspectNode(ctx, subArgs[0], hops, limit*5, userID, userEmail, groups)
		if err != nil {
			fmt.Fprintf(stderr, "inspect-node error: %v\n", err)
			return 1
		}
		writePrettyJSON(stdout, rep)
		return 0

	case "explain":
		if len(subArgs) < 2 {
			fmt.Fprintln(stderr, "usage: lattice explain <from-id> <to-id>")
			return 2
		}
		rep, err := cli.ExplainConnection(ctx, subArgs[0], subArgs[1], hops, userID, userEmail, groups)
		if err != nil {
			fmt.Fprintf(stderr, "explain error: %v\n", err)
			return 1
		}
		writePrettyJSON(stdout, rep)
		return 0

	case "simulate-acl":
		if len(subArgs) < 1 {
			fmt.Fprintln(stderr, "usage: lattice simulate-acl <target-id-1> [target-id-2 ...]")
			return 2
		}
		rep, err := cli.SimulateAccess(ctx, userID, userEmail, groups, subArgs)
		if err != nil {
			fmt.Fprintf(stderr, "simulate-acl error: %v\n", err)
			return 1
		}
		writePrettyJSON(stdout, rep)
		return 0

	case "search":
		if len(subArgs) < 1 {
			fmt.Fprintln(stderr, "usage: lattice search <query>")
			return 2
		}
		q := strings.Join(subArgs, " ")
		rep, err := cli.Search(ctx, q, userID, userEmail, groups, limit)
		if err != nil {
			fmt.Fprintf(stderr, "search error: %v\n", err)
			return 1
		}
		writePrettyJSON(stdout, rep)
		return 0

	case "admin":
		if len(subArgs) < 1 {
			fmt.Fprintln(stderr, "usage: lattice admin <crawl|enrich-sweep|rebuild|delete-doc|index-doc>")
			return 2
		}
		action := strings.ToLower(strings.TrimSpace(subArgs[0]))
		switch action {
		case "crawl":
			return execCrawlSubcommand(ctx, cli, subArgs[1:], globalDryRun, stdout, stderr)
		case "enrich-sweep":
			rep, err := cli.TriggerEnrichmentSweep(ctx, limit)
			if err != nil {
				fmt.Fprintf(stderr, "admin enrich-sweep error: %v\n", err)
				return 1
			}
			writePrettyJSON(stdout, rep)
			return 0
		case "rebuild":
			rep, err := cli.RebuildIndices(ctx)
			if err != nil {
				fmt.Fprintf(stderr, "admin rebuild error: %v\n", err)
				return 1
			}
			writePrettyJSON(stdout, rep)
			return 0
		case "delete-doc":
			if len(subArgs) < 2 {
				fmt.Fprintln(stderr, "usage: lattice admin delete-doc <doc-id>")
				return 2
			}
			if err := cli.DeleteDocument(ctx, subArgs[1]); err != nil {
				fmt.Fprintf(stderr, "admin delete-doc error: %v\n", err)
				return 1
			}
			writePrettyJSON(stdout, map[string]any{"deleted": subArgs[1], "status": "ok"})
			return 0
		case "index-doc":
			if len(subArgs) < 4 {
				fmt.Fprintln(stderr, "usage: lattice admin index-doc <doc-id> <title> <content> [perm1,perm2]")
				return 2
			}
			perms := []string{"*"}
			if len(subArgs) >= 5 {
				if p := parseCSV(subArgs[4]); len(p) > 0 {
					perms = p
				}
			}
			receipt, err := cli.IndexDocument(ctx, types.Document{
				ID:          subArgs[1],
				SourceType:  "cli",
				DocType:     "markdown",
				Title:       subArgs[2],
				Content:     subArgs[3],
				Permissions: perms,
			}, false)
			if err != nil {
				fmt.Fprintf(stderr, "admin index-doc error: %v\n", err)
				return 1
			}
			writePrettyJSON(stdout, receipt)
			return 0
		default:
			fmt.Fprintf(stderr, "unknown admin action %q\n", action)
			return 2
		}

	default:
		printUsage(stderr)
		return 2
	}
}

func runServe(ctx context.Context, dbPath, addr, adminToken string, stdout, stderr io.Writer) int {
	eng, err := engine.NewDefaultLocal(ctx, dbPath, 384)
	if err != nil {
		fmt.Fprintf(stderr, "failed to initialize engine: %v\n", err)
		return 1
	}
	defer eng.Close()

	adm, err := admin.NewService(eng)
	if err != nil {
		fmt.Fprintf(stderr, "failed to initialize admin service: %v\n", err)
		return 1
	}

	srv, err := server.New(eng, adm, server.Config{AdminToken: adminToken})
	if err != nil {
		fmt.Fprintf(stderr, "failed to initialize http server: %v\n", err)
		return 1
	}

	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           srv,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		fmt.Fprintf(stdout, "Lattice Control Plane & Inspector listening on http://%s\n", addr)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutCtx)
		return 0
	case err := <-errCh:
		if err != nil {
			fmt.Fprintf(stderr, "server error: %v\n", err)
			return 1
		}
		return 0
	}
}

func parseCSV(raw string) []string {
	var out []string
	for _, p := range strings.Split(raw, ",") {
		t := strings.TrimSpace(p)
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

func writePrettyJSON(w io.Writer, v any) {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func execCrawlSubcommand(
	ctx context.Context,
	cli *client.Client,
	subArgs []string,
	globalDryRun bool,
	stdout, stderr io.Writer,
) int {
	var (
		rootPath      string
		dryRun        = globalDryRun
		scope         = "all"
		includeTestGo bool
		maxFiles      = 250
		perms         = []string{"*"}
	)
	for i := 0; i < len(subArgs); i++ {
		arg := strings.TrimSpace(subArgs[i])
		switch {
		case arg == "--dry-run" || arg == "-dry-run" || arg == "-n":
			dryRun = true
		case arg == "--include-tests" || arg == "-include-tests":
			includeTestGo = true
		case (arg == "--scope" || arg == "-scope") && i+1 < len(subArgs):
			i++
			scope = strings.TrimSpace(subArgs[i])
		case (arg == "--path" || arg == "-path") && i+1 < len(subArgs):
			i++
			rootPath = strings.TrimSpace(subArgs[i])
		case (arg == "--perms" || arg == "--acl") && i+1 < len(subArgs):
			i++
			if p := parseCSV(subArgs[i]); len(p) > 0 {
				perms = p
			}
		case !strings.HasPrefix(arg, "-") && rootPath == "":
			rootPath = arg
		}
	}
	if rootPath == "" {
		fmt.Fprintln(stderr, "usage: lattice crawl <folder-path> [--dry-run] [--scope all|docs|code] [--include-tests] [--acl group:eng]")
		return 2
	}
	rep, err := cli.CrawlDirectory(ctx, admin.CrawlRequest{
		RootPath:      rootPath,
		DryRun:        dryRun,
		Scope:         scope,
		MaxFiles:      maxFiles,
		IncludeTestGo: includeTestGo,
		Permissions:   perms,
	})
	if err != nil {
		fmt.Fprintf(stderr, "crawl error: %v\n", err)
		return 1
	}
	writePrettyJSON(stdout, rep)
	return 0
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, `Lattice Thin Client & Control Plane CLI
Usage:
  lattice [flags] <command> [args...]

Commands:
  serve                                  Start the REST Control Plane & Visual Graph Explorer UI
  crawl <folder-path> [--dry-run]        Generate Context Graph from a folder (dry-run preview or live index)
  graph                                  Export the full Context Graph (nodes, edges, and PageRank scores)
  overview                               Inspect graph topology, HNSW vectors, and enrichment queue
  validate                               Audit graph integrity, supersession cycles, and fail-closed ACLs
  inspect-doc <doc-id>                   Inspect document, chunks, graph node, and supersession status
  inspect-node <node-id>                 Inspect vertex metadata, neighbors, and K-hop ego subgraph
  explain <from-id> <to-id>              Explain shortest path, ACL bridge blocking, and PageRank score
  simulate-acl <id1> [id2...]            Simulate PrincipalSet access decisions against target IDs
  search <query>                         Execute 4-stage hybrid + Context Graph search
  admin crawl <folder-path> [--dry-run]  Crawl a local directory (dry-run or live commit)
  admin index-doc <id> <title> <body...> Index a document via the admin control plane
  admin enrich-sweep                     Trigger synchronous Phase-2 semantic enrichment sweep
  admin rebuild                          Re-hydrate HNSW and CSR indices from SQLite WAL storage
  admin delete-doc <doc-id>              Atomically purge document across SQLite, HNSW, and CSR`)
}

// inMemoryRoundTripper routes http.Client requests directly to an in-process http.Handler
// without opening a TCP socket when operating in local --db CLI mode.
type inMemoryRoundTripper struct {
	handler http.Handler
}

func (rt *inMemoryRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	rec := newBufferedResponseWriter()
	rt.handler.ServeHTTP(rec, req)
	return rec.Result(req), nil
}

type bufferedResponseWriter struct {
	header http.Header
	body   strings.Builder
	code   int
}

func newBufferedResponseWriter() *bufferedResponseWriter {
	return &bufferedResponseWriter{
		header: make(http.Header),
		code:   http.StatusOK,
	}
}

func (w *bufferedResponseWriter) Header() http.Header {
	return w.header
}

func (w *bufferedResponseWriter) Write(b []byte) (int, error) {
	return w.body.Write(b)
}

func (w *bufferedResponseWriter) WriteHeader(statusCode int) {
	w.code = statusCode
}

func (w *bufferedResponseWriter) Result(req *http.Request) *http.Response {
	return &http.Response{
		StatusCode: w.code,
		Header:     w.header,
		Body:       io.NopCloser(strings.NewReader(w.body.String())),
		Request:    req,
	}
}
