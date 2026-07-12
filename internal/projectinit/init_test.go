package projectinit

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
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

func TestTemplateCanBeGeneratedWithoutHello(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test source")
	}
	source := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", "..", "..", "procyon"))
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
