package templateinit

type Options struct {
	Name          string
	Module        string
	OutputDir     string
	Database      string
	Auth          string
	IncludeDocker bool
	IncludeHello  bool
	Force         bool
}
