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
	fmt.Printf("route: GET /v1/%s/hello\n", name)
	fmt.Println("next: replace the hello example with the plugin's business logic")
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
	hello        *controllers.HelloController
}

func New(_ context.Context, dependencies coreplugins.Dependencies, raw json.RawMessage) (coreplugins.Plugin, error) {
	config, err := parseConfig(raw)
	if err != nil {
		return nil, fmt.Errorf("configure %%s: %%w", Name, err)
	}
	helloStore := store.NewHelloStore(config.Greeting)
	helloService := services.NewHelloService(helloStore)
	return &Plugin{
		dependencies: dependencies, config: config, events: dependencies.Events, capabilities: dependencies.Capabilities,
		hello: controllers.NewHelloController(helloService),
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
	Enabled  *bool  `+"`json:\"enabled\"`"+`
	Greeting string `+"`json:\"greeting\"`"+`
}

func parseConfig(raw json.RawMessage) (Config, error) {
	config := Config{Greeting: %q}
	if len(raw) != 0 {
		if err := json.Unmarshal(raw, &config); err != nil {
			return Config{}, err
		}
	}
	config.Greeting = strings.TrimSpace(config.Greeting)
	if config.Greeting == "" {
		config.Greeting = %q
	}
	return config, nil
}
`, packageName, "hello from "+name, "hello from "+name)

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
		routes.Public.GET(%q, p.hello.Hello)
	}
}
`, packageName, "/"+name+"/hello"),
		"start.go": fmt.Sprintf(`package %s

import "context"

func (p *Plugin) Start(ctx context.Context) error {
	_ = p
	_ = ctx
	return nil
}
`, packageName),
		"contracts/hello.go": fmt.Sprintf(`package contracts

const HelloRoute = %q
`, "/"+name+"/hello"),
		"models/hello.go": `package models

type HelloResponse struct {
	Message string ` + "`json:\"message\"`" + `
	Plugin  string ` + "`json:\"plugin\"`" + `
}
`,
		"store/hello.go": fmt.Sprintf(`package store

import "context"

type HelloStore struct { message string }

func NewHelloStore(message string) *HelloStore { return &HelloStore{message: message} }

func (s *HelloStore) Message(context.Context) (string, error) { return s.message, nil }
`),
		"services/hello.go": fmt.Sprintf(`package services

import (
	"context"

	%q
	%q
)

type HelloService struct { store *store.HelloStore }

func NewHelloService(store *store.HelloStore) *HelloService { return &HelloService{store: store} }

func (s *HelloService) Hello(ctx context.Context) (models.HelloResponse, error) {
	message, err := s.store.Message(ctx)
	return models.HelloResponse{Message: message, Plugin: %q}, err
}
`, module+"/plugins/"+name+"/models", module+"/plugins/"+name+"/store", name),
		"services/hello_test.go": fmt.Sprintf(`package services

import (
	"context"
	"testing"

	%q
)

func TestHelloReturnsStoreMessage(t *testing.T) {
	service := NewHelloService(store.NewHelloStore("hello test"))
	response, err := service.Hello(context.Background())
	if err != nil { t.Fatal(err) }
	if response.Message != "hello test" || response.Plugin != %q {
		t.Fatalf("unexpected response: %%+v", response)
	}
}
`, module+"/plugins/"+name+"/store", name),
		"controllers/hello.go": fmt.Sprintf(`package controllers

import (
	"net/http"

	%q
	"github.com/labstack/echo/v4"
)

type HelloController struct { service *services.HelloService }

func NewHelloController(service *services.HelloService) *HelloController {
	return &HelloController{service: service}
}

func (c *HelloController) Hello(ctx echo.Context) error {
	response, err := c.service.Hello(ctx.Request().Context())
	if err != nil { return err }
	return ctx.JSON(http.StatusOK, response)
}
`, module+"/plugins/"+name+"/services"),
		"docs/postman/overview.md": fmt.Sprintf("The `%s` plugin is private to this project. Its runnable example endpoint is `GET /%s/hello`.\n", name, name),
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
