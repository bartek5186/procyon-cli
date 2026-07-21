package projectinit

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTextPromptsDefaultOutputToCurrentDirectory(t *testing.T) {
	input := bytes.NewBufferString("demo-api\n\n\n1\n")
	var output bytes.Buffer
	opts, err := completeOptions(Options{}, "/tmp/work", input, &output)
	if err != nil {
		t.Fatal(err)
	}
	if opts.OutputDir != "./demo-api" {
		t.Fatalf("OutputDir = %q, want ./demo-api", opts.OutputDir)
	}
}

func TestCleanOutputDirPreservesCurrentDirectoryPrefix(t *testing.T) {
	if got := cleanOutputDir("./demo-api"); got != "./demo-api" {
		t.Fatalf("cleanOutputDir returned %q", got)
	}
}

func TestGeneratedFilePathReplacesGenericPackageDocs(t *testing.T) {
	tests := map[string]string{
		"controllers/doc.go": "controllers/controller.go",
		"models/doc.go":      "models/models.go",
		"store/doc.go":       "store/doc.go",
		"models/invoice.go":  "models/invoice.go",
	}
	for input, expected := range tests {
		if got := filepath.ToSlash(generatedFilePath(filepath.FromSlash(input))); got != expected {
			t.Errorf("generatedFilePath(%q) = %q, want %q", input, got, expected)
		}
	}
}

func TestCopyTemplateSkipsLegacyGeneratorDirectories(t *testing.T) {
	source := t.TempDir()
	destination := t.TempDir()
	legacyFiles := map[string]string{
		"scripts/generate-feature.sh": "#!/bin/sh\n",
		"tools/postman-gen/main.go":   "package main\n",
	}
	for name, body := range legacyFiles {
		path := filepath.Join(source, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(source, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := copyTemplate(source, destination, Options{IncludeDocker: true, IncludeHello: true}); err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{"scripts", "tools"} {
		if _, err := os.Stat(filepath.Join(destination, directory)); !os.IsNotExist(err) {
			t.Fatalf("legacy %s directory should not be copied into generated projects", directory)
		}
	}
	if _, err := os.Stat(filepath.Join(destination, "main.go")); err != nil {
		t.Fatalf("regular template file was not copied: %v", err)
	}
}

func TestReplaceTextFilesKeepsCoreModuleAndRewritesTemplateModule(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "go.mod")
	source := "module " + templateModule + "\n\nrequire " + coreModule + " v0.1.0\n"
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := replaceTextFiles(root, map[string]string{
		templateModule: "github.com/acme/demo-api",
		"procyon":      "demo-api",
	}); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw)
	if !strings.Contains(got, "module github.com/acme/demo-api") {
		t.Fatalf("template module was not rewritten: %s", got)
	}
	if !strings.Contains(got, "require "+coreModule+" v0.1.0") {
		t.Fatalf("core module was unexpectedly rewritten: %s", got)
	}
}

func TestReplaceTextFilesKeepsFrameworkIdentifiers(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "generator.go")
	source := `package generator

// Procyon is replaced with the project name, but Procyon Core remains the framework.
// Run procyon-cli and inspect .procyon.json with the procyon-core module.
const marker = "procyon:api-routes"
`
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := replaceTextFiles(root, map[string]string{
		"Procyon": "Demo API",
		"procyon": "demo-api",
	}); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw)
	for _, expected := range []string{"Demo API is replaced", "Procyon Core", "procyon-cli", ".procyon.json", "procyon-core", "procyon:api-routes"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("generated text lost %q:\n%s", expected, got)
		}
	}
}

func TestTemplateCanBeGeneratedWithoutHello(t *testing.T) {
	source := helloTemplateFixture(t)
	out := t.TempDir()
	opts := Options{
		Name:          "empty-api",
		Module:        "github.com/acme/empty-api",
		OutputDir:     out,
		Database:      "postgres",
		Auth:          "kratos-casbin",
		IncludeDocker: true,
		IncludeHello:  false,
	}
	if err := copyTemplate(source, out, opts); err != nil {
		t.Fatal(err)
	}
	if err := removeHelloWiring(out); err != nil {
		t.Fatal(err)
	}
	if err := rewriteProject(out, opts); err != nil {
		t.Fatal(err)
	}
	if err := runGofmt(out); err != nil {
		t.Fatal(err)
	}

	for _, rel := range []string{"app.go", "routes.go", "plugins_local.go", "store/appStore.go", "services/appService.go", "internal/migrate.go", "internal/authz/policies.go"} {
		raw, err := os.ReadFile(filepath.Join(out, rel))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "Hello") {
			t.Fatalf("%s still contains Hello wiring", rel)
		}
		if !strings.Contains(string(raw), "procyon:") {
			t.Fatalf("%s lost generator markers", rel)
		}
	}
	if _, err := os.Stat(filepath.Join(out, "routes_test.go")); !os.IsNotExist(err) {
		t.Fatal("legacy hello-only routes_test.go should be removed when hello is disabled")
	}
}

func TestRunGoModTidyIgnoresParentWorkspace(t *testing.T) {
	parent := t.TempDir()
	project := filepath.Join(parent, "project")
	if err := os.Mkdir(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, "go.work"), []byte("not a valid workspace\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "go.mod"), []byte("module example.com/generated\n\ngo 1.26.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := runGoModTidy(project); err != nil {
		t.Fatal(err)
	}
}

func TestAddToParentWorkspaceRegistersGeneratedModule(t *testing.T) {
	parent := t.TempDir()
	project := filepath.Join(parent, "generated-api")
	if err := os.Mkdir(project, 0o755); err != nil {
		t.Fatal(err)
	}
	workspacePath := filepath.Join(parent, "go.work")
	if err := os.WriteFile(workspacePath, []byte("go 1.26.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "go.mod"), []byte("module example.com/generated-api\n\ngo 1.26.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := addToParentWorkspace(project)
	if err != nil {
		t.Fatal(err)
	}
	if got != workspacePath {
		t.Fatalf("workspace = %q, want %q", got, workspacePath)
	}
	raw, err := os.ReadFile(workspacePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "./generated-api") {
		t.Fatalf("generated module was not added to workspace:\n%s", raw)
	}
}

func TestAddToParentWorkspaceDoesNothingWithoutWorkspace(t *testing.T) {
	root := t.TempDir()
	got, err := addToParentWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("workspace = %q, want empty", got)
	}
}

func helloTemplateFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"plugins_local.go": `package main

// procyon:local-plugin-registrations
`,
		"app.go": `package main

import "github.com/bartek5186/procyon/controllers"

type application struct {
	hello *controllers.HelloController
}

func newApplication() *application {
	return &application{
		hello: controllers.NewHelloController(),
	}
}

// procyon:application
`,
		"routes.go": `package main

func routes() {
	securedAdmin := "admin"
	app.hello.Register(securedAdmin)
}

// procyon:routes
`,
		"routes_test.go": `package main

// Legacy hello-only route test from templates before it was renamed.
// NewHelloController registers /admin/hello.
`,
		"store/appStore.go": `package store

type AppStore struct {
	hello *HelloStore
}

func NewAppStore() *AppStore {
	return &AppStore{
		hello: NewHelloStore(),
	}
}

func (s *AppStore) Hello() *HelloStore {
	return s.hello
}

// procyon:store
`,
		"services/appService.go": `package services

type AppService struct {
	Hello string
}

// procyon:services
`,
		"internal/migrate.go": `package internal

import "github.com/bartek5186/procyon/models"

func migrationModels() []any {
	return []any{
		&models.HelloMessage{},
	}
}

// procyon:migrations
`,
		"internal/authz/policies.go": `package authz

var policies = []struct{ Object string }{
	// procyon:module-user-policies
	{Object: "hello"},
	// procyon:module-admin-policies
}

// procyon:policies
`,
	}
	for rel, contents := range files {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}
