package gocommand

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// New creates a command and isolates a Go module from an unrelated parent
// workspace. An explicitly configured GOWORK value is always respected.
func New(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	if name == "go" {
		cmd.Env = projectEnvironment()
	}
	return cmd
}

func projectEnvironment() []string {
	environment := os.Environ()
	if _, explicitlyConfigured := os.LookupEnv("GOWORK"); explicitlyConfigured {
		return environment
	}
	current, err := os.Getwd()
	if err != nil || currentModuleBelongsToActiveWorkspace(current) {
		return environment
	}
	return append(environment, "GOWORK=off")
}

func currentModuleBelongsToActiveWorkspace(current string) bool {
	command := exec.Command("go", "list", "-m", "-f={{.Dir}}")
	output, err := command.Output()
	if err != nil {
		return false
	}
	current, err = filepath.Abs(current)
	if err != nil {
		return false
	}
	current = filepath.Clean(current)
	for _, directory := range strings.Split(string(output), "\n") {
		directory = strings.TrimSpace(directory)
		if directory == "" {
			continue
		}
		absolute, err := filepath.Abs(directory)
		if err == nil && filepath.Clean(absolute) == current {
			return true
		}
	}
	return false
}
