package dashboard

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const coreModule = "github.com/bartek5186/procyon-core"

type DirectoryKind string

const (
	DirectoryEmpty   DirectoryKind = "empty"
	DirectoryProject DirectoryKind = "procyon"
	DirectoryOther   DirectoryKind = "other"
)

type Context struct {
	Root            string
	Kind            DirectoryKind
	ProjectModule   string
	ProjectName     string
	TemplateVersion string
	CoreVersion     string
	MinimumCLI      string
	Modules         []Module
	Reason          string
}

type Module struct {
	Name        string
	Version     string
	Enabled     bool
	LocalSource string
}

type projectMetadata struct {
	SchemaVersion   int                       `json:"schema_version"`
	ProjectModule   string                    `json:"project_module"`
	TemplateVersion string                    `json:"template_version"`
	CoreVersion     string                    `json:"core_version"`
	CLIMinVersion   string                    `json:"cli_min_version"`
	Modules         map[string]moduleMetadata `json:"modules,omitempty"`
}

type moduleMetadata struct {
	Version     string `json:"version"`
	Enabled     *bool  `json:"enabled,omitempty"`
	LocalSource string `json:"local_source,omitempty"`
}

func Detect(root string) (Context, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return Context{}, err
	}
	entries, err := os.ReadDir(absRoot)
	if err != nil {
		return Context{}, fmt.Errorf("read current directory: %w", err)
	}
	ctx := Context{Root: absRoot}
	if len(entries) == 0 {
		ctx.Kind = DirectoryEmpty
		return ctx, nil
	}

	goModule, coreVersion, hasCore, goModErr := inspectGoMod(filepath.Join(absRoot, "go.mod"))
	metadata, metadataFound, metadataErr := readMetadata(filepath.Join(absRoot, ".procyon.json"))
	if !hasCore {
		ctx.Kind = DirectoryOther
		switch {
		case goModErr != nil && !errors.Is(goModErr, os.ErrNotExist):
			ctx.Reason = goModErr.Error()
		case metadataErr != nil:
			ctx.Reason = metadataErr.Error()
		case metadataFound:
			ctx.Reason = ".procyon.json exists, but go.mod does not depend on Procyon Core"
		default:
			ctx.Reason = "No Procyon Core dependency was found in go.mod"
		}
		return ctx, nil
	}

	ctx.Kind = DirectoryProject
	ctx.ProjectModule = firstNonEmpty(metadata.ProjectModule, goModule)
	ctx.ProjectName = filepath.Base(ctx.ProjectModule)
	ctx.CoreVersion = firstNonEmpty(coreVersion, metadata.CoreVersion, "unknown")
	ctx.TemplateVersion = firstNonEmpty(metadata.TemplateVersion, "legacy / unknown")
	ctx.MinimumCLI = firstNonEmpty(metadata.CLIMinVersion, "unknown")
	if metadataErr != nil {
		ctx.Reason = metadataErr.Error()
	}
	for name, installed := range metadata.Modules {
		enabled := installed.Enabled == nil || *installed.Enabled
		ctx.Modules = append(ctx.Modules, Module{
			Name: name, Version: firstNonEmpty(installed.Version, "unknown"), Enabled: enabled,
			LocalSource: strings.TrimSpace(installed.LocalSource),
		})
	}
	sort.Slice(ctx.Modules, func(i, j int) bool { return ctx.Modules[i].Name < ctx.Modules[j].Name })
	return ctx, nil
}

func inspectGoMod(path string) (module, coreVersion string, hasCore bool, err error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", "", false, err
	}
	for _, rawLine := range strings.Split(string(raw), "\n") {
		line := strings.TrimSpace(rawLine)
		if strings.HasPrefix(line, "module ") {
			module = strings.TrimSpace(strings.TrimPrefix(line, "module "))
		}
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == "require" {
			fields = fields[1:]
		}
		if len(fields) >= 1 && fields[0] == coreModule {
			hasCore = true
			if len(fields) >= 2 {
				coreVersion = fields[1]
			}
		}
	}
	return module, coreVersion, hasCore, nil
}

func readMetadata(path string) (projectMetadata, bool, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return projectMetadata{}, false, nil
	}
	if err != nil {
		return projectMetadata{}, false, err
	}
	var metadata projectMetadata
	if err := json.Unmarshal(raw, &metadata); err != nil {
		return projectMetadata{}, true, fmt.Errorf("invalid .procyon.json: %w", err)
	}
	if metadata.SchemaVersion != 1 {
		return projectMetadata{}, true, fmt.Errorf("unsupported .procyon.json schema version %d", metadata.SchemaVersion)
	}
	return metadata, true, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
