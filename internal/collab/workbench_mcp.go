package collab

import (
	"context"
	"encoding/json"
	"fmt"

	"teamcross/internal/mcp"
)

func (a *App) invokeWorkbenchTool(ctx context.Context, name string, args map[string]any, c agentCaller, runtimeID string) (any, error) {
	args = libraryClone(args)
	if runtimeID != "" {
		id := runtimeID
		a.mu.Lock()
		receiver := a.receivers[runtimeID]
		execution := a.executions[runtimeID]
		a.mu.Unlock()
		if receiver != nil {
			id = receiver.receiverSpace
		} else if execution != nil {
			id = execution.record.SpaceID
		} else {
			return nil, fmt.Errorf("绑定运行时不存在")
		}
		if _, supplied := args["spaceId"]; supplied {
			return nil, fmt.Errorf("绑定运行时不能选择其他空间")
		}
		args["spaceId"] = id
	} else if !mcp.WorkbenchReadTool(name) {
		if err := a.verifyConversation(ctx, c.Caller); err != nil {
			return nil, err
		}
	}
	if err := mcp.ValidateAgentCall(name, args); err != nil {
		return nil, err
	}
	id, _ := args["spaceId"].(string)
	if name == "list_space_shadows" || name == "read_space_shadow" || name == "send_space_shadow" {
		var payload struct {
			ID        string `json:"shadowId"`
			Offset    int    `json:"offset"`
			MessageID string `json:"messageId"`
			ResultID  string `json:"resultId"`
			Text      string `json:"text"`
		}
		encoded, err := json.Marshal(args)
		if err != nil {
			return nil, err
		}
		if err = json.Unmarshal(encoded, &payload); err != nil {
			return nil, err
		}
		in := shadowInput{ID: payload.ID, Offset: payload.Offset}
		op := "view"
		if name == "send_space_shadow" {
			op = "send"
			in.Message.ID, in.Message.Text = payload.MessageID, payload.Text
		} else if name == "read_space_shadow" && (payload.MessageID != "" || payload.ResultID != "") {
			op, in.MessageID, in.ResultID = "source", payload.MessageID, payload.ResultID
		}
		return a.shadowCall(ctx, id, op, in)
	}
	var in workbenchInput
	encoded, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(encoded, &in); err != nil {
		return nil, fmt.Errorf("空间工具参数类型无效: %w", err)
	}
	in.Actor = SpaceActor{Kind: "session", Provider: c.Caller.Provider, Session: contentHash([]string{c.Caller.Provider, c.Caller.SourceID})[:24]}
	if name == "update_space_brief" {
		return a.workbenchCall(ctx, id, "brief-update", in)
	}
	if name == "read_space_brief" {
		in.Actor = SpaceActor{Kind: "human"}
		return a.workbenchCall(ctx, id, "brief-read", in)
	}
	if name == "configure_space_inflow" {
		return a.configureCurrentInFlow(ctx, id, c.Caller, runtimeID, in.Enabled)
	}
	if name == "send_space_note" || name == "get_space_note" {
		op := "inflow-note"
		if name == "get_space_note" {
			op = "inflow-note-read"
			in.Actor = SpaceActor{Kind: "human"}
		}
		return a.workbenchCall(ctx, id, op, in)
	}
	if mcp.WorkbenchReadTool(name) {
		in.Actor = SpaceActor{Kind: "human"}
	}
	op := "view"
	if name == "send_space_request" {
		op = "send"
	}
	if name == "get_space_request" {
		op = "request"
	}
	out, err := a.workbenchCall(ctx, id, op, in)
	if err != nil {
		return nil, err
	}
	if name == "send_space_request" {
		a.wakeWorkbench()
		return out, nil
	}
	if name == "get_space_request" {
		return out, nil
	}
	view := workbenchDecode[WorkbenchView](out)
	return projectWorkbenchRead(name, id, in.Offset, view)
}

func projectWorkbenchRead(name, id string, offset int, view WorkbenchView) (any, error) {
	switch name {
	case "list_space_targets":
		return map[string]any{"spaceId": id, "targets": view.Targets}, nil
	case "read_space_brief":
		return map[string]any{"spaceId": id, "brief": view.Brief, "assistant": view.Assistant}, nil
	case "list_space_requests":
		items := []map[string]any{}
		for _, r := range view.Requests[:min(10, len(view.Requests))] {
			items = append(items, map[string]any{"id": r.ID, "targetId": r.TargetID, "actor": r.Actor, "parentRequestId": r.ParentRequestID, "state": r.State, "instructionPreview": shortText(r.Instruction, 160), "summaryPreview": shortText(r.Summary, 200), "updatedAt": r.UpdatedAt, "bootstrap": r.Bootstrap != nil})
		}
		out := map[string]any{"spaceId": id, "requests": items, "total": view.Total, "guidance": "已省略完整引用与长正文；按请求 ID 使用 get_space_request 读取。"}
		if offset+len(items) < view.Total {
			out["nextOffset"] = offset + len(items)
		}
		return out, nil
	}
	return nil, fmt.Errorf("未知的空间工具")
}
