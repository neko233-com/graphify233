package selfupdate

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// mirror accelerates GitHub downloads by prefixing an absolute URL, e.g.
// https://gh-proxy.com/https://github.com/owner/repo/releases/download/...
//
// Mirrors are untrusted transport only: the Ed25519 signature over
// checksums.txt is verified locally before the binary is installed, so a
// malicious mirror cannot inject code.
type mirror struct {
	name   string
	prefix string
}

// defaultMirrors are tried automatically (unless GRAPHIFY233_MIRROR pins one or
// GRAPHIFY233_NO_MIRROR disables them). The fastest reachable one is used.
var defaultMirrors = []mirror{
	{"gh-proxy", "https://gh-proxy.com/"},
	{"ghfast", "https://ghfast.top/"},
	{"ghproxy.net", "https://ghproxy.net/"},
	{"gitmirror", "https://hub.gitmirror.com/"},
	{"moeyy", "https://github.moeyy.xyz/"},
}

// source is one place a GitHub URL can be fetched from.
type source struct {
	name  string
	base  string // empty means direct
	delay time.Duration
}

func (s source) url(raw string) string {
	if s.base == "" || !strings.Contains(raw, "://") {
		return raw
	}
	return strings.TrimRight(s.base, "/") + "/" + raw
}

// isGitHubURL reports whether a mirror prefix can be applied to raw.
func isGitHubURL(raw string) bool {
	for _, host := range []string{
		"https://github.com/",
		"https://api.github.com/",
		"https://raw.githubusercontent.com/",
		"https://objects.githubusercontent.com/",
		"https://codeload.github.com/",
	} {
		if strings.HasPrefix(raw, host) {
			return true
		}
	}
	return false
}

// sources returns direct access plus the configured/built-in mirrors. An
// explicit GRAPHIFY233_MIRROR disables the built-in list so the user stays in
// control; GRAPHIFY233_NO_MIRROR disables mirrors entirely.
func sources() []source {
	out := make([]source, 0, len(defaultMirrors)+2)
	seen := map[string]bool{}
	add := func(name, base string) {
		key := strings.TrimRight(base, "/")
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, source{name: name, base: base})
	}
	add("direct", "")
	if m := strings.TrimSpace(os.Getenv("GRAPHIFY233_MIRROR")); m != "" {
		add("env", m)
		return out
	}
	if strings.TrimSpace(os.Getenv("GRAPHIFY233_NO_MIRROR")) != "" {
		return out
	}
	for _, m := range defaultMirrors {
		add(m.name, m.prefix)
	}
	return out
}

// applicable keeps only the sources that make sense for raw: mirrors are
// skipped entirely for non-GitHub URLs (local files, test servers), and for
// GitHub URLs every configured source is allowed to compete.
func applicable(raw string) []source {
	if !isGitHubURL(raw) {
		return []source{{name: "direct"}}
	}
	return sources()
}

func probeClient() *http.Client {
	return &http.Client{Timeout: 6 * time.Second}
}

// probe measures time-to-first-byte for a small GET. -1 means unreachable.
func probe(ctx context.Context, raw string) time.Duration {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return -1
	}
	req.Header.Set("User-Agent", "graphify233/"+Version)
	start := time.Now()
	resp, err := probeClient().Do(req)
	if err != nil {
		return -1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return -1
	}
	if _, err := io.CopyN(io.Discard, resp.Body, 1); err != nil && err != io.EOF {
		return -1
	}
	return time.Since(start)
}

// rankSources probes every source concurrently and orders them fastest-first.
// Unreachable sources keep -1 and sort last; direct access is not privileged, so
// a fast mirror wins automatically.
func rankSources(ctx context.Context, srcs []source, probeURL string) []source {
	if len(srcs) <= 1 {
		return srcs
	}
	pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	for i := range srcs {
		wg.Add(1)
		go func(s *source) {
			defer wg.Done()
			s.delay = probe(pctx, s.url(probeURL))
		}(&srcs[i])
	}
	wg.Wait()
	sort.SliceStable(srcs, func(i, j int) bool {
		di, dj := srcs[i].delay, srcs[j].delay
		if (di < 0) != (dj < 0) {
			return di >= 0
		}
		if di != dj {
			return di < dj
		}
		return srcs[i].name < srcs[j].name
	})
	return srcs
}

// fetchVia tries sources in order and returns the first successful body.
func fetchVia(ctx context.Context, srcs []source, raw string, limit int64) ([]byte, string, error) {
	var firstErr error
	for _, s := range srcs {
		data, err := fetch(ctx, s.url(raw), limit)
		if err == nil {
			return data, s.name, nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	if firstErr == nil {
		firstErr = fmt.Errorf("no download source for %s", raw)
	}
	return nil, "", firstErr
}
