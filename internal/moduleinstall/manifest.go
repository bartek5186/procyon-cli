package moduleinstall

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Manifest struct {
	SchemaVersion   int      `json:"schema_version"`
	Kind            string   `json:"kind,omitempty"`
	Name            string   `json:"name"`
	Version         string   `json:"version"`
	Description     string   `json:"description"`
	MinimumCLI      string   `json:"minimum_cli"`
	MinimumCore     string   `json:"minimum_core"`
	GoModule        string   `json:"go_module,omitempty"`
	Package         string   `json:"package,omitempty"`
	Factory         string   `json:"factory,omitempty"`
	DefaultProvider string   `json:"default_provider,omitempty"`
	Providers       []string `json:"providers,omitempty"`
}

func loadManifest(root string) (Manifest, error) {
	path := filepath.Join(root, "procyon-module.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("read module manifest: %w", err)
	}
	var manifest Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("parse module manifest: %w", err)
	}
	if err := manifest.validate(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func (m Manifest) validate() error {
	if m.SchemaVersion != 1 {
		return fmt.Errorf("unsupported module manifest schema %d", m.SchemaVersion)
	}
	if strings.TrimSpace(m.Name) == "" || strings.TrimSpace(m.Version) == "" {
		return fmt.Errorf("module manifest requires name and version")
	}
	if m.Kind != "go-plugin" {
		return fmt.Errorf("module %s uses unsupported kind %q; shared modules must be versioned Go plugins", m.Name, m.Kind)
	}
	if strings.TrimSpace(m.GoModule) == "" || strings.TrimSpace(m.Package) == "" || strings.TrimSpace(m.Factory) == "" {
		return fmt.Errorf("Go plugin %s requires go_module, package and factory", m.Name)
	}
	return nil
}
