package moduleinstall

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

type SetEnabledOptions struct {
	Name    string
	Enabled bool
	DryRun  bool
	Writer  io.Writer
}

func SetEnabled(opts SetEnabledOptions) error {
	if opts.Writer == nil {
		opts.Writer = os.Stdout
	}
	name := strings.TrimSpace(opts.Name)
	if name == "" {
		return errors.New("module name is required")
	}
	metadata, err := loadProjectMetadata()
	if err != nil {
		return err
	}
	installed, exists := metadata.Modules[name]
	if !exists {
		return fmt.Errorf("module %q is not installed", name)
	}
	if moduleEnabled(installed) == opts.Enabled {
		return fmt.Errorf("module %q is already %s", name, enabledLabel(opts.Enabled))
	}
	installed.Enabled = boolPointer(opts.Enabled)
	metadata.Modules[name] = installed
	state, err := buildGeneratedProjectState(metadata)
	if err != nil {
		return err
	}
	action := map[bool]string{true: "Enable", false: "Disable"}[opts.Enabled]
	fmt.Fprintf(opts.Writer, "%s Go plugin %s %s\n", action, name, installed.Version)
	fmt.Fprintln(opts.Writer, "  regenerate plugins_gen.go")
	fmt.Fprintln(opts.Writer, "  compose config/plugins.generated.json and .env.example")
	if opts.DryRun {
		return nil
	}

	backup, err := backupPaths(generatedProjectPaths("go.mod", "go.sum"))
	if err != nil {
		return err
	}
	rollback := func() { restoreBackup(backup) }
	if err := writeGeneratedProjectState(state); err != nil {
		rollback()
		return err
	}
	if opts.Enabled {
		if installed.LocalSource != "" {
			if err := runCommand(opts.Writer, "go", "mod", "edit", "-replace="+installed.GoModule+"="+installed.LocalSource); err != nil {
				rollback()
				return fmt.Errorf("restore local plugin replace: %w", err)
			}
		} else if err := runCommand(opts.Writer, "go", "mod", "edit", "-dropreplace="+installed.GoModule); err != nil {
			rollback()
			return fmt.Errorf("remove local plugin replace: %w", err)
		}
		if err := runCommand(opts.Writer, "go", "get", installed.GoModule+"@"+normalizedGoVersion(installed.Version)); err != nil {
			rollback()
			return fmt.Errorf("go get %s: %w", installed.GoModule, err)
		}
	} else if installed.LocalSource != "" {
		if err := runCommand(opts.Writer, "go", "mod", "edit", "-dropreplace="+installed.GoModule); err != nil {
			rollback()
			return fmt.Errorf("remove disabled local plugin replace: %w", err)
		}
	}
	for _, args := range [][]string{{"mod", "tidy"}, {"test", "./..."}} {
		if err := runCommand(opts.Writer, "go", args...); err != nil {
			rollback()
			return fmt.Errorf("go %s: %w", strings.Join(args, " "), err)
		}
	}
	fmt.Fprintf(opts.Writer, "Plugin %s is now %s. Database data was not removed.\n", name, enabledLabel(opts.Enabled))
	return nil
}

func enabledLabel(enabled bool) string {
	if enabled {
		return "enabled"
	}
	return "disabled"
}
