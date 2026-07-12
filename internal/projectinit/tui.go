package projectinit

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"charm.land/huh/v2"
)

func completeOptionsTUI(opts Options, wd string) (Options, error) {
	accessible := os.Getenv("ACCESSIBLE") != ""
	theme := huh.ThemeFunc(huh.ThemeCatppuccin)

	if opts.Name == "" {
		opts.Name = filepath.Base(wd)
		form := huh.NewForm(
			huh.NewGroup(
				huh.NewNote().
					Title("✦  P R O C Y O N").
					Description("Initialize a Go backend"),
				huh.NewInput().
					Title("Project name").
					Description("Used in configuration and service names.").
					Value(&opts.Name).
					Validate(required("project name")),
			),
		).WithTheme(theme).WithAccessible(accessible)
		if err := form.Run(); err != nil {
			return opts, err
		}
	}

	if opts.Module == "" {
		opts.Module = "github.com/acme/" + slug(opts.Name)
	}
	if opts.OutputDir == "" {
		opts.OutputDir = "./" + slug(opts.Name)
	}
	if opts.Database == "" {
		opts.Database = "postgres"
	}
	if opts.Auth == "" {
		opts.Auth = "kratos-casbin"
	}

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Go module").
				Description("Full import path for the project.").
				Value(&opts.Module).
				Validate(required("Go module")),
			huh.NewInput().
				Title("Output directory").
				Description("Path relative to the current directory.").
				Value(&opts.OutputDir).
				Validate(required("output directory")),
		),
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Database").
				Options(
					huh.NewOption("PostgreSQL", "postgres"),
					huh.NewOption("MySQL", "mysql"),
				).
				Value(&opts.Database),
			huh.NewSelect[string]().
				Title("Authentication").
				Options(
					huh.NewOption("Kratos + Casbin RBAC", "kratos-casbin"),
					huh.NewOption("Kratos only", "kratos"),
					huh.NewOption("X-Admin-Key only", "admin"),
					huh.NewOption("No authentication", "none"),
				).
				Value(&opts.Auth),
			huh.NewConfirm().
				Title("Include Docker files?").
				Affirmative("Yes").
				Negative("No").
				Value(&opts.IncludeDocker),
			huh.NewConfirm().
				Title("Include the example hello module?").
				Affirmative("Yes").
				Negative("No").
				Value(&opts.IncludeHello),
		),
	).WithTheme(theme).WithAccessible(accessible)
	if err := form.Run(); err != nil {
		return opts, err
	}

	opts.OutputDir = cleanOutputDir(opts.OutputDir)
	confirmed := true
	summary := fmt.Sprintf(
		"Name: %s\nModule: %s\nOutput: %s\nDatabase: %s\nAuth: %s\nDocker: %s\nHello: %s",
		opts.Name,
		opts.Module,
		opts.OutputDir,
		opts.Database,
		opts.Auth,
		yesNo(opts.IncludeDocker),
		yesNo(opts.IncludeHello),
	)
	confirmForm := huh.NewForm(
		huh.NewGroup(
			huh.NewNote().Title("Summary").Description(summary),
			huh.NewConfirm().
				Title("Create this project?").
				Affirmative("Create").
				Negative("Cancel").
				Value(&confirmed),
		),
	).WithTheme(theme).WithAccessible(accessible)
	if err := confirmForm.Run(); err != nil {
		return opts, err
	}
	if !confirmed {
		return opts, errors.New("project creation cancelled")
	}

	return opts, nil
}

func required(label string) func(string) error {
	return func(value string) error {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", label)
		}
		return nil
	}
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}
