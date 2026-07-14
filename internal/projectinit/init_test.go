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

	for _, rel := range []string{"app.go", "routes.go", "store/appStore.go", "services/appService.go", "internal/migrate.go", "policies.go"} {
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
}

func helloTemplateFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
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
		"policies.go": `package main

var policies = []struct{ Object string }{
	{Object: "hello"},
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
