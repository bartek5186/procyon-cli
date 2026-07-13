package dashboard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectEmptyDirectory(t *testing.T) {
	ctx, err := Detect(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if ctx.Kind != DirectoryEmpty {
		t.Fatalf("Kind = %q, want %q", ctx.Kind, DirectoryEmpty)
	}
}

func TestDetectCurrentProcyonProject(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "go.mod", `module github.com/acme/demo

go 1.26.0

require (
	github.com/bartek5186/procyon-core v0.2.1
)
`)
	writeFixture(t, root, ".procyon.json", `{
  "schema_version": 1,
  "project_module": "github.com/acme/demo",
  "template_version": "v0.2.0",
  "core_version": "v0.2.0",
  "cli_min_version": "0.2.0",
  "modules": {
    "payment-system": {"version": "0.1.1"},
    "example": {"version": "0.1.1"}
  }
}`)

	ctx, err := Detect(root)
	if err != nil {
		t.Fatal(err)
	}
	if ctx.Kind != DirectoryProject {
		t.Fatalf("Kind = %q, reason = %q", ctx.Kind, ctx.Reason)
	}
	if ctx.ProjectName != "demo" || ctx.CoreVersion != "v0.2.1" || ctx.TemplateVersion != "v0.2.0" {
		t.Fatalf("unexpected project context: %+v", ctx)
	}
	if len(ctx.Modules) != 2 || ctx.Modules[0].Name != "example" || ctx.Modules[1].Name != "payment-system" {
		t.Fatalf("modules are not sorted: %+v", ctx.Modules)
	}
}

func TestDetectLegacyProjectFromCoreDependency(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "go.mod", "module github.com/acme/legacy\n\nrequire github.com/bartek5186/procyon-core v0.1.0\n")

	ctx, err := Detect(root)
	if err != nil {
		t.Fatal(err)
	}
	if ctx.Kind != DirectoryProject || ctx.TemplateVersion != "legacy / unknown" {
		t.Fatalf("unexpected legacy context: %+v", ctx)
	}
}

func TestDetectNonProcyonDirectory(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "README.md", "not a Procyon project\n")

	ctx, err := Detect(root)
	if err != nil {
		t.Fatal(err)
	}
	if ctx.Kind != DirectoryOther || !strings.Contains(ctx.Reason, "No Procyon Core dependency") {
		t.Fatalf("unexpected directory context: %+v", ctx)
	}
}

func TestDetectProjectReportsBrokenMetadata(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "go.mod", "module github.com/acme/demo\n\nrequire github.com/bartek5186/procyon-core v0.2.0\n")
	writeFixture(t, root, ".procyon.json", "{broken")

	ctx, err := Detect(root)
	if err != nil {
		t.Fatal(err)
	}
	if ctx.Kind != DirectoryProject || !strings.Contains(ctx.Reason, "invalid .procyon.json") {
		t.Fatalf("unexpected broken-metadata context: %+v", ctx)
	}
}

func writeFixture(t *testing.T, root, name, body string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
