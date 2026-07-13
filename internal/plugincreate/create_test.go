package plugincreate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateMinimalPlugin(t *testing.T) {
	root := filepath.Join(t.TempDir(), "audit-log")
	result, err := Create(Options{
		Name: "audit-log", GoModule: "github.com/acme/app/plugins/audit-log",
		OutputDir: root, CoreVersion: "v0.2.0", CLIVersion: "0.2.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Root != root {
		t.Fatalf("Root = %q, want %q", result.Root, root)
	}
	for _, name := range []string{"go.mod", "plugin.go", "procyon-module.json", "README.md"} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
	}
	plugin, err := os.ReadFile(filepath.Join(root, "plugin.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(plugin), "package auditlog") || !strings.Contains(string(plugin), `const Name = "audit-log"`) {
		t.Fatalf("unexpected plugin source:\n%s", plugin)
	}
}

func TestCreateRejectsExistingOutput(t *testing.T) {
	root := t.TempDir()
	_, err := Create(Options{
		Name: "audit-log", GoModule: "github.com/acme/audit-log",
		OutputDir: root, CoreVersion: "v0.2.0", CLIVersion: "0.2.0",
	})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected existing output error, got %v", err)
	}
}
