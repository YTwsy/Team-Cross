package mcp

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"time"

	"teamcross/internal/inflow"
	"teamcross/internal/service"
)

type inFlowKey struct{}
type inFlowAttachment struct {
	Text   string
	Finish func(bool)
}
type inFlowPrepare func(context.Context) *inFlowAttachment

func withInFlow(ctx context.Context, dataDir, runtimeID, token string) context.Context {
	return context.WithValue(ctx, inFlowKey{}, inFlowPrepare(func(ctx context.Context) *inFlowAttachment {
		caller, err := conversationCaller(ctx)
		if err != nil && runtimeID == "" {
			return nil
		}
		return prepareInFlow(ctx, dataDir, runtimeID, token, caller)
	}))
}

func prepareInFlow(ctx context.Context, dataDir, runtimeID, token string, caller CallerSource) *inFlowAttachment {
	// Passive context never starts/restarts a Core, opens a native client or polls
	// for a reconnect. An unavailable host leaves the normal response untouched.
	connection, err := service.Read(dataDir)
	if err != nil {
		return nil
	}
	if runtimeID == "" {
		token = connection.Token
	}
	backend := Backend{URL: connection.URL, Token: token, Client: localClient()}
	call := func(ctx context.Context, op string, batch inflow.Batch) (json.RawMessage, error) {
		body := map[string]any{"operation": op, "batch": batch, "caller": caller}
		if runtimeID != "" {
			return backend.Call(ctx, "POST", "runtime-annotations/"+runtimeID, map[string]any{"name": "_inflow", "arguments": body, "caller": caller})
		}
		return backend.Call(ctx, "POST", "inflow", body)
	}
	prepareCtx, cancel := context.WithTimeout(ctx, 1200*time.Millisecond)
	raw, err := call(prepareCtx, "prepare", inflow.Batch{})
	cancel()
	var batch inflow.Batch
	if err != nil || json.Unmarshal(raw, &batch) != nil || len(batch.Items) == 0 || batch.Receipt == "" {
		return nil
	}
	text, _ := json.Marshal(batch.Items)
	return &inFlowAttachment{Text: inflow.Guidance + "\n" + string(text), Finish: func(delivered bool) {
		op := "release"
		if delivered {
			op = "delivered"
		}
		finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 1200*time.Millisecond)
		defer cancel()
		// Internal output bookkeeping; never exposed as a model acknowledgement.
		_, _ = call(finishCtx, op, inflow.Batch{SpaceID: batch.SpaceID, TargetID: batch.TargetID, ExecutionID: batch.ExecutionID, Receipt: batch.Receipt})
	}}
}

func passiveTool(name string) bool {
	// UI resources/mentions are not model conversation delivery opportunities.
	return name != "open_teamcross" && !strings.HasPrefix(name, "teamcross_ui_") && name != mentionSearchTool
}

// WriteInFlowHook only consumes a native hook identity supplied on stdin. Hook
// output uses the hosts' additionalContext envelope and is advisory JSON data.
func WriteInFlowHook(ctx context.Context, dataDir, runtimeID, token string, caller CallerSource, output io.Writer) error {
	attachment := prepareInFlow(ctx, dataDir, runtimeID, token, caller)
	if attachment == nil {
		return nil
	}
	err := json.NewEncoder(output).Encode(map[string]any{"hookSpecificOutput": map[string]string{"hookEventName": "UserPromptSubmit", "additionalContext": attachment.Text}})
	attachment.Finish(err == nil)
	return err
}
