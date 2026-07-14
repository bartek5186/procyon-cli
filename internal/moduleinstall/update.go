package moduleinstall

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

type UpdateOptions struct {
	Name      string
	Version   string
	Source    string
	DryRun    bool
	Published bool
	Writer    io.Writer
}

func Update(opts UpdateOptions) error {
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
	installed, ok := metadata.Modules[name]
	if !ok {
		return fmt.Errorf("module %q is not installed", name)
	}
	if installed.Kind != "go-plugin" {
		return fmt.Errorf("module %q is not an updatable Go plugin", name)
	}
	previousVersion := installed.Version
	if opts.Published {
		return updatePublishedPlugin(opts, metadata, installed, previousVersion)
	}

	source := strings.TrimSpace(opts.Source)
	if source == "" {
		source = installed.LocalSource
	}
	if source == "" {
		return fmt.Errorf("module %q has no source metadata; pass --source", name)
	}
	source, err = absoluteExistingDir(source)
	if err != nil {
		return err
	}
	manifest, err := loadManifest(source)
	if err != nil {
		return err
	}
	if manifest.Name != name || manifest.GoModule != installed.GoModule {
		return fmt.Errorf("source describes %s (%s), expected %s (%s)", manifest.Name, manifest.GoModule, name, installed.GoModule)
	}
	if err := validateCompatibility(manifest, metadata); err != nil {
		return err
	}
	requested := strings.TrimSpace(opts.Version)
	if requested != "" && requested != "latest" && normalizedGoVersion(requested) != normalizedGoVersion(manifest.Version) {
		return fmt.Errorf("source contains %s, not requested version %s", manifest.Version, requested)
	}

	installed.Version = manifest.Version
	installed.Package = manifest.Package
	installed.Factory = manifest.Factory
	installed.LocalSource = source
	installed.Environment = selectedEnvironment(manifest.Environment, installed.Providers)
	installed.InstalledAt = time.Now().UTC().Format(time.RFC3339)
	metadata.Modules[name] = installed
	state, err := buildGeneratedProjectState(metadata)
	if err != nil {
		return err
	}

	fmt.Fprintf(opts.Writer, "Update Go plugin %s: %s -> %s\n", name, previousVersion, manifest.Version)
	fmt.Fprintf(opts.Writer, "  require %s@%s\n", manifest.GoModule, normalizedGoVersion(manifest.Version))
	fmt.Fprintln(opts.Writer, "  regenerate plugin registration, config and environment example")
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
	if err := runCommand(opts.Writer, "go", "mod", "edit", "-replace="+manifest.GoModule+"="+source); err != nil {
		rollback()
		return fmt.Errorf("update local plugin replace: %w", err)
	}
	if err := runCommand(opts.Writer, "go", "get", manifest.GoModule+"@"+normalizedGoVersion(manifest.Version)); err != nil {
		rollback()
		return fmt.Errorf("go get %s: %w", manifest.GoModule, err)
	}
	for _, args := range [][]string{{"mod", "tidy"}, {"test", "./..."}} {
		if err := runCommand(opts.Writer, "go", args...); err != nil {
			rollback()
			return fmt.Errorf("go %s: %w", strings.Join(args, " "), err)
		}
	}
	fmt.Fprintf(opts.Writer, "Updated Go plugin %s to %s.\n", name, manifest.Version)
	return nil
}

func updatePublishedPlugin(opts UpdateOptions, metadata projectMetadata, installed InstalledModule, previousVersion string) error {
	requested := strings.TrimSpace(opts.Version)
	manifest, err := PublishedManifest(opts.Name)
	if err != nil {
		return err
	}
	if requested == "" || requested == "latest" {
		requested = manifest.Version
	}
	if normalizedGoVersion(requested) != normalizedGoVersion(manifest.Version) {
		return fmt.Errorf("published registry contains %s, not requested version %s", manifest.Version, requested)
	}
	if manifest.Name != opts.Name || manifest.GoModule != installed.GoModule {
		return fmt.Errorf("published manifest describes %s (%s), expected %s (%s)", manifest.Name, manifest.GoModule, opts.Name, installed.GoModule)
	}
	if err := validateCompatibility(manifest, metadata); err != nil {
		return err
	}
	installed.Version = manifest.Version
	installed.Package = manifest.Package
	installed.Factory = manifest.Factory
	installed.Environment = selectedEnvironment(manifest.Environment, installed.Providers)
	installed.LocalSource = ""
	installed.InstalledAt = time.Now().UTC().Format(time.RFC3339)
	metadata.Modules[opts.Name] = installed
	state, err := buildGeneratedProjectState(metadata)
	if err != nil {
		return err
	}
	fmt.Fprintf(opts.Writer, "Update published Go plugin %s: %s -> %s\n", opts.Name, previousVersion, installed.Version)
	fmt.Fprintf(opts.Writer, "  require %s@%s\n", installed.GoModule, normalizedGoVersion(requested))
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
	if err := runCommand(opts.Writer, "go", "mod", "edit", "-dropreplace="+installed.GoModule); err != nil {
		rollback()
		return fmt.Errorf("remove local plugin replace: %w", err)
	}
	if err := runCommand(opts.Writer, "go", "get", installed.GoModule+"@"+normalizedGoVersion(requested)); err != nil {
		rollback()
		return fmt.Errorf("go get %s: %w", installed.GoModule, err)
	}
	for _, args := range [][]string{{"mod", "tidy"}, {"test", "./..."}} {
		if err := runCommand(opts.Writer, "go", args...); err != nil {
			rollback()
			return fmt.Errorf("go %s: %w", strings.Join(args, " "), err)
		}
	}
	fmt.Fprintf(opts.Writer, "Updated published Go plugin %s to %s.\n", opts.Name, installed.Version)
	return nil
}
