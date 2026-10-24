package collab

import (
	"fmt"

	"teamcross/internal/shadow"
)

type ShadowSourceResult struct {
	Source      SpaceSourceRef     `json:"source"`
	Message     *shadow.Message    `json:"message,omitempty"`
	Result      *shadow.TaskResult `json:"result,omitempty"`
	Unavailable string             `json:"unavailable,omitempty"`
}

func shadowSource(shared ShadowShared, in shadowInput) (ShadowSourceResult, error) {
	out := ShadowSourceResult{Source: SpaceSourceRef{SpaceID: shared.View.SpaceID, ShadowID: shared.View.ID, ShadowMessageID: in.MessageID, ShadowResultID: in.ResultID}}
	if (in.MessageID == "") == (in.ResultID == "") || in.Offset != 0 {
		return out, fmt.Errorf("分身来源需要一个准确消息或结果标识，不能同时分页")
	}
	if in.MessageID != "" {
		for _, message := range shared.View.Messages {
			if message.ID == in.MessageID {
				out.Message = &message
				return out, nil
			}
		}
		out.Unavailable = "shadow_message_unavailable"
		return out, nil
	}
	for _, result := range shared.View.TaskResults {
		if result.ID == in.ResultID {
			out.Result = &result
			return out, nil
		}
	}
	out.Unavailable = "shadow_result_unavailable"
	return out, nil
}

func validateShadowResults(previous, incoming []shadow.TaskResult) error {
	if len(incoming) > shadow.TaskResultHistoryLimit {
		return fmt.Errorf("分身结果历史超过保留上限")
	}
	old := map[string]shadow.TaskResult{}
	for _, result := range previous {
		old[result.ID] = result
	}
	seen := map[string]bool{}
	for _, result := range incoming {
		if result.ID == "" || result.TaskID == "" || result.TaskVersion < 1 || seen[result.ID] || len(result.Text) > 64<<10 {
			return fmt.Errorf("分身结果身份或内容无效")
		}
		seen[result.ID] = true
		if prior, exists := old[result.ID]; exists && contentHash(prior) != contentHash(result) {
			return fmt.Errorf("分身历史结果不可覆盖")
		}
	}
	return nil
}
