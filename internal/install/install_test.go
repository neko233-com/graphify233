package install

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteGuidancePreservesAndRefreshes(t *testing.T) {
	root := t.TempDir()
	claude := filepath.Join(root, "CLAUDE.md")
	original := "# Project\n\nKeep me.\n"
	if err := os.WriteFile(claude, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if _, err := WriteGuidance(root, &out); err != nil {
		t.Fatal(err)
	}
	text := readFile(t, claude)
	if !strings.Contains(text, "Keep me.") || !strings.Contains(text, sectionStart) || !strings.Contains(text, sectionEnd) {
		t.Fatalf("guidance not appended correctly:\n%s", text)
	}
	// A refresh must not duplicate the section.
	if _, err := WriteGuidance(root, nil); err != nil {
		t.Fatal(err)
	}
	got := readFile(t, claude)
	if strings.Count(got, sectionStart) != 1 || strings.Count(got, sectionEnd) != 1 {
		t.Fatalf("section duplicated:\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(root, ".github", "copilot-instructions.md")); err != nil {
		t.Fatalf("copilot instructions missing: %v", err)
	}
}

func TestRemoveGuidanceRestoresOriginal(t *testing.T) {
	root := t.TempDir()
	claude := filepath.Join(root, "CLAUDE.md")
	if err := os.WriteFile(claude, []byte("# Project\n\nKeep me.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteGuidance(root, nil); err != nil {
		t.Fatal(err)
	}
	if err := RemoveGuidance(root, nil); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, claude); got != "# Project\n\nKeep me.\n" {
		t.Fatalf("CLAUDE.md = %q", got)
	}
	// The copilot file only contained our section, so it must be gone.
	if _, err := os.Stat(filepath.Join(root, ".github", "copilot-instructions.md")); !os.IsNotExist(err) {
		t.Fatalf("empty copilot file should be removed, err=%v", err)
	}
}

func TestVSCodeTaskCreatesAndDeduplicates(t *testing.T) {
	root := t.TempDir()
	path, err := VSCodeTask(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Version string `json:"version"`
		Tasks   []struct {
			Label   string   `json:"label"`
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"tasks"`
	}
	raw := readFile(t, path)
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("tasks.json is not valid JSON: %v\n%s", err, raw)
	}
	if doc.Version != "2.0.0" || len(doc.Tasks) != 1 {
		t.Fatalf("unexpected tasks.json: %s", raw)
	}
	if doc.Tasks[0].Command != "graphify233" || strings.Join(doc.Tasks[0].Args, " ") != "build . --update" {
		t.Fatalf("unexpected task: %+v", doc.Tasks[0])
	}
	if _, err := VSCodeTask(root, nil); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, path); strings.Count(got, vscodeTaskLabel) != 1 {
		t.Fatalf("task duplicated:\n%s", got)
	}
}

func TestVSCodeTaskRejectsJSONC(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".vscode")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "tasks.json")
	jsonc := "{\n  // comment\n  \"version\": \"2.0.0\",\n  \"tasks\": []\n}\n"
	if err := os.WriteFile(path, []byte(jsonc), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := VSCodeTask(root, nil); err == nil || !strings.Contains(err.Error(), "strict JSON") {
		t.Fatalf("err = %v, want strict JSON rejection", err)
	}
	if got := readFile(t, path); got != jsonc {
		t.Fatalf("JSONC file was modified:\n%s", got)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
