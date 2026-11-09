// Package detect classifies a corpus, counts words, and filters secrets.
package detect

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/neko233-com/graphify233/internal/pdf"
)

// Thresholds copied from the reference implementation.
const (
	CorpusWarnThreshold  = 50_000
	CorpusUpperThreshold = 500_000
	FileCountUpper       = 200
)

// Kind is a corpus file category.
type Kind string

const (
	Code     Kind = "code"
	Document Kind = "document"
	Paper    Kind = "paper"
	Image    Kind = "image"
)

// Result is the detection outcome.
type Result struct {
	Files            map[Kind][]string `json:"files"`
	TotalFiles       int               `json:"total_files"`
	TotalWords       int               `json:"total_words"`
	NeedsGraph       bool              `json:"needs_graph"`
	Warning          string            `json:"warning,omitempty"`
	SkippedSensitive []string          `json:"skipped_sensitive,omitempty"`
	Root             string            `json:"root"`
}

var codeExtensions = map[string]bool{
	".py": true, ".ts": true, ".tsx": true, ".js": true, ".jsx": true,
	".go": true, ".rs": true, ".java": true, ".cpp": true, ".cc": true,
	".cxx": true, ".c": true, ".h": true, ".hpp": true, ".rb": true,
	".swift": true, ".kt": true, ".kts": true, ".cs": true, ".scala": true,
	".php": true,
}

var docExtensions = map[string]bool{".md": true, ".txt": true, ".rst": true}

var paperExtensions = map[string]bool{".pdf": true}

var imageExtensions = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".svg": true,
}

var noiseDirs = map[string]bool{
	"venv": true, ".venv": true, "env": true, ".env": true,
	"node_modules": true, "__pycache__": true, ".git": true, "dist": true,
	"build": true, "target": true, "out": true, "site-packages": true,
	"lib64": true, ".pytest_cache": true, ".mypy_cache": true,
	".ruff_cache": true, ".tox": true, ".eggs": true, "graphify-out": true,
}

var sensitivePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(^|[\\/])\.env(\.[^\\/]*)?$`),
	regexp.MustCompile(`(?i)(^|[\\/])(id_rsa|id_dsa|id_ecdsa|id_ed25519)(\.pub)?$`),
	regexp.MustCompile(`(?i)(^|[\\/])\.netrc$`),
	regexp.MustCompile(`(?i)(^|[\\/])\.aws[\\/]credentials$`),
	regexp.MustCompile(`(?i)(^|[\\/])[^\\/]*(credential|secret|password|token|private[_-]?key)[^\\/]*$`),
	regexp.MustCompile(`(?i)\.(pem|key|p12|pfx|jks|keystore)$`),
}

// IsSensitive reports whether a path looks like a secret store.
func IsSensitive(path string) bool {
	base := filepath.Base(path)
	for _, re := range sensitivePatterns {
		if re.MatchString(base) {
			return true
		}
	}
	return false
}

func isNoiseDir(name string) bool {
	if noiseDirs[name] {
		return true
	}
	return strings.HasSuffix(name, "_venv") || strings.HasSuffix(name, "_env") ||
		strings.HasSuffix(name, ".egg-info")
}

// Classify returns the corpus category for a path, or "" when unsupported.
func Classify(path string) Kind {
	ext := strings.ToLower(filepath.Ext(path))
	switch {
	case codeExtensions[ext]:
		return Code
	case docExtensions[ext]:
		return Document
	case paperExtensions[ext]:
		return Paper
	case imageExtensions[ext]:
		return Image
	}
	return ""
}

// Detect walks root and classifies every supported file.
func Detect(root string) (*Result, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	res := &Result{
		Files: map[Kind][]string{
			Code: {}, Document: {}, Paper: {}, Image: {},
		},
		Root: root,
	}

	seen := make(map[string]bool, 256)
	addFile := func(full string, inMemory bool) {
		key := strings.ToLower(full)
		if seen[key] {
			return
		}
		seen[key] = true
		if !inMemory && strings.HasPrefix(filepath.Base(full), ".") {
			return
		}
		if IsSensitive(full) {
			res.SkippedSensitive = append(res.SkippedSensitive, displayPath(root, absRoot, full))
			return
		}
		k := Classify(full)
		if k == "" {
			return
		}
		rel := displayPath(root, absRoot, full)
		res.Files[k] = append(res.Files[k], rel)
		res.TotalWords += wordCountFile(full)
	}

	info, err := os.Stat(absRoot)
	if err != nil {
		return nil, fmt.Errorf("detect: %w", err)
	}
	if !info.IsDir() {
		addFile(absRoot, false)
		res.finish()
		return res, nil
	}

	// Primary tree, pruning noise and hidden directories.
	_ = walkPruned(absRoot, func(full string) { addFile(full, false) })

	// Ingested documents always live under graphify-out/memory and are
	// re-scanned regardless of the pruning rules.
	memDir := filepath.Join(absRoot, "graphify-out", "memory")
	if fi, err := os.Stat(memDir); err == nil && fi.IsDir() {
		_ = walkAll(memDir, func(full string) { addFile(full, true) })
	}
	res.finish()
	return res, nil
}

func (r *Result) finish() {
	for _, list := range r.Files {
		r.TotalFiles += len(list)
	}
	r.NeedsGraph = r.TotalWords >= CorpusWarnThreshold
	if !r.NeedsGraph {
		r.Warning = fmt.Sprintf("Corpus is ~%s words - fits in a single context window. You may not need a graph.", humanInt(r.TotalWords))
	} else if r.TotalWords >= CorpusUpperThreshold || r.TotalFiles >= FileCountUpper {
		r.Warning = fmt.Sprintf("Large corpus: %d files · ~%s words. Extraction will be expensive; consider a subfolder or --no-obsidian.", r.TotalFiles, humanInt(r.TotalWords))
	}
	sort.Strings(r.SkippedSensitive)
}

func humanInt(n int) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	pre := len(s) % 3
	if pre > 0 {
		b.WriteString(s[:pre])
		if len(s) > pre {
			b.WriteByte(',')
		}
	}
	for i := pre; i < len(s); i += 3 {
		b.WriteString(s[i : i+3])
		if i+3 < len(s) {
			b.WriteByte(',')
		}
	}
	return b.String()
}

// displayPath reproduces the reference's path strings: the path as it would
// be written when walking the user supplied root (e.g. "./src" -> "src/x.py",
// "repo" -> "repo/src/x.py", absolute root -> absolute path). Forward slashes
// keep graph output identical across platforms.
func displayPath(root, absRoot, full string) string {
	rel, err := filepath.Rel(absRoot, full)
	if err != nil {
		return filepath.ToSlash(full)
	}
	if root == "." || root == "" {
		return filepath.ToSlash(rel)
	}
	return filepath.ToSlash(filepath.Join(root, rel))
}

func walkPruned(dir string, visit func(string)) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := e.Name()
		full := filepath.Join(dir, name)
		if e.IsDir() {
			if strings.HasPrefix(name, ".") || isNoiseDir(name) {
				continue
			}
			if e.Type()&os.ModeSymlink != 0 {
				continue
			}
			if err := walkPruned(full, visit); err != nil {
				continue
			}
			continue
		}
		if e.Type()&os.ModeSymlink != 0 {
			continue
		}
		visit(full)
	}
	return nil
}

func walkAll(dir string, visit func(string)) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		full := filepath.Join(dir, e.Name())
		if e.IsDir() {
			if err := walkAll(full, visit); err != nil {
				continue
			}
			continue
		}
		visit(full)
	}
	return nil
}

// wordCountFile streams the file and counts whitespace separated tokens.
// PDFs are parsed for text first.
func wordCountFile(path string) int {
	if strings.EqualFold(filepath.Ext(path), ".pdf") {
		text, err := pdf.ExtractText(path)
		if err != nil {
			return 0
		}
		return CountWordsString(text)
	}
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	var (
		buf   [64 * 1024]byte
		count int
		inWord bool
	)
	for {
		n, err := f.Read(buf[:])
		for _, b := range buf[:n] {
			if isSpaceByte(b) {
				inWord = false
				continue
			}
			if !inWord {
				count++
				inWord = true
			}
		}
		if err != nil {
			break
		}
	}
	return count
}

func isSpaceByte(b byte) bool {
	return b == ' ' || b == '\n' || b == '\t' || b == '\r' || b == '\v' || b == '\f'
}

// CountWords counts whitespace separated tokens in a string.
func CountWords(s string) int {
	count := 0
	inWord := false
	for i := 0; i < len(s); i++ {
		b := s[i]
		if b < utf8.RuneSelf {
			if isSpaceByte(b) {
				inWord = false
			} else if !inWord {
				count++
				inWord = true
			}
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size - 1
		if unicode.IsSpace(r) {
			inWord = false
		} else if !inWord {
			count++
			inWord = true
		}
	}
	return count
}

// CountWordsString is an alias used by the PDF text path.
func CountWordsString(s string) int { return CountWords(s) }
