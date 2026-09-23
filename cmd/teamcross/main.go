package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/google/uuid"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"teamcross/internal/buildinfo"
	"teamcross/internal/cliinstall"
	"teamcross/internal/collab"
	"teamcross/internal/mcp"
	"teamcross/internal/problem"
	"teamcross/internal/service"
	"teamcross/internal/webassets"
	"time"
)

func main() {
	if e := run(os.Args[1:]); e != nil {
		fmt.Fprintln(os.Stderr, e)
		var detail *problem.Error
		if errors.As(e, &detail) && detail.Recovery != "" {
			fmt.Fprintln(os.Stderr, detail.Recovery)
		}
		os.Exit(1)
	}
}
func printJSON(v any) error { return json.NewEncoder(os.Stdout).Encode(v) }
func run(args []string) error {
	if len(args) > 0 && collaborationCommand(args[0]) {
		return runCollaboration(args, os.Stdout, os.Stderr)
	}
	command := "serve"
	if len(args) > 0 && (!strings.HasPrefix(args[0], "-") || args[0] == "--version") {
		command = args[0]
		args = args[1:]
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	data := flags.String("data-dir", collab.DefaultDataDir(), "独立数据目录")
	home, _ := os.UserHomeDir()
	repo := flags.String("repo", home, "本机辅助上下文目录（不筛选来源会话或选择共享仓库）")
	listen := flags.String("listen", "127.0.0.1:43210", "本机 Web 地址")
	noOpen := flags.Bool("no-open", false, "不打开浏览器")
	foreground := flags.Bool("foreground", false, "前台运行服务（供调试）")
	jsonOut := flags.Bool("json", false, "输出 JSON")
	force := flags.Bool("force", false, "确认停止活动协作")
	stdin := flags.Bool("stdin", false, "从标准输入读取邀请")
	preview := flags.Bool("preview", false, "打开邀请确认页，不立即加入")
	dev := flags.String("dev-web", "", "前端开发地址")
	loopback := flags.Bool("test-loopback", false, "邀请包含同机测试地址")
	binary := flags.String("codex-bin", "", "Codex CLI 路径")
	claudeBinary := flags.String("claude-bin", "", "Claude Code CLI 路径")
	desktop := flags.String("desktop-app", "", "Codex Desktop 应用路径")
	cliDir := flags.String("cli-dir", cliinstall.DefaultDir, "App 命令入口目录")
	runtimeID := flags.String("runtime-id", "", "共享运行时批注工具的协作 ID")
	setLanguage := flags.String("set", "", "界面语言：auto、zh-CN 或 en")
	if e := flags.Parse(args); e != nil {
		if errors.Is(e, flag.ErrHelp) {
			return nil
		}
		return e
	}
	if command == "version" || command == "--version" {
		if *jsonOut {
			return printJSON(map[string]any{"version": buildinfo.Version, "commit": buildinfo.Commit, "protocol": buildinfo.ControlProtocol})
		}
		fmt.Printf("Team Cross %s (%s)\n", buildinfo.Version, buildinfo.Commit)
		return nil
	}
	if command == "cli-status" || command == "install-cli" || command == "uninstall-cli" {
		executable, e := os.Executable()
		if e != nil {
			return e
		}
		status := cliinstall.Inspect(executable, *cliDir, os.Getenv("PATH"))
		switch command {
		case "install-cli":
			status, e = cliinstall.Install(executable, *cliDir, os.Getenv("PATH"))
		case "uninstall-cli":
			status, e = cliinstall.Uninstall(executable, *cliDir, os.Getenv("PATH"))
		}
		if *jsonOut {
			if e != nil {
				_ = printJSON(problem.Describe(e))
			} else {
				e = printJSON(status)
			}
		} else if e == nil {
			fmt.Printf("命令入口：%s\n当前命令：%s\n来源：%s\n", status.Target, status.Command, status.Source)
			if command == "install-cli" && !status.PathReady {
				fmt.Println("请将命令入口所在目录加入终端 PATH。")
			}
		}
		return e
	}
	switch command {
	case "serve", "join", "mcp", "status", "doctor", "stop", "ui-language":
	default:
		return fmt.Errorf("用法: teamcross [serve | join | sources | space | freeze | publication-preview | publish | publication-status | materials | read-material | withdraw-material | preview | create | share | invite | inspect-invitation | collaborations | input | open | end | leave | resume | share-status | cancel-share | mcp | status | doctor | stop | ui-language | version | cli-status | install-cli | uninstall-cli]")
	}
	var e error
	*data, e = service.Normalize(*data)
	if e != nil {
		return e
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if command == "mcp" {
		if *runtimeID != "" {
			return mcp.ServeRuntime(ctx, *data, *runtimeID, os.Getenv(mcp.RuntimeTokenEnv), os.Stdin, os.Stdout)
		}
		return mcp.Serve(ctx, *data, os.Stdin, os.Stdout)
	}
	if command == "ui-language" {
		s, err := service.Ensure(ctx, *data, "", nil)
		if err != nil {
			return err
		}
		var result map[string]string
		if *setLanguage != "" {
			err = s.Call(ctx, "POST", "ui-language", map[string]string{"mode": *setLanguage}, &result)
		} else {
			err = s.Call(ctx, "GET", "ui-language", nil, &result)
		}
		if err != nil {
			return err
		}
		if *jsonOut {
			return printJSON(result)
		}
		fmt.Printf("%s (%s)\n", result["mode"], result["resolved"])
		return nil
	}
	if command == "status" || command == "doctor" || command == "stop" {
		s, e := service.Probe(ctx, *data)
		if e != nil {
			if *jsonOut {
				_ = printJSON(map[string]any{"running": false, "error": e.Error(), "dataDir": *data})
			}
			if command == "status" {
				var typed *problem.Error
				if !errors.As(e, &typed) {
					return nil
				}
			}
			return e
		}
		if command == "stop" {
			if e = s.Call(ctx, "POST", "control/stop", map[string]bool{"force": *force}, nil); e != nil {
				return e
			}
			stopped := false
			for i := 0; i < 300; i++ {
				lock, e := service.Lock(*data, "core.lock")
				if e == nil {
					lock.Close()
					stopped = true
					break
				}
				time.Sleep(50 * time.Millisecond)
			}
			if !stopped {
				return fmt.Errorf("服务仍在收尾，请稍后检查状态")
			}
			if *jsonOut {
				return printJSON(map[string]bool{"stopped": true})
			}
			fmt.Println("Team Cross 已停止，会话与代码已保留。")
			return nil
		}
		if command == "doctor" {
			var info map[string]any
			if e = s.Call(ctx, "GET", "info", nil, &info); e != nil {
				return e
			}
			var probe any
			probeErr := s.Call(ctx, "POST", "mcp/probe", map[string]any{}, &probe)
			info["mcpProbe"] = probe
			executable, _ := os.Executable()
			info["cli"] = cliinstall.Inspect(executable, *cliDir, os.Getenv("PATH"))
			info["installedVersion"] = buildinfo.Version
			if probeErr != nil {
				info["mcpProbeError"] = probeErr.Error()
			}
			s.Token = ""
			return printJSON(map[string]any{"service": s, "diagnostics": info})
		}
		s.Token = ""
		if *jsonOut {
			return printJSON(s)
		}
		fmt.Printf("Team Cross %s · %s\n活动协作: %d\n", s.Version, s.URL, s.Active)
		return nil
	}
	explicitListen := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "listen" {
			explicitListen = true
		}
	})
	if command == "serve" && *foreground {
		return serve(ctx, cancel, collab.Config{DataDir: *data, Repo: *repo, Binary: *binary, ClaudeBinary: *claudeBinary, DesktopApp: *desktop, Loopback: *loopback}, *listen, explicitListen, *dev, *noOpen)
	}
	extra := []string{"--repo", *repo}
	if explicitListen {
		extra = append(extra, "--listen", *listen)
	}
	if *binary != "" {
		extra = append(extra, "--codex-bin", *binary)
	}
	if *claudeBinary != "" {
		extra = append(extra, "--claude-bin", *claudeBinary)
	}
	if *desktop != "" {
		extra = append(extra, "--desktop-app", *desktop)
	}
	if *loopback {
		extra = append(extra, "--test-loopback")
	}
	if *dev != "" {
		extra = append(extra, "--dev-web", *dev)
	}
	s, e := service.Ensure(ctx, *data, "", extra)
	if e != nil {
		return e
	}
	target := s.URL
	joinedID := ""
	if command == "join" {
		invitation := ""
		if *stdin {
			b, e := io.ReadAll(io.LimitReader(os.Stdin, 65537))
			if e != nil {
				return e
			}
			invitation = strings.TrimSpace(string(b))
		} else {
			if len(flags.Args()) != 1 {
				return fmt.Errorf("用法: teamcross join [--preview] <邀请>")
			}
			invitation = flags.Args()[0]
		}
		path := "join"
		if *preview {
			path = "invitations/pending"
		}
		var out struct {
			ID string `json:"id"`
		}
		if e = s.Call(ctx, "POST", path, map[string]string{"invitation": invitation}, &out); e != nil {
			return e
		}
		if *preview {
			target += "/#/join/" + out.ID
		} else {
			target += "/#/collaborations/" + out.ID
			joinedID = out.ID
		}
	}
	s.Token = ""
	if *jsonOut {
		if e = printJSON(map[string]any{"id": joinedID, "url": target, "service": s}); e != nil {
			return e
		}
	} else {
		fmt.Println(target)
	}
	if !*noOpen {
		return exec.Command("open", target).Run()
	}
	return nil
}
func serve(ctx context.Context, stop context.CancelFunc, cfg collab.Config, listen string, explicit bool, dev string, noOpen bool) error {
	host, _, e := net.SplitHostPort(listen)
	if e != nil {
		return e
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("管理服务必须监听 loopback 地址")
	}
	if e = os.MkdirAll(cfg.DataDir, 0700); e != nil {
		return e
	}
	lock, e := service.Lock(cfg.DataDir, "core.lock")
	if e != nil {
		return fmt.Errorf("该数据目录已有 Team Cross 在运行")
	}
	defer lock.Close()
	app, e := collab.Open(cfg)
	if e != nil {
		return e
	}
	defer app.Close()
	assets, _ := fs.Sub(webassets.Dist, "dist")
	var web http.Handler = http.FileServer(http.FS(assets))
	if dev != "" {
		u, e := url.Parse(dev)
		if e != nil {
			return e
		}
		web = httputil.NewSingleHostReverseProxy(u)
	}
	listener, e := net.Listen("tcp4", listen)
	if e != nil && !explicit {
		listener, e = net.Listen("tcp4", "127.0.0.1:0")
	}
	if e != nil {
		return e
	}
	defer listener.Close()
	app.URL = "http://" + listener.Addr().String()
	c := service.Connection{URL: app.URL, PID: os.Getpid(), Instance: uuid.NewString(), Token: app.Token, Version: buildinfo.Version, Commit: buildinfo.Commit, Protocol: buildinfo.ControlProtocol, DataDir: cfg.DataDir}
	inner := app.Handler(web)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/control/") {
			inner.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		if r.Header.Get("Origin") != "" || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+c.Token)) != 1 {
			w.WriteHeader(403)
			_ = json.NewEncoder(w).Encode(problem.New("unauthorized", "服务控制需要本机凭据", ""))
			return
		}
		public := c
		public.Token = ""
		switch {
		case r.Method == "GET" && r.URL.Path == "/api/control/status":
			mode, resolved := app.UILanguage()
			_ = json.NewEncoder(w).Encode(service.Status{Connection: public, Running: true, Active: app.Active(), UILanguage: mode, ResolvedLanguage: resolved})
		case r.Method == "POST" && r.URL.Path == "/api/control/stop":
			var in struct {
				Force bool `json:"force"`
			}
			if json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&in) != nil {
				w.WriteHeader(400)
				return
			}
			if app.Active() > 0 && !in.Force {
				w.WriteHeader(409)
				_ = json.NewEncoder(w).Encode(problem.New("active_collaborations", "停止会中断本机活动协作；会话和代码会保留", "确认后使用 stop --force"))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]bool{"stopping": true})
			stop()
		default:
			http.NotFound(w, r)
		}
	})
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	if e = service.Save(cfg.DataDir, c); e != nil {
		return e
	}
	defer func() {
		if old, e := service.Read(cfg.DataDir); e == nil && old.Instance == c.Instance {
			_ = os.Remove(filepath.Join(cfg.DataDir, "connection.json"))
		}
	}()
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	fmt.Printf("Team Cross %s · %s\n", buildinfo.Version, app.URL)
	if !noOpen {
		_ = exec.Command("open", app.URL).Run()
	}
	select {
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(shutdown)
	case e := <-done:
		if errors.Is(e, http.ErrServerClosed) {
			return nil
		}
		return e
	}
}
