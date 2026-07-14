package selfupdate

import (
	"bytes"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunDryRunPrintsGoInstallWithoutExecuting(t *testing.T) {
	t.Setenv(installMethodEnvironment, "")
	original := runCommand
	t.Cleanup(func() { runCommand = original })
	called := false
	runCommand = func(io.Writer, string, ...string) error {
		called = true
		return errors.New("must not run")
	}
	var output bytes.Buffer
	if err := Run(Options{Version: "0.3.4", DryRun: true, Writer: &output}); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("go install executed during dry-run")
	}
	if !strings.Contains(output.String(), "go install "+CLIModule+"@v0.3.4") {
		t.Fatalf("unexpected output: %s", output.String())
	}
}

func TestRunDryRunUsesNPMForNPMInstallation(t *testing.T) {
	t.Setenv(installMethodEnvironment, installMethodNPM)
	original := runCommand
	t.Cleanup(func() { runCommand = original })
	called := false
	runCommand = func(io.Writer, string, ...string) error {
		called = true
		return errors.New("must not run")
	}
	var output bytes.Buffer
	if err := Run(Options{Version: "v0.3.4", DryRun: true, Writer: &output}); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("npm install executed during dry-run")
	}
	if !strings.Contains(output.String(), "npm install --global "+npmPackage+"@0.3.4") {
		t.Fatalf("unexpected output: %s", output.String())
	}
}

func TestRunExecutesNPMUpdate(t *testing.T) {
	t.Setenv(installMethodEnvironment, installMethodNPM)
	original := runCommand
	t.Cleanup(func() { runCommand = original })
	var name string
	var args []string
	runCommand = func(_ io.Writer, command string, commandArgs ...string) error {
		name = command
		args = append([]string(nil), commandArgs...)
		return nil
	}
	var output bytes.Buffer
	if err := Run(Options{Version: "latest", Writer: &output}); err != nil {
		t.Fatal(err)
	}
	if name != "npm" || strings.Join(args, " ") != "install --global procyon-cli@latest" {
		t.Fatalf("command = %s %s", name, strings.Join(args, " "))
	}
}

func TestNormalizeVersion(t *testing.T) {
	for input, expected := range map[string]string{"": "latest", "latest": "latest", "0.3.4": "v0.3.4", "v1.2.3": "v1.2.3"} {
		actual, err := normalizeVersion(input)
		if err != nil || actual != expected {
			t.Fatalf("normalizeVersion(%q) = %q, %v; want %q", input, actual, err, expected)
		}
	}
	for _, input := range []string{"main", "0.3", "v0.3.4-beta", "01.2.3"} {
		if _, err := normalizeVersion(input); err == nil {
			t.Fatalf("normalizeVersion(%q) should fail", input)
		}
	}
}

func TestInstalledBinaryPathUsesGoBinThenGoPath(t *testing.T) {
	original := commandOutput
	t.Cleanup(func() { commandOutput = original })
	commandOutput = func(_ string, args ...string) ([]byte, error) {
		if args[len(args)-1] == "GOBIN" {
			return []byte("/custom/bin\n"), nil
		}
		return nil, errors.New("unexpected command")
	}
	path, err := installedBinaryPath()
	if err != nil || filepath.Dir(path) != "/custom/bin" {
		t.Fatalf("installedBinaryPath() = %q, %v", path, err)
	}

	commandOutput = func(_ string, args ...string) ([]byte, error) {
		if args[len(args)-1] == "GOBIN" {
			return nil, nil
		}
		return []byte("/home/test/go\n"), nil
	}
	path, err = installedBinaryPath()
	if err != nil || filepath.Dir(path) != "/home/test/go/bin" {
		t.Fatalf("GOPATH installedBinaryPath() = %q, %v", path, err)
	}
}
