package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/bartek5186/procyon-cli/internal/modulegen"
	"github.com/bartek5186/procyon-cli/internal/projectinit"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "init":
		initCmd := flag.NewFlagSet("procyon-cli init", flag.ExitOnError)
		opts := projectinit.Options{}
		initCmd.StringVar(&opts.Name, "name", "", "Project name")
		initCmd.StringVar(&opts.Module, "module", "", "Go module path")
		initCmd.StringVar(&opts.OutputDir, "out", "", "Output directory")
		initCmd.StringVar(&opts.Database, "db", "", "Database type: postgres or mysql")
		initCmd.BoolVar(&opts.IncludeDocker, "docker", true, "Include Docker files")
		initCmd.BoolVar(&opts.IncludeHello, "hello", true, "Keep example hello feature")
		initCmd.BoolVar(&opts.Force, "force", false, "Allow non-empty output directory")
		_ = initCmd.Parse(os.Args[2:])

		if err := projectinit.Run(opts); err != nil {
			fmt.Fprintf(os.Stderr, "procyon-cli init: %v\n", err)
			os.Exit(1)
		}
	case "module":
		if len(os.Args) < 3 || os.Args[2] != "create" {
			moduleUsage()
			os.Exit(2)
		}
		opts, err := parseModuleCreateArgs(os.Args[3:])
		if err != nil {
			fmt.Fprintf(os.Stderr, "procyon-cli module create: %v\n", err)
			moduleUsage()
			os.Exit(2)
		}
		if err := modulegen.Run(opts); err != nil {
			fmt.Fprintf(os.Stderr, "procyon-cli module create: %v\n", err)
			os.Exit(1)
		}
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
	fmt.Fprintf(os.Stderr, "usage:\n  procyon-cli init [flags]\n  procyon-cli module create <module_name> [--force]\n\n")
	fmt.Fprintf(os.Stderr, "examples:\n")
	fmt.Fprintf(os.Stderr, "  procyon-cli init\n")
	fmt.Fprintf(os.Stderr, "  procyon-cli init --name przyjazne-server --module github.com/acme/przyjazne-server --db postgres --out ../przyjazne-v2\n")
	fmt.Fprintf(os.Stderr, "  procyon-cli module create invoice\n")
}

func moduleUsage() {
	fmt.Fprintf(os.Stderr, "usage:\n  procyon-cli module create <module_name> [--force]\n\n")
	fmt.Fprintf(os.Stderr, "examples:\n")
	fmt.Fprintf(os.Stderr, "  procyon-cli module create invoice\n")
	fmt.Fprintf(os.Stderr, "  procyon-cli module create order_item --force\n")
}
