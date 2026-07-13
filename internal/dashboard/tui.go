package dashboard

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"charm.land/huh/v2"
	"github.com/bartek5186/procyon-cli/internal/buildinfo"
	"github.com/bartek5186/procyon-cli/internal/moduleinstall"
	"github.com/bartek5186/procyon-cli/internal/plugincreate"
	"github.com/bartek5186/procyon-cli/internal/projectinit"
	"github.com/bartek5186/procyon-cli/internal/projectupdate"
)

var ErrNonInteractive = errors.New("interactive terminal required")
var errMenuCancelled = errors.New("menu cancelled")

func Run() error {
	if !isInteractiveTerminal(os.Stdin, os.Stdout) {
		return ErrNonInteractive
	}
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	ctx, err := Detect(wd)
	if err != nil {
		return err
	}
	var runErr error
	switch ctx.Kind {
	case DirectoryEmpty:
		runErr = runEmptyDirectoryMenu(ctx)
	case DirectoryProject:
		runErr = runProjectMenu(ctx)
	default:
		runErr = runOtherDirectoryMenu(ctx)
	}
	if errors.Is(runErr, errMenuCancelled) {
		return nil
	}
	return runErr
}

func runEmptyDirectoryMenu(ctx Context) error {
	action := "init_here"
	form := newForm(
		huh.NewNote().Title("✦  P R O C Y O N").Description(emptyDirectorySummary(ctx)),
		huh.NewSelect[string]().
			Title("What would you like to do?").
			Options(
				huh.NewOption("Initialize a project in this directory", "init_here"),
				huh.NewOption("Create a project in a new subdirectory", "init_subdirectory"),
				huh.NewOption("Exit", "exit"),
			).
			Value(&action),
	)
	if err := runForm(form); err != nil {
		return err
	}
	switch action {
	case "init_here":
		return projectinit.Run(projectinit.Options{
			OutputDir: ".", Auth: "kratos-casbin", TemplateVersion: buildinfo.TemplateVersion,
			IncludeDocker: true, IncludeHello: true,
		})
	case "init_subdirectory":
		return projectinit.Run(projectinit.Options{
			Auth: "kratos-casbin", TemplateVersion: buildinfo.TemplateVersion,
			IncludeDocker: true, IncludeHello: true,
		})
	default:
		return nil
	}
}

func runOtherDirectoryMenu(ctx Context) error {
	action := "init_subdirectory"
	form := newForm(
		huh.NewNote().Title("✦  P R O C Y O N").Description(otherDirectorySummary(ctx)),
		huh.NewSelect[string]().
			Title("Available actions").
			Options(
				huh.NewOption("Create a project in a new subdirectory", "init_subdirectory"),
				huh.NewOption("Exit", "exit"),
			).
			Value(&action),
	)
	if err := runForm(form); err != nil {
		return err
	}
	if action != "init_subdirectory" {
		return nil
	}
	return projectinit.Run(projectinit.Options{
		Auth: "kratos-casbin", TemplateVersion: buildinfo.TemplateVersion,
		IncludeDocker: true, IncludeHello: true,
	})
}

func runProjectMenu(ctx Context) error {
	for {
		action := "exit"
		form := newForm(
			huh.NewNote().Title("✦  P R O C Y O N").Description(projectSummary(ctx)),
			huh.NewSelect[string]().
				Title("Project actions").
				Options(projectActionOptions(ctx)...).
				Value(&action),
		)
		if err := runForm(form); err != nil {
			return err
		}
		switch action {
		case "add_plugin":
			return promptAndAddPlugin(ctx)
		case "create_plugin":
			return promptCreateAndInstallPlugin(ctx)
		case "enable_plugin":
			return promptSetPluginEnabled(ctx, true)
		case "disable_plugin":
			return promptSetPluginEnabled(ctx, false)
		case "update":
			return projectupdate.Run(projectupdate.Options{Version: "latest", Writer: os.Stdout})
		case "plugins":
			if err := showPlugins(ctx); err != nil {
				return err
			}
		default:
			return nil
		}
	}
}

func projectActionOptions(ctx Context) []huh.Option[string] {
	options := []huh.Option[string]{
		huh.NewOption("Add a published plugin", "add_plugin"),
		huh.NewOption("Create a local plugin for development", "create_plugin"),
	}
	hasEnabled, hasDisabled := false, false
	for _, module := range ctx.Modules {
		hasEnabled = hasEnabled || module.Enabled
		hasDisabled = hasDisabled || !module.Enabled
	}
	if hasDisabled {
		options = append(options, huh.NewOption("Enable a plugin", "enable_plugin"))
	}
	if hasEnabled {
		options = append(options, huh.NewOption("Disable a plugin", "disable_plugin"))
	}
	return append(options,
		huh.NewOption("Show installed plugins", "plugins"),
		huh.NewOption("Update Procyon Core", "update"),
		huh.NewOption("Exit", "exit"),
	)
}

func promptSetPluginEnabled(ctx Context, enabled bool) error {
	var candidates []Module
	for _, module := range ctx.Modules {
		if module.Enabled != enabled {
			candidates = append(candidates, module)
		}
	}
	if len(candidates) == 0 {
		return fmt.Errorf("no plugins are available to %s", map[bool]string{true: "enable", false: "disable"}[enabled])
	}
	name := candidates[0].Name
	options := make([]huh.Option[string], 0, len(candidates))
	for _, module := range candidates {
		options = append(options, huh.NewOption(module.Name+"  v"+strings.TrimPrefix(module.Version, "v"), module.Name))
	}
	action := map[bool]string{true: "Enable", false: "Disable"}[enabled]
	form := newForm(huh.NewSelect[string]().
		Title(action + " a plugin").
		Description("Database tables and data are preserved when a plugin is disabled.").
		Options(options...).
		Value(&name))
	if err := runForm(form); err != nil {
		return err
	}
	return moduleinstall.SetEnabled(moduleinstall.SetEnabledOptions{Name: name, Enabled: enabled, Writer: os.Stdout})
}

func promptAndAddPlugin(ctx Context) error {
	catalog, err := moduleinstall.PublishedCatalog()
	if err != nil {
		return err
	}
	available := availableCatalogModules(catalog, ctx.Modules)
	if len(available) == 0 {
		return errors.New("no new published plugins are available")
	}

	name := available[0].Name
	moduleForm := newForm(
		huh.NewSelect[string]().
			Title("Choose a plugin").
			Description("Press / to search by name or description.").
			Options(catalogOptions(available)...).
			Height(12).
			Value(&name),
	)
	if err := runForm(moduleForm); err != nil {
		return err
	}

	manifest, err := moduleinstall.PublishedManifest(name)
	if err != nil {
		return err
	}
	providers := defaultProviders(manifest)
	if len(manifest.Providers) > 0 {
		providerForm := newForm(
			huh.NewMultiSelect[string]().
				Title("Choose providers").
				Description("Use Space to select one or more providers.").
				Options(providerOptions(manifest.Providers)...).
				Height(7).
				Value(&providers).
				Validate(func(selected []string) error {
					if len(selected) == 0 {
						return errors.New("select at least one provider")
					}
					return nil
				}),
		)
		if err := runForm(providerForm); err != nil {
			return err
		}
	}

	return moduleinstall.Run(moduleinstall.Options{
		Name: strings.TrimSpace(name), Provider: strings.Join(providers, ","),
		Published: true, Writer: os.Stdout, Values: map[string]string{},
	})
}

func availableCatalogModules(catalog []moduleinstall.CatalogModule, installed []Module) []moduleinstall.CatalogModule {
	installedNames := make(map[string]bool, len(installed))
	for _, module := range installed {
		installedNames[module.Name] = true
	}
	available := make([]moduleinstall.CatalogModule, 0, len(catalog))
	for _, module := range catalog {
		if !installedNames[module.Name] {
			available = append(available, module)
		}
	}
	return available
}

func catalogOptions(catalog []moduleinstall.CatalogModule) []huh.Option[string] {
	options := make([]huh.Option[string], 0, len(catalog))
	for _, module := range catalog {
		label := module.Name
		if module.Version != "" {
			label += "  v" + strings.TrimPrefix(module.Version, "v")
		}
		if module.Description != "" {
			label += " — " + module.Description
		}
		options = append(options, huh.NewOption(label, module.Name))
	}
	return options
}

func defaultProviders(manifest moduleinstall.Manifest) []string {
	if provider := strings.TrimSpace(manifest.DefaultProvider); provider != "" {
		return []string{provider}
	}
	return nil
}

func providerOptions(providers []string) []huh.Option[string] {
	options := make([]huh.Option[string], 0, len(providers))
	for _, provider := range providers {
		provider = strings.TrimSpace(provider)
		if provider != "" {
			options = append(options, huh.NewOption(providerDisplayName(provider), provider))
		}
	}
	return options
}

func providerDisplayName(provider string) string {
	switch strings.ToLower(provider) {
	case "stripe":
		return "Stripe"
	case "google":
		return "Google Play"
	case "apple":
		return "Apple App Store"
	default:
		return provider
	}
}

func promptCreateAndInstallPlugin(ctx Context) error {
	name := ""
	nameForm := newForm(
		huh.NewInput().
			Title("Plugin name").
			Description("Use kebab-case, for example audit-log or notifications.").
			Value(&name).
			Validate(func(value string) error {
				if strings.TrimSpace(value) == "" {
					return errors.New("plugin name is required")
				}
				return nil
			}),
	)
	if err := runForm(nameForm); err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	goModule := strings.TrimSuffix(ctx.ProjectModule, "/") + "/plugins/" + name
	outputDir := filepath.Join("plugins", name)
	settingsForm := newForm(
		huh.NewInput().
			Title("Go module").
			Description("Import path of the standalone plugin module.").
			Value(&goModule),
		huh.NewInput().
			Title("Output directory").
			Description("The new plugin source directory, relative to this project.").
			Value(&outputDir),
	)
	if err := runForm(settingsForm); err != nil {
		return err
	}
	result, err := plugincreate.Create(plugincreate.Options{
		Name: name, GoModule: goModule, OutputDir: outputDir,
		CoreVersion: ctx.CoreVersion, CLIVersion: buildinfo.CLIVersion,
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "Created local plugin source in %s.\n", result.Root)
	return moduleinstall.Run(moduleinstall.Options{
		Name: result.Name, Source: result.Root, Writer: os.Stdout, Values: map[string]string{},
	})
}

func showPlugins(ctx Context) error {
	description := "No shared plugins are installed."
	if len(ctx.Modules) > 0 {
		lines := make([]string, 0, len(ctx.Modules))
		for _, module := range ctx.Modules {
			status := "enabled"
			if !module.Enabled {
				status = "disabled"
			}
			lines = append(lines, fmt.Sprintf("• %s %s (%s)", module.Name, module.Version, status))
		}
		description = strings.Join(lines, "\n")
	}
	form := newForm(huh.NewNote().Title("Installed plugins").Description(description))
	return runForm(form)
}

func projectSummary(ctx Context) string {
	name := firstNonEmpty(ctx.ProjectName, filepath.Base(ctx.Root))
	lines := []string{
		"Procyon project detected",
		"",
		"Project: " + name,
		"Go module: " + firstNonEmpty(ctx.ProjectModule, "unknown"),
		"Template: " + ctx.TemplateVersion,
		"Core: " + ctx.CoreVersion,
		"CLI: v" + buildinfo.CLIVersion,
		fmt.Sprintf("Plugins: %d", len(ctx.Modules)),
		"Directory: " + ctx.Root,
	}
	if ctx.Reason != "" {
		lines = append(lines, "", "Warning: "+ctx.Reason)
	}
	return strings.Join(lines, "\n")
}

func emptyDirectorySummary(ctx Context) string {
	return strings.Join([]string{
		"No Procyon project detected",
		"",
		"This directory is empty and ready for a new Go backend.",
		"Directory: " + ctx.Root,
		"CLI: v" + buildinfo.CLIVersion,
	}, "\n")
}

func otherDirectorySummary(ctx Context) string {
	lines := []string{
		"No Procyon project detected",
		"",
		"This directory is not empty, so it will not be overwritten.",
		"Directory: " + ctx.Root,
	}
	if ctx.Reason != "" {
		lines = append(lines, "Reason: "+ctx.Reason)
	}
	return strings.Join(lines, "\n")
}

func newForm(fields ...huh.Field) *huh.Form {
	return huh.NewForm(huh.NewGroup(fields...)).
		WithTheme(huh.ThemeFunc(huh.ThemeCatppuccin)).
		WithAccessible(os.Getenv("ACCESSIBLE") != "")
}

func runForm(form *huh.Form) error {
	err := form.Run()
	if errors.Is(err, huh.ErrUserAborted) {
		return errMenuCancelled
	}
	return err
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
