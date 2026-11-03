package shadow

import (
	"fmt"
	"strings"
)

// ModelSettings is chosen once, before the first native thread is created.
// Native configuration still owns credentials, routing details and permissions.
type ModelSettings struct {
	Model           string `json:"model"`
	ModelProvider   string `json:"modelProvider"`
	ReasoningEffort string `json:"reasoningEffort"`
}

func (s ModelSettings) Validate() error {
	for _, value := range []string{s.Model, s.ModelProvider, s.ReasoningEffort} {
		if value == "" || strings.TrimSpace(value) != value || len(value) > 256 || strings.ContainsAny(value, "\r\n\x00") {
			return fmt.Errorf("请选择分身启动时的模型和推理强度")
		}
	}
	return nil
}

func (s ModelSettings) threadParams(params map[string]any) map[string]any {
	params["model"], params["modelProvider"] = s.Model, s.ModelProvider
	params["config"] = map[string]any{"model_reasoning_effort": s.ReasoningEffort}
	return params
}

func (s ModelSettings) confirm(model, provider, effort string) error {
	if s.Model != model || s.ModelProvider != provider || s.ReasoningEffort != effort {
		return fmt.Errorf("原生运行时未确认分身启动配置：请求 %s / %s / %s，返回 %s / %s / %s；已暂停，未自动改用其他设置", s.ModelProvider, s.Model, s.ReasoningEffort, provider, model, effort)
	}
	return nil
}
