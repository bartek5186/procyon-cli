package localplugin

import (
	"errors"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var namePattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

type Options struct {
	Name string
}

func Create(opts Options) error {
	name := strings.TrimSpace(opts.Name)
	if !namePattern.MatchString(name) {
		return errors.New("plugin name must use kebab-case and start with a letter")
	}
	module, err := readGoModule("go.mod")
	if err != nil {
		return err
	}
	if _, err := os.Stat("plugins_local.go"); err != nil {
		if os.IsNotExist(err) {
			return errors.New("current directory is not a current Procyon project; plugins_local.go is missing")
		}
		return err
	}

	root := filepath.Join("plugins", name)
	if _, err := os.Stat(root); err == nil {
		return fmt.Errorf("local plugin %q already exists", name)
	} else if !os.IsNotExist(err) {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(root)
		}
	}()

	packageName := strings.ReplaceAll(name, "-", "")
	files := generatedFiles(name, packageName)
	for path, body := range files {
		fullPath := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			return err
		}
		if strings.HasSuffix(path, ".go") {
			formatted, err := format.Source([]byte(body))
			if err != nil {
				return fmt.Errorf("format %s: %w", fullPath, err)
			}
			body = string(formatted)
		}
		if err := os.WriteFile(fullPath, []byte(body), 0o644); err != nil {
			return err
		}
	}

	if err := wireRegistration(name, module, packageName); err != nil {
		return err
	}
	committed = true
	fmt.Printf("created local plugin %s in %s\n", name, root)
	fmt.Println("next: implement its capabilities, migrations, events, routes and tests")
	return nil
}

func generatedFiles(name, packageName string) map[string]string {
	plugin := fmt.Sprintf(`package %s

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/bartek5186/procyon-core/authz"
	coreevents "github.com/bartek5186/procyon-core/events"
	coreplugins "github.com/bartek5186/procyon-core/plugins"
)

const Name = %q

type Plugin struct {
	dependencies coreplugins.Dependencies
	config       Config
	events       *coreevents.Bus
	capabilities *coreplugins.CapabilityRegistry
}

func New(_ context.Context, dependencies coreplugins.Dependencies, raw json.RawMessage) (coreplugins.Plugin, error) {
	config, err := parseConfig(raw)
	if err != nil {
		return nil, fmt.Errorf("configure %%s: %%w", Name, err)
	}
	return &Plugin{
		dependencies: dependencies, config: config, events: dependencies.Events, capabilities: dependencies.Capabilities,
	}, nil
}

func (*Plugin) Name() string                      { return Name }
func (*Plugin) Requires() []string                { return nil }
func (*Plugin) Policies() []authz.Policy          { return nil }
func (*Plugin) Shutdown(context.Context) error    { return nil }
func (*Plugin) Migrate(context.Context) error     { return nil }

var (
	_ coreplugins.Plugin              = (*Plugin)(nil)
	_ coreplugins.DependencyDeclarer  = (*Plugin)(nil)
	_ coreplugins.CapabilityRegistrar = (*Plugin)(nil)
	_ coreplugins.EventRegistrar      = (*Plugin)(nil)
	_ coreplugins.MigrationProvider   = (*Plugin)(nil)
	_ coreplugins.Starter             = (*Plugin)(nil)
)
`, packageName, name)

	config := fmt.Sprintf(`package %s

import "encoding/json"

type Config struct {
	Enabled *bool `+"`json:\"enabled\"`"+`
}

func parseConfig(raw json.RawMessage) (Config, error) {
	config := Config{}
	if len(raw) != 0 {
		if err := json.Unmarshal(raw, &config); err != nil {
			return Config{}, err
		}
	}
	return config, nil
}
`, packageName)

	return map[string]string{
		"plugin.go": plugin,
		"config.go": config,
		"capabilities.go": fmt.Sprintf(`package %s

import coreplugins "github.com/bartek5186/procyon-core/plugins"

func (p *Plugin) RegisterCapabilities(registry *coreplugins.CapabilityRegistry) error {
	_ = p
	_ = registry
	return nil
}
`, packageName),
		"events.go": fmt.Sprintf(`package %s

import coreevents "github.com/bartek5186/procyon-core/events"

func (p *Plugin) RegisterEvents(eventBus *coreevents.Bus) error {
	_ = p
	_ = eventBus
	return nil
}
`, packageName),
		"migrations.go": fmt.Sprintf(`package %s

import coreplugins "github.com/bartek5186/procyon-core/plugins"

func (*Plugin) Migrations() []coreplugins.Migration { return nil }
`, packageName),
		"routes.go": fmt.Sprintf(`package %s

import coreplugins "github.com/bartek5186/procyon-core/plugins"

func (p *Plugin) RegisterRoutes(routes coreplugins.Routes) {
	_ = p
	_ = routes
}
`, packageName),
		"start.go": fmt.Sprintf(`package %s

import "context"

func (p *Plugin) Start(ctx context.Context) error {
	_ = p
	_ = ctx
	return nil
}
`, packageName),
		"contracts/doc.go":         "// Package contracts contains stable commands, events and outputs owned by the plugin.\npackage contracts\n",
		"domain/doc.go":            "// Package domain contains persistence-independent business rules.\npackage domain\n",
		"models/doc.go":            "// Package models contains plugin-owned persistence models and DTOs.\npackage models\n",
		"store/doc.go":             "// Package store contains plugin database access.\npackage store\n",
		"services/doc.go":          "// Package services contains plugin use cases and workers.\npackage services\n",
		"controllers/doc.go":       "// Package controllers contains plugin HTTP handlers.\npackage controllers\n",
		"docs/postman/overview.md": fmt.Sprintf("The `%s` plugin is private to this project. Document its API and lifecycle here.\n", name),
	}
}

func wireRegistration(name, module, packageName string) error {
	path := "plugins_local.go"
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	source := string(raw)
	if strings.Contains(source, fmt.Sprintf("Name: %q", name)) {
		return fmt.Errorf("local plugin %q is already registered", name)
	}
	importMarker := "\t// procyon:local-plugin-imports"
	registrationMarker := "\t\t// procyon:local-plugin-registrations"
	if !strings.Contains(source, importMarker) || !strings.Contains(source, registrationMarker) {
		return errors.New("plugins_local.go does not contain local plugin generator markers")
	}
	alias := "localplugin" + packageName
	source = strings.Replace(source, importMarker, fmt.Sprintf("\t%s %q\n%s", alias, module+"/plugins/"+name, importMarker), 1)
	source = strings.Replace(source, registrationMarker, fmt.Sprintf("\t\t{Name: %q, Factory: %s.New},\n%s", name, alias, registrationMarker), 1)
	formatted, err := format.Source([]byte(source))
	if err != nil {
		return fmt.Errorf("format plugins_local.go: %w", err)
	}
	return os.WriteFile(path, formatted, 0o644)
}

func readGoModule(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if value, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
			if value = strings.TrimSpace(value); value != "" {
				return value, nil
			}
		}
	}
	return "", errors.New("unable to read module path from go.mod")
}
