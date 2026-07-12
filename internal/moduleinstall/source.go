package moduleinstall

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type registryFile struct {
	Modules map[string]registryEntry `json:"modules"`
}

type registryEntry struct {
	Source string `json:"source"`
}

func resolveSource(name, explicitSource, explicitRegistry string) (string, error) {
	if source := strings.TrimSpace(explicitSource); source != "" {
		return absoluteExistingDir(source)
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
		path, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		if _, err := os.Stat(path); err != nil {
			continue
		}
		source, err := sourceFromRegistry(path, name)
		if err != nil {
			return "", err
		}
		return absoluteExistingDir(source)
	}

	if modulesDir := strings.TrimSpace(os.Getenv("PROCYON_MODULES_DIR")); modulesDir != "" {
		return absoluteExistingDir(filepath.Join(modulesDir, name))
	}
	return "", fmt.Errorf("module %q not found; pass --source, --registry, PROCYON_MODULE_REGISTRY or PROCYON_MODULES_DIR", name)
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
