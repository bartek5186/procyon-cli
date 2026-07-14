package selfupdate

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/bartek5186/procyon-cli/internal/gocommand"
)

const CLIModule = "github.com/bartek5186/procyon-cli"

const (
	installMethodEnvironment = "PROCYON_CLI_INSTALL_METHOD"
	installMethodNPM         = "npm"
	npmPackage               = "procyon-cli"
)

type Options struct {
	Version string
	DryRun  bool
	Writer  io.Writer
}

var runCommand = func(writer io.Writer, name string, args ...string) error {
	cmd := gocommand.New(name, args...)
	cmd.Stdout = writer
	cmd.Stderr = writer
	return cmd.Run()
}

var commandOutput = func(name string, args ...string) ([]byte, error) {
	return gocommand.New(name, args...).Output()
}

func Run(opts Options) error {
	if opts.Writer == nil {
		opts.Writer = os.Stdout
	}
	version, err := normalizeVersion(opts.Version)
	if err != nil {
		return err
	}
	if strings.EqualFold(strings.TrimSpace(os.Getenv(installMethodEnvironment)), installMethodNPM) {
		return runNPMUpdate(opts, version)
	}
	return runGoUpdate(opts, version)
}

func runGoUpdate(opts Options, version string) error {
	target := CLIModule + "@" + version
	fmt.Fprintf(opts.Writer, "+ go install %s\n", target)
	if opts.DryRun {
		return nil
	}
	if err := runCommand(opts.Writer, "go", "install", target); err != nil {
		return fmt.Errorf("go install %s: %w", target, err)
	}
	path, pathErr := installedBinaryPath()
	if pathErr != nil {
		fmt.Fprintf(opts.Writer, "Procyon CLI %s installed successfully.\n", version)
		fmt.Fprintln(opts.Writer, "Run `procyon-cli version` in a new shell to verify the active binary.")
		return nil
	}
	fmt.Fprintf(opts.Writer, "Procyon CLI %s installed to %s.\n", version, path)
	fmt.Fprintf(opts.Writer, "Run `%s version` to verify it. If `procyon-cli version` differs, put %s earlier in PATH.\n",
		path, filepath.Dir(path))
	return nil
}

func runNPMUpdate(opts Options, version string) error {
	npmVersion := strings.TrimPrefix(version, "v")
	target := npmPackage + "@" + npmVersion
	fmt.Fprintf(opts.Writer, "+ npm install --global %s\n", target)
	if opts.DryRun {
		return nil
	}
	if err := runCommand(opts.Writer, "npm", "install", "--global", target); err != nil {
		return fmt.Errorf("npm install --global %s: %w", target, err)
	}
	fmt.Fprintf(opts.Writer, "Procyon CLI %s installed successfully through npm.\n", version)
	fmt.Fprintln(opts.Writer, "Run `procyon-cli version` in a new shell to verify the active binary.")
	return nil
}

func normalizeVersion(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || value == "latest" {
		return "latest", nil
	}
	value = strings.TrimPrefix(value, "v")
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("invalid CLI version %q; use latest or a semantic version such as v0.3.4", value)
	}
	for _, part := range parts {
		number, err := strconv.Atoi(part)
		if err != nil || number < 0 || strconv.Itoa(number) != part {
			return "", fmt.Errorf("invalid CLI version %q; use latest or a semantic version such as v0.3.4", value)
		}
	}
	return "v" + value, nil
}

func installedBinaryPath() (string, error) {
	output, err := commandOutput("go", "env", "GOBIN")
	if err != nil {
		return "", err
	}
	directory := strings.TrimSpace(string(output))
	if directory == "" {
		output, err = commandOutput("go", "env", "GOPATH")
		if err != nil {
			return "", err
		}
		paths := filepath.SplitList(strings.TrimSpace(string(output)))
		if len(paths) == 0 || strings.TrimSpace(paths[0]) == "" {
			return "", fmt.Errorf("go env GOPATH is empty")
		}
		directory = filepath.Join(paths[0], "bin")
	}
	name := "procyon-cli"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(directory, name), nil
}
