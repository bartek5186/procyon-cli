package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/bartek5186/procyon-cli/internal/buildinfo"
	"github.com/bartek5186/procyon-cli/internal/dashboard"
	"github.com/bartek5186/procyon-cli/internal/modulegen"
	"github.com/bartek5186/procyon-cli/internal/moduleinstall"
	"github.com/bartek5186/procyon-cli/internal/projectinit"
	"github.com/bartek5186/procyon-cli/internal/projectupdate"
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
	case "add":
		if len(os.Args) >= 3 && os.Args[2] == "module" {
			runModuleAdd(os.Args[3:])
			return
		}
		usage()
		os.Exit(2)
	case "update":
		updateCmd := flag.NewFlagSet("procyon-cli update", flag.ExitOnError)
		opts := projectupdate.Options{}
		updateCmd.StringVar(&opts.Version, "version", "latest", "Core version, tag, branch or latest")
		updateCmd.BoolVar(&opts.DryRun, "dry-run", false, "Print commands without changing the project")
		_ = updateCmd.Parse(os.Args[2:])

		if err := projectupdate.Run(opts); err != nil {
			fmt.Fprintf(os.Stderr, "procyon-cli update: %v\n", err)
			os.Exit(1)
		}
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
	fmt.Fprintf(os.Stderr, "usage:\n  procyon-cli init [flags]\n  procyon-cli module create <module_name> [--force]\n  procyon-cli module add <module_name> [flags]\n  procyon-cli module update <module_name> [flags]\n  procyon-cli module enable <module_name> [--dry-run]\n  procyon-cli module disable <module_name> [--dry-run]\n  procyon-cli module list\n  procyon-cli module info <module_name>\n  procyon-cli update [--version v0.3.0] [--dry-run]\n\n")
	fmt.Fprintf(os.Stderr, "examples:\n")
	fmt.Fprintf(os.Stderr, "  procyon-cli init\n")
	fmt.Fprintf(os.Stderr, "  procyon-cli init --name przyjazne-server --module github.com/acme/przyjazne-server --db postgres --out ../przyjazne-v2\n")
	fmt.Fprintf(os.Stderr, "  procyon-cli module create invoice\n")
	fmt.Fprintf(os.Stderr, "  procyon-cli module add payment-system --provider stripe\n")
	fmt.Fprintf(os.Stderr, "  procyon-cli update --version v0.3.0\n")
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
