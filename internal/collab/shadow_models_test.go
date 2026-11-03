package collab

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"teamcross/internal/shadow"
)

type shadowCatalogRuntime struct {
	Runtime
	config map[string]any
	pages  [][]shadowModelOption
	calls  []string
	closed bool
}

func (p *shadowCatalogRuntime) Close() { p.closed = true }
func (p *shadowCatalogRuntime) Call(ctx context.Context, method string, params, out any) error {
	p.calls = append(p.calls, method)
	var result any
	switch method {
	case "config/read":
		result = map[string]any{"config": p.config, "origins": map[string]any{"private": "secret-path"}}
	case "model/list":
		if cursor, supplied := params.(map[string]any)["cursor"]; supplied && cursor == "" {
			return fmt.Errorf("invalid empty native cursor")
		}
		index := 0
		if params.(map[string]any)["cursor"] == "next" {
			index = 1
		}
		page := map[string]any{"data": p.pages[index]}
		if index+1 < len(p.pages) {
			page["nextCursor"] = "next"
		}
		result = page
	default:
		return p.Runtime.Call(ctx, method, params, out)
	}
	b, _ := json.Marshal(result)
	return json.Unmarshal(b, out)
}
func catalogOption(model, effort string) shadowModelOption {
	return shadowModelOption{Model: model, DisplayName: model, DefaultReasoningEffort: effort, SupportedReasoningEfforts: []shadowEffortOption{{ReasoningEffort: effort}}}
}
func TestShadowModelCatalogUsesNativeMetadataAndRedactsConfiguration(t *testing.T) {
	a, f, cwd := fixture(t)
	runtime := &shadowCatalogRuntime{Runtime: f, config: map[string]any{"model": "configured", "model_provider": "custom-provider", "model_reasoning_effort": "high", "mcp_servers": "private-secret", "model_providers": "private-secret"}, pages: [][]shadowModelOption{{catalogOption("first", "low")}, {catalogOption("configured", "high")}}}
	a.Config.StartProcess = func(string, string, string, string) (Runtime, error) { return runtime, nil }
	options, err := a.shadowModels(context.Background(), cwd)
	if err != nil {
		t.Fatal(err)
	}
	if len(options.Models) != 2 || options.Defaults != (shadow.ModelSettings{Model: "configured", ModelProvider: "custom-provider", ReasoningEffort: "high"}) {
		t.Fatal(options)
	}
	b, _ := json.Marshal(options)
	if strings.Contains(string(b), "private") || strings.Contains(string(b), "secret") || !runtime.closed {
		t.Fatal("metadata leaked or reader left running", string(b))
	}
	for _, method := range runtime.calls {
		if method != "config/read" && method != "model/list" {
			t.Fatal("catalog created native work", method)
		}
	}
	bad := options.Defaults
	bad.ReasoningEffort = "unsupported"
	if options.validate(bad) == nil {
		t.Fatal("accepted unsupported effort")
	}
	runtime.config["model"] = "custom-not-listed"
	runtime.closed = false
	options, err = a.shadowModels(context.Background(), cwd)
	if err != nil || options.Defaults.Model != "custom-not-listed" || !options.Models[len(options.Models)-1].Custom {
		t.Fatal("replaced custom native model", options, err)
	}
	if err = options.validate(options.Defaults); err != nil {
		t.Fatal(err)
	}
}
func TestShadowCreateRetryKeepsSettingsWithoutRereadingChangedDefaults(t *testing.T) {
	a, f, s := spaceFixture(t)
	cwd := f.source.Cwd
	calls := 0
	a.Config.StartProcess = func(string, string, string, string) (Runtime, error) {
		calls++
		return &shadowCatalogRuntime{Runtime: f, config: map[string]any{"model": "configured", "model_provider": "custom-provider", "model_reasoning_effort": "high"}, pages: [][]shadowModelOption{{catalogOption("configured", "high")}}}, nil
	}
	settings := shadow.ModelSettings{Model: "configured", ModelProvider: "custom-provider", ReasoningEffort: "high"}
	in := shadowInput{RequestID: uuid.NewString(), Name: "fixed settings", Cwd: cwd, Settings: settings}
	if _, err := a.createShadow(context.Background(), s.record.ID, in); err != nil {
		t.Fatal(err)
	}
	e := a.shadows[in.RequestID]
	if err := e.SetEnabled(false); err != nil {
		t.Fatal(err)
	}
	before := calls
	if _, err := a.createShadow(context.Background(), s.record.ID, in); err != nil {
		t.Fatal(err)
	}
	if calls != before || e.View().Settings != settings {
		t.Fatal("retry re-resolved defaults")
	}
	in.Settings = shadow.ModelSettings{}
	if _, err := a.createShadow(context.Background(), s.record.ID, in); err != nil {
		t.Fatal("lost pre-update creation retry", err)
	}
	in.Settings = settings
	in.Settings.ReasoningEffort = "low"
	if _, err := a.createShadow(context.Background(), s.record.ID, in); err == nil {
		t.Fatal("same request changed pinned settings")
	}
}
