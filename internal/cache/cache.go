// Package cache implements the per-project incremental extraction cache.
//
// Each source file is content-addressed by its SHA-256 digest; the cached
// extraction lives at graphify-out/cache/<digest>.json. A manifest maps paths
// to their last-seen digest so unchanged files can be skipped entirely.
package cache

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/neko233-com/graphify233/internal/model"
)

// Dir returns the extraction cache directory for a project.
func Dir(root string) string { return filepath.Join(root, "graphify-out", "cache") }

// HashFile returns the SHA-256 digest of a file as lowercase hex.
func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	buf := make([]byte, 256*1024)
	if _, err := io.CopyBuffer(h, f, buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Get loads a cached extraction by digest.
func Get(root, digest string) (*model.Extraction, bool) {
	path := filepath.Join(Dir(root), digest+".json")
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	var ex model.Extraction
	br := bufio.NewReaderSize(f, 256*1024)
	if err := json.NewDecoder(br).Decode(&ex); err != nil {
		return nil, false
	}
	return &ex, true
}

// Put stores an extraction under its digest using an atomic rename.
func Put(root, digest string, ex *model.Extraction) error {
	dir := Dir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	w := bufio.NewWriterSize(tmp, 256*1024)
	if err := json.NewEncoder(w).Encode(ex); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := w.Flush(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, filepath.Join(dir, digest+".json")); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}

// FileEntry records what was seen for one path during the last run.
type FileEntry struct {
	Digest  string `json:"sha256"`
	Size    int64  `json:"size"`
	ModTime int64  `json:"mtime_unix_nano"`
	Kind    string `json:"kind"`
}

// Manifest is the per-project record of indexed files.
type Manifest struct {
	Version     int                  `json:"version"`
	Tool        string               `json:"tool"`
	GeneratedAt time.Time            `json:"generated_at"`
	Root        string               `json:"root"`
	Files       map[string]FileEntry `json:"files"`
	Nodes       int                  `json:"nodes"`
	Edges       int                  `json:"edges"`
	Communities int                  `json:"communities"`
	TotalWords  int                  `json:"total_words"`
}

// ManifestPath returns the manifest location for a project root.
func ManifestPath(root string) string { return filepath.Join(root, "graphify-out", "manifest.json") }

// LoadManifest reads the manifest, returning an empty one when absent.
func LoadManifest(root string) *Manifest {
	m := &Manifest{Version: 1, Tool: "graphify233", Files: map[string]FileEntry{}}
	f, err := os.Open(ManifestPath(root))
	if err != nil {
		return m
	}
	defer f.Close()
	if err := json.NewDecoder(bufio.NewReaderSize(f, 64*1024)).Decode(m); err != nil || m.Files == nil {
		m.Files = map[string]FileEntry{}
	}
	return m
}

// SaveManifest writes the manifest atomically.
func SaveManifest(root string, m *Manifest) error {
	dir := filepath.Join(root, "graphify-out")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	m.Version = 1
	if m.Tool == "" {
		m.Tool = "graphify233"
	}
	m.GeneratedAt = time.Now()
	tmp, err := os.CreateTemp(dir, ".manifest-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	w := bufio.NewWriterSize(tmp, 64*1024)
	err = json.NewEncoder(w).Encode(m)
	if err == nil {
		err = w.Flush()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, ManifestPath(root)); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

// Changed reports whether a file needs re-extraction compared with the manifest.
func Changed(m *Manifest, rel, digest string) bool {
	e, ok := m.Files[rel]
	return !ok || e.Digest != digest
}

// Clear removes every cached extraction for a project.
func Clear(root string) (int, error) {
	dir := Dir(root)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
			return n, fmt.Errorf("remove %s: %w", e.Name(), err)
		}
		n++
	}
	return n, nil
}
