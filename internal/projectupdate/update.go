package projectupdate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/bartek5186/procyon-cli/internal/buildinfo"
)

const CoreModule = "github.com/bartek5186/procyon-core"

type Options struct {
	Version string
	DryRun  bool
	Writer  io.Writer
}

func Run(opts Options) error {
	if opts.Writer == nil {
		opts.Writer = os.Stdout
	}
	version := strings.TrimSpace(opts.Version)
	if version == "" {
		version = "latest"
	}

	metadata, err := validateProject()
	if err != nil {
		return err
	}

	commands := [][]string{
		{"go", "get", CoreModule + "@" + version},
		{"go", "mod", "tidy"},
		{"go", "test", "./..."},
	}
	for _, args := range commands {
		fmt.Fprintf(opts.Writer, "+ %s\n", strings.Join(args, " "))
		if opts.DryRun {
			continue
		}
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Stdout = opts.Writer
		cmd.Stderr = opts.Writer
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("%s: %w", strings.Join(args, " "), err)
		}
	}

	if !opts.DryRun {
		installedVersion := installedCoreVersion()
		if installedVersion == "" {
			installedVersion = version
		}
		if metadata != nil {
			metadata.CoreVersion = installedVersion
			if err := writeMetadata(*metadata); err != nil {
				return err
			}
		}
		fmt.Fprintf(opts.Writer, "Procyon core updated to %s and project tests passed.\n", installedVersion)
	}
	return nil
}

type projectMetadata struct {
	SchemaVersion   int    `json:"schema_version"`
	ProjectModule   string `json:"project_module"`
	TemplateVersion string `json:"template_version"`
	CoreVersion     string `json:"core_version"`
	CLIMinVersion   string `json:"cli_min_version"`
}

func validateProject() (*projectMetadata, error) {
	raw, err := os.ReadFile("go.mod")
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, errors.New("current directory is not a Go module; run this command in a Procyon project")
		}
		return nil, err
	}
	if !bytes.Contains(raw, []byte(CoreModule)) {
		return nil, fmt.Errorf("current module does not depend on %s", CoreModule)
	}

	metadataRaw, err := os.ReadFile(".procyon.json")
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var metadata projectMetadata
	if err := json.Unmarshal(metadataRaw, &metadata); err != nil {
		return nil, fmt.Errorf("parse .procyon.json: %w", err)
	}
	if metadata.SchemaVersion != 1 {
		return nil, fmt.Errorf("unsupported .procyon.json schema version %d", metadata.SchemaVersion)
	}
	compatible, err := versionAtLeast(buildinfo.CLIVersion, metadata.CLIMinVersion)
	if err != nil {
		return nil, fmt.Errorf("check CLI compatibility: %w", err)
	}
	if !compatible {
		return nil, fmt.Errorf("project requires procyon-cli %s or newer; installed version is %s", metadata.CLIMinVersion, buildinfo.CLIVersion)
	}
	return &metadata, nil
}

func installedCoreVersion() string {
	cmd := exec.Command("go", "list", "-m", "-f={{.Version}}", CoreModule)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func writeMetadata(metadata projectMetadata) error {
	raw, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return os.WriteFile(".procyon.json", raw, 0o644)
}

func versionAtLeast(current, minimum string) (bool, error) {
	currentParts, err := parseVersion(current)
	if err != nil {
		return false, err
	}
	minimumParts, err := parseVersion(minimum)
	if err != nil {
		return false, err
	}
	for i := range currentParts {
		if currentParts[i] != minimumParts[i] {
			return currentParts[i] > minimumParts[i], nil
		}
	}
	return true, nil
}

func parseVersion(value string) ([3]int, error) {
	var out [3]int
	value = strings.TrimPrefix(strings.TrimSpace(value), "v")
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return out, fmt.Errorf("invalid semantic version %q", value)
	}
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return out, fmt.Errorf("invalid semantic version %q", value)
		}
		out[i] = n
	}
	return out, nil
}
