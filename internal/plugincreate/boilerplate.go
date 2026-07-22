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

type Plugin struct { events *coreevents.Bus }

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
func (*Plugin) RegisterEvents(eventBus *coreevents.Bus) error { return registerEventHandlers(eventBus) }

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
		fields.WriteString("\tcontroller *controllers.Controller\n")
		initialization.WriteString("\t\tcontroller: controllers.NewController(service),\n")
	}
	fmt.Fprintf(&layerImports, "\t%q\n\t%q\n", module+"/services", module+"/store")

	if hasController(controllers, ControllerStatus) {
		fmt.Fprintf(&routes, `
	if routes.Public != nil {
		routes.Public.GET(%q, p.controller.Status)
	}
`, "/"+name)
	}
	if hasController(controllers, ControllerRecords) {
		fmt.Fprintf(&routes, `
	if routes.Authenticated != nil {
		group := routes.Authenticated.Group(%q)
		if routes.Require != nil {
			group.Use(routes.Require("*", %q, "use"))
		}
		group.POST("", p.controller.Create)
		group.GET("", p.controller.List)
	}
`, "/"+name+"/records", name)
	}
	if hasController(controllers, ControllerAdmin) {
		fmt.Fprintf(&routes, `
	if routes.Operations != nil {
		routes.Operations.GET(%q, p.controller.Stats)
	}
`, "/"+name+"/stats")
	}

	policies := "return nil"
	if hasController(controllers, ControllerRecords) {
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
	if dependencies.DB == nil { return nil, errors.New(Name + " requires a database") }
	if dependencies.Events == nil { return nil, errors.New(Name + " requires the Procyon event bus") }
	config, err := parseConfig(raw)
	if err != nil { return nil, err }
	repository := store.NewStore(dependencies.DB)
	service := services.NewService(repository, config.StatusMessage)
	return &Plugin{
%s	}, nil
}

func (*Plugin) Name() string                  { return Name }
func (*Plugin) Migrate(context.Context) error { return nil }
func (*Plugin) Policies() []authz.Policy { %s }
func (p *Plugin) RegisterRoutes(routes coreplugins.Routes) {%s}
func (*Plugin) Shutdown(context.Context) error { return nil }
func (*Plugin) RegisterEvents(eventBus *coreevents.Bus) error { return registerEventHandlers(eventBus) }

var (
	_ coreplugins.Plugin            = (*Plugin)(nil)
	_ coreplugins.EventRegistrar    = (*Plugin)(nil)
	_ coreplugins.MigrationProvider = (*Plugin)(nil)
)
`, packageName, layerImports.String(), name, fields.String(), initialization.String(), policies, routes.String())
}

func fullBoilerplateFiles(packageName, name, module string, controllers []Controller) map[string]string {
	files := map[string]string{
		"config.go":                configSource(packageName, name),
		"migrations.go":            migrationsSource(packageName, module),
		"models/models.go":         modelsSource(),
		"store/store.go":           storeSource(module),
		"services/service.go":      serviceSource(module, name),
		"services/service_test.go": serviceTestSource(module),
		"contracts/events.go":      contractsSource(name),
		"README.md":                fullReadme(name, controllers),
		"docs/postman/overview.md": postmanOverview(name, controllers),
	}
	if len(controllers) > 0 {
		files["controllers/controller.go"] = controllerSource(module)
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

type Config struct { StatusMessage string `+"`json:\"status_message\"`"+` }

func parseConfig(raw json.RawMessage) (Config, error) {
	config := Config{StatusMessage: %q}
	if len(raw) != 0 {
		if err := json.Unmarshal(raw, &config); err != nil { return Config{}, fmt.Errorf("parse %%s config: %%w", Name, err) }
	}
	config.StatusMessage = strings.TrimSpace(config.StatusMessage)
	if config.StatusMessage == "" { return Config{}, fmt.Errorf("%%s status message cannot be empty", Name) }
	return config, nil
}
`, packageName, name+" is running")
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
		Version: "0001_create_records",
		Up: func(ctx context.Context, db *gorm.DB) error {
			return db.WithContext(ctx).AutoMigrate(&models.Record{})
		},
	}}
}
`, packageName, module+"/models")
}

func modelsSource() string {
	return `package models

import "time"

// Record is a neutral starter persistence model. Rename it when the plugin acquires
// its real domain language; avoid using transport DTOs as database models.
type Record struct {
	ID        uint      ` + "`json:\"id\" gorm:\"primaryKey\"`" + `
	CreatedAt time.Time ` + "`json:\"created_at\"`" + `
	UpdatedAt time.Time ` + "`json:\"updated_at\"`" + `
	Name      string    ` + "`json:\"name\" gorm:\"size:120;not null\"`" + `
}

type CreateRecordInput struct { Name string ` + "`json:\"name\"`" + ` }
type StatusResponse struct {
	Plugin string ` + "`json:\"plugin\"`" + `
	Status string ` + "`json:\"status\"`" + `
}
type StatsResponse struct { Records int64 ` + "`json:\"records\"`" + ` }
`
}

func storeSource(module string) string {
	return fmt.Sprintf(`package store

import (
	"context"
	%q
	"gorm.io/gorm"
)

type Store struct { db *gorm.DB }
func NewStore(db *gorm.DB) *Store { return &Store{db: db} }
func (s *Store) Create(ctx context.Context, record *models.Record) error {
	return s.db.WithContext(ctx).Create(record).Error
}
func (s *Store) List(ctx context.Context) ([]models.Record, error) {
	var records []models.Record
	return records, s.db.WithContext(ctx).Order("id DESC").Find(&records).Error
}
func (s *Store) Count(ctx context.Context) (int64, error) {
	var count int64
	err := s.db.WithContext(ctx).Model(&models.Record{}).Count(&count).Error
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

type Repository interface {
	Create(context.Context, *models.Record) error
	List(context.Context) ([]models.Record, error)
	Count(context.Context) (int64, error)
}
type Service struct {
	repository Repository
	statusMessage string
}
func NewService(repository Repository, statusMessage string) *Service {
	return &Service{repository: repository, statusMessage: statusMessage}
}
func (s *Service) Status() models.StatusResponse {
	return models.StatusResponse{Plugin: %q, Status: s.statusMessage}
}
func (s *Service) Create(ctx context.Context, input models.CreateRecordInput) (*models.Record, error) {
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" { return nil, errors.New("name is required") }
	record := &models.Record{Name: input.Name}
	if err := s.repository.Create(ctx, record); err != nil { return nil, err }
	return record, nil
}
func (s *Service) List(ctx context.Context) ([]models.Record, error) { return s.repository.List(ctx) }
func (s *Service) Stats(ctx context.Context) (models.StatsResponse, error) {
	count, err := s.repository.Count(ctx)
	return models.StatsResponse{Records: count}, err
}
`, module+"/models", name)
}

func controllerSource(module string) string {
	return fmt.Sprintf(`package controllers

import (
	"context"
	"net/http"
	%q
	%q
	"github.com/labstack/echo/v4"
)

type service interface {
	Status() models.StatusResponse
	Create(context.Context, models.CreateRecordInput) (*models.Record, error)
	List(context.Context) ([]models.Record, error)
	Stats(context.Context) (models.StatsResponse, error)
}
type Controller struct { service service }
func NewController(service *services.Service) *Controller { return &Controller{service: service} }
func (c *Controller) Status(ctx echo.Context) error { return ctx.JSON(http.StatusOK, c.service.Status()) }
func (c *Controller) Create(ctx echo.Context) error {
	var input models.CreateRecordInput
	if err := ctx.Bind(&input); err != nil { return echo.NewHTTPError(http.StatusBadRequest, "invalid payload") }
	record, err := c.service.Create(ctx.Request().Context(), input)
	if err != nil { return echo.NewHTTPError(http.StatusBadRequest, err.Error()) }
	return ctx.JSON(http.StatusCreated, record)
}
func (c *Controller) List(ctx echo.Context) error {
	records, err := c.service.List(ctx.Request().Context())
	if err != nil { return err }
	return ctx.JSON(http.StatusOK, records)
}
func (c *Controller) Stats(ctx echo.Context) error {
	stats, err := c.service.Stats(ctx.Request().Context())
	if err != nil { return err }
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

type repositoryStub struct { created *models.Record }
func (s *repositoryStub) Create(_ context.Context, record *models.Record) error { s.created = record; return nil }
func (*repositoryStub) List(context.Context) ([]models.Record, error) { return nil, nil }
func (*repositoryStub) Count(context.Context) (int64, error) { return 0, nil }

func TestServiceCreate(t *testing.T) {
	repository := &repositoryStub{}
	service := NewService(repository, "ready")
	created, err := service.Create(context.Background(), models.CreateRecordInput{Name: " first "})
	if err != nil { t.Fatal(err) }
	if created.Name != "first" || repository.created != created { t.Fatalf("unexpected record: %%+v", created) }
}
`, module+"/models")
}

func contractsSource(name string) string {
	return fmt.Sprintf(`package contracts

import coreevents "github.com/bartek5186/procyon-core/events"

type RecordCreated struct {
	ID uint `+"`json:\"id\"`"+`
	Name string `+"`json:\"name\"`"+`
}
const RecordCreatedTopic coreevents.Topic[RecordCreated] = %q
`, name+".record.created.v1")
}

func fullReadme(name string, controllers []Controller) string {
	return fmt.Sprintf(`# %s

Complete Procyon plugin boilerplate with configuration, a versioned migration, neutral model, repository, service, event contract and test.

## Generated routes

%s

Rename the neutral Record type when the plugin acquires its domain language. Keep migration versions immutable after release and document public contracts before publishing the module.
`, name, routeDocumentation(name, controllers))
}

func postmanOverview(name string, controllers []Controller) string {
	return fmt.Sprintf("The `%s` plugin includes runnable starter endpoints. Adapt their payloads after naming the plugin domain.\n\n%s\n", name, routeDocumentation(name, controllers))
}

func routeDocumentation(name string, controllers []Controller) string {
	var routes []string
	if hasController(controllers, ControllerStatus) {
		routes = append(routes, "- `GET /"+name+"` — public plugin status")
	}
	if hasController(controllers, ControllerRecords) {
		routes = append(routes, "- `POST /"+name+"/records` and `GET /"+name+"/records` — authenticated starter operations")
	}
	if hasController(controllers, ControllerAdmin) {
		routes = append(routes, "- `GET /"+name+"/stats` — admin statistics")
	}
	if len(routes) == 0 {
		return "No HTTP routes were selected."
	}
	return strings.Join(routes, "\n")
}
