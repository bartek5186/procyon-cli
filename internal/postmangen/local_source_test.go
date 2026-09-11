package postmangen

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestCollectPluginRoutesDistinguishesSameSourceFromNameCollision(t *testing.T) {
	for _, variant := range []string{"relative", "absolute", "normalized", "different_source"} {
		t.Run(variant, func(t *testing.T) {
			project := t.TempDir()
			plugin := filepath.Join(project, "plugins", "rewards")
			code := `package rewards
type Plugin struct{}
func (*Plugin) RegisterRoutes(routes Routes) {
	routes.Public.GET("/rewards", listRewards)
}
`
			writePostmanTestFile(t, filepath.Join(plugin, "plugin.go"), code)
			source := "plugins/rewards"
			switch variant {
			case "absolute":
				source = plugin
			case "normalized":
				source = "plugins/../plugins/rewards"
			case "different_source":
				source = t.TempDir()
				writePostmanTestFile(t, filepath.Join(source, "plugin.go"), code)
			}
			metadata, err := json.Marshal(map[string]any{"modules": map[string]any{
				"rewards": map[string]any{"kind": "go-plugin", "enabled": true, "local_source": source},
			}})
			if err != nil {
				t.Fatal(err)
			}
			writePostmanTestFile(t, filepath.Join(project, ".procyon.json"), string(metadata))
			routes, err := (&generator{root: project}).collectPluginRoutes()
			if variant == "different_source" {
				if err == nil || !strings.Contains(err.Error(), "exists as both local and installed") {
					t.Fatalf("expected actual source collision, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(routes) != 1 {
				t.Fatalf("got %d routes, want one registration", len(routes))
			}
			assertPluginRoute(t, routes, "GET", "/v1/rewards", routeAuthPublic, false)
		})
	}
}
