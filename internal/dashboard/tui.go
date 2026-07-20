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
	"github.com/bartek5186/procyon-cli/internal/localplugin"
	"github.com/bartek5186/procyon-cli/internal/moduleinstall"
	"github.com/bartek5186/procyon-cli/internal/plugincreate"
	"github.com/bartek5186/procyon-cli/internal/postmangen"
	"github.com/bartek5186/procyon-cli/internal/projectinit"
	"github.com/bartek5186/procyon-cli/internal/projectupdate"
	"github.com/bartek5186/procyon-cli/internal/selfupdate"
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
		catalog, catalogErr := moduleinstall.PublishedCatalog()
		updates := availablePluginUpdates(catalog, ctx.Modules)
		action := "exit"
		form := newForm(
			huh.NewNote().Title("✦  P R O C Y O N").Description(projectSummary(ctx, updates, catalogErr)),
			huh.NewSelect[string]().
				Title("Project actions").
				Options(projectActionOptions(ctx, updates)...).
				Value(&action),
		)
		if err := runForm(form); err != nil {
			return err
		}
		switch action {
		case "add_plugin":
			return promptAndAddPlugin(ctx)
		case "create_plugin":
			return promptCreatePlugin(ctx)
		case "enable_plugin":
			return promptSetPluginEnabled(ctx, true)
		case "disable_plugin":
			return promptSetPluginEnabled(ctx, false)
		case "update_plugin":
			return promptUpdatePlugin(updates)
		case "update":
			return projectupdate.Run(projectupdate.Options{Version: "latest", Writer: os.Stdout})
		case "self_update":
			return selfupdate.Run(selfupdate.Options{Version: "latest", Writer: os.Stdout})
		case "generate_postman":
			return generatePostmanCollection(ctx)
		case "sync_postman":
			return syncPostmanCollections(ctx)
		case "plugins":
			if err := showPlugins(ctx, updates, catalogErr); err != nil {
				return err
			}
		default:
			return nil
		}
	}
}

func projectActionOptions(ctx Context, updates []pluginUpdate) []huh.Option[string] {
	options := []huh.Option[string]{
		huh.NewOption("Add a published plugin", "add_plugin"),
		huh.NewOption("Create plugin", "create_plugin"),
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
	if len(updates) > 0 {
		options = append(options, huh.NewOption(fmt.Sprintf("Update a plugin  (%d available)", len(updates)), "update_plugin"))
	}
	return append(options,
		huh.NewOption("Show installed plugins", "plugins"),
		huh.NewOption("Generate Postman collection", "generate_postman"),
		huh.NewOption("Sync Postman collections", "sync_postman"),
		huh.NewOption("Update Procyon Core", "update"),
		huh.NewOption("Update Procyon CLI", "self_update"),
		huh.NewOption("Exit", "exit"),
	)
}

func promptCreatePlugin(ctx Context) error {
	pluginType := "project"
	form := newForm(
		huh.NewSelect[string]().
			Title("Plugin type").
			Description("Choose whether the plugin belongs only to this project or is a separately versioned module.").
			Options(
				huh.NewOption("Project-owned — part of this project (recommended)", "project"),
				huh.NewOption("Standalone reusable — separate Go module", "standalone"),
			).
			Value(&pluginType),
	)
	if err := runForm(form); err != nil {
		return err
	}
	if pluginType == "standalone" {
		return promptCreateStandalonePlugin(ctx)
	}
	return promptCreateLocalPlugin()
}

func promptCreateLocalPlugin() error {
	name := ""
	form := newForm(
		huh.NewInput().
			Title("Project-owned plugin name").
			Description("Use kebab-case, for example leagues or audit-log.").
			Value(&name).
			Validate(func(value string) error {
				if strings.TrimSpace(value) == "" {
					return errors.New("plugin name is required")
				}
				return nil
			}),
	)
	if err := runForm(form); err != nil {
		return err
	}
	return localplugin.Create(localplugin.Options{Name: strings.TrimSpace(name)})
}

func generatePostmanCollection(ctx Context) error {
	opts, err := postmanOptions(ctx)
	if err != nil {
		return err
	}
	result, err := postmangen.Generate(opts)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "Generated %s with %d routes.\n", result.OutputPath, result.RouteCount)
	return nil
}

func syncPostmanCollections(ctx Context) error {
	opts, err := postmanOptions(ctx)
	if err != nil {
		return err
	}
	result, err := postmangen.Sync(postmangen.SyncOptions{
		Generation: opts,
		Writer:     os.Stdout,
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "Generated %s with %d routes.\n", result.Generation.OutputPath, result.Generation.RouteCount)
	if len(result.Targets) == 0 {
		fmt.Fprintln(os.Stdout, "No Postman sync targets configured; collection generated locally only.")
		return nil
	}
	fmt.Fprintf(os.Stdout, "Synced %d Postman target(s).\n", len(result.Targets))
	return nil
}

func postmanOptions(ctx Context) (postmangen.Options, error) {
	opts, _, err := postmangen.ResolveEnvironment(postmangen.Options{Root: ctx.Root})
	if err != nil {
		return postmangen.Options{}, err
	}
	if strings.TrimSpace(opts.Name) == "" {
		opts.Name = firstNonEmpty(ctx.ProjectName, filepath.Base(ctx.Root)) + " Generated API"
	}
	return opts, nil
}

type pluginUpdate struct {
	Name             string
	InstalledVersion string
	AvailableVersion string
}

func availablePluginUpdates(catalog []moduleinstall.CatalogModule, installed []Module) []pluginUpdate {
	installedByName := make(map[string]Module, len(installed))
	for _, module := range installed {
		installedByName[module.Name] = module
	}
	updates := make([]pluginUpdate, 0)
	for _, published := range catalog {
		current, ok := installedByName[published.Name]
		if !ok || current.LocalSource != "" || !moduleinstall.IsNewerVersion(current.Version, published.Version) {
			continue
		}
		updates = append(updates, pluginUpdate{
			Name: published.Name, InstalledVersion: current.Version, AvailableVersion: published.Version,
		})
	}
	return updates
}

func promptUpdatePlugin(updates []pluginUpdate) error {
	if len(updates) == 0 {
		return errors.New("all published plugins are up to date")
	}
	name := updates[0].Name
	options := make([]huh.Option[string], 0, len(updates))
	versions := make(map[string]string, len(updates))
	for _, update := range updates {
		versions[update.Name] = update.AvailableVersion
		label := fmt.Sprintf("%s  v%s → v%s", update.Name,
			strings.TrimPrefix(update.InstalledVersion, "v"), strings.TrimPrefix(update.AvailableVersion, "v"))
		options = append(options, huh.NewOption(label, update.Name))
	}
	form := newForm(huh.NewSelect[string]().
		Title("Update a published plugin").
		Description("The module dependency, generated plugin configuration and environment example will be updated.").
		Options(options...).
		Value(&name))
	if err := runForm(form); err != nil {
		return err
	}
	return moduleinstall.Update(moduleinstall.UpdateOptions{
		Name: name, Version: versions[name], Published: true, Writer: os.Stdout,
	})
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

func promptCreateStandalonePlugin(ctx Context) error {
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
	scaffold := "full"
	scaffoldForm := newForm(
		huh.NewSelect[string]().
			Title("Module boilerplate").
			Description("The complete preset is runnable and provides the usual Procyon layers.").
			Options(
				huh.NewOption("Complete boilerplate (recommended)", "full"),
				huh.NewOption("Minimal plugin contract", "minimal"),
			).
			Value(&scaffold),
	)
	if err := runForm(scaffoldForm); err != nil {
		return err
	}
	controllerNames := []string{"status", "records", "admin"}
	if scaffold == "full" {
		controllerForm := newForm(
			huh.NewMultiSelect[string]().
				Title("HTTP surfaces to expose").
				Description("Use Space to toggle routes; an empty selection creates a service-only module.").
				Options(
					huh.NewOption("Public status — GET /"+name, "status"),
					huh.NewOption("Authenticated records — /"+name+"/records", "records"),
					huh.NewOption("Admin Stats — GET /"+name+"/stats", "admin"),
				).
				Value(&controllerNames),
		)
		if err := runForm(controllerForm); err != nil {
			return err
		}
	}
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
	controllers := make([]plugincreate.Controller, 0, len(controllerNames))
	for _, controller := range controllerNames {
		controllers = append(controllers, plugincreate.Controller(controller))
	}
	result, err := plugincreate.Create(plugincreate.Options{
		Name: name, GoModule: goModule, OutputDir: outputDir,
		CoreVersion: ctx.CoreVersion, CLIVersion: buildinfo.CLIVersion,
		Minimal: scaffold == "minimal", Controllers: controllers,
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "Created standalone plugin source in %s.\n", result.Root)
	if err := plugincreate.Prepare(result.Root); err != nil {
		return err
	}
	return moduleinstall.Run(moduleinstall.Options{
		Name: result.Name, Source: result.Root, Writer: os.Stdout, Values: map[string]string{},
	})
}

func showPlugins(ctx Context, updates []pluginUpdate, catalogErr error) error {
	updatesByName := make(map[string]pluginUpdate, len(updates))
	for _, update := range updates {
		updatesByName[update.Name] = update
	}
	description := "No shared plugins are installed."
	if len(ctx.Modules) > 0 {
		lines := make([]string, 0, len(ctx.Modules))
		for _, module := range ctx.Modules {
			status := "enabled"
			if !module.Enabled {
				status = "disabled"
			}
			line := fmt.Sprintf("• %s %s (%s)", module.Name, module.Version, status)
			if update, ok := updatesByName[module.Name]; ok {
				line += fmt.Sprintf(" → %s available", update.AvailableVersion)
			}
			if module.LocalSource != "" {
				line += " — local development source"
			}
			lines = append(lines, line)
		}
		if catalogErr != nil {
			lines = append(lines, "", "Published registry unavailable; update status could not be checked.")
		}
		description = strings.Join(lines, "\n")
	}
	form := newForm(huh.NewNote().Title("Installed plugins").Description(description))
	return runForm(form)
}

func projectSummary(ctx Context, updates []pluginUpdate, catalogErr error) string {
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
	if catalogErr != nil {
		lines = append(lines, "Plugin updates: unavailable")
	} else {
		lines = append(lines, fmt.Sprintf("Plugin updates: %d", len(updates)))
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
