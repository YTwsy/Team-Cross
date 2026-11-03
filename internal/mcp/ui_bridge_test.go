package mcp

import (
	"bytes"
	"context"
	"os"
	"strings"
	"teamcross/internal/mcpassets"
	"testing"
)

func TestUIBridgeRequirementBeforeCoreAndLoadedReceipt(t *testing.T) {
	if UIBridgeProtocol < mcpassets.Current.RequiredBridgeProtocol {
		t.Fatal("embedded UI requires a newer bridge")
	}
	for _, body := range []any{nil, map[string]any{"requiredBridgeProtocol": 0}, map[string]any{"requiredBridgeProtocol": UIBridgeProtocol}} {
		if err := checkUIBridgeRequirement(body); err != nil {
			t.Fatal(err)
		}
	}
	for _, body := range []any{map[string]any{"requiredBridgeProtocol": 2}, map[string]any{"requiredBridgeProtocol": -1}, map[string]any{"requiredBridgeProtocol": 1.5}} {
		if checkUIBridgeRequirement(body) == nil {
			t.Fatal("accepted invalid/incompatible requirement", body)
		}
	}
	dir := t.TempDir()
	var out bytes.Buffer
	input := `{"id":1,"method":"tools/call","params":{"name":"teamcross_ui_read","arguments":{"path":"plugin/bootstrap","body":{"requiredBridgeProtocol":2}}}}` + "\n"
	if err := ServeUI(context.Background(), dir, strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "plugin_reload_required") {
		t.Fatal(out.String())
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 0 {
		t.Fatal("incompatible bootstrap touched Core state", files, err)
	}
}
