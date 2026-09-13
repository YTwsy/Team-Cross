package collab

import (
	"strings"
	"teamcross/internal/mcp"
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
