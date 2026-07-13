package moduleinstall

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const OfficialRegistry = "https://raw.githubusercontent.com/bartek5186/procyon-modules/main/registry.json"

var loadRemoteJSON = getJSON

type registryFile struct {
	Modules map[string]registryEntry `json:"modules"`
}

type registryEntry struct {
	Source      string `json:"source"`
	Version     string `json:"version,omitempty"`
	Description string `json:"description,omitempty"`
}

type CatalogModule struct {
	Name        string
	Version     string
	Description string
}

// PublishedCatalog returns the searchable module summary from the official
// registry. It intentionally does not load every module manifest; the selected
// module manifest is fetched separately by PublishedManifest.
func PublishedCatalog() ([]CatalogModule, error) {
	return catalogFromRemoteRegistry(OfficialRegistry)
}

func PublishedManifest(name string) (Manifest, error) {
	return manifestFromRemoteRegistry(OfficialRegistry, strings.TrimSpace(name))
}

func resolveModule(name, explicitSource, explicitRegistry string, published bool) (string, Manifest, error) {
	if source := strings.TrimSpace(explicitSource); source != "" {
		root, err := absoluteExistingDir(source)
		if err != nil {
			return "", Manifest{}, err
		}
		manifest, err := loadManifest(root)
		return root, manifest, err
	}

	registryCandidates := make([]string, 0, 4)
	if explicitRegistry != "" {
		registryCandidates = append(registryCandidates, explicitRegistry)
	}
	if env := strings.TrimSpace(os.Getenv("PROCYON_MODULE_REGISTRY")); env != "" {
		registryCandidates = append(registryCandidates, env)
	}
	registryCandidates = append(registryCandidates,
		filepath.Join("procyon-modules", "registry.json"),
		filepath.Join("..", "procyon-modules", "registry.json"),
	)

	for _, candidate := range registryCandidates {
		if isHTTPURL(candidate) {
			if !published {
				return "", Manifest{}, fmt.Errorf("remote registries require --published")
			}
			manifest, err := manifestFromRemoteRegistry(candidate, name)
			return "", manifest, err
		}
		path, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		if _, err := os.Stat(path); err != nil {
			continue
		}
		source, err := sourceFromRegistry(path, name)
		if err != nil {
			return "", Manifest{}, err
		}
		root, err := absoluteExistingDir(source)
		if err != nil {
			return "", Manifest{}, err
		}
		manifest, err := loadManifest(root)
		if published {
			return "", manifest, err
		}
		return root, manifest, err
	}

	if modulesDir := strings.TrimSpace(os.Getenv("PROCYON_MODULES_DIR")); modulesDir != "" {
		root, err := absoluteExistingDir(filepath.Join(modulesDir, name))
		if err != nil {
			return "", Manifest{}, err
		}
		manifest, err := loadManifest(root)
		if published {
			return "", manifest, err
		}
		return root, manifest, err
	}
	if published {
		manifest, err := manifestFromRemoteRegistry(OfficialRegistry, name)
		return "", manifest, err
	}
	return "", Manifest{}, fmt.Errorf("module %q not found; pass --source, --registry, PROCYON_MODULE_REGISTRY or PROCYON_MODULES_DIR", name)
}

func sourceFromRegistry(path, name string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var registry registryFile
	if err := json.Unmarshal(raw, &registry); err != nil {
		return "", fmt.Errorf("parse registry %s: %w", path, err)
	}
	entry, ok := registry.Modules[name]
	if !ok || strings.TrimSpace(entry.Source) == "" {
		return "", fmt.Errorf("module %q is not present in %s", name, path)
	}
	source := entry.Source
	if !filepath.IsAbs(source) {
		source = filepath.Join(filepath.Dir(path), source)
	}
	return source, nil
}

func absoluteExistingDir(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("module source %s: %w", abs, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("module source %s is not a directory", abs)
	}
	return abs, nil
}

func manifestFromRemoteRegistry(registryURL, name string) (Manifest, error) {
	var registry registryFile
	if err := loadRemoteJSON(registryURL, &registry); err != nil {
		return Manifest{}, fmt.Errorf("load module registry: %w", err)
	}
	entry, ok := registry.Modules[name]
	if !ok || strings.TrimSpace(entry.Source) == "" {
		return Manifest{}, fmt.Errorf("module %q is not present in %s", name, registryURL)
	}
	base, err := url.Parse(registryURL)
	if err != nil {
		return Manifest{}, err
	}
	relative, err := url.Parse(strings.TrimSuffix(entry.Source, "/") + "/procyon-module.json")
	if err != nil {
		return Manifest{}, err
	}
	manifestURL := base.ResolveReference(relative).String()
	var manifest Manifest
	if err := loadRemoteJSON(manifestURL, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("load manifest for %s: %w", name, err)
	}
	if err := manifest.validate(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func catalogFromRemoteRegistry(registryURL string) ([]CatalogModule, error) {
	var registry registryFile
	if err := loadRemoteJSON(registryURL, &registry); err != nil {
		return nil, fmt.Errorf("load module registry: %w", err)
	}
	modules := make([]CatalogModule, 0, len(registry.Modules))
	for name, entry := range registry.Modules {
		name = strings.TrimSpace(name)
		if name == "" || strings.TrimSpace(entry.Source) == "" {
			continue
		}
		modules = append(modules, CatalogModule{
			Name:        name,
			Version:     strings.TrimSpace(entry.Version),
			Description: strings.TrimSpace(entry.Description),
		})
	}
	sort.Slice(modules, func(i, j int) bool { return modules[i].Name < modules[j].Name })
	return modules, nil
}

func getJSON(address string, output any) error {
	client := &http.Client{Timeout: 15 * time.Second}
	response, err := client.Get(address)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", address, response.Status)
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 1<<20))
	if err := decoder.Decode(output); err != nil {
		return fmt.Errorf("parse %s: %w", address, err)
	}
	return nil
}

func isHTTPURL(value string) bool {
	return strings.HasPrefix(value, "https://") || strings.HasPrefix(value, "http://")
}
