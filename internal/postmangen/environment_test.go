package postmangen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadEnvironmentParsesDotEnvAndLetsProcessOverride(t *testing.T) {
	root := t.TempDir()
	writePostmanTestFile(t, filepath.Join(root, ".env"), `
# project defaults
POSTMAN_COLLECTION_NAME="Demo API" # shown in Postman
POSTMAN_API_KEY='key with spaces and # hash'
POSTMAN_BASE_URL=https://example.test # local default
EMPTY=
export POSTMAN_TARGET_NAME_8="Production"
`)
	env, err := loadEnvironment(root, []string{
		"POSTMAN_COLLECTION_NAME=Process API",
		"PATH=/bin",
	})
	if err != nil {
		t.Fatal(err)
	}
	if env["POSTMAN_COLLECTION_NAME"] != "Process API" {
		t.Fatalf("process environment did not override .env: %#v", env)
	}
	if env["POSTMAN_API_KEY"] != "key with spaces and # hash" {
		t.Fatalf("quoted API key was parsed incorrectly: %q", env["POSTMAN_API_KEY"])
	}
	if env["POSTMAN_BASE_URL"] != "https://example.test" || env["POSTMAN_TARGET_NAME_8"] != "Production" {
		t.Fatalf("unexpected parsed environment: %#v", env)
	}
	if value, ok := env["EMPTY"]; !ok || value != "" {
		t.Fatalf("empty value was not preserved: %#v", env)
	}
}

func TestLoadEnvironmentRejectsMalformedDotEnv(t *testing.T) {
	root := t.TempDir()
	writePostmanTestFile(t, filepath.Join(root, ".env"), "POSTMAN_COLLECTION_NAME=\"unterminated\n")
	if _, err := loadEnvironment(root, nil); err == nil {
		t.Fatal("expected malformed .env error")
	}
}

func TestGenerateFromEnvironmentUsesProjectValues(t *testing.T) {
	project := t.TempDir()
	writePostmanTestFile(t, filepath.Join(project, "routes.go"), `package main
func registerPublicRoutes(e *Echo, app *application) { e.GET("/health", app.Health) }
func registerAdminRoutes(e *Echo, app *application) {}
func registerUploadRoutes(e *Echo, app *application) {}
`)
	writePostmanTestFile(t, filepath.Join(project, ".env"), `
POSTMAN_COLLECTION_NAME="Environment API"
POSTMAN_COLLECTION_FILE=build/from-env.json
POSTMAN_BASE_URL=https://api.example.test
`)
	t.Setenv("POSTMAN_COLLECTION_NAME", "Environment API")
	t.Setenv("POSTMAN_COLLECTION_FILE", "build/from-env.json")
	t.Setenv("POSTMAN_BASE_URL", "https://api.example.test")
	result, err := GenerateFromEnvironment(Options{Root: project})
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(result.OutputPath)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(result.OutputPath) != "from-env.json" || !containsAll(string(content), `"name": "Environment API"`, `"value": "https://api.example.test"`) {
		t.Fatalf("environment was not applied: path=%s content=%s", result.OutputPath, content)
	}
}

func containsAll(value string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(value, part) {
			return false
		}
	}
	return true
}
