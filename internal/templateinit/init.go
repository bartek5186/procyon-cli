package templateinit

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
)

const templateModule = "github.com/bartek5186/procyon"

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

	sourceDir, err := findTemplateRoot(wd)
	if err != nil {
		return err
	}

	if err := prepareOutputDir(opts.OutputDir, opts.Force); err != nil {
		return err
	}

	if err := copyTemplate(sourceDir, opts.OutputDir, opts); err != nil {
		return err
	}
	if err := rewriteProject(opts.OutputDir, opts); err != nil {
		return err
	}
	if err := runGofmt(opts.OutputDir); err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout, "\nProject created in %s\n", opts.OutputDir)
	fmt.Fprintf(os.Stdout, "Next steps:\n")
	fmt.Fprintf(os.Stdout, "  cd %s\n", opts.OutputDir)
	fmt.Fprintf(os.Stdout, "  go run . -migrate=true\n")

	return nil
}

func completeOptions(opts Options, wd string, in io.Reader, out io.Writer) (Options, error) {
	reader := bufio.NewReader(in)

	if opts.Name == "" {
		opts.Name = prompt(reader, out, "Project name", filepath.Base(wd))
	}
	if opts.Module == "" {
		opts.Module = prompt(reader, out, "Go module", "github.com/acme/"+slug(opts.Name))
	}
	if opts.OutputDir == "" {
		opts.OutputDir = prompt(reader, out, "Output directory", "../"+slug(opts.Name))
	}
	if opts.Database == "" {
		opts.Database = promptChoice(reader, out, "Database", []choice{
			{Value: "postgres", Label: "PostgreSQL"},
			{Value: "mysql", Label: "MySQL"},
		})
	}
	if opts.Auth == "" {
		opts.Auth = promptChoice(reader, out, "Auth", []choice{
			{Value: "kratos-casbin", Label: "Kratos + Casbin"},
			{Value: "kratos", Label: "Kratos only"},
			{Value: "admin", Label: "Admin key only"},
			{Value: "none", Label: "None"},
		})
	}

	opts.OutputDir = filepath.Clean(opts.OutputDir)
	return opts, nil
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

func findTemplateRoot(wd string) (string, error) {
	dir := wd
	for {
		if fileExists(filepath.Join(dir, "go.mod")) &&
			fileExists(filepath.Join(dir, "main.go")) &&
			fileExists(filepath.Join(dir, "internal", "config.go")) &&
			fileExists(filepath.Join(dir, "config", "config.example.json")) {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("unable to find procyon template root")
		}
		dir = parent
	}
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
			if rel == "cmd" || rel == "internal/templateinit" {
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
		templateModule:          opts.Module,
		"github.com/bartek5186": moduleParent(opts.Module),
		"Procyon":               opts.Name,
		"procyon-server":        slug(opts.Name),
		"procyon-api":           slug(opts.Name) + "-api",
		"procyon-mysql":         slug(opts.Name) + "-mysql",
		"procyon":               slug(opts.Name),
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
		text := string(raw)
		for old, next := range replacements {
			text = strings.ReplaceAll(text, old, next)
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
	cfg["auth"] = authConfig(opts.Auth)
	cfg["rbac"] = map[string]any{"enabled": opts.Auth == "kratos-casbin"}
	cfg["admin"] = map[string]any{
		"enabled":    opts.Auth == "kratos-casbin" || opts.Auth == "admin",
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

func authConfig(mode string) map[string]any {
	switch mode {
	case "kratos-casbin", "kratos":
		return map[string]any{
			"enabled":  true,
			"provider": "kratos",
			"domain":   "http://127.0.0.1:4433",
		}
	default:
		return map[string]any{
			"enabled":  false,
			"provider": "kratos",
			"domain":   "",
		}
	}
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

func moduleParent(module string) string {
	i := strings.LastIndex(module, "/")
	if i == -1 {
		return module
	}
	return module[:i]
}
