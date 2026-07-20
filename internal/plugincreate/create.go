package plugincreate

import (
	"encoding/json"
	"errors"
	"fmt"
	"go/format"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

const initialVersion = "0.1.0"

var pluginNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

type Options struct {
	Name        string
	GoModule    string
	OutputDir   string
	CoreVersion string
	CLIVersion  string
	Minimal     bool
	Controllers []Controller
}

type Controller string

const (
	ControllerStatus  Controller = "status"
	ControllerRecords Controller = "records"
	ControllerAdmin   Controller = "admin"
)

var defaultControllers = []Controller{ControllerStatus, ControllerRecords, ControllerAdmin}

type Result struct {
	Root     string
	Name     string
	GoModule string
}

type manifest struct {
	SchemaVersion int    `json:"schema_version"`
	Kind          string `json:"kind"`
	Name          string `json:"name"`
	Version       string `json:"version"`
	Description   string `json:"description"`
	MinimumCLI    string `json:"minimum_cli"`
	MinimumCore   string `json:"minimum_core"`
	GoModule      string `json:"go_module"`
	Package       string `json:"package"`
	Factory       string `json:"factory"`
}

func Create(opts Options) (Result, error) {
	opts.Name = strings.TrimSpace(opts.Name)
	opts.GoModule = strings.TrimSpace(opts.GoModule)
	opts.OutputDir = strings.TrimSpace(opts.OutputDir)
	if !pluginNamePattern.MatchString(opts.Name) {
		return Result{}, errors.New("plugin name must use kebab-case and start with a letter")
	}
	if opts.GoModule == "" || strings.ContainsAny(opts.GoModule, " \t\r\n") {
		return Result{}, errors.New("Go module path is required")
	}
	if opts.OutputDir == "" {
		return Result{}, errors.New("output directory is required")
	}
	if strings.TrimSpace(opts.CoreVersion) == "" || opts.CoreVersion == "unknown" {
		return Result{}, errors.New("cannot create a plugin without a known Procyon Core version")
	}
	controllers, err := normalizeControllers(opts.Controllers, opts.Minimal)
	if err != nil {
		return Result{}, err
	}
	root, err := filepath.Abs(opts.OutputDir)
	if err != nil {
		return Result{}, err
	}
	if _, err := os.Stat(root); err == nil {
		return Result{}, fmt.Errorf("output directory %s already exists", opts.OutputDir)
	} else if !errors.Is(err, os.ErrNotExist) {
		return Result{}, err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return Result{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(root)
		}
	}()

	manifestBody, err := json.MarshalIndent(manifest{
		SchemaVersion: 1,
		Kind:          "go-plugin",
		Name:          opts.Name,
		Version:       initialVersion,
		Description:   "Procyon plugin " + opts.Name,
		MinimumCLI:    cleanVersion(opts.CLIVersion),
		MinimumCore:   cleanVersion(opts.CoreVersion),
		GoModule:      opts.GoModule,
		Package:       opts.GoModule,
		Factory:       "New",
	}, "", "  ")
	if err != nil {
		return Result{}, err
	}
	manifestBody = append(manifestBody, '\n')

	packageName := goPackageName(opts.Name)
	pluginSource := minimalPluginSource(packageName, opts.Name)
	if !opts.Minimal {
		pluginSource = fullPluginSource(packageName, opts.Name, opts.GoModule, controllers)
	}
	pluginBody, err := format.Source([]byte(pluginSource))
	if err != nil {
		return Result{}, fmt.Errorf("generate plugin.go: %w", err)
	}

	files := map[string][]byte{
		"go.mod":              []byte(pluginGoMod(opts, !opts.Minimal)),
		"procyon-module.json": manifestBody,
		"plugin.go":           pluginBody,
		"events.go": []byte(fmt.Sprintf(`package %s

import coreevents "github.com/bartek5186/procyon-core/events"

// registerEventHandlers installs this plugin's synchronous event consumers.
// Keep handlers fast and idempotent; return an error to make a durable
// publisher retry the source event.
func registerEventHandlers(eventBus *coreevents.Bus) error {
	_ = eventBus
	// procyon:event-handlers
	return nil
}

`, packageName)),
		"README.md": []byte(fmt.Sprintf(
			"# %s\n\nStandalone Procyon plugin scaffold. Add business logic, routes, policies, migrations and typed event handlers in this module. Register handlers in `events.go`; publish through the shared bus stored by the plugin.\n",
			opts.Name,
		)),
		"docs/postman/overview.md": []byte(fmt.Sprintf(
			"The `%s` plugin extends this API. Document its purpose, trust boundaries and end-to-end flow here.\n\n## Flow\n\n1. Describe how the client starts the operation.\n2. Describe server-side validation and persistence.\n3. Describe asynchronous events and the final observable result.\n",
			opts.Name,
		)),
	}
	if !opts.Minimal {
		for name, body := range fullBoilerplateFiles(packageName, opts.Name, opts.GoModule, controllers) {
			files[name] = []byte(body)
		}
	}
	for name, body := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return Result{}, err
		}
		if strings.HasSuffix(name, ".go") {
			body, err = format.Source(body)
			if err != nil {
				return Result{}, fmt.Errorf("format %s: %w", name, err)
			}
		}
		if err := os.WriteFile(path, body, 0o644); err != nil {
			return Result{}, err
		}
	}
	committed = true
	return Result{Root: root, Name: opts.Name, GoModule: opts.GoModule}, nil
}

// Prepare resolves and records the dependencies of a generated standalone
// module. GOWORK is disabled so a parent Procyon workspace cannot accidentally
// make the new module appear complete without its own go.sum file.
func Prepare(root string) error {
	command := exec.Command("go", "mod", "tidy")
	command.Dir = root
	command.Env = append(os.Environ(), "GOWORK=off")
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("prepare generated module: go mod tidy: %w\n%s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func normalizeControllers(values []Controller, minimal bool) ([]Controller, error) {
	if minimal {
		return nil, nil
	}
	if values == nil {
		return append([]Controller(nil), defaultControllers...), nil
	}
	seen := make(map[Controller]bool, len(values))
	result := make([]Controller, 0, len(values))
	for _, value := range values {
		switch value {
		case ControllerStatus, ControllerRecords, ControllerAdmin:
		default:
			return nil, fmt.Errorf("unknown controller %q", value)
		}
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result, nil
}

func pluginGoMod(opts Options, full bool) string {
	if !full {
		return fmt.Sprintf("module %s\n\ngo 1.26.0\n\nrequire github.com/bartek5186/procyon-core %s\n", opts.GoModule, normalizeVersion(opts.CoreVersion))
	}
	return fmt.Sprintf(`module %s

go 1.26.0

require (
	github.com/bartek5186/procyon-core %s
	github.com/labstack/echo/v4 v4.13.4
	gorm.io/gorm v1.31.1
)
`, opts.GoModule, normalizeVersion(opts.CoreVersion))
}

func goPackageName(name string) string {
	return strings.ReplaceAll(name, "-", "")
}

func cleanVersion(version string) string {
	return strings.TrimPrefix(strings.TrimSpace(version), "v")
}

func normalizeVersion(version string) string {
	version = strings.TrimSpace(version)
	if strings.HasPrefix(version, "v") {
		return version
	}
	return "v" + version
}
