package plugincreate

import (
	"fmt"
	"strings"
)

func minimalPluginSource(packageName, name string) string {
	return fmt.Sprintf(`package %s

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/bartek5186/procyon-core/authz"
	coreevents "github.com/bartek5186/procyon-core/events"
	coreplugins "github.com/bartek5186/procyon-core/plugins"
)

const Name = %q

type Plugin struct {
	events *coreevents.Bus
}

func New(_ context.Context, dependencies coreplugins.Dependencies, _ json.RawMessage) (coreplugins.Plugin, error) {
	if dependencies.Events == nil {
		return nil, errors.New(Name + " requires the Procyon event bus; update the application runtime wiring")
	}
	return &Plugin{events: dependencies.Events}, nil
}

func (*Plugin) Name() string                      { return Name }
func (*Plugin) Migrate(context.Context) error     { return nil }
func (*Plugin) Policies() []authz.Policy          { return nil }
func (*Plugin) RegisterRoutes(coreplugins.Routes) {}
func (*Plugin) Shutdown(context.Context) error    { return nil }

func (*Plugin) RegisterEvents(eventBus *coreevents.Bus) error {
	return registerEventHandlers(eventBus)
}

var (
	_ coreplugins.Plugin         = (*Plugin)(nil)
	_ coreplugins.EventRegistrar = (*Plugin)(nil)
)
`, packageName, name)
}

func fullPluginSource(packageName, name, module string, controllers []Controller) string {
	var fields, initialization, routes, layerImports strings.Builder
	if len(controllers) > 0 {
		fmt.Fprintf(&layerImports, "\t%q\n", module+"/controllers")
	}
	fmt.Fprintf(&layerImports, "\t%q\n\t%q\n", module+"/services", module+"/store")
	if hasController(controllers, ControllerHello) {
		fields.WriteString("\thelloController *controllers.HelloController\n")
		initialization.WriteString("\t\thelloController: controllers.NewHelloController(service),\n")
		fmt.Fprintf(&routes, `
	if routes.Public != nil {
		routes.Public.GET(%q, p.helloController.Hello)
	}
`, "/"+name+"/hello")
	}
	if hasController(controllers, ControllerExample) {
		fields.WriteString("\texampleController *controllers.ExampleController\n")
		initialization.WriteString("\t\texampleController: controllers.NewExampleController(service),\n")
		fmt.Fprintf(&routes, `
	if routes.Authenticated != nil {
		group := routes.Authenticated.Group(%q)
		if routes.Require != nil {
			group.Use(routes.Require("*", %q, "use"))
		}
		group.POST("", p.exampleController.Create)
		group.GET("", p.exampleController.List)
	}
`, "/"+name+"/examples", name)
	}
	if hasController(controllers, ControllerAdmin) {
		fields.WriteString("\tadminController *controllers.AdminController\n")
		initialization.WriteString("\t\tadminController: controllers.NewAdminController(service),\n")
		fmt.Fprintf(&routes, `
	if routes.Admin != nil {
		routes.Admin.GET(%q, p.adminController.Stats)
	}
`, "/"+name+"/stats")
	}

	policies := "return nil"
	if hasController(controllers, ControllerExample) {
		policies = fmt.Sprintf(`return []authz.Policy{
		{Role: authz.RoleUser, Domain: "*", Object: %q, Action: "use"},
	}`, name)
	}

	return fmt.Sprintf(`package %s

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/bartek5186/procyon-core/authz"
	coreevents "github.com/bartek5186/procyon-core/events"
	coreplugins "github.com/bartek5186/procyon-core/plugins"
%s
)

const Name = %q

type Plugin struct {
%s}

func New(_ context.Context, dependencies coreplugins.Dependencies, raw json.RawMessage) (coreplugins.Plugin, error) {
	if dependencies.DB == nil {
		return nil, errors.New(Name + " requires a database")
	}
	if dependencies.Events == nil {
		return nil, errors.New(Name + " requires the Procyon event bus")
	}
	config, err := parseConfig(raw)
	if err != nil {
		return nil, err
	}
	repository := store.NewExampleStore(dependencies.DB)
	service := services.NewExampleService(repository, config.Greeting)
	return &Plugin{
%s	}, nil
}

func (*Plugin) Name() string                  { return Name }
func (*Plugin) Migrate(context.Context) error { return nil }
func (*Plugin) Policies() []authz.Policy {
	%s
}
func (p *Plugin) RegisterRoutes(routes coreplugins.Routes) {%s}
func (*Plugin) Shutdown(context.Context) error { return nil }

func (*Plugin) RegisterEvents(eventBus *coreevents.Bus) error {
	return registerEventHandlers(eventBus)
}

var (
	_ coreplugins.Plugin            = (*Plugin)(nil)
	_ coreplugins.EventRegistrar    = (*Plugin)(nil)
	_ coreplugins.MigrationProvider = (*Plugin)(nil)
)
`, packageName, layerImports.String(), name,
		fields.String(), initialization.String(), policies, routes.String())
}

func fullBoilerplateFiles(packageName, name, module string, controllers []Controller) map[string]string {
	files := map[string]string{
		"config.go":                configSource(packageName, name),
		"migrations.go":            migrationsSource(packageName, module),
		"models/example.go":        modelsSource(),
		"store/example.go":         storeSource(module),
		"services/example.go":      serviceSource(module, name),
		"services/example_test.go": serviceTestSource(module),
		"contracts/events.go":      contractsSource(name),
		"README.md":                fullReadme(name, controllers),
		"docs/postman/overview.md": postmanOverview(name, controllers),
	}
	if hasController(controllers, ControllerHello) {
		files["controllers/hello.go"] = helloControllerSource(module)
	}
	if hasController(controllers, ControllerExample) {
		files["controllers/example.go"] = exampleControllerSource(module)
	}
	if hasController(controllers, ControllerAdmin) {
		files["controllers/admin.go"] = adminControllerSource(module)
	}
	return files
}

func hasController(controllers []Controller, wanted Controller) bool {
	for _, controller := range controllers {
		if controller == wanted {
			return true
		}
	}
	return false
}

func configSource(packageName, name string) string {
	return fmt.Sprintf(`package %s

import (
	"encoding/json"
	"fmt"
	"strings"
)

type Config struct {
	Greeting string `+"`json:\"greeting\"`"+`
}

func parseConfig(raw json.RawMessage) (Config, error) {
	config := Config{Greeting: %q}
	if len(raw) != 0 {
		if err := json.Unmarshal(raw, &config); err != nil {
			return Config{}, fmt.Errorf("parse %%s config: %%w", Name, err)
		}
	}
	config.Greeting = strings.TrimSpace(config.Greeting)
	if config.Greeting == "" {
		return Config{}, fmt.Errorf("%%s greeting cannot be empty", Name)
	}
	return config, nil
}
`, packageName, "hello from "+name)
}

func migrationsSource(packageName, module string) string {
	return fmt.Sprintf(`package %s

import (
	"context"

	coreplugins "github.com/bartek5186/procyon-core/plugins"
	%q
	"gorm.io/gorm"
)

func (*Plugin) Migrations() []coreplugins.Migration {
	return []coreplugins.Migration{{
		Version: "0001_create_examples",
		Up: func(ctx context.Context, db *gorm.DB) error {
			return db.WithContext(ctx).AutoMigrate(&models.Example{})
		},
	}}
}
`, packageName, module+"/models")
}

func modelsSource() string {
	return `package models

import "time"

type Example struct {
	ID        uint      ` + "`json:\"id\" gorm:\"primaryKey\"`" + `
	CreatedAt time.Time ` + "`json:\"created_at\"`" + `
	UpdatedAt time.Time ` + "`json:\"updated_at\"`" + `
	Name      string    ` + "`json:\"name\" gorm:\"size:120;not null\"`" + `
}

type CreateExampleInput struct {
	Name string ` + "`json:\"name\"`" + `
}

type HelloResponse struct {
	Message string ` + "`json:\"message\"`" + `
	Module  string ` + "`json:\"module\"`" + `
}

type StatsResponse struct {
	Examples int64 ` + "`json:\"examples\"`" + `
}
`
}

func storeSource(module string) string {
	return fmt.Sprintf(`package store

import (
	"context"

	%q
	"gorm.io/gorm"
)

type ExampleStore struct { db *gorm.DB }

func NewExampleStore(db *gorm.DB) *ExampleStore { return &ExampleStore{db: db} }

func (s *ExampleStore) Create(ctx context.Context, example *models.Example) error {
	return s.db.WithContext(ctx).Create(example).Error
}

func (s *ExampleStore) List(ctx context.Context) ([]models.Example, error) {
	var examples []models.Example
	return examples, s.db.WithContext(ctx).Order("id DESC").Find(&examples).Error
}

func (s *ExampleStore) Count(ctx context.Context) (int64, error) {
	var count int64
	err := s.db.WithContext(ctx).Model(&models.Example{}).Count(&count).Error
	return count, err
}
`, module+"/models")
}

func serviceSource(module, name string) string {
	return fmt.Sprintf(`package services

import (
	"context"
	"errors"
	"strings"

	%q
)

type ExampleRepository interface {
	Create(context.Context, *models.Example) error
	List(context.Context) ([]models.Example, error)
	Count(context.Context) (int64, error)
}

type ExampleService struct {
	repository ExampleRepository
	greeting   string
}

func NewExampleService(repository ExampleRepository, greeting string) *ExampleService {
	return &ExampleService{repository: repository, greeting: greeting}
}

func (s *ExampleService) Hello() models.HelloResponse {
	return models.HelloResponse{Message: s.greeting, Module: %q}
}

func (s *ExampleService) Create(ctx context.Context, input models.CreateExampleInput) (*models.Example, error) {
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		return nil, errors.New("name is required")
	}
	example := &models.Example{Name: input.Name}
	if err := s.repository.Create(ctx, example); err != nil {
		return nil, err
	}
	return example, nil
}

func (s *ExampleService) List(ctx context.Context) ([]models.Example, error) {
	return s.repository.List(ctx)
}

func (s *ExampleService) Stats(ctx context.Context) (models.StatsResponse, error) {
	count, err := s.repository.Count(ctx)
	return models.StatsResponse{Examples: count}, err
}
`, module+"/models", name)
}

func helloControllerSource(module string) string {
	return fmt.Sprintf(`package controllers

import (
	"net/http"

	%q
	%q
	"github.com/labstack/echo/v4"
)

type helloService interface { Hello() models.HelloResponse }

type HelloController struct { service helloService }

func NewHelloController(service *services.ExampleService) *HelloController {
	return &HelloController{service: service}
}

func (c *HelloController) Hello(ctx echo.Context) error {
	return ctx.JSON(http.StatusOK, c.service.Hello())
}
`, module+"/models", module+"/services")
}

func exampleControllerSource(module string) string {
	return fmt.Sprintf(`package controllers

import (
	"context"
	"net/http"

	%q
	%q
	"github.com/labstack/echo/v4"
)

type exampleService interface {
	Create(context.Context, models.CreateExampleInput) (*models.Example, error)
	List(context.Context) ([]models.Example, error)
}

type ExampleController struct { service exampleService }

func NewExampleController(service *services.ExampleService) *ExampleController {
	return &ExampleController{service: service}
}

func (c *ExampleController) Create(ctx echo.Context) error {
	var input models.CreateExampleInput
	if err := ctx.Bind(&input); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid payload")
	}
	example, err := c.service.Create(ctx.Request().Context(), input)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	return ctx.JSON(http.StatusCreated, example)
}

func (c *ExampleController) List(ctx echo.Context) error {
	examples, err := c.service.List(ctx.Request().Context())
	if err != nil {
		return err
	}
	return ctx.JSON(http.StatusOK, examples)
}
`, module+"/models", module+"/services")
}

func adminControllerSource(module string) string {
	return fmt.Sprintf(`package controllers

import (
	"context"
	"net/http"

	%q
	%q
	"github.com/labstack/echo/v4"
)

type statsService interface { Stats(context.Context) (models.StatsResponse, error) }

type AdminController struct { service statsService }

func NewAdminController(service *services.ExampleService) *AdminController {
	return &AdminController{service: service}
}

func (c *AdminController) Stats(ctx echo.Context) error {
	stats, err := c.service.Stats(ctx.Request().Context())
	if err != nil {
		return err
	}
	return ctx.JSON(http.StatusOK, stats)
}
`, module+"/models", module+"/services")
}

func serviceTestSource(module string) string {
	return fmt.Sprintf(`package services

import (
	"context"
	"testing"

	%q
)

type exampleRepositoryStub struct { created *models.Example }

func (s *exampleRepositoryStub) Create(_ context.Context, example *models.Example) error { s.created = example; return nil }
func (*exampleRepositoryStub) List(context.Context) ([]models.Example, error) { return nil, nil }
func (*exampleRepositoryStub) Count(context.Context) (int64, error) { return 0, nil }

func TestExampleServiceCreate(t *testing.T) {
	repository := &exampleRepositoryStub{}
	service := NewExampleService(repository, "hello")
	created, err := service.Create(context.Background(), models.CreateExampleInput{Name: " first "})
	if err != nil { t.Fatal(err) }
	if created.Name != "first" || repository.created != created {
		t.Fatalf("unexpected example: %%+v", created)
	}
}
`, module+"/models")
}

func contractsSource(name string) string {
	return fmt.Sprintf(`package contracts

import coreevents "github.com/bartek5186/procyon-core/events"

type ExampleCreated struct {
	ID   uint   `+"`json:\"id\"`"+`
	Name string `+"`json:\"name\"`"+`
}

const ExampleCreatedTopic coreevents.Topic[ExampleCreated] = %q
`, name+".example.created.v1")
}

func fullReadme(name string, controllers []Controller) string {
	return fmt.Sprintf(`# %s

Complete Procyon plugin boilerplate with configuration, a versioned example migration, model, repository, service, events and tests.

## Generated routes

%s

Replace the example domain with real business logic, keep migration versions immutable after release, and document public contracts before publishing the module.
`, name, routeDocumentation(name, controllers))
}

func postmanOverview(name string, controllers []Controller) string {
	return fmt.Sprintf("The `%s` plugin includes runnable example endpoints. Replace their payloads and examples as the domain evolves.\n\n%s\n", name, routeDocumentation(name, controllers))
}

func routeDocumentation(name string, controllers []Controller) string {
	var routes []string
	if hasController(controllers, ControllerHello) {
		routes = append(routes, "- `GET /"+name+"/hello` — public hello endpoint")
	}
	if hasController(controllers, ControllerExample) {
		routes = append(routes, "- `POST /"+name+"/examples` and `GET /"+name+"/examples` — authenticated example CRUD")
	}
	if hasController(controllers, ControllerAdmin) {
		routes = append(routes, "- `GET /"+name+"/stats` — admin statistics")
	}
	if len(routes) == 0 {
		return "No HTTP controllers were selected."
	}
	return strings.Join(routes, "\n")
}
