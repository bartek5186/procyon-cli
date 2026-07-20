package postmangen

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type route struct {
	Method      string
	Path        string
	Handler     string
	DisplayName string
	Description string
	Folder      string
	Admin       bool
	AuthMode    string
}

const (
	routeAuthPublic = "public"
	routeAuthBearer = "bearer"
	routeAuthAdmin  = "admin"
)

type postmanCollection struct {
	Info     postmanInfo   `json:"info"`
	Variable []postmanVar  `json:"variable,omitempty"`
	Item     []postmanItem `json:"item"`
}

type postmanInfo struct {
	Name   string `json:"name"`
	Schema string `json:"schema"`
}

type postmanVar struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Type  string `json:"type,omitempty"`
}

type postmanItem struct {
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Auth        *postmanAuth      `json:"auth,omitempty"`
	Item        []postmanItem     `json:"item,omitempty"`
	Request     *postmanRequest   `json:"request,omitempty"`
	Response    []postmanResponse `json:"response,omitempty"`
}

type postmanAuth struct {
	Type   string          `json:"type"`
	APIKey []postmanAuthKV `json:"apikey,omitempty"`
	Bearer []postmanAuthKV `json:"bearer,omitempty"`
}

type postmanAuthKV struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Type  string `json:"type,omitempty"`
}

type postmanRequest struct {
	Method      string          `json:"method"`
	Description string          `json:"description,omitempty"`
	Auth        *postmanAuth    `json:"auth,omitempty"`
	Header      []postmanHeader `json:"header,omitempty"`
	Body        *postmanBody    `json:"body,omitempty"`
	URL         postmanURL      `json:"url"`
}

type postmanHeader struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Type  string `json:"type,omitempty"`
}

type postmanBody struct {
	Mode string `json:"mode"`
	Raw  string `json:"raw"`
}

type postmanURL struct {
	Raw      string               `json:"raw"`
	Host     []string             `json:"host"`
	Path     []string             `json:"path,omitempty"`
	Query    []postmanQueryParam  `json:"query,omitempty"`
	Variable []postmanURLVariable `json:"variable,omitempty"`
}

type postmanQueryParam struct {
	Key         string `json:"key"`
	Value       string `json:"value"`
	Description string `json:"description,omitempty"`
	Disabled    bool   `json:"disabled,omitempty"`
}

type postmanURLVariable struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type postmanResponse struct {
	Name            string          `json:"name"`
	OriginalRequest *postmanRequest `json:"originalRequest,omitempty"`
	Status          string          `json:"status"`
	Code            int             `json:"code"`
	Header          []postmanHeader `json:"header,omitempty"`
	Body            string          `json:"body,omitempty"`
}

type generator struct {
	root string

	routesFile *ast.File
	fset       *token.FileSet

	structs     map[string]*ast.StructType
	handlers    []*ast.FuncDecl
	handlerBody map[string]any
	funcReturns map[string][]ast.Expr

	manualExamples map[string][]manualExample
	folderDocs     map[string]string
}

type manualExamplesFile struct {
	Module      string          `json:"module,omitempty"`
	Version     int             `json:"version,omitempty"`
	Description string          `json:"description,omitempty"`
	Examples    []manualExample `json:"examples"`
}

type manualExample struct {
	Key      string                `json:"key"`
	Name     string                `json:"name,omitempty"`
	Default  bool                  `json:"default,omitempty"`
	Request  manualExampleRequest  `json:"request,omitempty"`
	Response manualExampleResponse `json:"response,omitempty"`
}

type manualExampleRequest struct {
	Path    map[string]string `json:"path,omitempty"`
	Query   map[string]string `json:"query,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    json.RawMessage   `json:"body,omitempty"`
}

type manualExampleResponse struct {
	Status int             `json:"status,omitempty"`
	Body   json.RawMessage `json:"body,omitempty"`
}

type generatorConfig struct {
	ServerDomain string `json:"server_domain"`
}

type collectionVars struct {
	BaseURL   string
	AdminURL  string
	UploadURL string
	AdminKey  string
	AuthKey   string
}

type Options struct {
	Root       string
	Out        string
	Name       string
	ConfigPath string
	BaseURL    string
	AdminURL   string
	UploadURL  string
	AdminKey   string
	AuthKey    string
}

type Result struct {
	OutputPath string
	RouteCount int
}

func Generate(opts Options) (Result, error) {
	if strings.TrimSpace(opts.Root) == "" {
		opts.Root = "."
	}
	root, err := filepath.Abs(opts.Root)
	if err != nil {
		return Result{}, fmt.Errorf("resolve project root: %w", err)
	}
	if strings.TrimSpace(opts.Out) == "" {
		opts.Out = "docs/json/PostmanCollection.generated.json"
	}
	if strings.TrimSpace(opts.Name) == "" {
		opts.Name = "Procyon Generated API"
	}
	if strings.TrimSpace(opts.ConfigPath) == "" {
		opts.ConfigPath = "config/config.dev-docker.json"
	}

	gen := &generator{
		root:        root,
		fset:        token.NewFileSet(),
		structs:     map[string]*ast.StructType{},
		handlerBody: map[string]any{},
		funcReturns: map[string][]ast.Expr{},

		manualExamples: map[string][]manualExample{},
		folderDocs:     map[string]string{},
	}
	if err := gen.load(); err != nil {
		return Result{}, err
	}
	if err := gen.loadManualExamples(filepath.Join(root, "docs", "postman")); err != nil {
		return Result{}, err
	}
	gen.collectHandlerBodies()

	routes := gen.collectRoutes()
	pluginRoutes, err := gen.collectPluginRoutes()
	if err != nil {
		return Result{}, err
	}
	routes = mergeRoutes(routes, pluginRoutes)
	vars := resolveCollectionVars(root, opts.ConfigPath, opts.BaseURL, opts.AdminURL, opts.UploadURL, opts.AdminKey, opts.AuthKey)
	collection := gen.collection(opts.Name, routes, vars)

	content, err := json.MarshalIndent(collection, "", "  ")
	if err != nil {
		return Result{}, err
	}
	content = append(content, '\n')

	outputPath := opts.Out
	if !filepath.IsAbs(outputPath) {
		outputPath = filepath.Join(root, outputPath)
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return Result{}, err
	}
	if err := os.WriteFile(outputPath, content, 0o644); err != nil {
		return Result{}, err
	}

	return Result{OutputPath: outputPath, RouteCount: len(routes)}, nil
}

func (g *generator) load() error {
	routesPath := filepath.Join(g.root, "routes.go")
	file, err := parser.ParseFile(g.fset, routesPath, nil, parser.ParseComments)
	if err != nil {
		return err
	}
	g.routesFile = file

	for _, dir := range []string{"controllers", "models", "services", "internal"} {
		if err := g.loadGoFiles(filepath.Join(g.root, dir)); err != nil {
			return err
		}
	}
	return nil
}

func (g *generator) loadManualExamples(dir string) error {
	if g.manualExamples == nil {
		g.manualExamples = map[string][]manualExample{}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var file manualExamplesFile
		if err := json.Unmarshal(content, &file); err != nil {
			return fmt.Errorf("parse manual Postman examples %s: %w", path, err)
		}
		for _, example := range file.Examples {
			key := canonicalManualExampleKey(example.Key)
			if key == "" {
				continue
			}
			example.Key = key
			g.manualExamples[key] = append(g.manualExamples[key], example)
		}
	}
	return nil
}

func canonicalManualExampleKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	parts := strings.Fields(key)
	if len(parts) < 2 {
		return ""
	}
	method := strings.ToUpper(parts[0])
	path := cleanPath(parts[1])
	return method + " " + path
}

func resolveCollectionVars(root, configPath, baseOverride, adminOverride, uploadOverride, adminKeyOverride, authKeyOverride string) collectionVars {
	baseURL := strings.TrimSpace(baseOverride)
	if baseURL == "" {
		baseURL = readServerDomain(filepath.Join(root, configPath))
	}
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}

	adminURL := strings.TrimSpace(adminOverride)
	if adminURL == "" {
		adminURL = "http://localhost:8081"
	}

	uploadURL := strings.TrimSpace(uploadOverride)
	if uploadURL == "" {
		uploadURL = inferUploadURL(baseURL)
	}
	if uploadURL == "" {
		uploadURL = "http://localhost:8082"
	}

	adminKey := strings.TrimSpace(adminKeyOverride)
	if adminKey == "" {
		adminKey = "CHANGE_ME_ADMIN_KEY"
	}

	authKey := strings.TrimSpace(authKeyOverride)
	if authKey == "" {
		authKey = "CHANGE_ME_AUTH_KEY"
	}

	return collectionVars{
		BaseURL:   baseURL,
		AdminURL:  adminURL,
		UploadURL: uploadURL,
		AdminKey:  adminKey,
		AuthKey:   authKey,
	}
}

func readServerDomain(path string) string {
	content, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var cfg generatorConfig
	if err := json.Unmarshal(content, &cfg); err != nil {
		return ""
	}
	return strings.TrimSpace(cfg.ServerDomain)
}

func inferUploadURL(baseURL string) string {
	switch {
	case strings.Contains(baseURL, "api-dev."):
		return strings.Replace(baseURL, "api-dev.", "upload-dev.", 1)
	case strings.Contains(baseURL, "api."):
		return strings.Replace(baseURL, "api.", "upload.", 1)
	case strings.Contains(baseURL, "localhost:8080"):
		return strings.Replace(baseURL, "localhost:8080", "localhost:8082", 1)
	default:
		return ""
	}
}

func (g *generator) loadGoFiles(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		file, err := parser.ParseFile(g.fset, path, nil, parser.ParseComments)
		if err != nil {
			return err
		}
		pkg := file.Name.Name
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					ts, ok := spec.(*ast.TypeSpec)
					if !ok {
						continue
					}
					st, ok := ts.Type.(*ast.StructType)
					if !ok {
						continue
					}
					g.structs[ts.Name.Name] = st
					g.structs[pkg+"."+ts.Name.Name] = st
				}
			case *ast.FuncDecl:
				g.handlers = append(g.handlers, d)
				g.funcReturns[d.Name.Name] = nonErrorResultTypes(d.Type.Results)
			}
		}
	}
	return nil
}

func nonErrorResultTypes(results *ast.FieldList) []ast.Expr {
	if results == nil {
		return nil
	}
	var out []ast.Expr
	for _, result := range results.List {
		if result.Type == nil || exprString(result.Type) == "error" {
			continue
		}
		count := 1
		if len(result.Names) > 0 {
			count = len(result.Names)
		}
		for i := 0; i < count; i++ {
			out = append(out, result.Type)
		}
	}
	return out
}

func (g *generator) collectHandlerBodies() {
	for _, handler := range g.handlers {
		if handler.Body == nil {
			continue
		}
		if body, ok := g.bindExampleForHandler(handler); ok {
			g.handlerBody[handler.Name.Name] = body
		}
	}
}

func (g *generator) bindExampleForHandler(fn *ast.FuncDecl) (any, bool) {
	varTypes := map[string]ast.Expr{}
	localStructs := map[string]*ast.StructType{}

	for _, stmt := range fn.Body.List {
		switch s := stmt.(type) {
		case *ast.DeclStmt:
			decl, ok := s.Decl.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, spec := range decl.Specs {
				switch x := spec.(type) {
				case *ast.TypeSpec:
					if st, ok := x.Type.(*ast.StructType); ok {
						localStructs[x.Name.Name] = st
					}
				case *ast.ValueSpec:
					for _, name := range x.Names {
						if x.Type != nil {
							varTypes[name.Name] = x.Type
						}
					}
				}
			}
		case *ast.AssignStmt:
			if s.Tok != token.DEFINE {
				continue
			}
			for i, lhs := range s.Lhs {
				id, ok := lhs.(*ast.Ident)
				if !ok || i >= len(s.Rhs) {
					continue
				}
				if cl, ok := s.Rhs[i].(*ast.CompositeLit); ok {
					varTypes[id.Name] = cl.Type
				}
			}
		}
	}

	var bindName string
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if bindName != "" {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Bind" || len(call.Args) == 0 {
			return true
		}
		unary, ok := call.Args[0].(*ast.UnaryExpr)
		if !ok || unary.Op != token.AND {
			return true
		}
		id, ok := unary.X.(*ast.Ident)
		if ok {
			bindName = id.Name
		}
		return true
	})
	if bindName == "" {
		return nil, false
	}

	typ, ok := varTypes[bindName]
	if !ok {
		return nil, false
	}
	if id, ok := typ.(*ast.Ident); ok {
		if st := localStructs[id.Name]; st != nil {
			return g.exampleFromStruct(st, 0), true
		}
	}
	return g.exampleFromExpr(typ, 0), true
}

func (g *generator) collectRoutes() []route {
	funcs := map[string]*ast.FuncDecl{}
	for _, decl := range g.routesFile.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok {
			funcs[fn.Name.Name] = fn
		}
	}

	var out []route
	seen := map[string]bool{}
	var walkFunc func(name string, prefixes map[string]string, admin bool)

	walkFunc = func(name string, prefixes map[string]string, admin bool) {
		fn := funcs[name]
		if fn == nil || fn.Body == nil {
			return
		}
		env := cloneMap(prefixes)

		var walkStatements func([]ast.Stmt, map[string]string)
		walkStatements = func(statements []ast.Stmt, env map[string]string) {
			for _, stmt := range statements {
				if conditional, ok := stmt.(*ast.IfStmt); ok {
					ifEnv := cloneMap(env)
					if assign, ok := conditional.Init.(*ast.AssignStmt); ok {
						g.captureGroup(assign, ifEnv)
					}
					walkStatements(conditional.Body.List, ifEnv)
					switch alternative := conditional.Else.(type) {
					case *ast.BlockStmt:
						walkStatements(alternative.List, cloneMap(env))
					case *ast.IfStmt:
						walkStatements([]ast.Stmt{alternative}, cloneMap(env))
					}
					continue
				}

				if assign, ok := stmt.(*ast.AssignStmt); ok {
					g.captureGroup(assign, env)
				}

				call := callFromStmt(stmt)
				if call == nil {
					continue
				}
				if fnName := calledFuncName(call.Fun); strings.HasPrefix(fnName, "register") {
					if len(call.Args) > 0 {
						if id, ok := call.Args[0].(*ast.Ident); ok {
							next := map[string]string{}
							if paramName := firstParamName(funcs[fnName]); paramName != "" {
								next[paramName] = env[id.Name]
							}
							walkFunc(fnName, next, admin)
						}
					}
					continue
				}

				selector, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || len(call.Args) < 2 {
					continue
				}
				method := strings.ToUpper(selector.Sel.Name)
				if !isHTTPRegistration(method) {
					continue
				}
				recv, ok := selector.X.(*ast.Ident)
				if !ok {
					continue
				}
				path, ok := stringLiteral(call.Args[0])
				if !ok {
					continue
				}
				if method == "ANY" {
					method = "POST"
				}
				fullPath := cleanPath(env[recv.Name] + path)
				handler := handlerName(call.Args[1])
				displayName := routeDisplayName(handler, g.routeCommentValue(stmt, "name"), fullPath)
				folder := g.routeCommentValue(stmt, "folder")
				keyPath := fullPath
				if admin {
					keyPath = adminCanonicalPath(fullPath)
				}
				key := method + " " + keyPath + " " + handler
				if !seen[key] {
					seen[key] = true
					out = append(out, route{Method: method, Path: fullPath, Handler: handler, DisplayName: displayName, Description: g.handlerDescription(handler), Folder: folder, Admin: admin})
				}
			}
		}

		walkStatements(fn.Body.List, env)
	}

	walkFunc("registerPublicRoutes", map[string]string{
		"e": "", "api": "/v1", "authenticated": "/v1",
	}, false)
	walkFunc("registerAdminRoutes", map[string]string{"e": ""}, true)
	walkFunc("registerUploadRoutes", map[string]string{"e": ""}, false)

	sort.Slice(out, func(i, j int) bool {
		if out[i].Path == out[j].Path {
			return out[i].Method < out[j].Method
		}
		return out[i].Path < out[j].Path
	})
	return out
}

type postmanProjectMetadata struct {
	Modules map[string]postmanInstalledModule `json:"modules,omitempty"`
}

type postmanInstalledModule struct {
	Enabled     *bool  `json:"enabled,omitempty"`
	Kind        string `json:"kind,omitempty"`
	GoModule    string `json:"go_module,omitempty"`
	Package     string `json:"package,omitempty"`
	LocalSource string `json:"local_source,omitempty"`
}

type pluginRouteGroup struct {
	Path     string
	AuthMode string
}

func (g *generator) collectPluginRoutes() ([]route, error) {
	var routes []route
	seenNames := make(map[string]struct{})
	installedSources := make(map[string]struct{})

	content, err := os.ReadFile(filepath.Join(g.root, ".procyon.json"))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	var metadata postmanProjectMetadata
	if err == nil {
		if decodeErr := json.Unmarshal(content, &metadata); decodeErr != nil {
			return nil, fmt.Errorf("parse .procyon.json for Postman plugins: %w", decodeErr)
		}
	}
	names := make([]string, 0, len(metadata.Modules))
	for name, module := range metadata.Modules {
		if module.Kind == "go-plugin" && (module.Enabled == nil || *module.Enabled) {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	for _, name := range names {
		seenNames[name] = struct{}{}
		module := metadata.Modules[name]
		source, err := g.pluginPackageDir(module)
		if err != nil {
			return nil, fmt.Errorf("resolve plugin %s for Postman: %w", name, err)
		}
		if absolute, err := filepath.Abs(source); err == nil {
			installedSources[filepath.Clean(absolute)] = struct{}{}
		}
		if err := g.loadManualExamples(filepath.Join(source, "docs", "postman")); err != nil {
			return nil, fmt.Errorf("load plugin %s Postman examples: %w", name, err)
		}
		overview, err := loadPluginOverview(source)
		if err != nil {
			return nil, fmt.Errorf("load plugin %s Postman overview: %w", name, err)
		}
		if overview != "" {
			if g.folderDocs == nil {
				g.folderDocs = map[string]string{}
			}
			g.folderDocs[titleForSegment(name)] = overview
		}
		pluginRoutes, err := collectRoutesFromPlugin(source, name)
		if err != nil {
			return nil, fmt.Errorf("collect plugin %s routes: %w", name, err)
		}
		routes = append(routes, pluginRoutes...)
	}

	localRoot := filepath.Join(g.root, "plugins")
	entries, err := os.ReadDir(localRoot)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if _, duplicate := seenNames[name]; duplicate {
			return nil, fmt.Errorf("plugin %s exists as both local and installed", name)
		}
		source := filepath.Join(localRoot, name)
		absoluteSource, err := filepath.Abs(source)
		if err != nil {
			return nil, err
		}
		if _, installed := installedSources[filepath.Clean(absoluteSource)]; installed {
			continue
		}
		if _, err := os.Stat(filepath.Join(source, "plugin.go")); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return nil, err
		}
		if err := g.loadManualExamples(filepath.Join(source, "docs", "postman")); err != nil {
			return nil, fmt.Errorf("load local plugin %s Postman examples: %w", name, err)
		}
		overview, err := loadPluginOverview(source)
		if err != nil {
			return nil, fmt.Errorf("load local plugin %s Postman overview: %w", name, err)
		}
		if overview != "" {
			if g.folderDocs == nil {
				g.folderDocs = map[string]string{}
			}
			g.folderDocs[titleForSegment(name)] = overview
		}
		pluginRoutes, err := collectRoutesFromPlugin(source, name)
		if err != nil {
			return nil, fmt.Errorf("collect local plugin %s routes: %w", name, err)
		}
		routes = append(routes, pluginRoutes...)
	}
	return routes, nil
}

type postmanModuleManifest struct {
	Description string `json:"description,omitempty"`
}

func loadPluginOverview(root string) (string, error) {
	path := filepath.Join(root, "docs", "postman", "overview.md")
	content, err := os.ReadFile(path)
	if err == nil {
		if overview := strings.TrimSpace(string(content)); overview != "" {
			return overview, nil
		}
	}
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	manifestPath := filepath.Join(root, "procyon-module.json")
	content, err = os.ReadFile(manifestPath)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	var manifest postmanModuleManifest
	if err := json.Unmarshal(content, &manifest); err != nil {
		return "", fmt.Errorf("parse %s: %w", manifestPath, err)
	}
	return strings.TrimSpace(manifest.Description), nil
}

func (g *generator) pluginPackageDir(module postmanInstalledModule) (string, error) {
	if source := strings.TrimSpace(module.LocalSource); source != "" {
		if !filepath.IsAbs(source) {
			source = filepath.Join(g.root, source)
		}
		if suffix := packageSuffix(module.GoModule, module.Package); suffix != "" {
			source = filepath.Join(source, filepath.FromSlash(suffix))
		}
		return filepath.Abs(source)
	}
	packagePath := strings.TrimSpace(module.Package)
	if packagePath == "" {
		packagePath = strings.TrimSpace(module.GoModule)
	}
	if packagePath == "" {
		return "", fmt.Errorf("missing package and go_module metadata")
	}
	return goPackageDir(g.root, packagePath)
}

func packageSuffix(modulePath, packagePath string) string {
	modulePath = strings.TrimSuffix(strings.TrimSpace(modulePath), "/")
	packagePath = strings.TrimSpace(packagePath)
	if modulePath == "" || packagePath == modulePath {
		return ""
	}
	return strings.TrimPrefix(strings.TrimPrefix(packagePath, modulePath), "/")
}

func goPackageDir(root, packagePath string) (string, error) {
	run := func(disableWorkspace bool) (string, error) {
		cmd := exec.Command("go", "list", "-f={{.Dir}}", packagePath)
		cmd.Dir = root
		if disableWorkspace {
			cmd.Env = withEnvironment(os.Environ(), "GOWORK", "off")
		}
		output, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("go list %s: %s", packagePath, strings.TrimSpace(string(output)))
		}
		return strings.TrimSpace(string(output)), nil
	}
	directory, err := run(false)
	if err == nil && directory != "" {
		return directory, nil
	}
	return run(true)
}

func withEnvironment(environment []string, key, value string) []string {
	prefix := key + "="
	out := make([]string, 0, len(environment)+1)
	for _, item := range environment {
		if !strings.HasPrefix(item, prefix) {
			out = append(out, item)
		}
	}
	return append(out, prefix+value)
}

func collectRoutesFromPlugin(root, moduleName string) ([]route, error) {
	descriptions, err := collectHandlerDescriptions(root)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	var routes []route
	seen := map[string]bool{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(root, entry.Name())
		file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			return nil, err
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Name.Name != "RegisterRoutes" || function.Body == nil {
				continue
			}
			parameter := firstParamName(function)
			if parameter == "" {
				continue
			}
			groups := map[string]pluginRouteGroup{}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				switch value := node.(type) {
				case *ast.AssignStmt:
					capturePluginGroup(value, parameter, groups)
				case *ast.CallExpr:
					pluginRoute, ok := pluginRouteFromCall(value, parameter, groups, moduleName)
					if !ok {
						return true
					}
					key := pluginRoute.Method + " " + pluginRoute.Path
					if !seen[key] {
						seen[key] = true
						pluginRoute.Description = descriptions[pluginRoute.Handler]
						routes = append(routes, pluginRoute)
					}
				}
				return true
			})
		}
	}
	sortRoutes(routes)
	return routes, nil
}

func collectHandlerDescriptions(root string) (map[string]string, error) {
	descriptions := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != root && (strings.HasPrefix(entry.Name(), ".") || entry.Name() == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ParseComments)
		if err != nil {
			return err
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Doc == nil {
				continue
			}
			description := strings.TrimSpace(function.Doc.Text())
			if description != "" {
				descriptions[function.Name.Name] = description
			}
		}
		return nil
	})
	return descriptions, err
}

func capturePluginGroup(assign *ast.AssignStmt, routesParameter string, groups map[string]pluginRouteGroup) {
	if len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
		return
	}
	name, ok := assign.Lhs[0].(*ast.Ident)
	if !ok {
		return
	}
	call, ok := assign.Rhs[0].(*ast.CallExpr)
	if !ok || len(call.Args) == 0 {
		return
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "Group" {
		return
	}
	parent, ok := resolvePluginGroup(selector.X, routesParameter, groups)
	if !ok {
		return
	}
	path, ok := stringLiteral(call.Args[0])
	if !ok {
		return
	}
	parent.Path = cleanPath(parent.Path + path)
	groups[name.Name] = parent
}

func pluginRouteFromCall(call *ast.CallExpr, routesParameter string, groups map[string]pluginRouteGroup, moduleName string) (route, bool) {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || len(call.Args) < 2 {
		return route{}, false
	}
	method := strings.ToUpper(selector.Sel.Name)
	if !isHTTPRegistration(method) {
		return route{}, false
	}
	group, ok := resolvePluginGroup(selector.X, routesParameter, groups)
	if !ok {
		return route{}, false
	}
	path, ok := stringLiteral(call.Args[0])
	if !ok {
		return route{}, false
	}
	if method == "ANY" {
		method = "POST"
	}
	fullPath := cleanPath(group.Path + path)
	handler := handlerName(call.Args[1])
	return route{
		Method: method, Path: fullPath, Handler: handler,
		DisplayName: routeDisplayName(handler, "", fullPath),
		Folder:      titleForSegment(moduleName),
		Admin:       group.AuthMode == routeAuthAdmin,
		AuthMode:    group.AuthMode,
	}, true
}

func resolvePluginGroup(expression ast.Expr, routesParameter string, groups map[string]pluginRouteGroup) (pluginRouteGroup, bool) {
	if identifier, ok := expression.(*ast.Ident); ok {
		group, found := groups[identifier.Name]
		return group, found
	}
	selector, ok := expression.(*ast.SelectorExpr)
	if !ok {
		return pluginRouteGroup{}, false
	}
	root, ok := selector.X.(*ast.Ident)
	if !ok || root.Name != routesParameter {
		return pluginRouteGroup{}, false
	}
	switch selector.Sel.Name {
	case "Public":
		return pluginRouteGroup{Path: "/v1", AuthMode: routeAuthPublic}, true
	case "Authenticated":
		return pluginRouteGroup{Path: "/v1", AuthMode: routeAuthBearer}, true
	case "Admin":
		return pluginRouteGroup{Path: "/v1/admin", AuthMode: routeAuthAdmin}, true
	default:
		return pluginRouteGroup{}, false
	}
}

func mergeRoutes(groups ...[]route) []route {
	seen := map[string]bool{}
	var routes []route
	for _, group := range groups {
		for _, item := range group {
			key := item.Method + " " + item.Path + " " + item.Handler
			if seen[key] {
				continue
			}
			seen[key] = true
			routes = append(routes, item)
		}
	}
	sortRoutes(routes)
	return routes
}

func sortRoutes(routes []route) {
	sort.Slice(routes, func(i, j int) bool {
		if routes[i].Path == routes[j].Path {
			return routes[i].Method < routes[j].Method
		}
		return routes[i].Path < routes[j].Path
	})
}

func (g *generator) captureGroup(assign *ast.AssignStmt, env map[string]string) {
	if len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
		return
	}
	left, ok := assign.Lhs[0].(*ast.Ident)
	if !ok {
		return
	}
	call, ok := assign.Rhs[0].(*ast.CallExpr)
	if !ok || len(call.Args) == 0 {
		return
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Group" {
		return
	}
	recv, ok := sel.X.(*ast.Ident)
	if !ok {
		return
	}
	path, ok := stringLiteral(call.Args[0])
	if !ok {
		return
	}
	env[left.Name] = cleanPath(env[recv.Name] + path)
}

func (g *generator) collection(name string, routes []route, vars collectionVars) postmanCollection {
	folders := map[string]*postmanItem{}
	for _, r := range routes {
		folderPath := modulePath(r)
		key := strings.Join(folderPath, "/")
		folder := folders[key]
		if folder == nil {
			folder = &postmanItem{Name: folderPath[len(folderPath)-1], Item: []postmanItem{}}
			folders[key] = folder
		}
		folder.Item = append(folder.Item, g.routeItem(r))
	}

	items := nestFolders(folders)
	applyFolderDocs(items, g.folderDocs, nil)
	setAdminFolderAuth(items)
	return postmanCollection{
		Info: postmanInfo{
			Name:   name,
			Schema: "https://schema.getpostman.com/json/collection/v2.1.0/collection.json",
		},
		Variable: []postmanVar{
			{Key: "baseURL", Value: vars.BaseURL, Type: "string"},
			{Key: "adminURL", Value: vars.AdminURL, Type: "string"},
			{Key: "uploadURL", Value: vars.UploadURL, Type: "string"},
			{Key: "adminKey", Value: vars.AdminKey, Type: "string"},
			{Key: "authKey", Value: vars.AuthKey, Type: "string"},
		},
		Item: items,
	}
}

func applyFolderDocs(items []postmanItem, docs map[string]string, parent []string) {
	for index := range items {
		path := append(append([]string(nil), parent...), items[index].Name)
		if description := strings.TrimSpace(docs[strings.Join(path, "/")]); description != "" {
			items[index].Description = description
		}
		applyFolderDocs(items[index].Item, docs, path)
	}
}

func modulePath(r route) []string {
	if folderPath := routeFolderPath(r.Folder); len(folderPath) > 0 {
		return folderPath
	}

	path := cleanPath(r.Path)
	if r.Admin || isAdminRoute(path) {
		return []string{"Admin"}
	}
	if strings.HasPrefix(path, "/payment") {
		return []string{"Payment"}
	}
	if strings.HasPrefix(path, "/upload") || strings.HasPrefix(path, "/v1/upload") {
		return []string{"Upload"}
	}
	if strings.HasPrefix(path, "/v1/messages") {
		return []string{"Inbox"}
	}
	if strings.HasPrefix(path, "/v1/notifications") {
		return []string{"Notifications"}
	}
	if strings.HasPrefix(path, "/v1/leagues") {
		return []string{"Leagues"}
	}
	if strings.HasPrefix(path, "/v1/stages") || strings.HasPrefix(path, "/v1/results") || strings.HasPrefix(path, "/v1/progress") || strings.HasPrefix(path, "/v1/fields") || strings.HasPrefix(path, "/v1/tasks") {
		return []string{"Game"}
	}
	if strings.HasPrefix(path, "/v1/lesson-list") || strings.HasPrefix(path, "/v1/lessons") {
		return []string{"Lessons"}
	}
	if strings.HasPrefix(path, "/v1/player") || strings.HasPrefix(path, "/v1/all-players") {
		return []string{"Players"}
	}
	if path == "/v1/error" {
		return []string{"Public", "Errors"}
	}
	if path == "/v1/session/extend" {
		return []string{"Session"}
	}
	if strings.HasPrefix(path, "/v1") {
		return []string{"Authenticated"}
	}
	return []string{"Public"}
}

func routeFolderPath(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == '/' || r == '>'
	})
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func nestFolders(folders map[string]*postmanItem) []postmanItem {
	tree := map[string]*folderNode{}
	for key, folder := range folders {
		parts := strings.Split(key, "/")
		parent := tree
		var node *folderNode
		for i, part := range parts {
			node = parent[part]
			if node == nil {
				node = &folderNode{name: part, children: map[string]*folderNode{}}
				parent[part] = node
			}
			if i == len(parts)-1 {
				node.items = append(node.items, folder.Item...)
				break
			}
			parent = node.children
		}
	}
	return renderFolderNodes(tree)
}

func setAdminFolderAuth(items []postmanItem) {
	for i := range items {
		if items[i].Name == "Admin" {
			items[i].Auth = adminAPIKeyAuth()
			return
		}
	}
}

func adminAPIKeyAuth() *postmanAuth {
	return &postmanAuth{
		Type: "apikey",
		APIKey: []postmanAuthKV{
			{Key: "key", Value: "X-Admin-Key", Type: "string"},
			{Key: "value", Value: "{{adminKey}}", Type: "string"},
			{Key: "in", Value: "header", Type: "string"},
		},
	}
}

func bearerAuth() *postmanAuth {
	return &postmanAuth{
		Type: "bearer",
		Bearer: []postmanAuthKV{
			{Key: "token", Value: "{{authKey}}", Type: "string"},
		},
	}
}

type folderNode struct {
	name     string
	children map[string]*folderNode
	items    []postmanItem
}

func renderFolderNodes(nodes map[string]*folderNode) []postmanItem {
	names := make([]string, 0, len(nodes))
	for name := range nodes {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]postmanItem, 0, len(names))
	for _, name := range names {
		node := nodes[name]
		item := postmanItem{Name: node.name, Item: renderFolderNodes(node.children)}
		sort.SliceStable(node.items, func(i, j int) bool {
			return node.items[i].Name < node.items[j].Name
		})
		item.Item = append(item.Item, node.items...)
		out = append(out, item)
	}
	return out
}

func titleForSegment(segment string) string {
	switch segment {
	case "activation-codes":
		return "Activation Codes"
	case "league-tiers":
		return "League Tiers"
	case "lesson-list":
		return "Lessons"
	case "results":
		return "Results"
	default:
		segment = strings.ReplaceAll(segment, "-", " ")
		words := strings.Fields(segment)
		for i, word := range words {
			words[i] = strings.ToUpper(word[:1]) + word[1:]
		}
		if len(words) == 0 {
			return "General"
		}
		return strings.Join(words, " ")
	}
}

func (g *generator) routeItem(r route) postmanItem {
	baseVar := "baseURL"
	if r.Admin || isAdminRoute(r.Path) {
		baseVar = "adminURL"
	}
	if routeUsesUploadURL(r) {
		baseVar = "uploadURL"
	}

	query := g.queryParamsForHandler(r.Handler)
	variables := pathVariables(r.Path)
	rawURL := "{{" + baseVar + "}}" + r.Path + queryString(query)

	req := &postmanRequest{
		Method:      r.Method,
		Description: r.Description,
		URL: postmanURL{
			Raw:      rawURL,
			Host:     []string{"{{" + baseVar + "}}"},
			Path:     pathParts(r.Path),
			Query:    query,
			Variable: variables,
		},
	}
	if body := g.handlerBody[r.Handler]; body != nil && methodCanHaveBody(r.Method) {
		raw, _ := json.MarshalIndent(body, "", "  ")
		req.Header = []postmanHeader{{Key: "Content-Type", Value: "application/json", Type: "text"}}
		req.Body = &postmanBody{Mode: "raw", Raw: string(raw)}
	}
	if examples := g.manualExamplesForRoute(r); len(examples) > 0 {
		applyManualRequestExample(req, examples[0].Request)
	}
	if routeRequiresBearerAuth(r) {
		auth := bearerAuth()
		req.Auth = auth
		item := postmanItem{Name: r.DisplayName, Auth: auth, Request: req, Response: g.responseExamples(r, req)}
		return item
	}
	item := postmanItem{Name: r.DisplayName, Request: req, Response: g.responseExamples(r, req)}
	return item
}

func applyManualRequestExample(req *postmanRequest, example manualExampleRequest) {
	if req == nil {
		return
	}
	if len(example.Query) > 0 {
		for i := range req.URL.Query {
			if value, ok := example.Query[req.URL.Query[i].Key]; ok {
				req.URL.Query[i].Value = value
			}
		}
		req.URL.Raw = rawURLFromParts(req.URL.Host, req.URL.Path, req.URL.Query)
	}
	if len(example.Path) > 0 {
		for i := range req.URL.Variable {
			if value, ok := example.Path[req.URL.Variable[i].Key]; ok {
				req.URL.Variable[i].Value = value
			}
		}
	}
	for key, value := range example.Headers {
		found := false
		for i := range req.Header {
			if strings.EqualFold(req.Header[i].Key, key) {
				req.Header[i].Value = value
				found = true
				break
			}
		}
		if !found {
			req.Header = append(req.Header, postmanHeader{Key: key, Value: value, Type: "text"})
		}
	}
	if len(example.Body) > 0 && string(example.Body) != "null" && methodCanHaveBody(req.Method) {
		var body any
		if err := json.Unmarshal(example.Body, &body); err == nil {
			raw, _ := json.MarshalIndent(body, "", "  ")
			req.Header = upsertContentTypeJSON(req.Header)
			req.Body = &postmanBody{Mode: "raw", Raw: string(raw)}
		}
	}
}

func rawURLFromParts(host []string, path []string, query []postmanQueryParam) string {
	raw := ""
	if len(host) > 0 {
		raw = host[0]
	}
	if len(path) > 0 {
		raw += "/" + strings.Join(path, "/")
	}
	return raw + queryString(query)
}

func upsertContentTypeJSON(headers []postmanHeader) []postmanHeader {
	for i := range headers {
		if strings.EqualFold(headers[i].Key, "Content-Type") {
			headers[i].Value = "application/json"
			headers[i].Type = "text"
			return headers
		}
	}
	return append(headers, postmanHeader{Key: "Content-Type", Value: "application/json", Type: "text"})
}

func (g *generator) manualExamplesForRoute(r route) []manualExample {
	exactKey := r.Method + " " + cleanPath(r.Path)
	examples := append([]manualExample(nil), g.manualExamples[exactKey]...)
	for key, candidates := range g.manualExamples {
		if key == exactKey {
			continue
		}
		method, path, found := strings.Cut(key, " ")
		if !found || method != r.Method || !routeExamplePathMatches(r.Path, path) {
			continue
		}
		examples = append(examples, candidates...)
	}
	sort.SliceStable(examples, func(i, j int) bool {
		if examples[i].Default != examples[j].Default {
			return examples[i].Default
		}
		return examples[i].Name < examples[j].Name
	})
	return examples
}

func routeExamplePathMatches(routePath, examplePath string) bool {
	routeParts := strings.Split(strings.Trim(cleanPath(routePath), "/"), "/")
	exampleParts := strings.Split(strings.Trim(cleanPath(examplePath), "/"), "/")
	if len(routeParts) != len(exampleParts) {
		return false
	}
	for index := range routeParts {
		if strings.HasPrefix(routeParts[index], ":") {
			if exampleParts[index] == "" {
				return false
			}
			continue
		}
		if routeParts[index] != exampleParts[index] {
			return false
		}
	}
	return true
}

func clonePostmanRequest(req *postmanRequest) *postmanRequest {
	if req == nil {
		return nil
	}
	clone := *req
	clone.Header = append([]postmanHeader(nil), req.Header...)
	clone.URL.Host = append([]string(nil), req.URL.Host...)
	clone.URL.Path = append([]string(nil), req.URL.Path...)
	clone.URL.Query = append([]postmanQueryParam(nil), req.URL.Query...)
	clone.URL.Variable = append([]postmanURLVariable(nil), req.URL.Variable...)
	if req.Body != nil {
		body := *req.Body
		clone.Body = &body
	}
	return &clone
}

func (g *generator) queryParamsForHandler(handlerName string) []postmanQueryParam {
	fn := g.handlerFunc(handlerName)
	if fn == nil || fn.Body == nil {
		return nil
	}

	seen := map[string]bool{}
	var out []postmanQueryParam
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "QueryParam" {
			return true
		}
		key, ok := stringLiteral(call.Args[0])
		if !ok || key == "" || seen[key] {
			return true
		}
		seen[key] = true
		out = append(out, postmanQueryParam{
			Key:   key,
			Value: queryParamExample(key),
		})
		return true
	})
	return out
}

func (g *generator) handlerFunc(handlerName string) *ast.FuncDecl {
	for _, fn := range g.handlers {
		if fn.Name.Name == handlerName {
			return fn
		}
	}
	return nil
}

func (g *generator) handlerDescription(handlerName string) string {
	fn := g.handlerFunc(handlerName)
	if fn == nil || fn.Doc == nil {
		return ""
	}
	return strings.TrimSpace(fn.Doc.Text())
}

func queryParamExample(key string) string {
	switch key {
	case "limit":
		return "20"
	case "page":
		return "1"
	case "after", "before", "query":
		return ""
	case "path", "kind", "status":
		return "all"
	case "order":
		return "desc"
	case "sort_by":
		return "created_at"
	case "compact", "include_me", "active_only", "force", "sandbox", "with_children", "grouped":
		return "false"
	case "reviewed", "used":
		return ""
	case "filter":
		return "all"
	case "type":
		return "all"
	case "stage", "level":
		return "1"
	case "base":
		return "https://api-dev.procyon.pl"
	default:
		return "example"
	}
}

func pathVariables(path string) []postmanURLVariable {
	parts := pathParts(path)
	var out []postmanURLVariable
	for _, part := range parts {
		if !strings.HasPrefix(part, ":") {
			continue
		}
		key := strings.TrimPrefix(part, ":")
		out = append(out, postmanURLVariable{
			Key:   key,
			Value: pathVariableExample(key),
		})
	}
	return out
}

func pathVariableExample(key string) string {
	switch {
	case strings.Contains(strings.ToLower(key), "uuid"):
		return "00000000-0000-0000-0000-000000000000"
	case strings.Contains(strings.ToLower(key), "provider"):
		return "stripe"
	case strings.Contains(strings.ToLower(key), "level"):
		return "1"
	default:
		return "1"
	}
}

func queryString(query []postmanQueryParam) string {
	if len(query) == 0 {
		return ""
	}
	parts := make([]string, 0, len(query))
	for _, param := range query {
		parts = append(parts, param.Key+"="+param.Value)
	}
	return "?" + strings.Join(parts, "&")
}

func (g *generator) responseExamples(r route, req *postmanRequest) []postmanResponse {
	if manual := g.manualExamplesForRoute(r); len(manual) > 0 {
		out := make([]postmanResponse, 0, len(manual))
		for _, example := range manual {
			status, code := successStatus(r.Method)
			if example.Response.Status > 0 {
				code = example.Response.Status
				status = statusTextForCode(code)
			}
			response := postmanResponse{
				Name:            manualResponseName(example, code, status),
				OriginalRequest: clonePostmanRequest(req),
				Status:          status,
				Code:            code,
			}
			applyManualRequestExample(response.OriginalRequest, example.Request)
			if len(example.Response.Body) > 0 && string(example.Response.Body) != "null" {
				var body any
				if err := json.Unmarshal(example.Response.Body, &body); err == nil {
					raw, _ := json.MarshalIndent(body, "", "  ")
					response.Header = []postmanHeader{{Key: "Content-Type", Value: "application/json", Type: "text"}}
					response.Body = string(raw)
				}
			}
			out = append(out, response)
		}
		return out
	}

	status, code := successStatus(r.Method)
	body := g.responseBodyExample(r)
	raw, _ := json.MarshalIndent(body, "", "  ")
	return []postmanResponse{
		{
			Name:   fmt.Sprintf("%d %s - example", code, status),
			Status: status,
			Code:   code,
			Header: []postmanHeader{{Key: "Content-Type", Value: "application/json", Type: "text"}},
			Body:   string(raw),
		},
	}
}

func manualResponseName(example manualExample, code int, status string) string {
	if strings.TrimSpace(example.Name) != "" {
		return fmt.Sprintf("%d %s - %s", code, status, strings.TrimSpace(example.Name))
	}
	return fmt.Sprintf("%d %s - example", code, status)
}

func statusTextForCode(code int) string {
	switch code {
	case 200:
		return "OK"
	case 201:
		return "Created"
	case 202:
		return "Accepted"
	case 204:
		return "No Content"
	case 400:
		return "Bad Request"
	case 401:
		return "Unauthorized"
	case 403:
		return "Forbidden"
	case 404:
		return "Not Found"
	case 409:
		return "Conflict"
	case 422:
		return "Unprocessable Entity"
	case 500:
		return "Internal Server Error"
	default:
		return "OK"
	}
}

func successStatus(method string) (string, int) {
	switch method {
	case "POST":
		return "Created", 201
	case "DELETE":
		return "OK", 200
	default:
		return "OK", 200
	}
}

func (g *generator) responseBodyExample(r route) any {
	switch r.Handler {
	case "CloneTask":
		return map[string]any{
			"root": map[string]any{
				"source_id": 1,
				"cloned_id": 2,
			},
			"children": []any{
				map[string]any{"source_id": 3, "cloned_id": 4},
			},
		}
	case "ListAllTasks":
		return map[string]any{
			"items":      []any{g.exampleForType("SimpleTaskResponse")},
			"nextCursor": "",
			"prevCursor": "",
		}
	case "ListTasksForPath":
		return []any{g.exampleForType("Task")}
	case "GetTaskFamily":
		return g.exampleForType("TaskFamilyResponse")
	case "GetTask", "GetTaskByID":
		return g.exampleForType("TaskWithTextResponse")
	default:
		if inferred, ok := g.inferResponseBody(r.Handler); ok {
			return inferred
		}
		if r.Method == "GET" && strings.HasPrefix(r.DisplayName, "List") {
			return []any{map[string]any{"example": true}}
		}
		if body := g.handlerBody[r.Handler]; body != nil && methodCanHaveBody(r.Method) {
			return map[string]any{"ok": true}
		}
		return map[string]any{"status": "ok"}
	}
}

func (g *generator) inferResponseBody(handlerName string) (any, bool) {
	fn := g.handlerFunc(handlerName)
	if fn == nil || fn.Body == nil {
		return nil, false
	}

	varTypes := map[string]ast.Expr{}
	localStructs := map[string]*ast.StructType{}
	for _, stmt := range fn.Body.List {
		g.captureResponseVarTypes(stmt, varTypes, localStructs)
	}

	var responseExpr ast.Expr
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if responseExpr != nil {
			return false
		}
		ret, ok := n.(*ast.ReturnStmt)
		if !ok {
			return true
		}
		for _, result := range ret.Results {
			call, ok := result.(*ast.CallExpr)
			if !ok || len(call.Args) < 2 {
				continue
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "JSON" {
				continue
			}
			responseExpr = call.Args[1]
			return false
		}
		return true
	})
	if responseExpr == nil {
		return nil, false
	}
	inferred := g.exampleFromResponseExpr(responseExpr, varTypes, localStructs, 0)
	if !meaningfulInferredExample(inferred) {
		return nil, false
	}
	return inferred, true
}

func (g *generator) captureResponseVarTypes(stmt ast.Stmt, varTypes map[string]ast.Expr, localStructs map[string]*ast.StructType) {
	switch s := stmt.(type) {
	case *ast.DeclStmt:
		decl, ok := s.Decl.(*ast.GenDecl)
		if !ok {
			return
		}
		for _, spec := range decl.Specs {
			switch x := spec.(type) {
			case *ast.TypeSpec:
				if st, ok := x.Type.(*ast.StructType); ok {
					localStructs[x.Name.Name] = st
				}
			case *ast.ValueSpec:
				for i, name := range x.Names {
					if x.Type != nil {
						varTypes[name.Name] = x.Type
						continue
					}
					if i < len(x.Values) {
						if typ := g.typeFromValueExpr(x.Values[i]); typ != nil {
							varTypes[name.Name] = typ
						}
					}
				}
			}
		}
	case *ast.AssignStmt:
		if s.Tok != token.DEFINE && s.Tok != token.ASSIGN {
			return
		}
		if len(s.Rhs) == 1 {
			returnTypes := g.returnTypesFromCall(s.Rhs[0])
			if len(returnTypes) > 0 {
				for i, lhs := range s.Lhs {
					id, ok := lhs.(*ast.Ident)
					if !ok || id.Name == "_" || i >= len(returnTypes) {
						continue
					}
					varTypes[id.Name] = returnTypes[i]
				}
				return
			}
		}
		for i, lhs := range s.Lhs {
			id, ok := lhs.(*ast.Ident)
			if !ok || id.Name == "_" || i >= len(s.Rhs) {
				continue
			}
			if typ := g.typeFromValueExpr(s.Rhs[i]); typ != nil {
				varTypes[id.Name] = typ
			}
		}
	}
}

func (g *generator) typeFromValueExpr(expr ast.Expr) ast.Expr {
	switch x := expr.(type) {
	case *ast.CompositeLit:
		return x.Type
	case *ast.UnaryExpr:
		if x.Op == token.AND {
			return g.typeFromValueExpr(x.X)
		}
	case *ast.CallExpr:
		if ret := g.firstReturnTypeFromCall(x); ret != nil {
			return ret
		}
		return x.Fun
	}
	return nil
}

func (g *generator) returnTypesFromCall(expr ast.Expr) []ast.Expr {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return nil
	}
	name := calledSelectorName(call.Fun)
	if name == "" {
		return nil
	}
	return g.funcReturns[name]
}

func (g *generator) firstReturnTypeFromCall(call *ast.CallExpr) ast.Expr {
	returnTypes := g.returnTypesFromCall(call)
	if len(returnTypes) == 0 {
		return nil
	}
	return returnTypes[0]
}

func calledSelectorName(expr ast.Expr) string {
	switch x := expr.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		return x.Sel.Name
	default:
		return ""
	}
}

func (g *generator) exampleFromResponseExpr(expr ast.Expr, varTypes map[string]ast.Expr, localStructs map[string]*ast.StructType, depth int) any {
	if depth > 10 || expr == nil {
		return map[string]any{}
	}
	switch x := expr.(type) {
	case *ast.Ident:
		if typ := varTypes[x.Name]; typ != nil {
			if id, ok := typ.(*ast.Ident); ok {
				if st := localStructs[id.Name]; st != nil {
					return g.exampleFromStruct(st, depth+1)
				}
			}
			return g.exampleFromExpr(typ, depth+1)
		}
	case *ast.UnaryExpr:
		if x.Op == token.AND {
			return g.exampleFromResponseExpr(x.X, varTypes, localStructs, depth+1)
		}
	case *ast.CompositeLit:
		return g.exampleFromCompositeLit(x, varTypes, localStructs, depth+1)
	case *ast.CallExpr:
		if ret := g.firstReturnTypeFromCall(x); ret != nil {
			return g.exampleFromExpr(ret, depth+1)
		}
	}
	return g.exampleFromExpr(expr, depth+1)
}

func (g *generator) exampleFromCompositeLit(lit *ast.CompositeLit, varTypes map[string]ast.Expr, localStructs map[string]*ast.StructType, depth int) any {
	if lit == nil {
		return map[string]any{}
	}
	if isMapLikeLiteralType(lit.Type) {
		out := map[string]any{}
		for _, elt := range lit.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, ok := literalString(kv.Key)
			if !ok || key == "" {
				continue
			}
			out[key] = g.exampleFromLiteralValue(kv.Value, varTypes, localStructs, depth+1)
		}
		return out
	}
	if _, ok := lit.Type.(*ast.ArrayType); ok {
		out := make([]any, 0, len(lit.Elts))
		for _, elt := range lit.Elts {
			out = append(out, g.exampleFromLiteralValue(elt, varTypes, localStructs, depth+1))
		}
		if len(out) == 0 {
			return g.exampleFromExpr(lit.Type, depth+1)
		}
		return out
	}
	return g.exampleFromExpr(lit.Type, depth+1)
}

func (g *generator) exampleFromLiteralValue(expr ast.Expr, varTypes map[string]ast.Expr, localStructs map[string]*ast.StructType, depth int) any {
	switch x := expr.(type) {
	case *ast.BasicLit:
		switch x.Kind {
		case token.STRING:
			value, err := strconv.Unquote(x.Value)
			if err == nil {
				return value
			}
		case token.INT:
			value, err := strconv.Atoi(x.Value)
			if err == nil {
				return value
			}
		case token.FLOAT:
			value, err := strconv.ParseFloat(x.Value, 64)
			if err == nil {
				return value
			}
		}
	case *ast.Ident:
		switch x.Name {
		case "true":
			return true
		case "false":
			return false
		case "nil":
			return nil
		}
	case *ast.CompositeLit:
		return g.exampleFromCompositeLit(x, varTypes, localStructs, depth+1)
	}
	return g.exampleFromResponseExpr(expr, varTypes, localStructs, depth+1)
}

func isMapLikeLiteralType(expr ast.Expr) bool {
	switch x := expr.(type) {
	case *ast.MapType:
		return true
	case *ast.SelectorExpr:
		return exprString(x) == "echo.Map"
	case *ast.Ident:
		return x.Name == "Map"
	default:
		return false
	}
}

func literalString(expr ast.Expr) (string, bool) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(lit.Value)
	return value, err == nil
}

func meaningfulInferredExample(example any) bool {
	switch x := example.(type) {
	case string:
		return x != "" && x != "example"
	case map[string]any:
		return len(x) > 0
	case []any:
		return len(x) > 0
	default:
		return example != nil
	}
}

func (g *generator) exampleForType(typeName string) any {
	if st := g.structs[typeName]; st != nil {
		return g.exampleFromStruct(st, 0)
	}
	return map[string]any{"example": true}
}

func (g *generator) exampleFromExpr(expr ast.Expr, depth int) any {
	if depth > 10 || expr == nil {
		return map[string]any{}
	}
	switch t := expr.(type) {
	case *ast.Ident:
		if st := g.structs[t.Name]; st != nil {
			return g.exampleFromStruct(st, depth+1)
		}
		return scalarExample(t.Name)
	case *ast.SelectorExpr:
		name := exprString(t)
		if st := g.structs[name]; st != nil {
			return g.exampleFromStruct(st, depth+1)
		}
		return selectorExample(name)
	case *ast.StarExpr:
		return g.exampleFromExpr(t.X, depth+1)
	case *ast.ArrayType:
		return []any{g.exampleFromExpr(t.Elt, depth+1)}
	case *ast.MapType:
		return map[string]any{"example": g.exampleFromExpr(t.Value, depth+1)}
	case *ast.StructType:
		return g.exampleFromStruct(t, depth+1)
	case *ast.InterfaceType:
		return map[string]any{}
	case *ast.IndexExpr:
		return g.exampleFromGenericExpr(t.X, []ast.Expr{t.Index}, depth+1)
	case *ast.IndexListExpr:
		return g.exampleFromGenericExpr(t.X, t.Indices, depth+1)
	default:
		return "example"
	}
}

func (g *generator) exampleFromGenericExpr(base ast.Expr, indices []ast.Expr, depth int) any {
	baseName := exprString(base)
	switch baseName {
	case "CursorPage", "services.CursorPage":
		var item any = map[string]any{}
		if len(indices) > 0 {
			item = g.exampleFromExpr(indices[0], depth+1)
		}
		return map[string]any{
			"items":      []any{item},
			"nextCursor": "",
			"prevCursor": "",
		}
	default:
		return g.exampleFromExpr(base, depth+1)
	}
}

func (g *generator) exampleFromStruct(st *ast.StructType, depth int) map[string]any {
	out := map[string]any{}
	if st == nil || st.Fields == nil {
		return out
	}
	for _, field := range st.Fields.List {
		if len(field.Names) == 0 {
			if embedded, ok := g.exampleFromExpr(field.Type, depth+1).(map[string]any); ok {
				for key, value := range embedded {
					out[key] = value
				}
			}
			continue
		}
		name := jsonFieldName(field)
		if name == "" || name == "-" {
			continue
		}
		out[name] = g.exampleForField(name, field.Type, depth+1)
	}
	return out
}

func (g *generator) exampleForField(name string, expr ast.Expr, depth int) any {
	switch strings.ToLower(name) {
	case "mode":
		return "as_child"
	case "status":
		return "draft"
	case "kind":
		return "quant"
	case "per_side":
		return "both"
	case "parent_id", "task_id", "player_id", "path_id", "stage_id", "field_id", "lesson_id", "list_id":
		return 1
	case "limit":
		return 20
	case "order":
		return "desc"
	case "after", "before", "query":
		return ""
	}
	return g.exampleFromExpr(expr, depth)
}

func jsonFieldName(field *ast.Field) string {
	if field.Tag != nil {
		tag, err := strconv.Unquote(field.Tag.Value)
		if err == nil {
			jsonTag := reflectTag(tag, "json")
			if jsonTag == "-" {
				return "-"
			}
			if jsonTag != "" {
				return strings.Split(jsonTag, ",")[0]
			}
		}
	}
	if len(field.Names) == 0 {
		return ""
	}
	return lowerFirst(field.Names[0].Name)
}

func reflectTag(tag, key string) string {
	for tag != "" {
		tag = strings.TrimLeft(tag, " ")
		if tag == "" {
			break
		}
		i := strings.Index(tag, ":")
		if i <= 0 {
			break
		}
		k := tag[:i]
		tag = tag[i+1:]
		if tag == "" || tag[0] != '"' {
			break
		}
		q, err := strconv.QuotedPrefix(tag)
		if err != nil {
			break
		}
		tag = tag[len(q):]
		v, _ := strconv.Unquote(q)
		if k == key {
			return v
		}
	}
	return ""
}

func scalarExample(name string) any {
	switch name {
	case "string":
		return "example"
	case "bool":
		return true
	case "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64":
		return 1
	case "float32", "float64":
		return 1.23
	case "RawMessage":
		return map[string]any{"example": true}
	case "TaskKind":
		return "quant"
	case "TaskSide":
		return "both"
	case "TaskStatus":
		return "draft"
	default:
		return "example"
	}
}

func selectorExample(name string) any {
	switch name {
	case "json.RawMessage", "datatypes.JSON":
		return map[string]any{"example": true}
	case "models.TaskKind":
		return "quant"
	case "models.TaskSide":
		return "both"
	case "models.TaskStatus":
		return "draft"
	case "uuid.UUID":
		return "00000000-0000-0000-0000-000000000000"
	case "time.Time":
		return "2026-01-01T00:00:00Z"
	default:
		return "example"
	}
}

func callFromStmt(stmt ast.Stmt) *ast.CallExpr {
	exprStmt, ok := stmt.(*ast.ExprStmt)
	if !ok {
		return nil
	}
	call, _ := exprStmt.X.(*ast.CallExpr)
	return call
}

func calledFuncName(expr ast.Expr) string {
	if id, ok := expr.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

func firstParamName(fn *ast.FuncDecl) string {
	if fn == nil || fn.Type == nil || fn.Type.Params == nil || len(fn.Type.Params.List) == 0 {
		return ""
	}
	first := fn.Type.Params.List[0]
	if len(first.Names) == 0 {
		return ""
	}
	return first.Names[0].Name
}

func isHTTPRegistration(method string) bool {
	switch method {
	case "GET", "POST", "PUT", "PATCH", "DELETE", "ANY":
		return true
	default:
		return false
	}
}

func methodCanHaveBody(method string) bool {
	switch method {
	case "POST", "PUT", "PATCH", "DELETE":
		return true
	default:
		return false
	}
}

func stringLiteral(expr ast.Expr) (string, bool) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(lit.Value)
	return value, err == nil
}

func handlerName(expr ast.Expr) string {
	switch x := expr.(type) {
	case *ast.SelectorExpr:
		return x.Sel.Name
	case *ast.Ident:
		return x.Name
	case *ast.FuncLit:
		return ""
	case *ast.CallExpr:
		// Calls are adapters or factories, not stable endpoint names. Falling
		// back to the route path avoids leaking names such as WrapHandler.
		return ""
	default:
		return ""
	}
}

func routeDisplayName(handler, override, path string) string {
	override = strings.TrimSpace(override)
	if override != "" {
		return override
	}
	handler = strings.TrimSpace(handler)
	if handler != "" {
		return handler
	}
	if name := routePathDisplayName(path); name != "" {
		return name
	}
	return "Request"
}

func routePathDisplayName(path string) string {
	parts := pathParts(cleanPath(path))
	for i := len(parts) - 1; i >= 0; i-- {
		part := strings.TrimSpace(parts[i])
		if part == "" || strings.HasPrefix(part, ":") || strings.HasPrefix(part, "{") {
			continue
		}
		return titleForSegment(part)
	}
	return ""
}

func (g *generator) routeCommentValue(stmt ast.Stmt, key string) string {
	pos := stmt.End()
	for _, group := range g.routesFile.Comments {
		if group == nil {
			continue
		}
		groupPos := group.Pos()
		if groupPos < pos {
			continue
		}
		stmtEnd := g.fset.Position(pos)
		commentStart := g.fset.Position(groupPos)
		if stmtEnd.Filename != commentStart.Filename || stmtEnd.Line != commentStart.Line {
			continue
		}
		for _, comment := range group.List {
			if value := parseRouteCommentValue(comment.Text, key); value != "" {
				return value
			}
		}
	}
	return ""
}

func parseRouteCommentValue(text, expectedKey string) string {
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "//")
	text = strings.TrimSpace(text)
	expectedKey = strings.ToLower(strings.TrimSpace(expectedKey))
	for _, part := range strings.Split(text, ",") {
		key, value, ok := strings.Cut(part, ":")
		if !ok || strings.TrimSpace(strings.ToLower(key)) != expectedKey {
			continue
		}
		return strings.TrimSpace(value)
	}
	return ""
}

func exprString(expr ast.Expr) string {
	switch x := expr.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		return exprString(x.X) + "." + x.Sel.Name
	case *ast.StarExpr:
		return exprString(x.X)
	case *ast.ArrayType:
		return "[]" + exprString(x.Elt)
	case *ast.MapType:
		return "map[" + exprString(x.Key) + "]" + exprString(x.Value)
	case *ast.IndexExpr:
		return exprString(x.X) + "[" + exprString(x.Index) + "]"
	case *ast.IndexListExpr:
		parts := make([]string, 0, len(x.Indices))
		for _, index := range x.Indices {
			parts = append(parts, exprString(index))
		}
		return exprString(x.X) + "[" + strings.Join(parts, ", ") + "]"
	default:
		return fmt.Sprintf("%T", expr)
	}
}

func cleanPath(path string) string {
	if path == "" {
		return ""
	}
	for strings.Contains(path, "//") {
		path = strings.ReplaceAll(path, "//", "/")
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if len(path) > 1 && strings.HasSuffix(path, "/") {
		path = strings.TrimRight(path, "/")
	}
	return path
}

func pathParts(path string) []string {
	path = strings.Trim(path, "/")
	if path == "" {
		return nil
	}
	return strings.Split(path, "/")
}

func isAdminRoute(path string) bool {
	if strings.HasPrefix(path, "/admin") {
		return true
	}
	switch path {
	case "/metrics", "/ping":
		return true
	}
	return false
}

func routeUsesUploadURL(r route) bool {
	return r.Handler == "TusUploadHandler" || strings.HasPrefix(r.Path, "/upload") || strings.HasPrefix(r.Path, "/v1/upload")
}

func routeRequiresBearerAuth(r route) bool {
	if r.Admin || isAdminRoute(r.Path) {
		return false
	}
	if r.AuthMode == routeAuthPublic {
		return false
	}
	if r.AuthMode == routeAuthBearer {
		return true
	}
	if routeUsesUploadURL(r) {
		return true
	}
	if strings.HasPrefix(r.Path, "/v1/") && r.Path != "/v1/error" {
		return true
	}
	return isAuthenticatedPaymentRoute(r.Path)
}

func isAuthenticatedPaymentRoute(path string) bool {
	switch path {
	case "/payment/create-checkout-session",
		"/payment/create-subscription-session",
		"/payment/subscription/list",
		"/payment/subscription/cancel",
		"/payment/status",
		"/payment/portal",
		"/payment/verify/google",
		"/payment/subscription/by-code":
		return true
	default:
		return false
	}
}

func adminCanonicalPath(path string) string {
	path = cleanPath(path)
	if path == "/admin" {
		return "/"
	}
	if strings.HasPrefix(path, "/admin/") {
		return cleanPath(strings.TrimPrefix(path, "/admin"))
	}
	return path
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

func cloneMap(in map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range in {
		out[k] = v
	}
	return out
}
