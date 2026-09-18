package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"teamcross/internal/collab"
	"teamcross/internal/mcp"
	"teamcross/internal/problem"
	"teamcross/internal/service"
)

func collaborationCommand(command string) bool {
	switch command {
	case "space", "freeze", "publication-preview", "publish", "publication-status", "materials", "read-material", "withdraw-material", "remove-member", "revoke-invitation", "collaborations", "input", "sources", "preview", "create", "share", "invite", "inspect-invitation", "open", "end", "leave", "resume", "share-status", "cancel-share":
		return true
	}
	return false
}

// Keep management commands independent of browser launch and Core diagnostics.
// All commands use the same local API and input coordination as the WebGUI.
func runCollaboration(args []string, output, diagnostic io.Writer) error {
	command, args := args[0], args[1:]
	action := ""
	if command == "input" {
		if len(args) == 0 {
			return fmt.Errorf("用法: teamcross input request|cancel|handoff|reclaim|return --id <协作> --epoch <版本>")
		}
		if !strings.HasPrefix(args[0], "-") {
			action, args = args[0], args[1:]
		}
	}
	f := flag.NewFlagSet(command, flag.ContinueOnError)
	f.SetOutput(diagnostic)
	data := f.String("data-dir", collab.DefaultDataDir(), "本机数据目录")
	spaceID := f.String("space", "", "为已有只读空间启用执行；会关闭旧共享并要求重新邀请")
	draftID := f.String("draft", "", "本机冻结草稿 ID")
	previewID := f.String("preview-id", "", "确认的材料预览 ID")
	startTurn := f.String("start-turn", "", "公开首轮 ID")
	endTurn := f.String("end-turn", "", "公开末轮 ID")
	readingStart := f.String("reading-start", "", "建议阅读起点")
	materialID := f.String("material", "", "已发布材料 ID")
	materialVersion := f.Int("version", 0, "固定材料版本")
	baseVersion := f.Int("base-version", 0, "更新前的材料版本")
	id := f.String("id", "", "协作 ID；省略时列出协作")
	memberID := f.String("member", "", "输入接收者或要移除的具体成员 ID")
	invitationID := f.String("invitation-id", "", "要撤销的待用邀请 ID")
	epoch := f.Uint64("epoch", 0, "查看协作后得到的输入状态版本")
	jsonOut := f.Bool("json", false, "输出 JSON")
	provider := f.String("provider", "", "来源会话的 codex 或 claude")
	source := f.String("source", "", "明确选择的来源原生会话 UUID")
	workspace := f.String("workspace", "", "existing 原目录或 worktree 独立目录")
	runtimeMode := f.String("runtime-mode", "restricted", "restricted 受限或 trusted 信任；创建后固定")
	title := f.String("title", "", "协作名称")
	requestID := f.String("request-id", "", "预览返回的创建 UUID；重试保留")
	previewHash := f.String("preview-hash", "", "确认的预览哈希")
	transport := f.String("transport", "", "明确选择 lan 或 tailcat")
	search := f.String("search", "", "来源名称或内容")
	cursor := f.String("cursor", "", "来源列表下一页游标")
	client := f.String("client", "tui", "tui 新终端窗口或 desktop 专用窗口")
	printCommand := f.Bool("print-command", false, "只生成原生客户端启动计划")
	invitation := f.String("invitation", "", "要查看的原始邀请或 App 链接")
	stdin := f.Bool("stdin", false, "从标准输入读取要查看的邀请")
	if err := f.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if len(f.Args()) != 0 {
		return fmt.Errorf("不接受位置参数，请用 --id 指定协作")
	}
	var tool string
	var params map[string]any
	var err error
	switch command {
	case "space":
		tool, params = "create_readonly_space", map[string]any{"title": *title, "requestId": *requestID}
	case "freeze":
		tool, params = "freeze_source_session", map[string]any{"provider": *provider, "sourceId": *source}
	case "publication-preview":
		tool, params = "preview_publication", map[string]any{"draftId": *draftID, "title": *title, "startTurnId": *startTurn, "endTurnId": *endTurn, "readingStartId": *readingStart}
	case "publish":
		tool, params = "publish_material", map[string]any{"id": *id, "previewId": *previewID, "previewHash": *previewHash, "requestId": *requestID}
		if *materialID != "" {
			params["materialId"], params["baseVersion"] = *materialID, *baseVersion
		}
	case "publication-status":
		tool, params = "get_publication_status", map[string]any{"id": *id, "requestId": *requestID}
	case "materials":
		tool, params = "list_materials", map[string]any{"id": *id}
	case "read-material":
		tool, params = "read_material", map[string]any{"id": *id, "materialId": *materialID, "version": *materialVersion, "cursor": *cursor}
		if *startTurn != "" {
			params["turnId"] = *startTurn
		}
	case "withdraw-material":
		tool, params = "withdraw_material", map[string]any{"id": *id, "materialId": *materialID}
	case "collaborations", "input":
		tool, params, err = collaborationInvocation(command, action, *id, *epoch)
		if command == "input" && *memberID != "" {
			params["memberId"] = *memberID
		}
	case "sources":
		tool, params = "list_source_sessions", map[string]any{"provider": *provider, "search": *search, "cursor": *cursor}
	case "preview", "create", "share":
		params = map[string]any{"provider": *provider, "sourceId": *source, "workspaceMode": *workspace, "runtimeMode": *runtimeMode, "title": *title}
		if *requestID != "" {
			params["requestId"] = *requestID
		}
		if *spaceID != "" {
			params["spaceId"] = *spaceID
		}
		tool = "preview_collaboration"
		if command != "preview" {
			tool = "create_collaboration"
			params["previewHash"] = *previewHash
		}
		if command == "share" && *transport != "lan" && *transport != "tailcat" {
			err = fmt.Errorf("share 必须明确指定 --transport lan 或 tailcat")
		}
	case "invite":
		tool, params = "create_invitation", map[string]any{"id": *id, "transport": *transport}
		if *requestID != "" {
			params["requestId"] = *requestID
		}
	case "remove-member":
		tool, params = "remove_member", map[string]any{"id": *id, "memberId": *memberID}
	case "revoke-invitation":
		tool, params = "revoke_invitation", map[string]any{"id": *id, "invitationId": *invitationID}
	case "inspect-invitation":
		if *stdin {
			var raw []byte
			raw, err = io.ReadAll(io.LimitReader(os.Stdin, 65537))
			*invitation = strings.TrimSpace(string(raw))
		}
		tool, params = "preview_invitation", map[string]any{"invitation": *invitation}
	case "open":
		tool, params = "open_client", map[string]any{"id": *id, "client": *client, "launch": !*printCommand}
	case "end", "leave", "resume":
		tool = map[string]string{"end": "end_sharing", "leave": "leave_collaboration", "resume": "resume_collaboration"}[command]
		params = map[string]any{"id": *id}
	case "share-status", "cancel-share":
		tool = map[string]string{"share-status": "get_share_request", "cancel-share": "cancel_share_request"}[command]
		params = map[string]any{"id": *id}
	}
	if err != nil {
		return commandError(output, *jsonOut, err)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	s, err := service.Ensure(ctx, *data, "", nil)
	if err != nil {
		return commandError(output, *jsonOut, err)
	}
	b := mcp.Backend{URL: s.URL, Token: s.Token, Client: &http.Client{Timeout: 50 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	out, err := b.Invoke(ctx, tool, params)
	if err != nil {
		return commandError(output, *jsonOut, err)
	}
	if command == "share" {
		var created map[string]any
		if err = json.Unmarshal(out, &created); err != nil {
			return commandError(output, *jsonOut, err)
		}
		if created["state"] != "ready" {
			err = fmt.Errorf("协作创建状态为 %v，请查询原协作后继续", created["state"])
		} else {
			out, err = b.Invoke(ctx, "create_invitation", map[string]any{"id": created["id"], "transport": *transport})
		}
		if err != nil {
			partial, _ := json.Marshal(map[string]any{"stage": "created", "collaboration": created, "invitationError": problem.Describe(err), "recovery": "保留协作 ID，使用 teamcross invite --id <id> --transport " + *transport + " 重试邀请；不重新创建"})
			if e := printCollaborationResult(output, partial, *jsonOut); e != nil {
				return e
			}
			return err
		}
	}
	return printCollaborationResult(output, out, *jsonOut)
}

func collaborationInvocation(command, action, id string, epoch uint64) (string, map[string]any, error) {
	params := map[string]any{"id": id, "epoch": epoch}
	if command == "collaborations" {
		if id == "" {
			return "list_collaborations", map[string]any{}, nil
		}
		return "get_collaboration", params, nil
	}
	names := map[string]string{"request": "request_input", "cancel": "cancel_input_request", "handoff": "handoff_input", "reclaim": "reclaim_input", "return": "return_input"}
	name, ok := names[action]
	if !ok || id == "" || strings.ContainsAny(id, "/?#\\") || epoch == 0 || epoch > 9007199254740991 {
		return "", nil, fmt.Errorf("请指定 request|cancel|handoff|reclaim|return、--id 和查询得到的 --epoch")
	}
	return name, params, nil
}

func commandError(w io.Writer, jsonOut bool, err error) error {
	if jsonOut {
		_ = json.NewEncoder(w).Encode(problem.Describe(err))
	}
	return err
}

func printCollaborationResult(w io.Writer, raw json.RawMessage, jsonOut bool) error {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	if jsonOut {
		return json.NewEncoder(w).Encode(value)
	}
	rows, ok := value.([]any)
	if !ok {
		rows = []any{value}
	}
	if len(rows) == 0 {
		_, err := fmt.Fprintln(w, "暂无协作。")
		return err
	}
	for _, row := range rows {
		v, ok := row.(map[string]any)
		if ok && v["id"] != nil && v["role"] != nil {
			if v["hasExecution"] == false {
				_, err := fmt.Fprintf(w, "%v · %v\n托管主机：%v · 只读分享与讨论 · %v\n", v["id"], v["title"], v["host"], v["state"])
				if err != nil {
					return err
				}
			} else if _, err := fmt.Fprintf(w, "%v · %v\n主机：%v · 当前输入者：%v · 本机角色：%v\n状态：%v · 运行中：%v · 输入版本：%v\n目录：%v\n", v["id"], v["title"], v["host"], v["writer"], v["role"], v["state"], v["busy"], v["epoch"], v["executionCwd"]); err != nil {
				return err
			}
			if v["invitationUrl"] != nil {
				if _, err := fmt.Fprintf(w, "邀请链接：%v\n邀请码：%v\n首次加入期限：%v\n", v["invitationUrl"], v["invitation"], v["expiresAt"]); err != nil {
					return err
				}
			}
		} else {
			enc := json.NewEncoder(w)
			enc.SetIndent("", "  ")
			if err := enc.Encode(row); err != nil {
				return err
			}
		}
	}
	return nil
}
