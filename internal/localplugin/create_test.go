package localplugin

import (
	"os"
	"os/exec"
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
		"models/models.go", "store/store.go", "services/service.go", "services/service_test.go", "controllers/controller.go",
		"docs/postman/overview.md",
	} {
		if _, err := os.Stat(filepath.Join("plugins", "audit-log", path)); err != nil {
			t.Fatalf("missing %s: %v", path, err)
		}
	}
	if _, err := os.Stat(filepath.Join("plugins", "audit-log", "go.mod")); !os.IsNotExist(err) {
		t.Fatalf("local plugin must not have go.mod")
	}
	for _, directory := range []string{"contracts", "domain"} {
		if _, err := os.Stat(filepath.Join("plugins", "audit-log", directory)); !os.IsNotExist(err) {
			t.Fatalf("the base scaffold should not create an unused %s directory", directory)
		}
	}
	for path, expected := range map[string][]string{
		"plugin.go":                 {"controllers.NewController", "services.NewService", "store.NewStore"},
		"routes.go":                 {`routes.Public.GET("/audit-log", p.controller.Status)`},
		"store/store.go":            {"func (s *Store) Status(context.Context) (string, error)"},
		"services/service.go":       {"s.store.Status(ctx)", `Plugin: "audit-log"`},
		"controllers/controller.go": {"c.service.Status(ctx.Request().Context())", "http.StatusOK"},
	} {
		raw, err := os.ReadFile(filepath.Join("plugins", "audit-log", path))
		if err != nil {
			t.Fatal(err)
		}
		for _, needle := range expected {
			if !strings.Contains(string(raw), needle) {
				t.Fatalf("%s is missing %q:\n%s", path, needle, raw)
			}
		}
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

func TestGeneratedLocalPluginCompiles(t *testing.T) {
	root := t.TempDir()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })

	goMod := "module github.com/acme/app\n\ngo 1.26.0\n\nrequire github.com/bartek5186/procyon-core v0.6.0\n"
	if err := os.WriteFile("go.mod", []byte(goMod), 0o644); err != nil {
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

	corePath, err := filepath.Abs(filepath.Join(previous, "..", "..", "..", "procyon-core"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(corePath, "go.mod")); err == nil {
		command := exec.Command("go", "mod", "edit", "-replace=github.com/bartek5186/procyon-core="+corePath)
		command.Env = append(os.Environ(), "GOWORK=off")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("replace local core: %v\n%s", err, output)
		}
	}
	for _, args := range [][]string{{"mod", "tidy"}, {"test", "./..."}} {
		command := exec.Command("go", args...)
		command.Env = append(os.Environ(), "GOWORK=off")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("go %s: %v\n%s", strings.Join(args, " "), err, output)
		}
	}
}
