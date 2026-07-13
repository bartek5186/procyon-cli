package moduleinstall

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const generatedPluginConfigPath = "config/plugins.generated.json"

type generatedProjectState struct {
	Metadata []byte
	Plugins  []byte
	Config   []byte
	Env      []byte
}

func buildGeneratedProjectState(metadata projectMetadata) (generatedProjectState, error) {
	metadataBody, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return generatedProjectState{}, err
	}
	metadataBody = append(metadataBody, '\n')
	pluginsBody, err := generatePluginsFile(metadata.Modules)
	if err != nil {
		return generatedProjectState{}, err
	}
	configBody, err := generatePluginConfig(metadata.Modules)
	if err != nil {
		return generatedProjectState{}, err
	}
	currentEnv, err := os.ReadFile(".env.example")
	if err != nil && !os.IsNotExist(err) {
		return generatedProjectState{}, err
	}
	envBody := syncEnvironmentExample(currentEnv, metadata.Modules)
	return generatedProjectState{Metadata: metadataBody, Plugins: pluginsBody, Config: configBody, Env: envBody}, nil
}

func writeGeneratedProjectState(state generatedProjectState) error {
	if err := os.MkdirAll(filepath.Dir(generatedPluginConfigPath), 0o755); err != nil {
		return err
	}
	files := []struct {
		path string
		body []byte
	}{
		{path: ".procyon.json", body: state.Metadata},
		{path: "plugins_gen.go", body: state.Plugins},
		{path: generatedPluginConfigPath, body: state.Config},
		{path: ".env.example", body: state.Env},
	}
	for _, file := range files {
		if err := os.WriteFile(file.path, file.body, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func generatedProjectPaths(extra ...string) []string {
	return append([]string{".procyon.json", "plugins_gen.go", generatedPluginConfigPath, ".env.example"}, extra...)
}

func generatePluginConfig(modules map[string]InstalledModule) ([]byte, error) {
	config := map[string]json.RawMessage{}
	for name, module := range modules {
		if module.Kind != "go-plugin" || !moduleEnabled(module) {
			continue
		}
		body, err := json.Marshal(map[string]any{
			"providers": module.Providers,
			"values":    nonNilStringMap(module.Values),
		})
		if err != nil {
			return nil, err
		}
		config[name] = body
	}
	body, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(body, '\n'), nil
}

func syncEnvironmentExample(current []byte, modules map[string]InstalledModule) []byte {
	base := removeManagedEnvironmentBlocks(string(current))
	names := make([]string, 0, len(modules))
	for name, module := range modules {
		if moduleEnabled(module) && len(module.Environment) > 0 {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	var output strings.Builder
	output.WriteString(strings.TrimRight(base, "\n"))
	for _, name := range names {
		if output.Len() > 0 {
			output.WriteString("\n\n")
		}
		fmt.Fprintf(&output, "# >>> procyon module: %s\n", name)
		for _, variable := range modules[name].Environment {
			if description := strings.TrimSpace(variable.Description); description != "" {
				fmt.Fprintf(&output, "# %s\n", description)
			}
			fmt.Fprintf(&output, "%s=%s\n", variable.Name, variable.Default)
		}
		fmt.Fprintf(&output, "# <<< procyon module: %s", name)
	}
	if output.Len() > 0 {
		output.WriteByte('\n')
	}
	return []byte(output.String())
}

func removeManagedEnvironmentBlocks(content string) string {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	kept := make([]string, 0, len(lines))
	inManagedBlock := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# >>> procyon module:") {
			inManagedBlock = true
			continue
		}
		if inManagedBlock {
			if strings.HasPrefix(trimmed, "# <<< procyon module:") {
				inManagedBlock = false
			}
			continue
		}
		kept = append(kept, line)
	}
	return strings.TrimRight(strings.Join(kept, "\n"), "\n")
}

func selectedEnvironment(environment []EnvironmentVariable, providers []string) []EnvironmentVariable {
	selected := make(map[string]bool, len(providers))
	for _, provider := range providers {
		selected[strings.ToLower(strings.TrimSpace(provider))] = true
	}
	result := make([]EnvironmentVariable, 0, len(environment))
	for _, variable := range environment {
		if len(variable.Providers) == 0 || intersectsProviders(variable.Providers, selected) {
			result = append(result, variable)
		}
	}
	return result
}

func intersectsProviders(providers []string, selected map[string]bool) bool {
	for _, provider := range providers {
		if selected[strings.ToLower(strings.TrimSpace(provider))] {
			return true
		}
	}
	return false
}

func moduleEnabled(module InstalledModule) bool {
	return module.Enabled == nil || *module.Enabled
}

func boolPointer(value bool) *bool { return &value }

func nonNilStringMap(values map[string]string) map[string]string {
	if values == nil {
		return map[string]string{}
	}
	return values
}
