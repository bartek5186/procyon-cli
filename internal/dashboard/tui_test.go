package dashboard

import (
	"testing"

	"github.com/bartek5186/procyon-cli/internal/moduleinstall"
)

func TestAvailableCatalogModulesExcludesInstalledPlugins(t *testing.T) {
	catalog := []moduleinstall.CatalogModule{
		{Name: "example"},
		{Name: "payment-system"},
	}
	available := availableCatalogModules(catalog, []Module{{Name: "example", Version: "0.1.1"}})
	if len(available) != 1 || available[0].Name != "payment-system" {
		t.Fatalf("unexpected available modules: %+v", available)
	}
}

func TestCatalogOptionsIncludeSearchableMetadataButNotProviders(t *testing.T) {
	options := catalogOptions([]moduleinstall.CatalogModule{{
		Name: "payment-system", Version: "0.1.1", Description: "Provider-based payments",
	}})
	if len(options) != 1 {
		t.Fatalf("options = %d, want 1", len(options))
	}
	if options[0].Key != "payment-system  v0.1.1 — Provider-based payments" {
		t.Fatalf("unexpected option label %q", options[0].Key)
	}
	if options[0].Value != "payment-system" {
		t.Fatalf("unexpected option value %q", options[0].Value)
	}
}

func TestProviderChoicesComeFromSelectedManifest(t *testing.T) {
	manifest := moduleinstall.Manifest{
		DefaultProvider: "stripe",
		Providers:       []string{"stripe", "google", "apple"},
	}
	defaults := defaultProviders(manifest)
	options := providerOptions(manifest.Providers)
	if len(defaults) != 1 || defaults[0] != "stripe" {
		t.Fatalf("unexpected defaults: %+v", defaults)
	}
	if len(options) != 3 || options[1].Key != "Google Play" || options[2].Key != "Apple App Store" {
		t.Fatalf("unexpected provider options: %+v", options)
	}
}
