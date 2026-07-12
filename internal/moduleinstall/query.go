package moduleinstall

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

func List(writer io.Writer) error {
	if writer == nil {
		writer = os.Stdout
	}
	metadata, err := loadProjectMetadata()
	if err != nil {
		return err
	}
	if len(metadata.Modules) == 0 {
		fmt.Fprintln(writer, "No shared modules installed.")
		return nil
	}
	names := make([]string, 0, len(metadata.Modules))
	for name := range metadata.Modules {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		module := metadata.Modules[name]
		providers := ""
		if len(module.Providers) > 0 {
			providers = " [" + strings.Join(module.Providers, ", ") + "]"
		}
		fmt.Fprintf(writer, "%s %s%s\n", name, module.Version, providers)
	}
	return nil
}

func Info(name string, writer io.Writer) error {
	if writer == nil {
		writer = os.Stdout
	}
	metadata, err := loadProjectMetadata()
	if err != nil {
		return err
	}
	module, ok := metadata.Modules[strings.TrimSpace(name)]
	if !ok {
		return fmt.Errorf("module %q is not installed", name)
	}
	fmt.Fprintf(writer, "Name: %s\nVersion: %s\n", name, module.Version)
	if module.Kind != "" {
		fmt.Fprintf(writer, "Kind: %s\n", module.Kind)
	}
	if module.GoModule != "" {
		fmt.Fprintf(writer, "Go module: %s\nPackage: %s\nFactory: %s\n", module.GoModule, module.Package, module.Factory)
	}
	if len(module.Providers) > 0 {
		fmt.Fprintf(writer, "Providers: %s\n", strings.Join(module.Providers, ", "))
	}
	if module.LocalSource != "" {
		fmt.Fprintf(writer, "Local source: %s\n", module.LocalSource)
	}
	fmt.Fprintf(writer, "Installed: %s\n", module.InstalledAt)
	return nil
}
