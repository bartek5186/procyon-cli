package projectinit

type Options struct {
	Name          string
	Module        string
	OutputDir     string
	Database      string
	IncludeDocker bool
	IncludeHello  bool
	Force         bool
}
