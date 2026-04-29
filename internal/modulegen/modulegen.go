package modulegen

import (
	"bytes"
	"errors"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type Options struct {
	Name  string
	Force bool
}

type moduleSpec struct {
	Name     string
	Pascal   string
	Field    string
	Table    string
	Route    string
	Module   string
	Force    bool
	MigStamp string
}

func Run(opts Options) error {
	if ok, _ := regexp.MatchString(`^[a-z][a-z0-9_]*$`, opts.Name); !ok {
		return errors.New("module name must be snake_case and start with a letter")
	}
	if err := validateProcyonProject(); err != nil {
		return err
	}

	mod, err := readGoModule()
	if err != nil {
		return err
	}

	spec := moduleSpec{
		Name:     opts.Name,
		Pascal:   toPascal(opts.Name),
		Field:    toLowerCamel(opts.Name),
		Table:    pluralize(opts.Name),
		Route:    "/" + pluralize(opts.Name),
		Module:   mod,
		Force:    opts.Force,
		MigStamp: time.Now().UTC().Format("20060102150405"),
	}

	if err := validateModuleDoesNotExist(spec); err != nil {
		return err
	}

	files := generatedFiles(spec)

	for path, body := range files {
		if err := writeGenerated(path, body, spec.Force); err != nil {
			return err
		}
	}

	for _, wire := range []func(moduleSpec) error{
		wireStore,
		wireService,
		wireApp,
		wireRoutes,
		wireAutoMigrate,
		wirePolicies,
	} {
		if err := wire(spec); err != nil {
			return err
		}
	}

	fmt.Printf("generated and wired module %s\n", spec.Name)
	fmt.Printf("routes: POST /v1%s, GET /v1%s/:id\n", spec.Route, spec.Route)
	fmt.Println("next: review migrations, then run gofmt/go test")
	return nil
}

func generatedFiles(spec moduleSpec) map[string]string {
	return map[string]string{
		"models/" + spec.Name + "_inputs.go":                                               inputsFile(spec),
		"models/" + spec.Name + "_outputs.go":                                              outputsFile(spec),
		"models/" + spec.Name + "_models.go":                                               modelFile(spec),
		"models/" + spec.Name + "_mappers.go":                                              mapperFile(spec),
		"store/" + spec.Field + "Store.go":                                                 storeFile(spec),
		"services/" + spec.Field + "Service.go":                                            serviceFile(spec),
		"controllers/" + spec.Field + "Controller.go":                                      controllerFile(spec),
		"internal/migrations/mysql/" + spec.MigStamp + "_create_" + spec.Table + ".sql":    mysqlMigration(spec),
		"internal/migrations/postgres/" + spec.MigStamp + "_create_" + spec.Table + ".sql": postgresMigration(spec),
	}
}

func validateModuleDoesNotExist(spec moduleSpec) error {
	var hits []string

	for path := range generatedFiles(spec) {
		if strings.Contains(path, spec.MigStamp) {
			continue
		}
		if fileExists(path) {
			hits = append(hits, path)
		}
	}

	migrationPatterns := []string{
		filepath.Join("internal", "migrations", "mysql", "*_create_"+spec.Table+".sql"),
		filepath.Join("internal", "migrations", "postgres", "*_create_"+spec.Table+".sql"),
	}
	for _, pattern := range migrationPatterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return err
		}
		hits = append(hits, matches...)
	}

	codeChecks := map[string][]string{
		"store/appStore.go": {
			"func (s *AppStore) " + spec.Pascal + "() *" + spec.Pascal + "Store",
			spec.Field + " *" + spec.Pascal + "Store",
		},
		"services/appService.go": {
			spec.Pascal + " *" + spec.Pascal + "Service",
		},
		"app.go": {
			spec.Field + " *controllers." + spec.Pascal + "Controller",
		},
		"routes.go": {
			"secured.Group(\"" + spec.Route + "\")",
			"app.rbac.Require(\"" + spec.Name + "\",",
		},
		"internal/migrate.go": {
			"&models." + spec.Pascal + "{},",
		},
		"internal/authz/casbin.go": {
			"{RoleUser, \"" + spec.Name + "\", \"read\"}",
			"{RoleAdmin, \"" + spec.Name + "\", \"manage\"}",
		},
	}
	for path, needles := range codeChecks {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		src := string(data)
		for _, needle := range needles {
			if strings.Contains(src, needle) {
				hits = append(hits, path)
				break
			}
		}
	}

	if len(hits) > 0 {
		return fmt.Errorf("module %q already exists or is already wired: %s", spec.Name, strings.Join(uniqueStrings(hits), ", "))
	}
	return nil
}

func validateProcyonProject() error {
	required := []string{
		"go.mod",
		"app.go",
		"routes.go",
		"store/appStore.go",
		"services/appService.go",
		"internal/migrate.go",
		"internal/authz/casbin.go",
	}
	for _, path := range required {
		if _, err := os.Stat(path); err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("current directory does not look like a Procyon project; missing %s", path)
			}
			return err
		}
	}
	return nil
}

func readGoModule() (string, error) {
	data, err := os.ReadFile("go.mod")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if mod, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
			mod = strings.TrimSpace(mod)
			if mod != "" {
				return mod, nil
			}
		}
	}
	return "", errors.New("unable to read module path from go.mod")
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func writeGenerated(path, body string, force bool) error {
	if _, err := os.Stat(path); err == nil && !force {
		fmt.Printf("skip existing %s\n", path)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if strings.HasSuffix(path, ".go") {
		formatted, err := format.Source([]byte(body))
		if err != nil {
			return fmt.Errorf("format %s: %w", path, err)
		}
		body = string(formatted)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return err
	}
	fmt.Printf("write %s\n", path)
	return nil
}

func wireStore(s moduleSpec) error {
	path := "store/appStore.go"
	return updateGoFile(path, func(src string) (string, error) {
		var err error
		src, err = insertInBlock(src, "type Datastore interface {", "\t"+s.Pascal+"() *"+s.Pascal+"Store")
		if err != nil {
			return "", err
		}
		src, err = insertInBlock(src, "type AppStore struct {", "\t"+s.Field+" *"+s.Pascal+"Store")
		if err != nil {
			return "", err
		}
		src, err = insertAfter(src, "\t\thello:  NewHelloStore(db),", "\t\t"+s.Field+": New"+s.Pascal+"Store(db),")
		if err != nil {
			return "", err
		}
		method := "\nfunc (s *AppStore) " + s.Pascal + "() *" + s.Pascal + "Store {\n\treturn s." + s.Field + "\n}\n"
		if !strings.Contains(src, "func (s *AppStore) "+s.Pascal+"() *"+s.Pascal+"Store") {
			src += method
		}
		return src, nil
	})
}

func wireService(s moduleSpec) error {
	path := "services/appService.go"
	return updateGoFile(path, func(src string) (string, error) {
		var err error
		src, err = insertInBlock(src, "type AppService struct {", "\t"+s.Pascal+" *"+s.Pascal+"Service")
		if err != nil {
			return "", err
		}
		src, err = insertAfter(src, "\t\tHello:   NewHelloService(store, logger, metrics),", "\t\t"+s.Pascal+": New"+s.Pascal+"Service(store, logger),")
		if err != nil {
			return "", err
		}
		return src, nil
	})
}

func wireApp(s moduleSpec) error {
	path := "app.go"
	return updateGoFile(path, func(src string) (string, error) {
		var err error
		src, err = insertInBlock(src, "type application struct {", "\t"+s.Field+" *controllers."+s.Pascal+"Controller")
		if err != nil {
			return "", err
		}
		src, err = insertAfter(src, "\t\thello:      controllers.NewHelloController(appService, logger.GetLogger()),", "\t\t"+s.Field+": controllers.New"+s.Pascal+"Controller(appService, logger.GetLogger()),")
		if err != nil {
			return "", err
		}
		return src, nil
	})
}

func wireRoutes(s moduleSpec) error {
	path := "routes.go"
	return updateGoFile(path, func(src string) (string, error) {
		line := "\n\t" + s.Field + " := secured.Group(\"" + s.Route + "\")\n" +
			"\t" + s.Field + ".POST(\"\", app." + s.Field + ".Create, app.rbac.Require(\"" + s.Name + "\", \"manage\"))\n" +
			"\t" + s.Field + ".GET(\"/:id\", app." + s.Field + ".GetByID, app.rbac.Require(\"" + s.Name + "\", \"read\"))"
		return insertAfter(src, "\tsecuredAdmin.GET(\"/hello\", app.hello.HelloAdmin)", line)
	})
}

func wireAutoMigrate(s moduleSpec) error {
	path := "internal/migrate.go"
	return updateGoFile(path, func(src string) (string, error) {
		return insertAfter(src, "\t\t&models.HelloMessage{},", "\t\t&models."+s.Pascal+"{},")
	})
}

func wirePolicies(s moduleSpec) error {
	path := "internal/authz/casbin.go"
	return updateGoFile(path, func(src string) (string, error) {
		var err error
		src, err = insertAfter(src, "\t{RoleUser, \"hello\", \"read\"},", "\t{RoleUser, \""+s.Name+"\", \"read\"},")
		if err != nil {
			return "", err
		}
		return insertAfter(src, "\t{RoleAdmin, \"hello\", \"manage\"},", "\t{RoleAdmin, \""+s.Name+"\", \"manage\"},")
	})
}

func updateGoFile(path string, fn func(string) (string, error)) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	next, err := fn(string(data))
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	formatted, err := format.Source([]byte(next))
	if err != nil {
		return fmt.Errorf("format %s: %w", path, err)
	}
	if bytes.Equal(data, formatted) {
		return nil
	}
	if err := os.WriteFile(path, formatted, 0o644); err != nil {
		return err
	}
	fmt.Printf("wire %s\n", path)
	return nil
}

func insertInBlock(src, start, line string) (string, error) {
	if strings.Contains(src, line) {
		return src, nil
	}
	idx := strings.Index(src, start)
	if idx < 0 {
		return "", fmt.Errorf("marker not found: %s", start)
	}
	blockStart := idx + len(start)
	end := strings.Index(src[blockStart:], "\n}")
	if end < 0 {
		return "", fmt.Errorf("end of block not found: %s", start)
	}
	insertAt := blockStart + end
	return src[:insertAt] + "\n" + line + src[insertAt:], nil
}

func insertAfter(src, marker, line string) (string, error) {
	if strings.Contains(src, line) {
		return src, nil
	}
	idx := strings.Index(src, marker)
	if idx < 0 {
		return "", fmt.Errorf("marker not found: %s", marker)
	}
	insertAt := idx + len(marker)
	return src[:insertAt] + "\n" + line + src[insertAt:], nil
}

func inputsFile(s moduleSpec) string {
	bt := "`"
	return fmt.Sprintf(`package models

type %[1]sCreateInput struct {
	Name string %[2]sjson:"name" validate:"required,max=120"%[2]s
}
`, s.Pascal, bt)
}

func outputsFile(s moduleSpec) string {
	bt := "`"
	return fmt.Sprintf(`package models

type %[1]sResponse struct {
	ID   uint   %[2]sjson:"id"%[2]s
	Name string %[2]sjson:"name"%[2]s
}
`, s.Pascal, bt)
}

func modelFile(s moduleSpec) string {
	return fmt.Sprintf(`package models

import "gorm.io/gorm"

type %[1]s struct {
	gorm.Model
	Name string `+"`"+`gorm:"size:120;not null"`+"`"+`
}

func (%[1]s) TableName() string {
	return "%[2]s"
}
`, s.Pascal, s.Table)
}

func mapperFile(s moduleSpec) string {
	return fmt.Sprintf(`package models

func Map%[1]sResponse(row *%[1]s) *%[1]sResponse {
	if row == nil {
		return nil
	}
	return &%[1]sResponse{
		ID:   row.ID,
		Name: row.Name,
	}
}
`, s.Pascal)
}

func storeFile(s moduleSpec) string {
	return fmt.Sprintf(`package store

import (
	"context"

	"%[1]s/models"
	"gorm.io/gorm"
)

type %[2]sStore struct {
	db *gorm.DB
}

func New%[2]sStore(db *gorm.DB) *%[2]sStore {
	return &%[2]sStore{db: db}
}

func (s *%[2]sStore) Create(ctx context.Context, row *models.%[2]s) error {
	return s.db.WithContext(ctx).Create(row).Error
}

func (s *%[2]sStore) GetByID(ctx context.Context, id uint) (*models.%[2]s, error) {
	var row models.%[2]s
	if err := s.db.WithContext(ctx).First(&row, id).Error; err != nil {
		return nil, err
	}
	return &row, nil
}
`, s.Module, s.Pascal)
}

func serviceFile(s moduleSpec) string {
	return fmt.Sprintf(`package services

import (
	"context"

	"%[1]s/models"
	"%[1]s/store"
	"go.uber.org/zap"
)

type %[2]sService struct {
	Store  store.Datastore
	logger *zap.Logger
}

func New%[2]sService(store store.Datastore, logger *zap.Logger) *%[2]sService {
	return &%[2]sService{
		Store:  store,
		logger: logger,
	}
}

func (s *%[2]sService) Create(ctx context.Context, in models.%[2]sCreateInput) (*models.%[2]sResponse, error) {
	row := &models.%[2]s{Name: in.Name}
	if err := s.Store.%[2]s().Create(ctx, row); err != nil {
		return nil, err
	}
	return models.Map%[2]sResponse(row), nil
}

func (s *%[2]sService) GetByID(ctx context.Context, id uint) (*models.%[2]sResponse, error) {
	row, err := s.Store.%[2]s().GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return models.Map%[2]sResponse(row), nil
}
`, s.Module, s.Pascal)
}

func controllerFile(s moduleSpec) string {
	return fmt.Sprintf(`package controllers

import (
	"net/http"
	"strconv"

	"%[1]s/internal/apierr"
	"%[1]s/models"
	"%[1]s/services"
	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

type %[2]sController struct {
	appService *services.AppService
	logger     *zap.Logger
}

func New%[2]sController(appService *services.AppService, logger *zap.Logger) *%[2]sController {
	return &%[2]sController{
		appService: appService,
		logger:     logger,
	}
}

func (c *%[2]sController) Create(ec echo.Context) error {
	var in models.%[2]sCreateInput
	if err := ec.Bind(&in); err != nil {
		return apierr.ReplyBadRequest(ec, "invalid payload")
	}
	if err := ec.Validate(&in); err != nil {
		return apierr.ReplyValidation(ec, err)
	}

	out, err := c.appService.%[2]s.Create(ec.Request().Context(), in)
	if err != nil {
		c.logger.Error("%[3]s create failed", zap.Error(err))
		return apierr.Reply(ec, err)
	}

	return ec.JSON(http.StatusCreated, out)
}

func (c *%[2]sController) GetByID(ec echo.Context) error {
	id, err := strconv.ParseUint(ec.Param("id"), 10, 0)
	if err != nil || id == 0 {
		return apierr.ReplyBadRequest(ec, "invalid id")
	}

	out, err := c.appService.%[2]s.GetByID(ec.Request().Context(), uint(id))
	if err != nil {
		c.logger.Error("%[3]s get failed", zap.Error(err))
		return apierr.Reply(ec, err)
	}

	return ec.JSON(http.StatusOK, out)
}
`, s.Module, s.Pascal, s.Name)
}

func mysqlMigration(s moduleSpec) string {
	return fmt.Sprintf(`-- +goose Up
CREATE TABLE IF NOT EXISTS %[1]s (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  created_at DATETIME(3) NULL,
  updated_at DATETIME(3) NULL,
  deleted_at DATETIME(3) NULL,
  name VARCHAR(120) NOT NULL,
  PRIMARY KEY (id),
  KEY idx_%[1]s_deleted_at (deleted_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
DROP TABLE IF EXISTS %[1]s;
`, s.Table)
}

func postgresMigration(s moduleSpec) string {
	return fmt.Sprintf(`-- +goose Up
CREATE TABLE IF NOT EXISTS %[1]s (
  id BIGSERIAL PRIMARY KEY,
  created_at TIMESTAMPTZ NULL,
  updated_at TIMESTAMPTZ NULL,
  deleted_at TIMESTAMPTZ NULL,
  name VARCHAR(120) NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_%[1]s_deleted_at ON %[1]s (deleted_at);

-- +goose Down
DROP TABLE IF EXISTS %[1]s;
`, s.Table)
}

func toPascal(input string) string {
	parts := strings.Split(input, "_")
	var out strings.Builder
	for _, part := range parts {
		if part == "" {
			continue
		}
		out.WriteString(strings.ToUpper(part[:1]))
		out.WriteString(part[1:])
	}
	return out.String()
}

func toLowerCamel(input string) string {
	p := toPascal(input)
	return strings.ToLower(p[:1]) + p[1:]
}

func pluralize(input string) string {
	if strings.HasSuffix(input, "s") {
		return input
	}
	return input + "s"
}
