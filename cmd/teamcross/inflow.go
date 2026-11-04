package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"teamcross/internal/collab"
	"teamcross/internal/mcp"
	"teamcross/internal/service"
)

func runInFlowHook(args []string, input io.Reader, output io.Writer) error {
	flags := flag.NewFlagSet("inflow-hook", flag.ContinueOnError)
	data := flags.String("data-dir", collab.DefaultDataDir(), "Team Cross 数据目录")
	provider := flags.String("provider", "", "codex 或 claude，由 Hook 配置明确指定")
	runtime := flags.String("runtime-id", "", "可选的绑定运行时 ID")
	config := flags.Bool("print-config", false, "输出 Hook 配置，不修改个人设置")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *provider != "codex" && *provider != "claude" {
		return fmt.Errorf("Hook 需要 --provider codex 或 claude")
	}
	if strings.ContainsAny(*runtime, "/\\?#") {
		return fmt.Errorf("运行时 ID 无效")
	}
	if *config {
		executable, err := os.Executable()
		if err != nil {
			return err
		}
		quote := func(v string) string { return "'" + strings.ReplaceAll(v, "'", "'\"'\"'") + "'" }
		command := quote(service.StableExecutable(executable)) + " inflow-hook --provider " + *provider + " --data-dir " + quote(*data)
		if *runtime != "" {
			command += " --runtime-id " + quote(*runtime)
		}
		return json.NewEncoder(output).Encode(map[string]any{"hooks": map[string]any{"UserPromptSubmit": []any{map[string]any{"hooks": []any{map[string]any{"type": "command", "command": command, "timeout": 4}}}}}})
	}
	var event struct {
		Session string `json:"session_id"`
		Event   string `json:"hook_event_name"`
	}
	// Do not inspect prompt, cwd or transcript content, and never guess a session.
	if json.NewDecoder(io.LimitReader(input, 256<<10)).Decode(&event) != nil || event.Event != "UserPromptSubmit" {
		return nil
	}
	if _, err := uuid.Parse(event.Session); err != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return mcp.WriteInFlowHook(ctx, *data, *runtime, os.Getenv(mcp.RuntimeTokenEnv), mcp.CallerSource{Provider: *provider, SourceID: event.Session}, output)
}
