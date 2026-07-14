package moduleinstall

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsNewerVersion(t *testing.T) {
	if !IsNewerVersion("0.3.0", "0.3.1") {
		t.Fatal("expected 0.3.1 to be newer than 0.3.0")
	}
	for _, test := range [][2]string{{"0.3.1", "0.3.1"}, {"0.4.0", "0.3.1"}, {"unknown", "0.3.1"}} {
		if IsNewerVersion(test[0], test[1]) {
			t.Fatalf("unexpected update from %s to %s", test[0], test[1])
		}
	}
}

func TestRunInstallsAndRecordsModule(t *testing.T) {
	project, source := moduleInstallFixture(t)
	withWorkingDirectory(t, project)

	originalRunner := runCommand
	runCommand = func(_ io.Writer, _ string, _ ...string) error { return nil }
	t.Cleanup(func() { runCommand = originalRunner })

	var output bytes.Buffer
	err := Run(Options{Name: "example", Source: source, Provider: "stripe", Writer: &output})
	if err != nil {
		t.Fatalf("install module: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(project, "plugins_gen.go"))
	if err != nil {
		t.Fatalf("read generated plugin registry: %v", err)
	}
	if got := string(raw); !strings.Contains(got, `plugin0 "github.com/acme/procyon-example"`) || !strings.Contains(got, "Factory: plugin0.New") {
		t.Fatalf("unexpected generated plugin registry:\n%s", got)
	}
	metadata, err := loadProjectMetadata()
	if err != nil {
		t.Fatalf("load metadata: %v", err)
	}
	installed, ok := metadata.Modules["example"]
	if !ok || installed.Version != "0.1.0" || installed.Kind != "go-plugin" || len(installed.Providers) != 1 {
		t.Fatalf("unexpected installed metadata: %+v", installed)
	}
}

func TestRunRollsBackWhenValidationCommandFails(t *testing.T) {
	project, source := moduleInstallFixture(t)
	withWorkingDirectory(t, project)
	originalMetadata, err := os.ReadFile(".procyon.json")
	if err != nil {
		t.Fatal(err)
	}

	originalRunner := runCommand
	runCommand = func(_ io.Writer, _ string, _ ...string) error { return errors.New("test failed") }
	t.Cleanup(func() { runCommand = originalRunner })

	err = Run(Options{Name: "example", Source: source, Provider: "stripe", Writer: &bytes.Buffer{}})
	if err == nil {
		t.Fatal("expected install failure")
	}
	generated, err := os.ReadFile("plugins_gen.go")
	if err != nil {
		t.Fatal(err)
	}
	if string(generated) != "package main\n" {
		t.Fatalf("generated registry was not rolled back: %q", generated)
	}
	currentMetadata, err := os.ReadFile(".procyon.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(originalMetadata, currentMetadata) {
		t.Fatal("metadata was not rolled back")
	}
}

func TestRunDryRunDoesNotWriteFiles(t *testing.T) {
	project, source := moduleInstallFixture(t)
	withWorkingDirectory(t, project)

	if err := Run(Options{Name: "example", Source: source, Provider: "stripe", DryRun: true, Writer: &bytes.Buffer{}}); err != nil {
		t.Fatalf("dry run: %v", err)
	}
	generated, err := os.ReadFile("plugins_gen.go")
	if err != nil {
		t.Fatal(err)
	}
	if string(generated) != "package main\n" {
		t.Fatalf("dry run changed plugin registry: %q", generated)
	}
}

func TestDisableAndEnablePluginSynchronizesGeneratedState(t *testing.T) {
	project, source := moduleInstallFixture(t)
	withWorkingDirectory(t, project)
	writeTestFile(t, filepath.Join(project, ".env.example"), "APP_ENV=development\n")

	originalRunner := runCommand
	runCommand = func(_ io.Writer, _ string, _ ...string) error { return nil }
	t.Cleanup(func() { runCommand = originalRunner })

	if err := Run(Options{Name: "example", Source: source, Provider: "stripe", Writer: &bytes.Buffer{}}); err != nil {
		t.Fatal(err)
	}
	assertGeneratedPluginState(t, true)
	if err := SetEnabled(SetEnabledOptions{Name: "example", Enabled: false, Writer: &bytes.Buffer{}}); err != nil {
		t.Fatal(err)
	}
	assertGeneratedPluginState(t, false)
	if err := SetEnabled(SetEnabledOptions{Name: "example", Enabled: true, Writer: &bytes.Buffer{}}); err != nil {
		t.Fatal(err)
	}
	assertGeneratedPluginState(t, true)
}

func assertGeneratedPluginState(t *testing.T, enabled bool) {
	t.Helper()
	generated, err := os.ReadFile("plugins_gen.go")
	if err != nil {
		t.Fatal(err)
	}
	config, err := os.ReadFile(generatedPluginConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	env, err := os.ReadFile(".env.example")
	if err != nil {
		t.Fatal(err)
	}
	for label, present := range map[string]bool{
		"registration": strings.Contains(string(generated), "github.com/acme/procyon-example"),
		"config":       strings.Contains(string(config), `"example"`),
		"environment":  strings.Contains(string(env), "EXAMPLE_TOKEN="),
	} {
		if present != enabled {
			t.Fatalf("%s presence = %v, want %v\nplugins:\n%s\nconfig:\n%s\nenv:\n%s", label, present, enabled, generated, config, env)
		}
	}
	if !strings.Contains(string(env), "APP_ENV=development") {
		t.Fatal("application environment values were not preserved")
	}
}

func moduleInstallFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	project := filepath.Join(root, "project")
	source := filepath.Join(root, "module")
	for _, dir := range []string{project, source} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeTestFile(t, filepath.Join(project, "go.mod"), "module github.com/acme/app\n\ngo 1.26.0\n")
	writeTestFile(t, filepath.Join(project, "plugins_gen.go"), "package main\n")
	writeTestFile(t, filepath.Join(project, ".procyon.json"), `{
  "schema_version": 1,
  "project_module": "github.com/acme/app",
  "template_version": "v0.1.0",
  "core_version": "v0.1.0",
  "cli_min_version": "0.1.0"
}
`)
	writeTestFile(t, filepath.Join(source, "procyon-module.json"), `{
  "schema_version": 1,
  "kind": "go-plugin",
  "name": "example",
  "version": "0.1.0",
  "minimum_cli": "0.1.0",
  "minimum_core": "0.1.0",
  "go_module": "github.com/acme/procyon-example",
  "package": "github.com/acme/procyon-example",
  "factory": "New",
  "providers": ["stripe"],
  "environment": [
    {"name":"EXAMPLE_TOKEN","description":"Example token.","providers":["stripe"],"secret":true},
    {"name":"GOOGLE_ONLY","providers":["google"]}
  ]
}
`)
	writeTestFile(t, filepath.Join(source, "go.mod"), "module github.com/acme/procyon-example\n\ngo 1.26.0\n")
	return project, source
}

func writeTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func withWorkingDirectory(t *testing.T, path string) {
	t.Helper()
	current, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(current) })
}
