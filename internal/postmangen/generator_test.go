package postmangen

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateWritesCollectionThroughPublicAPI(t *testing.T) {
	project := t.TempDir()
	writePostmanTestFile(t, filepath.Join(project, "routes.go"), `package main
func registerPublicRoutes(e *Echo, app *application) {
	e.GET("/health", app.Health)
}
func registerAdminRoutes(e *Echo, app *application) {}
func registerUploadRoutes(e *Echo, app *application) {}
`)
	writePostmanTestFile(t, filepath.Join(project, "controllers", "health.go"), `package controllers
// Health reports whether the application is available.
func Health() {}
`)
	output := filepath.Join(t.TempDir(), "collection.json")
	result, err := Generate(Options{Root: project, Out: output, Name: "Test API"})
	if err != nil {
		t.Fatal(err)
	}
	if result.OutputPath != output || result.RouteCount != 1 {
		t.Fatalf("unexpected generation result: %+v", result)
	}
	content, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), `"name": "Health"`) || !strings.Contains(string(content), `"name": "Test API"`) {
		t.Fatalf("unexpected generated collection: %s", content)
	}
}

func TestCollectPluginRoutesPreservesAccessModeAndGroups(t *testing.T) {
	project := t.TempDir()
	plugin := filepath.Join(project, "plugins", "payments")
	writePostmanTestFile(t, filepath.Join(project, ".procyon.json"), `{
  "modules": {
    "payment-system": {
      "kind": "go-plugin",
      "go_module": "example.com/payment-system",
      "package": "example.com/payment-system",
      "local_source": "plugins/payments"
    }
  }
}`)
	writePostmanTestFile(t, filepath.Join(plugin, "plugin.go"), `package payments

type Plugin struct{}

func (*Plugin) RegisterRoutes(routes Routes) {
	if routes.Public != nil {
		routes.Public.GET("/payments/prices/:provider", controller.PriceList)
	}
	payments := routes.Authenticated.Group("/payments")
	payments.POST("/checkout", checkout)
	routes.Admin.DELETE("/payments/:id", removePayment)
}
`)
	writePostmanTestFile(t, filepath.Join(plugin, "controllers", "controller.go"), `package controllers

type Controller struct{}

// PriceList returns the purchasable products exposed by one payment provider.
// The provider path parameter can be stripe, google, or apple.
func (*Controller) PriceList() {}
`)
	writePostmanTestFile(t, filepath.Join(plugin, "docs", "postman", "examples.json"), `{
  "examples": [{"key":"POST /v1/payments/checkout","name":"Stripe checkout","request":{"headers":{"Idempotency-Key":"{{$guid}}"}},"response":{"status":201,"body":{"checkout_url":"https://checkout.stripe.com/example"}}}]
}`)
	writePostmanTestFile(t, filepath.Join(plugin, "docs", "postman", "overview.md"), `Payment flow overview.

## Stripe

Checkout is completed on Stripe and finalized by a signed webhook.`)

	generator := &generator{root: project, folderDocs: map[string]string{}}
	routes, err := generator.collectPluginRoutes()
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 3 {
		t.Fatalf("routes = %d, want 3: %+v", len(routes), routes)
	}
	assertPluginRoute(t, routes, "GET", "/v1/payments/prices/:provider", routeAuthPublic, false)
	assertPluginRoute(t, routes, "POST", "/v1/payments/checkout", routeAuthBearer, false)
	assertPluginRoute(t, routes, "DELETE", "/v1/admin/payments/:id", routeAuthAdmin, true)
	for _, item := range routes {
		if item.Folder != "Payment System" {
			t.Fatalf("unexpected folder %q", item.Folder)
		}
	}
	collection := generator.collection("Test API", routes, collectionVars{})
	if len(collection.Item) != 1 || collection.Item[0].Name != "Payment System" {
		t.Fatalf("plugin collection root = %+v, want Payment System", collection.Item)
	}
	if !strings.Contains(collection.Item[0].Description, "finalized by a signed webhook") {
		t.Fatalf("plugin overview was not generated: %+v", collection.Item[0])
	}
	priceList := findPostmanItem(collection.Item[0].Item, "PriceList")
	if priceList == nil || priceList.Request == nil || !strings.Contains(priceList.Request.Description, "purchasable products") {
		t.Fatalf("plugin request docs were not generated: %+v", priceList)
	}
	checkout := findPostmanItem(collection.Item[0].Item, "checkout")
	if checkout == nil || len(checkout.Response) != 1 || checkout.Response[0].Name != "201 Created - Stripe checkout" {
		t.Fatalf("named plugin response example was not generated: %+v", checkout)
	}
}

func TestLoadPluginOverviewFallsBackToManifestDescription(t *testing.T) {
	plugin := t.TempDir()
	writePostmanTestFile(t, filepath.Join(plugin, "docs", "postman", "overview.md"), "\n")
	writePostmanTestFile(t, filepath.Join(plugin, "procyon-module.json"), `{"description":"Short plugin overview"}`)
	overview, err := loadPluginOverview(plugin)
	if err != nil {
		t.Fatal(err)
	}
	if overview != "Short plugin overview" {
		t.Fatalf("overview = %q", overview)
	}
}

func TestManualExampleAppliesHeaders(t *testing.T) {
	req := &postmanRequest{Method: "POST"}
	applyManualRequestExample(req, manualExampleRequest{Headers: map[string]string{"Idempotency-Key": "{{$guid}}"}})
	if len(req.Header) != 1 || req.Header[0].Key != "Idempotency-Key" {
		t.Fatalf("headers = %+v", req.Header)
	}
}

func TestManualExamplesMatchConcretePluginRouteParameters(t *testing.T) {
	generator := &generator{manualExamples: map[string][]manualExample{
		"POST /v1/payments/verify/google": {{Name: "Google"}},
		"POST /v1/payments/verify/apple":  {{Name: "Apple"}},
		"POST /v1/payments/history":       {{Name: "Wrong route"}},
	}}
	examples := generator.manualExamplesForRoute(route{Method: "POST", Path: "/v1/payments/verify/:provider"})
	if len(examples) != 2 || examples[0].Name != "Apple" || examples[1].Name != "Google" {
		t.Fatalf("unexpected parameter examples: %+v", examples)
	}
}

func TestManualExamplesPlaceDefaultVariantFirst(t *testing.T) {
	generator := &generator{manualExamples: map[string][]manualExample{
		"POST /v1/payments/checkout": {
			{Name: "Validation error"},
			{Name: "Stripe success", Default: true},
		},
	}}
	examples := generator.manualExamplesForRoute(route{Method: "POST", Path: "/v1/payments/checkout"})
	if len(examples) != 2 || examples[0].Name != "Stripe success" {
		t.Fatalf("default example is not first: %+v", examples)
	}
}

func TestCollectPluginRoutesWithoutMetadataReturnsNoRoutes(t *testing.T) {
	routes, err := (&generator{root: t.TempDir()}).collectPluginRoutes()
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 0 {
		t.Fatalf("unexpected routes: %+v", routes)
	}
}

func TestCollectPluginRoutesSkipsDisabledPlugin(t *testing.T) {
	project := t.TempDir()
	writePostmanTestFile(t, filepath.Join(project, ".procyon.json"), `{
  "modules": {
    "disabled": {
      "enabled": false,
      "kind": "go-plugin",
      "local_source": "missing-but-must-not-be-resolved"
    }
  }
}`)
	routes, err := (&generator{root: project}).collectPluginRoutes()
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 0 {
		t.Fatalf("disabled plugin routes were generated: %+v", routes)
	}
}

func TestAdminRoutesArePlacedDirectlyInAdminFolder(t *testing.T) {
	for _, item := range []route{
		{Path: "/metrics", Admin: true},
		{Path: "/admin/ping"},
	} {
		path := modulePath(item)
		if len(path) != 1 || path[0] != "Admin" {
			t.Fatalf("modulePath(%q) = %#v, want [Admin]", item.Path, path)
		}
	}
}

func TestRouteDisplayNameUsesPathWhenHandlerHasNoStableName(t *testing.T) {
	tests := []struct {
		handler string
		path    string
		want    string
	}{
		{handler: "", path: "/metrics", want: "Metrics"},
		{handler: "", path: "/ping", want: "Ping"},
		{handler: "UpdateCarrier", path: "/carriers/:uuid", want: "UpdateCarrier"},
	}
	for _, tt := range tests {
		if got := routeDisplayName(tt.handler, "", tt.path); got != tt.want {
			t.Fatalf("routeDisplayName(%q, %q) = %q, want %q", tt.handler, tt.path, got, tt.want)
		}
	}

	if got := routeDisplayName("WrapHandler", "PrometheusMetrics", "/metrics"); got != "PrometheusMetrics" {
		t.Fatalf("explicit route name was not preserved: %q", got)
	}
}

func TestHandlerNameIgnoresAdaptersAndAnonymousFunctions(t *testing.T) {
	expr, err := parser.ParseExpr("echo.WrapHandler(app.obs.MetricsHandler())")
	if err != nil {
		t.Fatal(err)
	}
	if got := handlerName(expr); got != "" {
		t.Fatalf("wrapped handler name = %q, want empty", got)
	}

	file, err := parser.ParseFile(token.NewFileSet(), "routes.go", `package routes
func register() { admin.GET("/ping", func(c Context) error { return nil }) }
`, 0)
	if err != nil {
		t.Fatal(err)
	}
	call := file.Decls[0].(*ast.FuncDecl).Body.List[0].(*ast.ExprStmt).X.(*ast.CallExpr)
	if got := handlerName(call.Args[1]); got != "" {
		t.Fatalf("anonymous handler name = %q, want empty", got)
	}
}

func TestCollectRoutesIncludesConditionalRegistrations(t *testing.T) {
	project := t.TempDir()
	writePostmanTestFile(t, filepath.Join(project, "routes.go"), `package main
func registerPublicRoutes(e *Echo, app *application) {
	api := e.Group("/v1")
	if app.auth != nil {
		api.GET("/profile", app.profile.Get)
	}
}
func registerAdminRoutes(e *Echo, app *application) {
	if app.adminAuth != nil {
		admin := e.Group("", app.adminAuth.RequireAdminKey)
		admin.GET("/ping", func(c Context) error { return nil })
	}
}
func registerUploadRoutes(e *Echo, app *application) {}
`)

	generator := &generator{
		root:        project,
		fset:        token.NewFileSet(),
		structs:     map[string]*ast.StructType{},
		handlerBody: map[string]any{},
		funcReturns: map[string][]ast.Expr{},
	}
	if err := generator.load(); err != nil {
		t.Fatal(err)
	}
	routes := generator.collectRoutes()
	if len(routes) != 2 {
		t.Fatalf("routes = %d, want 2: %+v", len(routes), routes)
	}
	assertGeneratedRoute(t, routes, "GET", "/v1/profile", "Get")
	assertGeneratedRoute(t, routes, "GET", "/ping", "Ping")
}

func assertPluginRoute(t *testing.T, routes []route, method, path, authMode string, admin bool) {
	t.Helper()
	for _, item := range routes {
		if item.Method == method && item.Path == path {
			if item.AuthMode != authMode || item.Admin != admin {
				t.Fatalf("unexpected route metadata: %+v", item)
			}
			return
		}
	}
	t.Fatalf("missing route %s %s in %+v", method, path, routes)
}

func assertGeneratedRoute(t *testing.T, routes []route, method, path, displayName string) {
	t.Helper()
	for _, item := range routes {
		if item.Method == method && item.Path == path {
			if item.DisplayName != displayName {
				t.Fatalf("route %s %s display name = %q, want %q", method, path, item.DisplayName, displayName)
			}
			return
		}
	}
	t.Fatalf("missing route %s %s in %+v", method, path, routes)
}

func findPostmanItem(items []postmanItem, name string) *postmanItem {
	for index := range items {
		if items[index].Name == name {
			return &items[index]
		}
	}
	return nil
}

func writePostmanTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
