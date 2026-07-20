package plugincreate

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateMinimalPlugin(t *testing.T) {
	root := filepath.Join(t.TempDir(), "audit-log")
	result, err := Create(Options{
		Name: "audit-log", GoModule: "github.com/acme/app/plugins/audit-log",
		OutputDir: root, CoreVersion: "v0.2.0", CLIVersion: "0.2.0", Minimal: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Root != root {
		t.Fatalf("Root = %q, want %q", result.Root, root)
	}
	for _, name := range []string{"go.mod", "plugin.go", "events.go", "procyon-module.json", "README.md", "docs/postman/overview.md"} {
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
	if !strings.Contains(string(plugin), "dependencies.Events") || !strings.Contains(string(plugin), "RegisterEvents") {
		t.Fatalf("plugin does not wire the shared event bus:\n%s", plugin)
	}
}

func TestCreateCompletePluginByDefault(t *testing.T) {
	root := filepath.Join(t.TempDir(), "catalog")
	_, err := Create(Options{
		Name: "catalog", GoModule: "github.com/acme/catalog",
		OutputDir: root, CoreVersion: "v0.6.0", CLIVersion: "0.7.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"config.go", "migrations.go", "models/models.go", "store/store.go",
		"services/service.go", "services/service_test.go", "contracts/events.go",
		"controllers/controller.go",
	} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
	}
	plugin, err := os.ReadFile(filepath.Join(root, "plugin.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"controllers.NewController", "store.NewStore", "services.NewService",
		`routes.Public.GET("/catalog"`, `Group("/catalog/records")`, `routes.Admin.GET("/catalog/stats"`,
		"coreplugins.MigrationProvider",
	} {
		if !strings.Contains(string(plugin), expected) {
			t.Fatalf("plugin source is missing %q:\n%s", expected, plugin)
		}
	}
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		base := strings.ToLower(entry.Name())
		if strings.Contains(base, "hello") || strings.Contains(base, "example") {
			t.Fatalf("generated path uses placeholder feature name: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	goMod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(goMod), "github.com/labstack/echo/v4") || !strings.Contains(string(goMod), "gorm.io/gorm") {
		t.Fatalf("complete module dependencies are missing:\n%s", goMod)
	}
}

func TestCreateOnlySelectedControllers(t *testing.T) {
	root := filepath.Join(t.TempDir(), "reports")
	_, err := Create(Options{
		Name: "reports", GoModule: "github.com/acme/reports", OutputDir: root,
		CoreVersion: "v0.6.0", CLIVersion: "0.7.0", Controllers: []Controller{ControllerStatus},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "controllers", "controller.go")); err != nil {
		t.Fatal(err)
	}
	plugin, err := os.ReadFile(filepath.Join(root, "plugin.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(plugin), `routes.Public.GET("/reports"`) ||
		strings.Contains(string(plugin), "/records") || strings.Contains(string(plugin), "/stats") {
		t.Fatalf("unexpected route was wired:\n%s", plugin)
	}
}

func TestCreateServiceOnlyBoilerplateDoesNotImportControllers(t *testing.T) {
	root := filepath.Join(t.TempDir(), "worker")
	_, err := Create(Options{
		Name: "worker", GoModule: "github.com/acme/worker", OutputDir: root,
		CoreVersion: "v0.6.0", CLIVersion: "0.7.0", Controllers: []Controller{},
	})
	if err != nil {
		t.Fatal(err)
	}
	plugin, err := os.ReadFile(filepath.Join(root, "plugin.go"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(plugin), "/controllers") {
		t.Fatalf("service-only plugin imports controllers:\n%s", plugin)
	}
	if _, err := os.Stat(filepath.Join(root, "controllers")); !os.IsNotExist(err) {
		t.Fatal("service-only boilerplate should not create controllers")
	}
}

func TestGeneratedCompletePluginCompiles(t *testing.T) {
	root := filepath.Join(t.TempDir(), "catalog")
	_, err := Create(Options{
		Name: "catalog", GoModule: "github.com/acme/catalog", OutputDir: root,
		CoreVersion: "v0.6.0", CLIVersion: "0.7.0",
	})
	if err != nil {
		t.Fatal(err)
	}

	corePath, err := filepath.Abs(filepath.Join("..", "..", "..", "procyon-core"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(corePath, "go.mod")); err == nil {
		command := exec.Command("go", "mod", "edit", "-replace=github.com/bartek5186/procyon-core="+corePath)
		command.Dir = root
		command.Env = append(os.Environ(), "GOWORK=off")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("replace local core: %v\n%s", err, output)
		}
	}
	if err := Prepare(root); err != nil {
		t.Fatal(err)
	}

	command := exec.Command("go", "test", "./...")
	command.Dir = root
	command.Env = append(os.Environ(), "GOWORK=off", "GOCACHE="+filepath.Join(t.TempDir(), "go-cache"))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generated plugin does not compile: %v\n%s", err, output)
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
