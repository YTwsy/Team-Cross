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
	return command == "collaborations" || command == "input"
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
		action, args = args[0], args[1:]
	}
	f := flag.NewFlagSet(command, flag.ContinueOnError)
	f.SetOutput(diagnostic)
	data := f.String("data-dir", collab.DefaultDataDir(), "本机数据目录")
	id := f.String("id", "", "协作 ID；省略时列出协作")
	epoch := f.Uint64("epoch", 0, "查看协作后得到的输入状态版本")
	jsonOut := f.Bool("json", false, "输出 JSON")
	if err := f.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if len(f.Args()) != 0 {
		return fmt.Errorf("不接受位置参数，请用 --id 指定协作")
	}
	tool, params, err := collaborationInvocation(command, action, *id, *epoch)
	if err != nil {
		return err
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
		if ok && v["id"] != nil {
			if _, err := fmt.Fprintf(w, "%v · %v\n主机：%v · 当前输入者：%v · 本机角色：%v\n状态：%v · 运行中：%v · 输入版本：%v\n目录：%v\n", v["id"], v["title"], v["host"], v["writer"], v["role"], v["state"], v["busy"], v["epoch"], v["executionCwd"]); err != nil {
				return err
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
