package dashboard

import (
	"strings"
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

func TestAvailablePluginUpdatesComparesPublishedVersions(t *testing.T) {
	catalog := []moduleinstall.CatalogModule{
		{Name: "example", Version: "0.1.1"},
		{Name: "payment-system", Version: "0.3.1"},
		{Name: "local-plugin", Version: "9.0.0"},
	}
	installed := []Module{
		{Name: "example", Version: "0.1.1"},
		{Name: "payment-system", Version: "0.3.0"},
		{Name: "local-plugin", Version: "0.1.0", LocalSource: "plugins/local-plugin"},
	}
	updates := availablePluginUpdates(catalog, installed)
	if len(updates) != 1 || updates[0].Name != "payment-system" || updates[0].AvailableVersion != "0.3.1" {
		t.Fatalf("unexpected updates: %+v", updates)
	}
}

func TestProjectActionsOfferPluginUpdateWhenAvailable(t *testing.T) {
	options := projectActionOptions(Context{}, []pluginUpdate{{Name: "payment-system"}})
	for _, option := range options {
		if option.Value == "update_plugin" {
			return
		}
	}
	t.Fatal("missing update_plugin action")
}

func TestProjectActionsExposeOnePluginCreationEntry(t *testing.T) {
	options := projectActionOptions(Context{}, nil)
	count := 0
	for _, option := range options {
		if option.Value == "create_plugin" {
			count++
			if option.Key != "Create plugin" {
				t.Fatalf("unexpected create plugin label %q", option.Key)
			}
		}
		if option.Value == "create_local_plugin" {
			t.Fatal("legacy project-owned creation action is still exposed")
		}
	}
	if count != 1 {
		t.Fatalf("create plugin actions = %d, want 1", count)
	}
}

func TestProjectActionsIncludePostmanGeneration(t *testing.T) {
	options := projectActionOptions(Context{}, nil)
	foundGenerate, foundSync := false, false
	for _, option := range options {
		if option.Value == "generate_postman" {
			foundGenerate = true
		}
		if option.Value == "sync_postman" {
			foundSync = true
		}
	}
	if !foundGenerate || !foundSync {
		t.Fatalf("missing Postman actions: generate=%t sync=%t", foundGenerate, foundSync)
	}
}

func TestProjectActionsIncludeExplicitCoreAndCLIUpdates(t *testing.T) {
	options := projectActionOptions(Context{}, nil)
	foundCore, foundCLI := false, false
	for _, option := range options {
		foundCore = foundCore || option.Value == "update"
		foundCLI = foundCLI || option.Value == "self_update"
	}
	if !foundCore || !foundCLI {
		t.Fatalf("missing update actions: core=%t cli=%t", foundCore, foundCLI)
	}
}

func TestProjectSummaryShowsAvailablePluginUpdates(t *testing.T) {
	summary := projectSummary(Context{ProjectName: "demo"}, []pluginUpdate{{Name: "payment-system"}}, nil)
	if !strings.Contains(summary, "Plugin updates: 1") {
		t.Fatalf("missing update count in summary: %q", summary)
	}
}
