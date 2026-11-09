// Package selfupdate performs signed, checksum-verified binary updates from
// GitHub releases (optionally through a mirror).
//
// Security model: release checksums are signed with an Ed25519 key whose public
// half is embedded in the binary at build time; the downloaded archive is
// accepted only when the signature verifies and the SHA-256 matches.
package selfupdate

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// PublicKeyB64 is injected at build time:
//
//	-ldflags "-X github.com/neko233-com/graphify233/internal/selfupdate.PublicKeyB64=<base64>"
var PublicKeyB64 = ""

// Version is injected at build time the same way.
var Version = "dev"

// Repo is the GitHub repository that publishes releases.
var Repo = "neko233-com/graphify233"

// APIBase can be pointed at a mirror or a test server.
var APIBase = "https://api.github.com"

// Mirror, when set (env GRAPHIFY233_MIRROR), is prefixed to asset download
// URLs, e.g. "https://ghfast.top/" + "https://github.com/...".
var Mirror = os.Getenv("GRAPHIFY233_MIRROR")

func init() {
	// Allow pointing the release metadata lookup at an API mirror
	// (e.g. https://api.kkgithub.com) without rebuilding the binary.
	if v := strings.TrimSpace(os.Getenv("GRAPHIFY233_API")); v != "" {
		APIBase = v
	}
}

// AssetName returns the release asset name for the current platform.
func AssetName() string {
	name := fmt.Sprintf("graphify233_%s_%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}

// release is the subset of the GitHub release API we need.
type release struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
		Size int64  `json:"size"`
	} `json:"assets"`
}

func client() *http.Client {
	return &http.Client{Timeout: 60 * time.Second}
}

func assetURL(u string) string {
	if Mirror == "" {
		return u
	}
	if strings.Contains(u, "://") {
		return strings.TrimRight(Mirror, "/") + "/" + u
	}
	return u
}

// Latest fetches the latest release metadata.
func Latest(ctx context.Context) (*release, error) {
	url := fmt.Sprintf("%s/repos/%s/releases/latest", strings.TrimRight(APIBase, "/"), Repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "graphify233/"+Version)
	resp, err := client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("release lookup failed: HTTP %d", resp.StatusCode)
	}
	var r release
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&r); err != nil {
		return nil, err
	}
	if r.TagName == "" {
		return nil, errors.New("release metadata has no tag")
	}
	return &r, nil
}

// Check returns the latest published tag.
func Check(ctx context.Context) (string, error) {
	r, err := Latest(ctx)
	if err != nil {
		return "", err
	}
	return r.TagName, nil
}

func findAsset(r *release, name string) (string, bool) {
	for _, a := range r.Assets {
		if a.Name == name {
			return a.URL, true
		}
	}
	return "", false
}

// Update downloads and installs the latest release. force reinstalls when the
// running version already matches.
func Update(ctx context.Context, out io.Writer, force bool) error {
	if PublicKeyB64 == "" {
		return errors.New("this build has no embedded signing key; refusing self-update")
	}
	pub, err := base64.StdEncoding.DecodeString(PublicKeyB64)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return errors.New("invalid embedded signing key")
	}
	rel, err := Latest(ctx)
	if err != nil {
		return err
	}
	if !force && rel.TagName == Version {
		fmt.Fprintf(out, "already up to date (%s)\n", Version)
		return nil
	}
	fmt.Fprintf(out, "updating %s → %s\n", Version, rel.TagName)

	sumsURL, ok := findAsset(rel, "checksums.txt")
	if !ok {
		return errors.New("release has no checksums.txt (unsigned releases are rejected)")
	}
	sigURL, ok := findAsset(rel, "checksums.txt.sig")
	if !ok {
		return errors.New("release has no checksums.txt.sig (unsigned releases are rejected)")
	}
	binURL, ok := findAsset(rel, AssetName())
	if !ok {
		return fmt.Errorf("release has no asset %s", AssetName())
	}

	sums, err := fetch(ctx, assetURL(sumsURL), 1<<20)
	if err != nil {
		return fmt.Errorf("download checksums: %w", err)
	}
	sig, err := fetch(ctx, assetURL(sigURL), 1<<16)
	if err != nil {
		return fmt.Errorf("download signature: %w", err)
	}
	sigBytes, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sig)))
	if err != nil {
		return fmt.Errorf("signature is not base64: %w", err)
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), sums, sigBytes) {
		return errors.New("signature verification failed — refusing to install")
	}
	want, err := hashFor(sums, AssetName())
	if err != nil {
		return err
	}
	bin, err := fetch(ctx, assetURL(binURL), 512<<20)
	if err != nil {
		return fmt.Errorf("download binary: %w", err)
	}
	got := sha256.Sum256(bin)
	if !strings.EqualFold(hex.EncodeToString(got[:]), want) {
		return errors.New("checksum mismatch — refusing to install")
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return err
	}
	dir := filepath.Dir(exe)
	tmp, err := os.CreateTemp(dir, ".graphify233-update-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(bin); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o755); err != nil && runtime.GOOS != "windows" {
		return err
	}
	oldName := exe + ".old"
	_ = os.Remove(oldName)
	if err := os.Rename(exe, oldName); err != nil {
		return fmt.Errorf("cannot replace running binary: %w", err)
	}
	if err := os.Rename(tmpName, exe); err != nil {
		_ = os.Rename(oldName, exe)
		return err
	}
	_ = os.Remove(oldName)
	fmt.Fprintf(out, "installed %s at %s\n", rel.TagName, exe)
	return nil
}

func fetch(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "graphify233/"+Version)
	resp, err := client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d for %s", resp.StatusCode, url)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("response exceeds %d bytes", limit)
	}
	return data, nil
}

func hashFor(sums []byte, name string) (string, error) {
	for _, line := range strings.Split(string(sums), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		file := strings.TrimPrefix(fields[1], "*")
		if file == name {
			if len(fields[0]) != 64 {
				return "", fmt.Errorf("malformed checksum for %s", name)
			}
			return strings.ToLower(fields[0]), nil
		}
	}
	return "", fmt.Errorf("checksums.txt has no entry for %s", name)
}
