package hooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func repo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git", "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestInstallAppendsAndKeepsExistingHook(t *testing.T) {
	root := repo(t)
	path := filepath.Join(root, ".git", "hooks", "post-commit")
	original := "#!/bin/sh\necho existing\n"
	if err := os.WriteFile(path, []byte(original), 0o755); err != nil {
		t.Fatal(err)
	}
	gotPath, appended, err := Install(root)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != path {
		t.Fatalf("path = %q, want %q", gotPath, path)
	}
	if !appended {
		t.Fatal("expected appended=true for an existing hook")
	}
	text := read(t, path)
	if !strings.Contains(text, "echo existing") || !strings.Contains(text, Marker) {
		t.Fatalf("hook content wrong:\n%s", text)
	}
	// Installing twice must refresh the block, not duplicate it.
	if _, _, err := Install(root); err != nil {
		t.Fatal(err)
	}
	if got := read(t, path); strings.Count(got, Marker) != 1 {
		t.Fatalf("marker duplicated:\n%s", got)
	}
	if _, ok, err := Status(root); err != nil || !ok {
		t.Fatalf("Status = %v, %v; want installed", ok, err)
	}
}

func TestUninstallRestoresExistingHook(t *testing.T) {
	root := repo(t)
	path := filepath.Join(root, ".git", "hooks", "post-commit")
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho existing\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Install(root); err != nil {
		t.Fatal(err)
	}
	if _, removed, err := Uninstall(root); err != nil || !removed {
		t.Fatalf("Uninstall removed=%v err=%v", removed, err)
	}
	text := read(t, path)
	if strings.Contains(text, Marker) || !strings.Contains(text, "echo existing") {
		t.Fatalf("hook not restored:\n%s", text)
	}
	if _, removed, err := Uninstall(root); err != nil || removed {
		t.Fatalf("second Uninstall removed=%v err=%v, want false", removed, err)
	}
	if _, ok, _ := Status(root); ok {
		t.Fatal("Status still reports installed")
	}
}

func TestInstallCreatesFreshHook(t *testing.T) {
	root := repo(t)
	path, appended, err := Install(root)
	if err != nil {
		t.Fatal(err)
	}
	if appended {
		t.Fatal("fresh hook must not report appended")
	}
	text := read(t, path)
	if !strings.HasPrefix(text, "#!/bin/sh") || !strings.Contains(text, "graphify233 update") {
		t.Fatalf("fresh hook content wrong:\n%s", text)
	}
	// Uninstalling a file that only holds our block removes it.
	if _, removed, err := Uninstall(root); err != nil || !removed {
		t.Fatalf("Uninstall removed=%v err=%v", removed, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("hook file should be removed, err=%v", err)
	}
}

func TestDirRequiresRepository(t *testing.T) {
	if _, err := Dir(t.TempDir()); err == nil {
		t.Fatal("expected an error outside a git repository")
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
