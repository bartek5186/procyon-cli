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
	files := generatedFiles(name, packageName, module)
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
	fmt.Printf("route: GET /v1/%s\n", name)
	fmt.Println("next: replace the status scaffold with the plugin's business logic")
	return nil
}

func generatedFiles(name, packageName, module string) map[string]string {
	plugin := fmt.Sprintf(`package %s

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/bartek5186/procyon-core/authz"
	coreevents "github.com/bartek5186/procyon-core/events"
	coreplugins "github.com/bartek5186/procyon-core/plugins"
	%q
	%q
	%q
)

const Name = %q

type Plugin struct {
	dependencies coreplugins.Dependencies
	config       Config
	events       *coreevents.Bus
	capabilities *coreplugins.CapabilityRegistry
	controller   *controllers.Controller
}

func New(_ context.Context, dependencies coreplugins.Dependencies, raw json.RawMessage) (coreplugins.Plugin, error) {
	config, err := parseConfig(raw)
	if err != nil {
		return nil, fmt.Errorf("configure %%s: %%w", Name, err)
	}
	pluginStore := store.NewStore(config.StatusMessage)
	pluginService := services.NewService(pluginStore)
	return &Plugin{
		dependencies: dependencies, config: config, events: dependencies.Events, capabilities: dependencies.Capabilities,
		controller: controllers.NewController(pluginService),
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
`, packageName, module+"/plugins/"+name+"/controllers", module+"/plugins/"+name+"/services", module+"/plugins/"+name+"/store", name)

	config := fmt.Sprintf(`package %s

import (
	"encoding/json"
	"strings"
)

type Config struct {
	Enabled       *bool  `+"`json:\"enabled\"`"+`
	StatusMessage string `+"`json:\"status_message\"`"+`
}

func parseConfig(raw json.RawMessage) (Config, error) {
	config := Config{StatusMessage: %q}
	if len(raw) != 0 {
		if err := json.Unmarshal(raw, &config); err != nil {
			return Config{}, err
		}
	}
	config.StatusMessage = strings.TrimSpace(config.StatusMessage)
	if config.StatusMessage == "" {
		config.StatusMessage = %q
	}
	return config, nil
}
`, packageName, name+" is running", name+" is running")

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
	if routes.Public != nil {
		routes.Public.GET(%q, p.controller.Status)
	}
}
`, packageName, "/"+name),
		"start.go": fmt.Sprintf(`package %s

import "context"

func (p *Plugin) Start(ctx context.Context) error {
	_ = p
	_ = ctx
	return nil
}
`, packageName),
		"models/models.go": `package models

type StatusResponse struct {
	Plugin string ` + "`json:\"plugin\"`" + `
	Status string ` + "`json:\"status\"`" + `
}
`,
		"store/store.go": `package store

import "context"

type Store struct { status string }

func NewStore(status string) *Store { return &Store{status: status} }

func (s *Store) Status(context.Context) (string, error) { return s.status, nil }
`,
		"services/service.go": fmt.Sprintf(`package services

import (
	"context"

	%q
	%q
)

type Service struct { store *store.Store }

func NewService(store *store.Store) *Service { return &Service{store: store} }

func (s *Service) Status(ctx context.Context) (models.StatusResponse, error) {
	status, err := s.store.Status(ctx)
	return models.StatusResponse{Plugin: %q, Status: status}, err
}
`, module+"/plugins/"+name+"/models", module+"/plugins/"+name+"/store", name),
		"services/service_test.go": fmt.Sprintf(`package services

import (
	"context"
	"testing"

	%q
)

func TestStatusReturnsStoreValue(t *testing.T) {
	service := NewService(store.NewStore("ready"))
	response, err := service.Status(context.Background())
	if err != nil { t.Fatal(err) }
	if response.Status != "ready" || response.Plugin != %q {
		t.Fatalf("unexpected response: %%+v", response)
	}
}
`, module+"/plugins/"+name+"/store", name),
		"controllers/controller.go": fmt.Sprintf(`package controllers

import (
	"net/http"

	%q
	"github.com/labstack/echo/v4"
)

type Controller struct { service *services.Service }

func NewController(service *services.Service) *Controller {
	return &Controller{service: service}
}

func (c *Controller) Status(ctx echo.Context) error {
	response, err := c.service.Status(ctx.Request().Context())
	if err != nil { return err }
	return ctx.JSON(http.StatusOK, response)
}
`, module+"/plugins/"+name+"/services"),
		"docs/postman/overview.md": fmt.Sprintf("The `%s` plugin is private to this project. Its initial status endpoint is `GET /%s`.\n", name, name),
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
