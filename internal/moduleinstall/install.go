package moduleinstall

import (
	"encoding/json"
	"errors"
	"fmt"
	"go/format"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bartek5186/procyon-cli/internal/buildinfo"
)

type Options struct {
	Name      string
	Source    string
	Registry  string
	Provider  string
	Values    map[string]string
	DryRun    bool
	Published bool
	Writer    io.Writer
}

type projectMetadata struct {
	SchemaVersion   int                        `json:"schema_version"`
	ProjectModule   string                     `json:"project_module"`
	TemplateVersion string                     `json:"template_version"`
	CoreVersion     string                     `json:"core_version"`
	CLIMinVersion   string                     `json:"cli_min_version"`
	Modules         map[string]InstalledModule `json:"modules,omitempty"`
}

type InstalledModule struct {
	Version     string            `json:"version"`
	Kind        string            `json:"kind,omitempty"`
	GoModule    string            `json:"go_module,omitempty"`
	Package     string            `json:"package,omitempty"`
	Factory     string            `json:"factory,omitempty"`
	LocalSource string            `json:"local_source,omitempty"`
	Providers   []string          `json:"providers,omitempty"`
	Values      map[string]string `json:"values,omitempty"`
	InstalledAt string            `json:"installed_at"`
}

type backupFile struct {
	Body   []byte
	Exists bool
	Mode   os.FileMode
}

var runCommand = func(writer io.Writer, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = writer
	cmd.Stderr = writer
	return cmd.Run()
}

func Run(opts Options) error {
	if opts.Writer == nil {
		opts.Writer = os.Stdout
	}
	name := strings.TrimSpace(opts.Name)
	if name == "" {
		return errors.New("module name is required")
	}

	metadata, err := loadProjectMetadata()
	if err != nil {
		return err
	}
	if _, exists := metadata.Modules[name]; exists {
		return fmt.Errorf("module %q is already installed", name)
	}

	source, err := resolveSource(name, opts.Source, opts.Registry)
	if err != nil {
		return err
	}
	manifest, err := loadManifest(source)
	if err != nil {
		return err
	}
	if manifest.Name != name {
		return fmt.Errorf("requested module %q but source contains %q", name, manifest.Name)
	}
	if err := validateCompatibility(manifest, metadata); err != nil {
		return err
	}

	providers, _, err := selectProviders(manifest, opts.Provider)
	if err != nil {
		return err
	}
	return runGoPluginInstall(opts, source, manifest, metadata, providers)
}

func runGoPluginInstall(opts Options, source string, manifest Manifest, metadata projectMetadata, providers []string) error {
	localSource := source
	if opts.Published {
		localSource = ""
	}
	metadata.Modules[manifest.Name] = InstalledModule{
		Version:     manifest.Version,
		Kind:        manifest.Kind,
		GoModule:    manifest.GoModule,
		Package:     manifest.Package,
		Factory:     manifest.Factory,
		LocalSource: localSource,
		Providers:   providers,
		Values:      cloneStringMap(opts.Values),
		InstalledAt: time.Now().UTC().Format(time.RFC3339),
	}
	generated, err := generatePluginsFile(metadata.Modules)
	if err != nil {
		return err
	}
	metadataBody, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return err
	}
	metadataBody = append(metadataBody, '\n')

	fmt.Fprintf(opts.Writer, "%s Go plugin %s %s\n", map[bool]string{true: "Plan", false: "Install"}[opts.DryRun], manifest.Name, manifest.Version)
	fmt.Fprintf(opts.Writer, "  require %s@%s\n", manifest.GoModule, normalizedGoVersion(manifest.Version))
	if localSource != "" {
		fmt.Fprintf(opts.Writer, "  replace %s => %s\n", manifest.GoModule, localSource)
	}
	fmt.Fprintln(opts.Writer, "  generate plugins_gen.go")
	if opts.DryRun {
		return nil
	}

	backup, err := backupPaths([]string{".procyon.json", "plugins_gen.go", "go.mod", "go.sum"})
	if err != nil {
		return err
	}
	rollback := func() { restoreBackup(backup) }
	if err := os.WriteFile(".procyon.json", metadataBody, 0o644); err != nil {
		rollback()
		return err
	}
	if err := os.WriteFile("plugins_gen.go", generated, 0o644); err != nil {
		rollback()
		return err
	}
	if localSource != "" {
		if err := runCommand(opts.Writer, "go", "mod", "edit", "-replace="+manifest.GoModule+"="+localSource); err != nil {
			rollback()
			return fmt.Errorf("add local plugin replace: %w", err)
		}
	} else if err := runCommand(opts.Writer, "go", "mod", "edit", "-dropreplace="+manifest.GoModule); err != nil {
		rollback()
		return fmt.Errorf("remove local plugin replace: %w", err)
	}
	if err := runCommand(opts.Writer, "go", "get", manifest.GoModule+"@"+normalizedGoVersion(manifest.Version)); err != nil {
		rollback()
		return fmt.Errorf("go get %s: %w", manifest.GoModule, err)
	}
	for _, args := range [][]string{{"mod", "tidy"}, {"test", "./..."}} {
		if err := runCommand(opts.Writer, "go", args...); err != nil {
			rollback()
			return fmt.Errorf("go %s: %w", strings.Join(args, " "), err)
		}
	}
	fmt.Fprintf(opts.Writer, "Installed Go plugin %s %s.\n", manifest.Name, manifest.Version)
	return nil
}

func generatePluginsFile(installed map[string]InstalledModule) ([]byte, error) {
	names := make([]string, 0, len(installed))
	for name, module := range installed {
		if module.Kind == "go-plugin" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	var source strings.Builder
	source.WriteString("// Code generated by procyon-cli. DO NOT EDIT.\n\npackage main\n\n")
	if len(names) == 0 {
		source.WriteString("import coreplugins \"github.com/bartek5186/procyon-core/plugins\"\n\n")
		source.WriteString("func installedPluginFactories() []coreplugins.Registration { return nil }\n")
		return format.Source([]byte(source.String()))
	}
	source.WriteString("import (\n\t\"encoding/json\"\n\tcoreplugins \"github.com/bartek5186/procyon-core/plugins\"\n")
	for index, name := range names {
		module := installed[name]
		fmt.Fprintf(&source, "\tplugin%d %q\n", index, module.Package)
	}
	source.WriteString(")\n\nfunc installedPluginFactories() []coreplugins.Registration {\n\treturn []coreplugins.Registration{\n")
	for index, name := range names {
		module := installed[name]
		defaultConfig, err := json.Marshal(map[string]any{"providers": module.Providers, "values": module.Values})
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(&source, "\t\t{Name: %q, Factory: plugin%d.%s, DefaultConfig: json.RawMessage(%q)},\n", name, index, module.Factory, string(defaultConfig))
	}
	source.WriteString("\t}\n}\n")
	formatted, err := format.Source([]byte(source.String()))
	if err != nil {
		return nil, fmt.Errorf("format plugins_gen.go: %w", err)
	}
	return formatted, nil
}

func normalizedGoVersion(version string) string {
	version = strings.TrimSpace(version)
	if version == "" || version == "latest" {
		return "latest"
	}
	if !strings.HasPrefix(version, "v") {
		return "v" + version
	}
	return version
}

func loadProjectMetadata() (projectMetadata, error) {
	raw, err := os.ReadFile(".procyon.json")
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return projectMetadata{}, errors.New("current directory is not a Procyon project; .procyon.json is missing")
		}
		return projectMetadata{}, err
	}
	var metadata projectMetadata
	if err := json.Unmarshal(raw, &metadata); err != nil {
		return projectMetadata{}, fmt.Errorf("parse .procyon.json: %w", err)
	}
	if metadata.SchemaVersion != 1 {
		return projectMetadata{}, fmt.Errorf("unsupported .procyon.json schema version %d", metadata.SchemaVersion)
	}
	if metadata.Modules == nil {
		metadata.Modules = map[string]InstalledModule{}
	}
	if metadata.ProjectModule == "" {
		metadata.ProjectModule, err = readProjectModule()
		if err != nil {
			return projectMetadata{}, err
		}
	}
	return metadata, nil
}

func readProjectModule() (string, error) {
	raw, err := os.ReadFile("go.mod")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if value, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value), nil
		}
	}
	return "", errors.New("unable to read project module from go.mod")
}

func validateCompatibility(manifest Manifest, metadata projectMetadata) error {
	for label, versions := range map[string][2]string{
		"CLI":  {buildinfo.CLIVersion, manifest.MinimumCLI},
		"core": {metadata.CoreVersion, manifest.MinimumCore},
	} {
		if strings.TrimSpace(versions[1]) == "" {
			continue
		}
		ok, err := versionAtLeast(versions[0], versions[1])
		if err != nil {
			return fmt.Errorf("check %s compatibility: %w", label, err)
		}
		if !ok {
			return fmt.Errorf("module requires %s %s or newer; project has %s", label, versions[1], versions[0])
		}
	}
	return nil
}

func selectProviders(manifest Manifest, raw string) ([]string, map[string]bool, error) {
	if strings.TrimSpace(raw) == "" {
		raw = manifest.DefaultProvider
	}
	providers := make([]string, 0)
	selected := map[string]bool{}
	allowed := map[string]bool{}
	for _, provider := range manifest.Providers {
		allowed[strings.ToLower(strings.TrimSpace(provider))] = true
	}
	for _, provider := range strings.Split(raw, ",") {
		provider = strings.ToLower(strings.TrimSpace(provider))
		if provider == "" || selected[provider] {
			continue
		}
		if len(allowed) > 0 && !allowed[provider] {
			return nil, nil, fmt.Errorf("provider %q is not supported by module %s", provider, manifest.Name)
		}
		selected[provider] = true
		providers = append(providers, provider)
	}
	if len(manifest.Providers) > 0 && len(providers) == 0 {
		return nil, nil, fmt.Errorf("module %s requires --provider (%s)", manifest.Name, strings.Join(manifest.Providers, ", "))
	}
	return providers, selected, nil
}

func backupPaths(paths []string) (map[string]backupFile, error) {
	backup := map[string]backupFile{}
	for _, path := range unique(paths) {
		info, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			backup[path] = backupFile{}
			continue
		}
		if err != nil {
			return nil, err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		backup[path] = backupFile{Body: body, Exists: true, Mode: info.Mode()}
	}
	return backup, nil
}

func restoreBackup(backup map[string]backupFile) {
	for path, file := range backup {
		if !file.Exists {
			_ = os.Remove(path)
			continue
		}
		_ = os.MkdirAll(filepath.Dir(path), 0o755)
		_ = os.WriteFile(path, file.Body, file.Mode)
	}
}

func unique(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func versionAtLeast(current, minimum string) (bool, error) {
	currentParts, err := parseVersion(current)
	if err != nil {
		return false, err
	}
	minimumParts, err := parseVersion(minimum)
	if err != nil {
		return false, err
	}
	for i := range currentParts {
		if currentParts[i] != minimumParts[i] {
			return currentParts[i] > minimumParts[i], nil
		}
	}
	return true, nil
}

func parseVersion(value string) ([3]int, error) {
	var out [3]int
	value = strings.TrimPrefix(strings.TrimSpace(value), "v")
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return out, fmt.Errorf("invalid semantic version %q", value)
	}
	for index, part := range parts {
		number, err := strconv.Atoi(part)
		if err != nil || number < 0 {
			return out, fmt.Errorf("invalid semantic version %q", value)
		}
		out[index] = number
	}
	return out, nil
}
