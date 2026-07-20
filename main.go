package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/bartek5186/procyon-cli/internal/buildinfo"
	"github.com/bartek5186/procyon-cli/internal/dashboard"
	"github.com/bartek5186/procyon-cli/internal/localplugin"
	"github.com/bartek5186/procyon-cli/internal/modulegen"
	"github.com/bartek5186/procyon-cli/internal/moduleinstall"
	"github.com/bartek5186/procyon-cli/internal/postmangen"
	"github.com/bartek5186/procyon-cli/internal/projectinit"
	"github.com/bartek5186/procyon-cli/internal/projectupdate"
	"github.com/bartek5186/procyon-cli/internal/selfupdate"
)

func main() {
	if len(os.Args) < 2 {
		if err := dashboard.Run(); err != nil {
			if errors.Is(err, dashboard.ErrNonInteractive) {
				usage()
				os.Exit(2)
			}
			fmt.Fprintf(os.Stderr, "procyon-cli: %v\n", err)
			os.Exit(1)
		}
		return
	}

	switch os.Args[1] {
	case "init":
		initCmd := flag.NewFlagSet("procyon-cli init", flag.ExitOnError)
		opts := projectinit.Options{}
		initCmd.StringVar(&opts.Name, "name", "", "Project name")
		initCmd.StringVar(&opts.Module, "module", "", "Go module path")
		initCmd.StringVar(&opts.OutputDir, "out", "", "Output directory")
		initCmd.StringVar(&opts.Database, "db", "", "Database type: postgres or mysql")
		initCmd.StringVar(&opts.Auth, "auth", "kratos-casbin", "Auth mode: kratos-casbin, kratos, admin or none")
		initCmd.StringVar(&opts.TemplateVersion, "template-version", buildinfo.TemplateVersion, "Template Git tag or branch")
		initCmd.BoolVar(&opts.IncludeDocker, "docker", true, "Include Docker files")
		initCmd.BoolVar(&opts.IncludeHello, "hello", true, "Keep example hello feature")
		initCmd.BoolVar(&opts.Force, "force", false, "Allow non-empty output directory")
		_ = initCmd.Parse(os.Args[2:])

		if err := projectinit.Run(opts); err != nil {
			fmt.Fprintf(os.Stderr, "procyon-cli init: %v\n", err)
			os.Exit(1)
		}
	case "module":
		runModuleCommand(os.Args[2:])
	case "plugin":
		runPluginCommand(os.Args[2:])
	case "postman":
		runPostmanCommand(os.Args[2:])
	case "core":
		runCoreCommand(os.Args[2:])
	case "self-update":
		runSelfUpdate(os.Args[2:])
	case "add":
		if len(os.Args) >= 3 && os.Args[2] == "module" {
			runModuleAdd(os.Args[3:])
			return
		}
		usage()
		os.Exit(2)
	case "update":
		fmt.Fprintln(os.Stderr, "warning: `procyon-cli update` is deprecated; use `procyon-cli core update`")
		runCoreUpdate(os.Args[2:], "procyon-cli update")
	case "version":
		fmt.Printf("procyon-cli %s (template %s, core %s)\n", buildinfo.CLIVersion, buildinfo.TemplateVersion, buildinfo.CoreVersion)
	default:
		usage()
		os.Exit(2)
	}
}

func parseModuleCreateArgs(args []string) (modulegen.Options, error) {
	opts := modulegen.Options{}
	for _, arg := range args {
		switch arg {
		case "--force", "-force":
			opts.Force = true
		default:
			if opts.Name != "" {
				return opts, fmt.Errorf("unexpected argument %q", arg)
			}
			opts.Name = arg
		}
	}
	if opts.Name == "" {
		return opts, fmt.Errorf("module name is required")
	}
	return opts, nil
}

func usage() {
	fmt.Fprintf(os.Stderr, "usage:\n  procyon-cli init [flags]\n  procyon-cli plugin create <plugin-name>\n  procyon-cli module create <module_name> [--force]\n  procyon-cli module add <module_name> [flags]\n  procyon-cli module update <module_name> [flags]\n  procyon-cli module enable <module_name> [--dry-run]\n  procyon-cli module disable <module_name> [--dry-run]\n  procyon-cli module list\n  procyon-cli module info <module_name>\n  procyon-cli postman generate [flags]\n  procyon-cli postman sync [flags]\n  procyon-cli core update [--version v0.5.0] [--dry-run]\n  procyon-cli self-update [--version v0.5.0] [--dry-run]\n\n")
	fmt.Fprintf(os.Stderr, "examples:\n")
	fmt.Fprintf(os.Stderr, "  procyon-cli init\n")
	fmt.Fprintf(os.Stderr, "  procyon-cli init --name przyjazne-server --module github.com/acme/przyjazne-server --db postgres --out ../przyjazne-v2\n")
	fmt.Fprintf(os.Stderr, "  procyon-cli module create invoice\n")
	fmt.Fprintf(os.Stderr, "  procyon-cli plugin create leagues\n")
	fmt.Fprintf(os.Stderr, "  procyon-cli module add payment-system --provider stripe\n")
	fmt.Fprintf(os.Stderr, "  procyon-cli postman generate\n")
	fmt.Fprintf(os.Stderr, "  procyon-cli postman sync\n")
	fmt.Fprintf(os.Stderr, "  procyon-cli core update --version v0.5.0\n")
	fmt.Fprintf(os.Stderr, "  procyon-cli self-update --version v0.5.0\n")
}

func runPluginCommand(args []string) {
	if len(args) != 2 || args[0] != "create" {
		fmt.Fprintln(os.Stderr, "usage: procyon-cli plugin create <plugin-name>")
		os.Exit(2)
	}
	if err := localplugin.Create(localplugin.Options{Name: args[1]}); err != nil {
		fmt.Fprintf(os.Stderr, "procyon-cli plugin create: %v\n", err)
		os.Exit(1)
	}
}

func runCoreCommand(args []string) {
	if len(args) == 0 || args[0] != "update" {
		fmt.Fprintln(os.Stderr, "usage: procyon-cli core update [--version v0.5.0] [--dry-run]")
		os.Exit(2)
	}
	runCoreUpdate(args[1:], "procyon-cli core update")
}

func runCoreUpdate(args []string, commandName string) {
	flags := flag.NewFlagSet(commandName, flag.ExitOnError)
	opts := projectupdate.Options{}
	flags.StringVar(&opts.Version, "version", "latest", "Core version or latest")
	flags.BoolVar(&opts.DryRun, "dry-run", false, "Print commands without changing the project")
	_ = flags.Parse(args)
	if flags.NArg() != 0 {
		fmt.Fprintf(os.Stderr, "%s: unexpected arguments: %s\n", commandName, strings.Join(flags.Args(), " "))
		os.Exit(2)
	}
	if err := projectupdate.Run(opts); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", commandName, err)
		os.Exit(1)
	}
}

func runSelfUpdate(args []string) {
	flags := flag.NewFlagSet("procyon-cli self-update", flag.ExitOnError)
	opts := selfupdate.Options{}
	flags.StringVar(&opts.Version, "version", "latest", "CLI semantic version or latest")
	flags.BoolVar(&opts.DryRun, "dry-run", false, "Print commands without installing the CLI")
	_ = flags.Parse(args)
	if flags.NArg() != 0 {
		fmt.Fprintf(os.Stderr, "procyon-cli self-update: unexpected arguments: %s\n", strings.Join(flags.Args(), " "))
		os.Exit(2)
	}
	if err := selfupdate.Run(opts); err != nil {
		fmt.Fprintf(os.Stderr, "procyon-cli self-update: %v\n", err)
		os.Exit(1)
	}
}

func runPostmanCommand(args []string) {
	if len(args) == 0 || args[0] != "generate" && args[0] != "sync" {
		postmanUsage()
		os.Exit(2)
	}
	command := args[0]
	opts := postmangen.Options{}
	flags := flag.NewFlagSet("procyon-cli postman "+command, flag.ExitOnError)
	flags.StringVar(&opts.Root, "root", ".", "Procyon project directory containing routes.go")
	flags.StringVar(&opts.Out, "out", "", "Output collection path relative to the project")
	flags.StringVar(&opts.Name, "name", "", "Postman collection name")
	flags.StringVar(&opts.ConfigPath, "config", "", "Runtime config used for default collection variables")
	flags.StringVar(&opts.BaseURL, "base-url", "", "baseURL variable override")
	flags.StringVar(&opts.AdminURL, "admin-url", "", "adminURL variable override")
	flags.StringVar(&opts.UploadURL, "upload-url", "", "uploadURL variable override")
	flags.StringVar(&opts.AdminKey, "admin-key", "", "adminKey variable override")
	flags.StringVar(&opts.AuthKey, "auth-key", "", "authKey variable override")
	_ = flags.Parse(args[1:])
	if flags.NArg() != 0 {
		fmt.Fprintf(os.Stderr, "procyon-cli postman %s: unexpected arguments: %s\n", command, strings.Join(flags.Args(), " "))
		os.Exit(2)
	}
	if command == "generate" {
		result, err := postmangen.GenerateFromEnvironment(opts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "procyon-cli postman generate: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Generated %s with %d routes.\n", result.OutputPath, result.RouteCount)
		return
	}
	result, err := postmangen.Sync(postmangen.SyncOptions{Generation: opts, Writer: os.Stdout})
	if err != nil {
		fmt.Fprintf(os.Stderr, "procyon-cli postman sync: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Generated %s with %d routes.\n", result.Generation.OutputPath, result.Generation.RouteCount)
	if len(result.Targets) == 0 {
		fmt.Println("No Postman sync targets configured; collection generated locally only.")
		return
	}
	fmt.Printf("Synced %d Postman target(s).\n", len(result.Targets))
}

func postmanUsage() {
	fmt.Fprintln(os.Stderr, "usage:\n  procyon-cli postman generate [--root path] [--out path] [--name name]\n  procyon-cli postman sync [--root path] [--out path] [--name name]")
}

func moduleUsage() {
	fmt.Fprintf(os.Stderr, "usage:\n  procyon-cli module create <module_name> [--force]\n  procyon-cli module add <module_name> [--source path] [--registry path] [--provider names] [--published] [--dry-run]\n  procyon-cli module update <module_name> [--version version] [--source path] [--published] [--dry-run]\n  procyon-cli module enable <module_name> [--dry-run]\n  procyon-cli module disable <module_name> [--dry-run]\n  procyon-cli module list\n  procyon-cli module info <module_name>\n\n")
	fmt.Fprintf(os.Stderr, "examples:\n")
	fmt.Fprintf(os.Stderr, "  procyon-cli module create invoice\n")
	fmt.Fprintf(os.Stderr, "  procyon-cli module create order_item --force\n")
	fmt.Fprintf(os.Stderr, "  procyon-cli module add example --dry-run\n")
	fmt.Fprintf(os.Stderr, "  procyon-cli module add payment-system --provider stripe\n")
	fmt.Fprintf(os.Stderr, "  procyon-cli module update payment-system\n")
}

func runModuleCommand(args []string) {
	if len(args) == 0 {
		moduleUsage()
		os.Exit(2)
	}
	switch args[0] {
	case "create":
		opts, err := parseModuleCreateArgs(args[1:])
		if err != nil {
			fmt.Fprintf(os.Stderr, "procyon-cli module create: %v\n", err)
			os.Exit(2)
		}
		if err := modulegen.Run(opts); err != nil {
			fmt.Fprintf(os.Stderr, "procyon-cli module create: %v\n", err)
			os.Exit(1)
		}
	case "add":
		runModuleAdd(args[1:])
	case "update":
		runModuleUpdate(args[1:])
	case "enable":
		runModuleSetEnabled(args[1:], true)
	case "disable":
		runModuleSetEnabled(args[1:], false)
	case "list":
		if err := moduleinstall.List(os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "procyon-cli module list: %v\n", err)
			os.Exit(1)
		}
	case "info":
		if len(args) != 2 {
			moduleUsage()
			os.Exit(2)
		}
		if err := moduleinstall.Info(args[1], os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "procyon-cli module info: %v\n", err)
			os.Exit(1)
		}
	default:
		moduleUsage()
		os.Exit(2)
	}
}

func runModuleSetEnabled(args []string, enabled bool) {
	command := map[bool]string{true: "enable", false: "disable"}[enabled]
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		moduleUsage()
		os.Exit(2)
	}
	flags := flag.NewFlagSet("procyon-cli module "+command, flag.ExitOnError)
	dryRun := flags.Bool("dry-run", false, "Show the plan without changing files")
	_ = flags.Parse(args[1:])
	if flags.NArg() != 0 {
		fmt.Fprintf(os.Stderr, "procyon-cli module %s: unexpected arguments: %s\n", command, strings.Join(flags.Args(), " "))
		os.Exit(2)
	}
	if err := moduleinstall.SetEnabled(moduleinstall.SetEnabledOptions{
		Name: args[0], Enabled: enabled, DryRun: *dryRun, Writer: os.Stdout,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "procyon-cli module %s: %v\n", command, err)
		os.Exit(1)
	}
}

func runModuleUpdate(args []string) {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		moduleUsage()
		os.Exit(2)
	}
	opts := moduleinstall.UpdateOptions{Name: args[0], Version: "latest"}
	flags := flag.NewFlagSet("procyon-cli module update", flag.ExitOnError)
	flags.StringVar(&opts.Version, "version", "latest", "Plugin version available in the selected source")
	flags.StringVar(&opts.Source, "source", "", "Module source directory (defaults to the installed source)")
	flags.BoolVar(&opts.DryRun, "dry-run", false, "Show the update plan without changing files")
	flags.BoolVar(&opts.Published, "published", false, "Update from a published Go module and remove the local replace")
	_ = flags.Parse(args[1:])
	if flags.NArg() != 0 {
		fmt.Fprintf(os.Stderr, "procyon-cli module update: unexpected arguments: %s\n", strings.Join(flags.Args(), " "))
		os.Exit(2)
	}
	if err := moduleinstall.Update(opts); err != nil {
		fmt.Fprintf(os.Stderr, "procyon-cli module update: %v\n", err)
		os.Exit(1)
	}
}

func runModuleAdd(args []string) {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		moduleUsage()
		os.Exit(2)
	}
	opts := moduleinstall.Options{Name: args[0], Values: map[string]string{}}
	flags := flag.NewFlagSet("procyon-cli module add", flag.ExitOnError)
	flags.StringVar(&opts.Source, "source", "", "Module source directory")
	flags.StringVar(&opts.Registry, "registry", "", "Module registry JSON path")
	flags.StringVar(&opts.Provider, "provider", "", "Comma-separated provider selection")
	flags.BoolVar(&opts.DryRun, "dry-run", false, "Show the installation plan without changing files")
	flags.BoolVar(&opts.Published, "published", false, "Use the published Go module instead of a local replace")
	values := keyValueFlags(opts.Values)
	flags.Var(values, "set", "Module value in key=value form (repeatable)")
	_ = flags.Parse(args[1:])
	if flags.NArg() != 0 {
		fmt.Fprintf(os.Stderr, "procyon-cli module add: unexpected arguments: %s\n", strings.Join(flags.Args(), " "))
		os.Exit(2)
	}
	if err := moduleinstall.Run(opts); err != nil {
		fmt.Fprintf(os.Stderr, "procyon-cli module add: %v\n", err)
		os.Exit(1)
	}
}

type keyValueFlags map[string]string

func (values keyValueFlags) String() string { return "" }

func (values keyValueFlags) Set(raw string) error {
	key, value, ok := strings.Cut(raw, "=")
	key = strings.TrimSpace(key)
	if !ok || key == "" {
		return fmt.Errorf("expected key=value, got %q", raw)
	}
	values[key] = value
	return nil
}
