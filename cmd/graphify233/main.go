// Command graphify233 builds and queries a deterministic knowledge graph of a
// directory: code, docs, papers, configs.
package main

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/neko233-com/graphify233/internal/benchmark"
	"github.com/neko233-com/graphify233/internal/cache"
	"github.com/neko233-com/graphify233/internal/detect"
	"github.com/neko233-com/graphify233/internal/export"
	"github.com/neko233-com/graphify233/internal/hooks"
	"github.com/neko233-com/graphify233/internal/install"
	"github.com/neko233-com/graphify233/internal/mcp"
	"github.com/neko233-com/graphify233/internal/model"
	"github.com/neko233-com/graphify233/internal/pipeline"
	"github.com/neko233-com/graphify233/internal/query"
	"github.com/neko233-com/graphify233/internal/selfupdate"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		usage()
		return flag.ErrHelp
	}
	cmd := args[0]
	rest := args[1:]
	switch cmd {
	case "version", "--version", "-v":
		fmt.Printf("graphify233 %s (%s %s/%s)\n", selfupdate.Version, runtime.Version(), runtime.GOOS, runtime.GOARCH)
		return nil
	case "build":
		return buildCmd(rest, false)
	case "update":
		return buildCmd(rest, true)
	case "scan", "detect":
		return scanCmd(rest)
	case "query":
		return queryCmd(rest)
	case "path":
		return pathCmd(rest)
	case "explain":
		return explainCmd(rest)
	case "mcp", "serve":
		return mcpCmd(rest)
	case "benchmark", "bench":
		return benchCmd(rest)
	case "hook":
		return hookCmd(rest)
	case "install":
		return installCmd(rest)
	case "add":
		return addCmd(rest)
	case "cache":
		return cacheCmd(rest)
	case "watch":
		return watchCmd(rest)
	case "self-update", "upgrade":
		return selfUpdateCmd(rest)
	case "verify-release":
		return verifyReleaseCmd(rest)
	case "help", "-h", "--help":
		usage()
		return nil
	}
	if fi, err := os.Stat(cmd); err == nil && fi.IsDir() {
		return buildCmd(args, false)
	}
	usage()
	return fmt.Errorf("unknown command %q", cmd)
}

func usage() {
	fmt.Print(`graphify233 — deterministic code knowledge graph

Usage:
  graphify233 [dir] [build flags]        build the graph for dir (default: .)
  graphify233 update [dir]               incremental rebuild
  graphify233 scan [dir]                 detect files and corpus size
  graphify233 query "question" [--depth 3] [--budget 2000] [--graph path]
  graphify233 path FROM TO [--graph path]
  graphify233 explain NAME [--graph path]
  graphify233 mcp [--graph path]         MCP server over stdio
  graphify233 benchmark [graph.json]     token-reduction estimate
  graphify233 watch [dir]                rebuild on file changes
  graphify233 add URL [dir]              fetch a page into graphify-out/memory
  graphify233 cache clear|info [dir]
  graphify233 hook install|uninstall|status [dir]
  graphify233 install [claude|uninstall]  editor integration
  graphify233 self-update [--check|--force]
  graphify233 version

Build flags:
  --mode standard|deep   extraction mode (default standard)
  --update               reuse cached extractions
  --workers N            parallel extractors (default: CPU count)
  --out DIR              output directory (default <dir>/graphify-out)
  --wiki --svg --graphml --cypher --canvas
  --no-json --no-report --no-html --no-obsidian
  --obsidian             force obsidian/ for very large graphs
  --neo4j URL --neo4j-user U --neo4j-pass P
  --quiet
`)
}

// splitRoot pulls the first non-flag argument out of args as the target dir.
func splitRoot(args []string, def string) (string, []string) {
	root := def
	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			rest = append(rest, args[i:]...)
			break
		}
		if !strings.HasPrefix(a, "-") && root == def && !isFlagValue(args, i) {
			root = a
			continue
		}
		rest = append(rest, a)
	}
	return root, rest
}

// isFlagValue is a conservative heuristic: a value follows a flag that ends
// with a known value-taking name.
func isFlagValue(args []string, i int) bool {
	if i == 0 {
		return false
	}
	prev := args[i-1]
	for _, f := range []string{"--mode", "--workers", "--out", "--graph", "--depth", "--budget", "--neo4j", "--neo4j-user", "--neo4j-pass", "--words", "--top"} {
		if prev == f {
			return true
		}
	}
	return false
}

// splitDir peels a leading existing-directory argument off args, so that
// `graphify233 query ./project "question"` reads ./project/graphify-out.
func splitDir(args []string) (string, []string) {
	root := ""
	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		if root == "" && !strings.HasPrefix(a, "-") && !isFlagValue(args, i) {
			if st, err := os.Stat(a); err == nil && st.IsDir() {
				root = a
				continue
			}
		}
		rest = append(rest, a)
	}
	return root, rest
}

// graphPath resolves the graph file for a command, preferring an explicit
// --graph flag and otherwise a project directory's graphify-out.
func graphPath(root, flagPath string) string {
	if flagPath != "" {
		return flagPath
	}
	if root != "" && root != "." {
		return filepath.Join(root, "graphify-out", "graph.json")
	}
	return ""
}

func buildCmd(args []string, forceUpdate bool) error {
	root, rest := splitRoot(args, ".")
	fs := flag.NewFlagSet("build", flag.ContinueOnError)
	mode := fs.String("mode", "standard", "extraction mode: standard|deep")
	update := fs.Bool("update", forceUpdate, "incremental rebuild")
	workers := fs.Int("workers", 0, "parallel extractors")
	out := fs.String("out", "", "output directory")
	wiki := fs.Bool("wiki", false, "write wiki/")
	svg := fs.Bool("svg", false, "write graph.svg")
	graphml := fs.Bool("graphml", false, "write graph.graphml")
	cypher := fs.Bool("cypher", false, "write graph.cypher")
	canvas := fs.Bool("canvas", false, "write graph.canvas")
	noJSON := fs.Bool("no-json", false, "skip graph.json")
	noReport := fs.Bool("no-report", false, "skip GRAPH_REPORT.md")
	noHTML := fs.Bool("no-html", false, "skip graph.html")
	noObsidian := fs.Bool("no-obsidian", false, "skip obsidian/")
	forceObsidian := fs.Bool("obsidian", false, "write obsidian/ even for very large graphs")
	neo4jURL := fs.String("neo4j", "", "push to Neo4j at URL")
	neo4jUser := fs.String("neo4j-user", "neo4j", "Neo4j user")
	neo4jPass := fs.String("neo4j-pass", "", "Neo4j password")
	quiet := fs.Bool("quiet", false, "suppress progress output")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		root = fs.Arg(0)
	}
	if forceUpdate {
		*update = true
	}
	formats := pipeline.DefaultFormats()
	formats.JSON = !*noJSON
	formats.Report = !*noReport
	formats.HTML = !*noHTML
	formats.Obsidian = !*noObsidian
	formats.ObsidianForced = *forceObsidian
	formats.Wiki = *wiki
	formats.SVG = *svg
	formats.GraphML = *graphml
	formats.Cypher = *cypher
	formats.Canvas = *canvas

	var logf func(string, ...any)
	if !*quiet {
		logf = func(format string, a ...any) { fmt.Printf(format+"\n", a...) }
	}
	opts := pipeline.Options{
		Root: root, OutDir: *out, Mode: *mode, Update: *update, Workers: *workers,
		Formats: formats, Logf: logf,
	}
	if *neo4jURL != "" {
		opts.Neo4j = &export.Neo4jConfig{URL: *neo4jURL, User: *neo4jUser, Password: *neo4jPass}
	}
	res, err := pipeline.Run(opts)
	if err != nil {
		return err
	}
	if !*quiet {
		printSummary(res)
	}
	return nil
}

func printSummary(res *pipeline.Result) {
	d := res.Detection
	fmt.Printf("\n%d files · %d words  ·  %d nodes · %d edges · %d communities  ·  %s\n",
		d.TotalFiles, d.TotalWords, len(res.Graph.Nodes), len(res.Graph.Edges), len(res.Communities), res.Elapsed.Round(time.Millisecond))
	fmt.Printf("extracted %d files (cached %d", res.FilesExtract, res.FilesCached)
	if res.FilesFailed > 0 {
		fmt.Printf(", failed %d", res.FilesFailed)
	}
	fmt.Printf(")  ·  outputs: %s\n", strings.Join(res.Outputs, ", "))
	if res.DetectTime > 0 {
		fmt.Printf("phase: detect %s · extract %s · cluster %s · write %s\n",
			res.DetectTime.Round(time.Millisecond), res.ExtractTime.Round(time.Millisecond),
			res.ClusterTime.Round(time.Millisecond), res.WriteTime.Round(time.Millisecond))
	}
	if len(res.Gods) > 0 {
		names := make([]string, 0, 3)
		for i, g := range res.Gods {
			if i == 3 {
				break
			}
			names = append(names, fmt.Sprintf("%s (%d)", g.Label, g.Edges))
		}
		fmt.Printf("god nodes: %s\n", strings.Join(names, ", "))
	}
	if d.TotalWords > 5000 {
		rep := benchmark.Run(res.Graph, d.TotalWords, nil)
		fmt.Printf("query cost: ~%d tokens/query  ·  %.1fx fewer than the corpus\n", rep.AvgQueryTokens, rep.ReductionRatio)
	}
	outDir := res.OutDir
	if outDir == "" {
		outDir = filepath.Join(res.Root, "graphify-out")
	}
	fmt.Printf("report: %s\n", filepath.Join(outDir, "GRAPH_REPORT.md"))
}

func scanCmd(args []string) error {
	root, rest := splitRoot(args, ".")
	fs := flag.NewFlagSet("scan", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	r, err := detect.Detect(root)
	if err != nil {
		return err
	}
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(r)
	}
	fmt.Printf("root: %s\n", r.Root)
	fmt.Printf("files: %d (code %d · docs %d · papers %d · images %d)\n", r.TotalFiles,
		len(r.Files[detect.Code]), len(r.Files[detect.Document]), len(r.Files[detect.Paper]), len(r.Files[detect.Image]))
	fmt.Printf("words: %d\n", r.TotalWords)
	if r.Warning != "" {
		fmt.Printf("warning: %s\n", r.Warning)
	}
	if len(r.SkippedSensitive) > 0 {
		fmt.Printf("skipped sensitive: %d\n", len(r.SkippedSensitive))
	}
	return nil
}

func loadGraph(path string) (*model.Graph, error) {
	if path == "" {
		path = filepath.Join("graphify-out", "graph.json")
	}
	g, err := export.LoadJSON(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no graph at %s — run `graphify233 .` first", path)
		}
		return nil, err
	}
	return g, nil
}

func graphFlag(fs *flag.FlagSet) *string {
	return fs.String("graph", "", "graph.json path (default graphify-out/graph.json)")
}

func queryCmd(args []string) error {
	root, args := splitDir(args)
	fs := flag.NewFlagSet("query", flag.ContinueOnError)
	gp := graphFlag(fs)
	depth := fs.Int("depth", 3, "BFS depth")
	budget := fs.Int("budget", 2000, "token budget")
	if err := fs.Parse(args); err != nil {
		return err
	}
	q := strings.Join(fs.Args(), " ")
	if q == "" {
		return errors.New("usage: graphify233 query \"question\"")
	}
	g, err := loadGraph(graphPath(root, *gp))
	if err != nil {
		return err
	}
	text, ids := query.New(g).Query(q, *depth, *budget)
	fmt.Println(text)
	if len(ids) > 0 {
		fmt.Fprintf(os.Stderr, "\nmatched: %s\n", strings.Join(ids, ", "))
	}
	return nil
}

func pathCmd(args []string) error {
	root, args := splitDir(args)
	fs := flag.NewFlagSet("path", flag.ContinueOnError)
	gp := graphFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 2 {
		return errors.New("usage: graphify233 path FROM TO")
	}
	g, err := loadGraph(graphPath(root, *gp))
	if err != nil {
		return err
	}
	p, err := query.New(g).Path(fs.Arg(0), fs.Arg(1))
	if err != nil {
		return err
	}
	fmt.Println(strings.Join(p, " -> "))
	return nil
}

func explainCmd(args []string) error {
	root, args := splitDir(args)
	fs := flag.NewFlagSet("explain", flag.ContinueOnError)
	gp := graphFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return errors.New("usage: graphify233 explain NAME")
	}
	g, err := loadGraph(graphPath(root, *gp))
	if err != nil {
		return err
	}
	text, err := query.New(g).Explain(fs.Arg(0))
	if err != nil {
		return err
	}
	fmt.Println(text)
	return nil
}

func mcpCmd(args []string) error {
	root, args := splitDir(args)
	fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
	gp := graphFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	g, err := loadGraph(graphPath(root, *gp))
	if err != nil {
		return err
	}
	return mcp.Serve(g, os.Stdin, os.Stdout)
}

func benchCmd(args []string) error {
	root, args := splitDir(args)
	fs := flag.NewFlagSet("benchmark", flag.ContinueOnError)
	words := fs.Int("words", 0, "corpus word count (default: from manifest)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	g, err := loadGraph(graphPath(root, ""))
	if err != nil {
		return err
	}
	w := *words
	if w == 0 {
		if root == "" {
			root = "."
		}
		m := cache.LoadManifest(root)
		w = m.TotalWords
	}
	if w == 0 {
		w = len(g.Nodes) * 40 // rough fallback
	}
	benchmark.Print(os.Stdout, benchmark.Run(g, w, nil))
	return nil
}

func hookCmd(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: graphify233 hook install|uninstall|status [dir]")
	}
	action := args[0]
	root, _ := splitRoot(args[1:], ".")
	switch action {
	case "install":
		path, appended, err := hooks.Install(root)
		if err != nil {
			return err
		}
		if appended {
			fmt.Printf("appended graphify233 block to existing hook: %s\n", path)
		} else {
			fmt.Printf("installed post-commit hook: %s\n", path)
		}
	case "uninstall":
		path, removed, err := hooks.Uninstall(root)
		if err != nil {
			return err
		}
		if removed {
			fmt.Printf("removed graphify233 block: %s\n", path)
		} else {
			fmt.Printf("hook not installed: %s\n", path)
		}
	case "status":
		path, ok, err := hooks.Status(root)
		if err != nil {
			return err
		}
		if ok {
			fmt.Printf("installed: %s\n", path)
		} else {
			fmt.Printf("not installed: %s\n", path)
		}
	default:
		return fmt.Errorf("unknown hook action %q", action)
	}
	return nil
}

func installCmd(args []string) error {
	action := "claude"
	if len(args) > 0 {
		action = args[0]
	}
	switch action {
	case "claude", "all":
		_, err := install.Claude(os.Stdout)
		return err
	case "uninstall", "remove":
		return install.RemoveClaude(os.Stdout)
	default:
		return fmt.Errorf("unknown install target %q (use claude|uninstall)", action)
	}
}

func addCmd(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: graphify233 add URL [dir]")
	}
	rawURL := args[0]
	root := "."
	if len(args) > 1 {
		root = args[1]
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "graphify233/"+selfupdate.Version)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("fetch %s: HTTP %d", rawURL, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	text := string(body)
	if strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "html") {
		text = htmlToText(text)
	}
	dir := filepath.Join(root, "graphify-out", "memory")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	name := urlSlug(rawURL)
	path := filepath.Join(dir, name+".md")
	doc := fmt.Sprintf("# %s\n\n> source: %s\n> captured: %s\n\n%s\n", name, rawURL, time.Now().Format(time.RFC3339), text)
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		return err
	}
	fmt.Printf("saved %s\n", path)
	return buildCmd([]string{root, "--update", "--quiet"}, true)
}

func htmlToText(s string) string {
	var b strings.Builder
	skip := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '<' {
			end := strings.IndexByte(s[i:], '>')
			if end < 0 {
				break
			}
			tag := strings.ToLower(s[i : i+end+1])
			if strings.HasPrefix(tag, "<script") || strings.HasPrefix(tag, "<style") {
				skip = true
			}
			if strings.HasPrefix(tag, "</script") || strings.HasPrefix(tag, "</style") {
				skip = false
			}
			if strings.HasPrefix(tag, "<p") || strings.HasPrefix(tag, "<br") || strings.HasPrefix(tag, "</h") || strings.HasPrefix(tag, "<li") {
				b.WriteByte('\n')
			}
			i += end
			continue
		}
		if !skip {
			b.WriteByte(c)
		}
	}
	return strings.Join(strings.Fields(strings.ReplaceAll(b.String(), "\n", " \n")), " ")
}

func urlSlug(u string) string {
	u = strings.TrimPrefix(u, "https://")
	u = strings.TrimPrefix(u, "http://")
	var b strings.Builder
	for _, r := range u {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	s := strings.Trim(b.String(), "_")
	if len(s) > 80 {
		s = s[:80]
	}
	if s == "" {
		s = "page"
	}
	return s
}

func cacheCmd(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: graphify233 cache clear|info [dir]")
	}
	action := args[0]
	root, _ := splitRoot(args[1:], ".")
	switch action {
	case "clear":
		n, err := cache.Clear(root)
		if err != nil {
			return err
		}
		fmt.Printf("removed %d cached extractions\n", n)
	case "info":
		m := cache.LoadManifest(root)
		entries, _ := os.ReadDir(cache.Dir(root))
		fmt.Printf("cached extractions: %d\n", len(entries))
		fmt.Printf("manifest files: %d  ·  nodes %d · edges %d · communities %d · words %d\n",
			len(m.Files), m.Nodes, m.Edges, m.Communities, m.TotalWords)
		if !m.GeneratedAt.IsZero() {
			fmt.Printf("last run: %s\n", m.GeneratedAt.Format(time.RFC3339))
		}
	default:
		return fmt.Errorf("unknown cache action %q", action)
	}
	return nil
}

func watchCmd(args []string) error {
	root, rest := splitRoot(args, ".")
	fs := flag.NewFlagSet("watch", flag.ContinueOnError)
	interval := fs.Duration("interval", 2*time.Second, "poll interval")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	fmt.Printf("watching %s (Ctrl+C to stop)\n", root)
	last := fingerprint(root)
	runOnce := func() {
		if err := buildCmd([]string{root, "--update", "--quiet"}, true); err != nil {
			fmt.Fprintf(os.Stderr, "build failed: %v\n", err)
			return
		}
		m := cache.LoadManifest(root)
		fmt.Printf("[%s] rebuilt: %d nodes · %d edges · %d communities\n", time.Now().Format("15:04:05"), m.Nodes, m.Edges, m.Communities)
	}
	runOnce()
	for {
		time.Sleep(*interval)
		fp := fingerprint(root)
		if fp != last {
			last = fp
			fmt.Println("change detected")
			runOnce()
		}
	}
}

// fingerprint hashes path+size+mtime for supported files.
func fingerprint(root string) string {
	var parts []string
	skip := map[string]bool{".git": true, "node_modules": true, "graphify-out": true, "dist": true, "build": true, "target": true, "vendor": true, "__pycache__": true}
	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != root && (skip[d.Name()] || strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return nil
		}
		parts = append(parts, fmt.Sprintf("%s:%d:%d", path, fi.Size(), fi.ModTime().UnixNano()))
		return nil
	})
	sort.Strings(parts)
	return strings.Join(parts, "|")
}

func selfUpdateCmd(args []string) error {
	fs := flag.NewFlagSet("self-update", flag.ContinueOnError)
	check := fs.Bool("check", false, "only print the latest version")
	force := fs.Bool("force", false, "reinstall even when up to date")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if *check {
		tag, err := selfupdate.Check(ctx)
		if err != nil {
			return err
		}
		fmt.Printf("current: %s\nlatest:  %s\n", selfupdate.Version, tag)
		return nil
	}
	return selfupdate.Update(ctx, os.Stdout, *force)
}

func verifyReleaseCmd(args []string) error {
	fs := flag.NewFlagSet("verify-release", flag.ContinueOnError)
	in := fs.String("in", "checksums.txt", "signed checksums file")
	sigPath := fs.String("sig", "checksums.txt.sig", "base64 Ed25519 signature")
	pubB64 := fs.String("pub", selfupdate.PublicKeyB64, "base64 public key")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *pubB64 == "" {
		return errors.New("no public key: pass --pub or use a release build")
	}
	pub, err := base64.StdEncoding.DecodeString(*pubB64)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return errors.New("invalid public key")
	}
	data, err := os.ReadFile(*in)
	if err != nil {
		return err
	}
	sigRaw, err := os.ReadFile(*sigPath)
	if err != nil {
		return err
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sigRaw)))
	if err != nil {
		return errors.New("signature is not base64")
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), data, sig) {
		return errors.New("signature verification failed")
	}
	fmt.Println("signature ok")
	return nil
}