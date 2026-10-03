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
		a.mu.Unlock()
		if receiver != nil {
			id = receiver.receiverSpace
		}
		if _, supplied := args["spaceId"]; supplied {
			return nil, fmt.Errorf("绑定运行时不能选择其他空间")
		}
		args["spaceId"] = id
	} else {
		if _, err := a.currentSource(ctx, c.Caller); err != nil {
			return nil, err
		}
	}
	if err := mcp.ValidateAgentCall(name, args); err != nil {
		return nil, err
	}
	id, _ := args["spaceId"].(string)
	var in workbenchInput
	encoded, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(encoded, &in); err != nil {
		return nil, fmt.Errorf("空间工具参数类型无效: %w", err)
	}
	in.Actor = SpaceActor{Kind: "session", Provider: c.Caller.Provider, Session: contentHash([]string{c.Caller.Provider, c.Caller.SourceID})[:24]}
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
		if in.Offset+len(items) < view.Total {
			out["nextOffset"] = in.Offset + len(items)
		}
		return out, nil
	}
	return nil, fmt.Errorf("未知的空间工具")
}
