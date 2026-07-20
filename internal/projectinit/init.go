package projectinit

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/bartek5186/procyon-cli/internal/buildinfo"
)

const templateModule = "github.com/bartek5186/procyon"
const templateRepoURL = "https://github.com/bartek5186/procyon"
const coreModule = "github.com/bartek5186/procyon-core"
const coreModulePlaceholder = "__PROCYON_CORE_MODULE__"
const templateModulePlaceholder = "__PROCYON_TEMPLATE_MODULE__"
const generatorMarker = "procyon:"
const generatorMarkerPlaceholder = "__PROCYON_GENERATOR_MARKER__"

var frameworkTextPlaceholders = []struct {
	text        string
	placeholder string
}{
	{text: ".procyon.json", placeholder: "__PROCYON_PROJECT_METADATA_FILE__"},
	{text: "procyon-cli", placeholder: "__PROCYON_CLI_NAME__"},
	{text: "procyon-core", placeholder: "__PROCYON_CORE_NAME__"},
	{text: "Procyon Core", placeholder: "__PROCYON_CORE_TITLE__"},
}

var skipDirs = map[string]struct{}{
	".git":        {},
	".gocache":    {},
	".gomodcache": {},
	"build":       {},
	"log":         {},
	"procyon-cli": {},
	"tmp":         {},
}

func Run(opts Options) error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}

	opts, err = completeOptions(opts, wd, os.Stdin, os.Stdout)
	if err != nil {
		return err
	}

	if err := validateOptions(opts); err != nil {
		return err
	}

	sourceDir, cleanup, err := resolveTemplateSource(wd, opts.TemplateVersion)
	if err != nil {
		return err
	}
	defer cleanup()

	if err := prepareOutputDir(opts.OutputDir, opts.Force); err != nil {
		return err
	}

	if err := copyTemplate(sourceDir, opts.OutputDir, opts); err != nil {
		return err
	}
	if !opts.IncludeHello {
		if err := removeHelloWiring(opts.OutputDir); err != nil {
			return err
		}
	}
	if err := rewriteProject(opts.OutputDir, opts); err != nil {
		return err
	}
	if err := writeProjectMetadata(opts.OutputDir, opts); err != nil {
		return err
	}
	if err := runGofmt(opts.OutputDir); err != nil {
		return err
	}
	if err := runGoModTidy(opts.OutputDir); err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout, "\nProject created in %s\n", opts.OutputDir)
	fmt.Fprintf(os.Stdout, "Next steps:\n")
	fmt.Fprintf(os.Stdout, "  cd %s\n", opts.OutputDir)
	fmt.Fprintf(os.Stdout, "  go run . -migrate=true\n")

	return nil
}

func removeHelloWiring(root string) error {
	lineRemovals := map[string][]string{
		"app.go": {
			"/controllers\"",
			"hello *controllers.HelloController",
			"hello: controllers.NewHelloController",
		},
		"routes.go": {
			"app.hello.",
			"securedAdmin :=",
		},
		"store/appStore.go": {
			"Hello() *HelloStore",
			"hello *HelloStore",
			"hello: NewHelloStore",
		},
		"services/appService.go": {
			"Hello",
		},
		"internal/migrate.go": {
			"/models\"",
			"&models.HelloMessage{}",
		},
		"policies.go": {
			"Object: \"hello\"",
		},
	}

	for rel, needles := range lineRemovals {
		path := filepath.Join(root, rel)
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		next := string(raw)
		if rel == "store/appStore.go" {
			next, err = removeGoFunction(next, "func (s *AppStore) Hello()")
			if err != nil {
				return err
			}
		}
		lines := strings.Split(next, "\n")
		out := lines[:0]
		for _, line := range lines {
			remove := false
			for _, needle := range needles {
				if strings.Contains(line, needle) {
					remove = true
					break
				}
			}
			if !remove {
				out = append(out, line)
			}
		}
		next = strings.Join(out, "\n")
		if err := os.WriteFile(path, []byte(next), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func removeGoFunction(src, signature string) (string, error) {
	start := strings.Index(src, signature)
	if start < 0 {
		return "", fmt.Errorf("optional hello wiring marker not found: %s", signature)
	}
	bodyStart := strings.Index(src[start:], "{")
	if bodyStart < 0 {
		return "", fmt.Errorf("function body not found: %s", signature)
	}
	bodyStart += start
	depth := 0
	for i := bodyStart; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				end := i + 1
				for end < len(src) && (src[end] == '\n' || src[end] == '\r') {
					end++
				}
				return src[:start] + src[end:], nil
			}
		}
	}
	return "", fmt.Errorf("unterminated function: %s", signature)
}

func completeOptions(opts Options, wd string, in io.Reader, out io.Writer) (Options, error) {
	if needsInteractiveInput(opts) && isInteractiveTerminal(in, out) {
		return completeOptionsTUI(opts, wd)
	}

	reader := bufio.NewReader(in)

	if opts.Name == "" {
		opts.Name = prompt(reader, out, "Project name", filepath.Base(wd))
	}
	if opts.Module == "" {
		opts.Module = prompt(reader, out, "Go module", "github.com/acme/"+slug(opts.Name))
	}
	if opts.OutputDir == "" {
		opts.OutputDir = prompt(reader, out, "Output directory", "./"+slug(opts.Name))
	}
	if opts.Database == "" {
		opts.Database = promptChoice(reader, out, "Database", []choice{
			{Value: "postgres", Label: "PostgreSQL"},
			{Value: "mysql", Label: "MySQL"},
		})
	}
	if opts.TemplateVersion == "" {
		opts.TemplateVersion = buildinfo.TemplateVersion
	}
	if opts.Auth == "" {
		opts.Auth = "kratos-casbin"
	}

	opts.OutputDir = cleanOutputDir(opts.OutputDir)
	return opts, nil
}

func needsInteractiveInput(opts Options) bool {
	return opts.Name == "" || opts.Module == "" || opts.OutputDir == "" || opts.Database == ""
}

func isInteractiveTerminal(in io.Reader, out io.Writer) bool {
	inFile, inOK := in.(*os.File)
	outFile, outOK := out.(*os.File)
	if !inOK || !outOK {
		return false
	}
	inInfo, inErr := inFile.Stat()
	outInfo, outErr := outFile.Stat()
	return inErr == nil && outErr == nil &&
		inInfo.Mode()&os.ModeCharDevice != 0 &&
		outInfo.Mode()&os.ModeCharDevice != 0
}

func cleanOutputDir(value string) string {
	value = strings.TrimSpace(value)
	hadCurrentPrefix := strings.HasPrefix(value, "."+string(filepath.Separator))
	cleaned := filepath.Clean(value)
	if hadCurrentPrefix && cleaned != "." && !filepath.IsAbs(cleaned) {
		return "." + string(filepath.Separator) + cleaned
	}
	return cleaned
}

func validateOptions(opts Options) error {
	if strings.TrimSpace(opts.Name) == "" {
		return errors.New("project name is required")
	}
	if strings.TrimSpace(opts.Module) == "" {
		return errors.New("go module is required")
	}
	switch opts.Database {
	case "postgres", "mysql":
	default:
		return fmt.Errorf("unsupported database %q", opts.Database)
	}
	switch opts.Auth {
	case "kratos-casbin", "kratos", "admin", "none":
	default:
		return fmt.Errorf("unsupported auth mode %q", opts.Auth)
	}
	return nil
}

func resolveTemplateSource(wd, templateVersion string) (string, func(), error) {
	if sourceDir, err := findTemplateRoot(wd); err == nil {
		return sourceDir, func() {}, nil
	}

	tmpDir, err := os.MkdirTemp("", "procyon-template-*")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() {
		_ = os.RemoveAll(tmpDir)
	}

	args := []string{"clone", "--depth=1"}
	if strings.TrimSpace(templateVersion) != "" {
		args = append(args, "--branch", templateVersion)
	}
	args = append(args, templateRepoURL, tmpDir)
	cmd := exec.Command("git", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("clone template from %s: %w: %s", templateRepoURL, err, strings.TrimSpace(string(out)))
	}
	if !isTemplateRoot(tmpDir) {
		cleanup()
		return "", nil, fmt.Errorf("cloned repository %s does not look like a Procyon template", templateRepoURL)
	}

	return tmpDir, cleanup, nil
}

type projectMetadata struct {
	SchemaVersion   int    `json:"schema_version"`
	ProjectModule   string `json:"project_module"`
	TemplateVersion string `json:"template_version"`
	CoreVersion     string `json:"core_version"`
	CLIMinVersion   string `json:"cli_min_version"`
}

func writeProjectMetadata(root string, opts Options) error {
	metadata := projectMetadata{
		SchemaVersion:   1,
		ProjectModule:   opts.Module,
		TemplateVersion: opts.TemplateVersion,
		CoreVersion:     buildinfo.CoreVersion,
		CLIMinVersion:   buildinfo.CLIVersion,
	}
	raw, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return os.WriteFile(filepath.Join(root, ".procyon.json"), raw, 0o644)
}

func findTemplateRoot(wd string) (string, error) {
	dir := wd
	for {
		if isTemplateRoot(dir) {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("unable to find procyon template root")
		}
		dir = parent
	}
}

func isTemplateRoot(dir string) bool {
	return fileExists(filepath.Join(dir, "go.mod")) &&
		fileExists(filepath.Join(dir, "main.go")) &&
		fileExists(filepath.Join(dir, "app.go")) &&
		fileExists(filepath.Join(dir, "policies.go")) &&
		fileExists(filepath.Join(dir, "config", "config.example.json"))
}

func prepareOutputDir(out string, force bool) error {
	info, err := os.Stat(out)
	if err != nil {
		if os.IsNotExist(err) {
			return os.MkdirAll(out, 0o755)
		}
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("output path %s exists and is not a directory", out)
	}

	entries, err := os.ReadDir(out)
	if err != nil {
		return err
	}
	if len(entries) > 0 && !force {
		return fmt.Errorf("output directory %s is not empty; use --force to continue", out)
	}
	return nil
}

func copyTemplate(source, dest string, opts Options) error {
	return filepath.WalkDir(source, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}

		name := d.Name()
		if d.IsDir() {
			if _, ok := skipDirs[name]; ok {
				return filepath.SkipDir
			}
			if rel == "internal/projectinit" {
				return filepath.SkipDir
			}
			if !opts.IncludeDocker && (rel == "docker" || rel == ".github") {
				return filepath.SkipDir
			}
			if !opts.IncludeHello && isHelloPath(rel) {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(dest, rel), 0o755)
		}

		if shouldSkipFile(rel, opts) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return copyFile(path, filepath.Join(dest, rel), info.Mode())
	})
}

func shouldSkipFile(rel string, opts Options) bool {
	base := filepath.Base(rel)
	if base == ".codex" || base == ".env" {
		return true
	}
	if !opts.IncludeDocker {
		switch rel {
		case "Dockerfile", "compose.yaml", ".dockerignore", "deploy.sh", "prod.deploy.sh":
			return true
		}
	}
	if !opts.IncludeHello && isHelloPath(rel) {
		return true
	}
	return false
}

func isHelloPath(rel string) bool {
	base := filepath.Base(rel)
	return strings.Contains(strings.ToLower(base), "hello")
}

func copyFile(src, dst string, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if mode.Perm() == 0 {
		mode = 0o644
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode.Perm())
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

func rewriteProject(root string, opts Options) error {
	replacements := map[string]string{
		templateModule:   opts.Module,
		"Procyon":        opts.Name,
		"procyon-server": slug(opts.Name),
		"procyon-api":    slug(opts.Name) + "-api",
		"procyon-mysql":  slug(opts.Name) + "-mysql",
		"procyon":        slug(opts.Name),
	}

	if err := replaceTextFiles(root, replacements); err != nil {
		return err
	}
	if err := rewriteConfigFile(filepath.Join(root, "config", "config.example.json"), opts); err != nil {
		return err
	}
	if err := rewriteConfigFile(filepath.Join(root, "config", "config.docker.json"), opts); err != nil {
		return err
	}
	if err := rewriteConfigFile(filepath.Join(root, "config", "config.json"), opts); err != nil {
		return err
	}

	if opts.Database == "postgres" {
		_ = os.Remove(filepath.Join(root, "config", "config.postgres.example.json"))
	} else {
		_ = os.Rename(filepath.Join(root, "config", "config.example.json"), filepath.Join(root, "config", "config.mysql.example.json"))
	}

	return nil
}

func replaceTextFiles(root string, replacements map[string]string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !isTextFile(path) {
			return nil
		}

		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := strings.ReplaceAll(string(raw), coreModule, coreModulePlaceholder)
		text = strings.ReplaceAll(text, templateModule, templateModulePlaceholder)
		text = strings.ReplaceAll(text, generatorMarker, generatorMarkerPlaceholder)
		for _, protected := range frameworkTextPlaceholders {
			text = strings.ReplaceAll(text, protected.text, protected.placeholder)
		}
		for old, next := range replacements {
			text = strings.ReplaceAll(text, old, next)
		}
		text = strings.ReplaceAll(text, templateModulePlaceholder, replacements[templateModule])
		text = strings.ReplaceAll(text, coreModulePlaceholder, coreModule)
		text = strings.ReplaceAll(text, generatorMarkerPlaceholder, generatorMarker)
		for _, protected := range frameworkTextPlaceholders {
			text = strings.ReplaceAll(text, protected.placeholder, protected.text)
		}
		return os.WriteFile(path, []byte(text), 0o644)
	})
}

func rewriteConfigFile(path string, opts Options) error {
	if !fileExists(path) {
		return nil
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return err
	}

	cfg["app_name"] = opts.Name
	cfg["auth_domain"] = "http://127.0.0.1:4433"
	authEnabled := opts.Auth == "kratos-casbin" || opts.Auth == "kratos"
	rbacEnabled := opts.Auth == "kratos-casbin"
	adminEnabled := opts.Auth != "none"
	cfg["auth"] = map[string]any{
		"enabled":  authEnabled,
		"provider": "kratos",
		"domain":   "http://127.0.0.1:4433",
	}
	cfg["rbac"] = map[string]any{"enabled": rbacEnabled}
	cfg["admin"] = map[string]any{
		"enabled":    adminEnabled,
		"secret_key": "CHANGE_ME_ADMIN_KEY",
	}

	obs, _ := cfg["observability"].(map[string]any)
	if obs == nil {
		obs = map[string]any{}
	}
	obs["service_name"] = slug(opts.Name)
	obs["namespace"] = slug(opts.Name)
	cfg["observability"] = obs

	db, _ := cfg["database"].(map[string]any)
	if db == nil {
		db = map[string]any{}
	}
	db["auto_migrate"] = true
	if opts.Database == "postgres" {
		db["driver"] = "postgres"
		db["host"] = "127.0.0.1"
		db["user"] = "postgres"
		db["password"] = "postgres"
		db["dbname"] = slug(opts.Name)
		db["port"] = float64(5432)
		db["sslmode"] = "disable"
		db["migrations_dir"] = "migrations/postgres"
		delete(db, "charset")
	} else {
		db["driver"] = "mysql"
		db["host"] = "127.0.0.1"
		db["user"] = "root"
		db["password"] = "root"
		db["dbname"] = slug(opts.Name)
		db["port"] = float64(3306)
		db["charset"] = "utf8mb4"
		db["migrations_dir"] = "migrations/mysql"
		delete(db, "sslmode")
	}
	cfg["database"] = db

	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')
	return os.WriteFile(path, out, 0o644)
}

func runGofmt(root string) error {
	cmd := exec.Command("gofmt", "-w", ".")
	cmd.Dir = root
	if runtime.GOOS == "windows" {
		cmd = exec.Command("gofmt", "-w", ".")
		cmd.Dir = root
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("gofmt: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func runGoModTidy(root string) error {
	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOWORK=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("go mod tidy: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func isTextFile(path string) bool {
	switch filepath.Ext(path) {
	case ".go", ".mod", ".sum", ".md", ".json", ".yaml", ".yml", ".sh", ".html", ".conf", ".example":
		return true
	default:
		base := filepath.Base(path)
		return strings.HasPrefix(base, ".") || base == "Dockerfile"
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func slug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "_", "-")
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-')
	})
	out := strings.Join(parts, "-")
	out = strings.Trim(out, "-")
	if out == "" {
		return "app"
	}
	return out
}
