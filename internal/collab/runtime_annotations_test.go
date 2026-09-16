package collab

import (
	"strings"
	"teamcross/internal/mcp"
	"teamcross/internal/runtimeconfig"
	"testing"
)

func TestRuntimeAnnotationConfigDisablesInheritedServersWithoutTransportCollision(t *testing.T) {
	launch := annotationLaunch{Command: "/path with spaces/teamcross", Args: []string{"mcp", "--runtime-id", "shared"}, Env: map[string]string{mcp.RuntimeTokenEnv: "scoped-token"}}
	config := launch.codexOverrides([]string{"personal", mcp.RuntimeServer, "name.with.dots"})
	if len(config) != 1 || !strings.HasPrefix(config[0], `mcp_servers={"personal"={enabled=false},"teamcross_annotations"={enabled=false},"name.with.dots"={enabled=false},`) {
		t.Fatal(config)
	}
	if !strings.Contains(config[0], `teamcross_annotations_1={command="/path with spaces/teamcross"`) || strings.Contains(config[0], "model") {
		t.Fatal(config[0])
	}
}

func TestTrustedAnnotationsPreservePersonalAndPluginServers(t *testing.T) {
	launch := annotationLaunch{Command: "/fixture/teamcross", Args: []string{"mcp"}, Env: map[string]string{mcp.RuntimeTokenEnv: "fixture-token"}}
	config := launch.codexOverridesForMode([]string{"personal", "codex_app", "cua_repl", mcp.RuntimeServer}, runtimeconfig.Trusted)
	if len(config) != 1 || strings.Contains(config[0], "enabled=false") || !strings.Contains(config[0], "teamcross_annotations_1={") {
		t.Fatalf("trusted runtime disabled inherited tools or collided: %v", config)
	}
}
