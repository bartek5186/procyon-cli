package moduleinstall

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestResolvePublishedModuleFromRemoteRegistry(t *testing.T) {
	originalLoader := loadRemoteJSON
	loadRemoteJSON = func(address string, output any) error {
		var body string
		switch {
		case strings.HasSuffix(address, "/registry.json"):
			body = `{"modules":{"example":{"source":"./example"}}}`
		case strings.HasSuffix(address, "/example/procyon-module.json"):
			body = `{
  "schema_version": 1,
  "kind": "go-plugin",
  "name": "example",
  "version": "0.1.1",
  "minimum_cli": "0.2.0",
  "minimum_core": "0.2.0",
  "go_module": "github.com/acme/procyon-modules/example",
  "package": "github.com/acme/procyon-modules/example",
  "factory": "New"
}`
		default:
			return fmt.Errorf("unexpected URL %s", address)
		}
		return json.Unmarshal([]byte(body), output)
	}
	t.Cleanup(func() { loadRemoteJSON = originalLoader })

	source, manifest, err := resolveModule("example", "", "https://registry.test/registry.json", true)
	if err != nil {
		t.Fatal(err)
	}
	if source != "" || manifest.Name != "example" || manifest.Version != "0.1.1" {
		t.Fatalf("unexpected resolved module: source=%q manifest=%+v", source, manifest)
	}
}

func TestRemoteRegistryRequiresPublishedMode(t *testing.T) {
	_, _, err := resolveModule("example", "", "https://example.invalid/registry.json", false)
	if err == nil {
		t.Fatal("expected remote registry mode error")
	}
}

func TestCatalogFromRemoteRegistryUsesSummaryWithoutLoadingManifests(t *testing.T) {
	originalLoader := loadRemoteJSON
	requests := 0
	loadRemoteJSON = func(address string, output any) error {
		requests++
		if !strings.HasSuffix(address, "/registry.json") {
			return fmt.Errorf("unexpected manifest request %s", address)
		}
		return json.Unmarshal([]byte(`{
  "modules": {
    "payment-system": {
      "source": "./payment-system",
      "version": "0.1.1",
      "description": "Provider-based payments"
    },
    "example": {"source": "./example", "version": "0.1.0"}
  }
}`), output)
	}
	t.Cleanup(func() { loadRemoteJSON = originalLoader })

	modules, err := catalogFromRemoteRegistry("https://registry.test/registry.json")
	if err != nil {
		t.Fatal(err)
	}
	if requests != 1 {
		t.Fatalf("registry requests = %d, want 1", requests)
	}
	if len(modules) != 2 || modules[0].Name != "example" || modules[1].Name != "payment-system" {
		t.Fatalf("unexpected sorted catalog: %+v", modules)
	}
	if modules[1].Description != "Provider-based payments" || modules[1].Version != "0.1.1" {
		t.Fatalf("missing catalog summary: %+v", modules[1])
	}
}
