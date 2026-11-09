// Package hooks installs a git post-commit hook that keeps the graph fresh.
package hooks

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Marker identifies the block graphify233 owns inside a hook file.
const Marker = "# graphify233-hook"

const hookBody = Marker + `
command -v graphify233 >/dev/null 2>&1 || exit 0
graphify233 update . >/dev/null 2>&1 || true
`

// Dir resolves the git hooks directory for a repository.
func Dir(root string) (string, error) {
	if git, err := exec.LookPath("git"); err == nil {
		cmd := exec.Command(git, "-C", root, "rev-parse", "--path-format=absolute", "--git-path", "hooks")
		cmd.Stderr = nil
		if out, err := cmd.Output(); err == nil {
			if p := strings.TrimSpace(string(out)); p != "" {
				if fi, statErr := os.Stat(p); statErr == nil && fi.IsDir() {
					return p, nil
				}
			}
		}
	}
	dir := filepath.Join(root, ".git", "hooks")
	if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
		return dir, nil
	}
	return "", errors.New("not a git repository (no .git/hooks directory)")
}

// Path returns the post-commit hook path.
func Path(root string) (string, error) {
	dir, err := Dir(root)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "post-commit"), nil
}

// Install adds (or refreshes) the hook. Returns the hook path and whether an
// existing hook was appended to.
func Install(root string) (string, bool, error) {
	path, err := Path(root)
	if err != nil {
		return "", false, err
	}
	existing, readErr := os.ReadFile(path)
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return "", false, readErr
	}
	if bytes.Contains(existing, []byte(Marker)) {
		// Replace our block, keep any user script above it.
		idx := bytes.Index(existing, []byte(Marker))
		head := existing[:idx]
		content := string(head) + hookBody
		if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
			return "", false, err
		}
		return path, false, nil
	}
	var content string
	if len(existing) == 0 {
		content = "#!/bin/sh\n" + hookBody
	} else {
		content = string(existing)
		if !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		content += "\n" + hookBody
	}
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		return "", false, err
	}
	return path, len(existing) > 0, nil
}

// Uninstall removes the graphify233 block from the hook, deleting the file when
// nothing else remains.
func Uninstall(root string) (string, bool, error) {
	path, err := Path(root)
	if err != nil {
		return "", false, err
	}
	existing, readErr := os.ReadFile(path)
	if readErr != nil {
		if errors.Is(readErr, os.ErrNotExist) {
			return path, false, nil
		}
		return path, false, readErr
	}
	idx := bytes.Index(existing, []byte(Marker))
	if idx < 0 {
		return path, false, nil
	}
	head := strings.TrimRight(string(existing[:idx]), "\n \t")
	// Drop a bare shebang left behind.
	if strings.TrimSpace(strings.TrimPrefix(head, "#!/bin/sh")) == "" {
		head = ""
	}
	if head == "" {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return path, false, err
		}
		return path, true, nil
	}
	if err := os.WriteFile(path, []byte(head+"\n"), 0o755); err != nil {
		return path, false, err
	}
	return path, true, nil
}

// Status reports whether the hook is installed.
func Status(root string) (string, bool, error) {
	path, err := Path(root)
	if err != nil {
		return "", false, err
	}
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		if errors.Is(readErr, os.ErrNotExist) {
			return path, false, nil
		}
		return path, false, readErr
	}
	return path, bytes.Contains(data, []byte(Marker)), nil
}
