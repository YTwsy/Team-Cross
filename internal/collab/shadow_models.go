package collab

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"teamcross/internal/runtimeconfig"
	"teamcross/internal/shadow"
)

type shadowEffortOption struct {
	ReasoningEffort string `json:"reasoningEffort"`
	Description     string `json:"description"`
}
type shadowModelOption struct {
	Model                     string               `json:"model"`
	DisplayName               string               `json:"displayName"`
	DefaultReasoningEffort    string               `json:"defaultReasoningEffort"`
	SupportedReasoningEfforts []shadowEffortOption `json:"supportedReasoningEfforts"`
	IsDefault                 bool                 `json:"isDefault"`
	Hidden                    bool                 `json:"hidden,omitempty"`
	// Custom configured models may have no catalog capability entry.
	Custom bool `json:"custom,omitempty"`
}
type shadowModelOptions struct {
	Models   []shadowModelOption  `json:"models"`
	Defaults shadow.ModelSettings `json:"defaults"`
}

// Read only model metadata through the same native home, cwd and trusted config
// as the shadow. This process creates no thread and sends no model input.
func (a *App) shadowModels(ctx context.Context, cwd string) (shadowModelOptions, error) {
	out := shadowModelOptions{Models: []shadowModelOption{}}
	info, err := os.Stat(cwd)
	if !filepath.IsAbs(cwd) || err != nil || !info.IsDir() {
		return out, fmt.Errorf("分身工作目录不可用")
	}
	home, err := a.providerHome("codex")
	if err != nil {
		return out, err
	}
	dir, err := os.MkdirTemp(a.Config.DataDir, "shadow-models-")
	if err != nil {
		return out, err
	}
	defer os.RemoveAll(dir)
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	p, err := a.startProcessWithMode(ctx, home, cwd, filepath.Join(dir, "native.log"), runtimeconfig.Trusted)
	if err != nil {
		return out, err
	}
	defer p.Close()
	var cfg struct {
		Config struct {
			Model    string `json:"model"`
			Provider string `json:"model_provider"`
			Effort   string `json:"model_reasoning_effort"`
		} `json:"config"`
	}
	if err = p.Call(ctx, "config/read", map[string]any{"cwd": cwd, "includeLayers": false}, &cfg); err != nil {
		return out, err
	}
	// An omitted provider selects Codex's built-in OpenAI provider. Do not copy
	// provider definitions or credentials into the public catalog or shadow record.
	if cfg.Config.Provider == "" {
		cfg.Config.Provider = "openai"
	}
	out.Defaults = shadow.ModelSettings{Model: cfg.Config.Model, ModelProvider: cfg.Config.Provider, ReasoningEffort: cfg.Config.Effort}
	cursor := ""
	seen := map[string]bool{}
	for page := 0; ; page++ {
		if page >= 25 {
			return out, fmt.Errorf("原生模型列表分页超出上限")
		}
		var list struct {
			Data []shadowModelOption `json:"data"`
			Next string              `json:"nextCursor"`
		}
		params := map[string]any{"limit": 100, "includeHidden": true}
		if cursor != "" {
			params["cursor"] = cursor
		}
		if err = p.Call(ctx, "model/list", params, &list); err != nil {
			return out, err
		}
		for _, model := range list.Data {
			if model.Model == "" || seen[model.Model] {
				continue
			}
			seen[model.Model] = true
			if out.Defaults.Model == "" && model.IsDefault {
				out.Defaults.Model = model.Model
			}
			if model.Hidden && model.Model != out.Defaults.Model {
				continue
			}
			if model.DisplayName == "" {
				model.DisplayName = model.Model
			}
			if model.SupportedReasoningEfforts == nil {
				model.SupportedReasoningEfforts = []shadowEffortOption{}
			}
			out.Models = append(out.Models, model)
		}
		if list.Next == "" {
			break
		}
		if list.Next == cursor {
			return out, fmt.Errorf("原生模型列表分页未推进")
		}
		cursor = list.Next
	}
	found := false
	for i := range out.Models {
		model := &out.Models[i]
		if len(model.SupportedReasoningEfforts) == 0 {
			model.Custom = true
		}
		if model.Model == out.Defaults.Model {
			found = true
			if out.Defaults.ReasoningEffort == "" || (!model.Custom && !model.supports(out.Defaults.ReasoningEffort)) {
				out.Defaults.ReasoningEffort = model.DefaultReasoningEffort
			}
		}
	}
	if !found && out.Defaults.Model != "" {
		// Preserve an explicitly configured third-party model, even if the native
		// catalog lacks its capabilities. The user supplies an effort and the native
		// start response must confirm it; we never replace it with a catalog default.
		out.Models = append(out.Models, shadowModelOption{Model: out.Defaults.Model, DisplayName: out.Defaults.Model, DefaultReasoningEffort: out.Defaults.ReasoningEffort, SupportedReasoningEfforts: []shadowEffortOption{}, Custom: true})
	}
	if out.Defaults.Model == "" || len(out.Models) == 0 {
		return out, fmt.Errorf("本机 Codex 未提供可选择的模型")
	}
	return out, nil
}
func (m shadowModelOption) supports(effort string) bool {
	for _, option := range m.SupportedReasoningEfforts {
		if option.ReasoningEffort == effort {
			return true
		}
	}
	return false
}
func (o shadowModelOptions) validate(settings shadow.ModelSettings) error {
	if err := settings.Validate(); err != nil {
		return err
	}
	if settings.ModelProvider != o.Defaults.ModelProvider {
		return fmt.Errorf("本机模型 Provider 已变化，请重新读取模型并创建分身")
	}
	for _, model := range o.Models {
		if model.Model == settings.Model && (model.Custom || model.supports(settings.ReasoningEffort)) {
			return nil
		}
	}
	return fmt.Errorf("所选模型或推理强度不在本机可用列表中，请重新读取模型")
}
