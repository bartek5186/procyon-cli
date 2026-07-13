package gocommand

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProjectEnvironmentDisablesUnrelatedParentWorkspace(t *testing.T) {
	withoutExplicitGoWork(t)
	root := t.TempDir()
	workspaceModule := filepath.Join(root, "workspace-module")
	nestedModule := filepath.Join(root, "workspace-module", "nested-project")
	writeFile(t, filepath.Join(root, "go.work"), "go 1.26.0\n\nuse ./workspace-module\n")
	writeFile(t, filepath.Join(workspaceModule, "go.mod"), "module example.com/workspace\n\ngo 1.26.0\n")
	writeFile(t, filepath.Join(nestedModule, "go.mod"), "module example.com/nested\n\ngo 1.26.0\n")
	withDirectory(t, nestedModule)

	environment := projectEnvironment()
	if !containsEnvironment(environment, "GOWORK=off") {
		t.Fatalf("GOWORK=off missing from environment: %v", environment)
	}
}

func TestProjectEnvironmentKeepsWorkspaceForIncludedModule(t *testing.T) {
	withoutExplicitGoWork(t)
	root := t.TempDir()
	module := filepath.Join(root, "project")
	writeFile(t, filepath.Join(root, "go.work"), "go 1.26.0\n\nuse ./project\n")
	writeFile(t, filepath.Join(module, "go.mod"), "module example.com/project\n\ngo 1.26.0\n")
	withDirectory(t, module)

	environment := projectEnvironment()
	if containsEnvironment(environment, "GOWORK=off") {
		t.Fatalf("included workspace was disabled: %v", environment)
	}
}

func containsEnvironment(environment []string, expected string) bool {
	for _, value := range environment {
		if value == expected {
			return true
		}
	}
	return false
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func withDirectory(t *testing.T, directory string) {
	t.Helper()
	current, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(directory); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(current)
	})
}

func withoutExplicitGoWork(t *testing.T) {
	t.Helper()
	value, exists := os.LookupEnv("GOWORK")
	if err := os.Unsetenv("GOWORK"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if exists {
			_ = os.Setenv("GOWORK", value)
			return
		}
		_ = os.Unsetenv("GOWORK")
	})
}
