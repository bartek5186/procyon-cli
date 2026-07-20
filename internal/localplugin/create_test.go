package localplugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateLocalPluginAndWireRegistration(t *testing.T) {
	root := t.TempDir()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })

	if err := os.WriteFile("go.mod", []byte("module github.com/acme/app\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	localFile := `package main

import (
	coreplugins "github.com/bartek5186/procyon-core/plugins"
	// procyon:local-plugin-imports
)

func localPluginFactories() []coreplugins.Registration {
	return []coreplugins.Registration{
		// procyon:local-plugin-registrations
	}
}
`
	if err := os.WriteFile("plugins_local.go", []byte(localFile), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Create(Options{Name: "audit-log"}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"plugin.go", "config.go", "capabilities.go", "events.go", "migrations.go", "routes.go", "start.go",
		"contracts/doc.go", "domain/doc.go", "models/doc.go", "store/doc.go", "services/doc.go", "controllers/doc.go",
		"docs/postman/overview.md",
	} {
		if _, err := os.Stat(filepath.Join("plugins", "audit-log", path)); err != nil {
			t.Fatalf("missing %s: %v", path, err)
		}
	}
	if _, err := os.Stat(filepath.Join("plugins", "audit-log", "go.mod")); !os.IsNotExist(err) {
		t.Fatalf("local plugin must not have go.mod")
	}
	wiring, err := os.ReadFile("plugins_local.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		`localpluginauditlog "github.com/acme/app/plugins/audit-log"`,
		`{Name: "audit-log", Factory: localpluginauditlog.New}`,
	} {
		if !strings.Contains(string(wiring), expected) {
			t.Fatalf("missing %q in wiring:\n%s", expected, wiring)
		}
	}
}

func TestCreateLocalPluginRejectsDuplicate(t *testing.T) {
	root := t.TempDir()
	previous, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })
	if err := os.WriteFile("go.mod", []byte("module github.com/acme/app\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("plugins_local.go", []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join("plugins", "leagues"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Create(Options{Name: "leagues"}); err == nil {
		t.Fatal("expected duplicate error")
	}
}
