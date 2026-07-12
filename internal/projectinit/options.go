package projectinit

type Options struct {
	Name            string
	Module          string
	OutputDir       string
	Database        string
	Auth            string
	TemplateVersion string
	IncludeDocker   bool
	IncludeHello    bool
	Force           bool
}
